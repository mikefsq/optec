package focuslynx

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

// replyTimeout bounds how long a command waits for its reply. A var so tests can
// shrink it.
var replyTimeout = 3 * time.Second

// Hub is an opened FocusLynx hub (one serial connection driving channels F1/F2).
type Hub struct {
	t    Transport
	info DeviceInfo

	mu   sync.Mutex
	rbuf []byte // line-reader carry-over between reads
}

// New wraps an already-open Transport. Most callers use OpenFirst / OpenPort; New
// is for a custom Transport (alternate backend, or a fake for testing).
func New(t Transport, info DeviceInfo) *Hub { return &Hub{t: t, info: info} }

// OpenFirst finds and opens the first attached FocusLynx/ThirdLynx hub (at the
// variant's baud).
func OpenFirst() (*Hub, error) {
	t, info, err := openFirst()
	if err != nil {
		return nil, err
	}
	return New(t, info), nil
}

// OpenPort opens the hub on a specific serial port. The baud is resolved from the
// port's variant (FocusLynx 115200 / ThirdLynx 19200), defaulting to 115200.
func OpenPort(port string) (*Hub, error) {
	t, info, err := openPort(port, baudForPort(port))
	if err != nil {
		return nil, err
	}
	return New(t, info), nil
}

func (h *Hub) Info() DeviceInfo { return h.info }
func (h *Hub) Close() error     { return h.t.Close() }

// OpenByNickname opens the hub whose focuser nickname matches nick (case-insensitive,
// trimmed) and returns it with the matched channel (1 or 2). It scans every attached
// FocusLynx/ThirdLynx port and asks each channel its nickname over the protocol — a
// platform-independent identity that needs no OS USB-descriptor access and, because
// only a real FocusLynx answers HELLO, also disambiguates among several FTDI devices
// sharing one VID/PID. Non-matching hubs are closed.
//
// Unlike a factory serial the nickname is user-assigned (set it once per unit with
// Focuser.SetNickname); the factory default is not guaranteed unique.
func OpenByNickname(nick string) (*Hub, int, error) {
	want := strings.TrimSpace(strings.ToLower(nick))
	if want == "" {
		return nil, 0, fmt.Errorf("focuslynx: empty nickname")
	}
	ports, err := enumeratePorts()
	if err != nil {
		return nil, 0, err
	}
	for _, d := range ports {
		t, info, err := openPort(d.Port, d.Baud)
		if err != nil {
			continue
		}
		h := New(t, info)
		// Channel 1 always exists; channel 2 only on a two-channel FocusLynx (115200).
		// A ThirdLynx (19200) is F1-only, so don't probe F2 there — it would just cost
		// a reply timeout per scan.
		channels := []int{1}
		if info.Baud == baudFocusLynx {
			channels = []int{1, 2}
		}
		matched := 0
		for _, ch := range channels {
			name, err := h.Focuser(ch).Hello()
			if err != nil {
				continue
			}
			if strings.TrimSpace(strings.ToLower(name)) == want {
				matched = ch
				break
			}
		}
		if matched != 0 {
			return h, matched, nil
		}
		h.Close()
	}
	return nil, 0, fmt.Errorf("focuslynx: no focuser with nickname %q found", nick)
}

// nextLine returns the next non-empty CR/LF-delimited line (trimmed), reading from
// the transport as needed until the deadline. ok=false on timeout. Caller holds mu.
func (h *Hub) nextLine(deadline time.Time) (string, bool, error) {
	for {
		if i := bytes.IndexAny(h.rbuf, "\r\n"); i >= 0 {
			line := strings.TrimSpace(string(h.rbuf[:i]))
			h.rbuf = h.rbuf[i+1:]
			if line == "" {
				continue // collapse blank lines / the paired byte of CRLF
			}
			return line, true, nil
		}
		if !time.Now().Before(deadline) {
			return "", false, nil
		}
		tmp := make([]byte, 128)
		n, err := h.t.Read(tmp)
		if err != nil {
			return "", false, err
		}
		if n == 0 {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		h.rbuf = append(h.rbuf, tmp[:n]...)
	}
}

// readAck consumes the leading "!" line that every reply begins with. The controller
// sends "!" even when the command is rejected — the error then follows as the next
// line ("ER=<n> <message>"), which Action/Query detect — so a missing "!" here means a
// protocol/framing fault. Caller holds mu.
func (h *Hub) readAck(cmd string, deadline time.Time) error {
	line, ok, err := h.nextLine(deadline)
	if err != nil {
		return fmt.Errorf("focuslynx: read %s: %w", cmd, err)
	}
	if !ok {
		return fmt.Errorf("focuslynx: timeout on %s", cmd)
	}
	if line != "!" {
		return fmt.Errorf("focuslynx: %s rejected: %s", cmd, line)
	}
	return nil
}

// isError reports whether a post-"!" reply line is a controller error ("ER=<n> …").
func isError(line string) bool { return strings.HasPrefix(line, "ER=") }

// Query sends cmd (e.g. "<F1GETSTATUS>") and parses the reply: a "!" ack, a header
// line (STATUSn / CONFIGn / HUB INFO), then "key = value" lines up to "END".
func (h *Hub) Query(cmd string) (map[string]string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.t.Write([]byte(cmd)); err != nil {
		return nil, fmt.Errorf("focuslynx: write %s: %w", cmd, err)
	}
	deadline := time.Now().Add(replyTimeout)
	if err := h.readAck(cmd, deadline); err != nil {
		return nil, err
	}
	m := map[string]string{}
	for {
		line, ok, err := h.nextLine(deadline)
		if err != nil {
			return nil, fmt.Errorf("focuslynx: read %s: %w", cmd, err)
		}
		if !ok {
			return nil, fmt.Errorf("focuslynx: timeout on %s", cmd)
		}
		switch {
		case line == "END":
			return m, nil
		case isError(line): // checked before the '=' split — "ER=3 …" itself contains '='
			return nil, fmt.Errorf("focuslynx: %s rejected: %s", cmd, line)
		default:
			if i := strings.IndexByte(line, '='); i >= 0 {
				m[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
			}
			// header line (STATUSn / CONFIGn / HUB INFO) and any stray text: ignored
		}
	}
}

// Action sends cmd and returns the controller's status keyword. Every command is
// answered by an immediate "!" ack followed by a one-word status line — "M"
// (moving), "H" (homing), "HALTED", "STOPPED", or "SET" (config saved). That
// keyword is consumed here so it can't bleed into the next reply; most callers
// discard it, but Say Hello reads its nickname from it.
func (h *Hub) Action(cmd string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := h.t.Write([]byte(cmd)); err != nil {
		return "", fmt.Errorf("focuslynx: write %s: %w", cmd, err)
	}
	deadline := time.Now().Add(replyTimeout)
	if err := h.readAck(cmd, deadline); err != nil {
		return "", err
	}
	// The status keyword follows the ack; consume it so it can't desync the next
	// command. A timeout here is tolerated — the ack already confirmed receipt. A
	// rejected command answers "!" then "ER=<n> <message>" rather than a keyword.
	word, _, _ := h.nextLine(deadline)
	if isError(word) {
		return "", fmt.Errorf("focuslynx: %s rejected: %s", cmd, word)
	}
	return word, nil
}

// HubInfo returns the parsed GETHUBINFO block (Hub FVer, Sleeping, Wired IP, and
// the WF … Wi-Fi fields).
func (h *Hub) HubInfo() (map[string]string, error) { return h.Query("<FHGETHUBINFO>") }

// SetLEDBrightness sets the hub's power-LED brightness, 0-100 (<FHSCLBzzz>); 0
// turns the LED off. This is a hub-level setting, not per-channel.
func (h *Hub) SetLEDBrightness(level int) error {
	if level < 0 || level > 100 {
		return fmt.Errorf("focuslynx: LED brightness %d out of range 0-100", level)
	}
	_, err := h.Action(fmt.Sprintf("<FHSCLB%03d>", level))
	return err
}

// Focuser is one of the hub's two focuser channels (ch 1 or 2).
type Focuser struct {
	h  *Hub
	ch int
}

// Focuser returns the controller for channel ch (1 or 2).
func (h *Hub) Focuser(ch int) *Focuser { return &Focuser{h: h, ch: ch} }

func (f *Focuser) verb(v string) string { return fmt.Sprintf("<F%d%s>", f.ch, v) }

// Status returns the parsed GETSTATUS block (Curr Pos, Temp(C), IsMoving, …).
func (f *Focuser) Status() (map[string]string, error) { return f.h.Query(f.verb("GETSTATUS")) }

// Config returns the parsed GETCONFIG block (Nickname, Max Pos, Dev Typ, TComp ON, …).
func (f *Focuser) Config() (map[string]string, error) { return f.h.Query(f.verb("GETCONFIG")) }

// Position returns the current absolute step (status "Curr Pos").
func (f *Focuser) Position() (int, error) {
	st, err := f.Status()
	if err != nil {
		return 0, err
	}
	return atoiKey(st, "Curr Pos")
}

// IsMoving reports motion state (status "IsMoving" = 1).
func (f *Focuser) IsMoving() (bool, error) {
	st, err := f.Status()
	if err != nil {
		return false, err
	}
	return strings.Contains(st["IsMoving"], "1"), nil
}

// Temperature returns the probe temperature in °C (status "Temp(C)").
func (f *Focuser) Temperature() (float64, error) {
	st, err := f.Status()
	if err != nil {
		return 0, err
	}
	v, ok := st["Temp(C)"]
	if !ok {
		return 0, fmt.Errorf("focuslynx: no Temp(C) in status")
	}
	return strconv.ParseFloat(strings.TrimSpace(v), 64)
}

// MaxStep returns the configured maximum position (config "Max Pos").
func (f *Focuser) MaxStep() (int, error) {
	cfg, err := f.Config()
	if err != nil {
		return 0, err
	}
	return atoiKey(cfg, "Max Pos")
}

// act sends a per-channel action command and discards the status keyword.
func (f *Focuser) act(v string) error { _, err := f.h.Action(f.verb(v)); return err }

// MoveTo commands an absolute move to step (6-digit zero-padded, per the protocol).
// Returns once the hub ACKs; poll IsMoving/Position for completion.
func (f *Focuser) MoveTo(step int) error {
	if step < 0 {
		return fmt.Errorf("focuslynx: negative position %d", step)
	}
	return f.act(fmt.Sprintf("MA%06d", step))
}

// MoveIn begins a continuous relative move inward; it runs until the travel limit
// or EndRelative. lowSpeed picks the slow rate (high speed otherwise).
func (f *Focuser) MoveIn(lowSpeed bool) error { return f.act("MIR" + speedDigit(lowSpeed)) }

// MoveOut begins a continuous relative move outward (see MoveIn).
func (f *Focuser) MoveOut(lowSpeed bool) error { return f.act("MOR" + speedDigit(lowSpeed)) }

// EndRelative stops a relative move started by MoveIn/MoveOut.
func (f *Focuser) EndRelative() error { return f.act("ERM") }

// Halt stops any in-progress motion (and disables temperature compensation).
func (f *Focuser) Halt() error { return f.act("HALT") }

// Home drives to the home position.
func (f *Focuser) Home() error { return f.act("HOME") }

// Center drives to the center of travel (Max Pos / 2).
func (f *Focuser) Center() error { return f.act("CENTER") }

// Hello returns the focuser's user-defined nickname (<FxHELLO>).
func (f *Focuser) Hello() (string, error) { return f.h.Action(f.verb("HELLO")) }

// SetNickname sets this channel's user-defined nickname (<FxSCNN…>, 1-16 chars), the
// string Hello reports. The nickname is the focuser's stable, protocol-readable
// identity — see OpenByNickname.
func (f *Focuser) SetNickname(name string) error {
	if n := len(name); n == 0 || n > 16 {
		return fmt.Errorf("focuslynx: nickname %q must be 1-16 characters", name)
	}
	if strings.ContainsAny(name, "<>") {
		return fmt.Errorf("focuslynx: nickname %q must not contain < or >", name)
	}
	return f.act("SCNN" + name)
}

// Sync sets ("syncs") the current position to step without moving (<FxSCCP……>,
// 6-digit). Only focusers that cannot home accept this; Optec focusers (which
// home) reject it.
func (f *Focuser) Sync(step int) error {
	if step < 0 {
		return fmt.Errorf("focuslynx: negative position %d", step)
	}
	return f.act(fmt.Sprintf("SCCP%06d", step))
}

// SetDeviceType selects the attached focuser model by its two-letter code (e.g.
// "OA" = Optec TCF-S 2"). The code drives safe speed/power limits and Max Pos —
// an incorrect type can damage the focuser. See the protocol's Appendix A.
func (f *Focuser) SetDeviceType(code string) error {
	if len(code) != 2 {
		return fmt.Errorf("focuslynx: device type %q must be 2 characters", code)
	}
	return f.act("SCDT" + strings.ToUpper(code))
}

// SetBacklashComp enables or disables backlash compensation (<FxSCBE…>).
func (f *Focuser) SetBacklashComp(on bool) error { return f.act("SCBE" + boolDigit(on)) }

// SetBacklashSteps sets the backlash step count (0-99), applied on outward moves
// only (<FxSCBSzz>).
func (f *Focuser) SetBacklashSteps(steps int) error {
	if steps < 0 || steps > 99 {
		return fmt.Errorf("focuslynx: backlash steps %d out of range 0-99", steps)
	}
	return f.act(fmt.Sprintf("SCBS%02d", steps))
}

// SetTempComp enables or disables temperature compensation on this channel
// (<FxSCTE…>).
func (f *Focuser) SetTempComp(on bool) error { return f.act("SCTE" + boolDigit(on)) }

// SetTempCompAtStart toggles compensate-on-power-up (<FxSCTS…>).
func (f *Focuser) SetTempCompAtStart(on bool) error { return f.act("SCTS" + boolDigit(on)) }

// SetTempCompMode selects the active temperature-coefficient slot, A-E (<FxSCTM…>).
func (f *Focuser) SetTempCompMode(mode byte) error {
	if mode < 'A' || mode > 'E' {
		return fmt.Errorf("focuslynx: temp-comp mode %q must be A-E", string(mode))
	}
	return f.act("SCTM" + string(mode))
}

// SetTempCoefficient sets the temperature coefficient (signed steps per °C) for slot
// mode A-E (<FxSCTC[A-E]±zzzz>, e.g. <F1SCTCD+0092>). Note: the command is "SCTC"
// followed by the mode letter — the rev1 doc's example "<F1SCTD+0092>" drops the
// second C and is rejected by firmware (ER=3); its syntax line "SCTCmszzzz" is right.
func (f *Focuser) SetTempCoefficient(mode byte, stepsPerDegree int) error {
	if mode < 'A' || mode > 'E' {
		return fmt.Errorf("focuslynx: temp-comp mode %q must be A-E", string(mode))
	}
	sign, mag := '+', stepsPerDegree
	if mag < 0 {
		sign, mag = '-', -mag
	}
	if mag > 9999 {
		return fmt.Errorf("focuslynx: temp coefficient %d out of range", stepsPerDegree)
	}
	return f.act(fmt.Sprintf("SCTC%c%c%04d", mode, sign, mag))
}

// FactoryReset restores this channel's configuration and status to defaults
// (<FxRESET>).
func (f *Focuser) FactoryReset() error { return f.act("RESET") }

// speedDigit maps a relative-move speed flag to the protocol digit (0 high, 1 low).
func speedDigit(low bool) string {
	if low {
		return "1"
	}
	return "0"
}

// boolDigit maps an enable flag to the protocol digit (1 on, 0 off).
func boolDigit(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

// atoiKey parses an integer status/config value, tolerating zero-padding.
func atoiKey(m map[string]string, key string) (int, error) {
	v, ok := m[key]
	if !ok {
		return 0, fmt.Errorf("focuslynx: key %q not in reply", key)
	}
	return strconv.Atoi(strings.TrimSpace(v))
}
