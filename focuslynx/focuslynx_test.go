package focuslynx

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHub is an in-memory Transport modeling a FocusLynx channel-1 focuser: it
// answers GETSTATUS/GETCONFIG with key:value blocks and ACKs action commands, so
// the protocol layer is testable with no hardware/cgo.
type fakeHub struct {
	mu      sync.Mutex
	pos     int
	maxStep int
	moving  bool
	out     []byte
	written []string
	failW   bool
}

func (f *fakeHub) Write(b []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failW {
		return 0, errGone
	}
	cmd := string(b)
	f.written = append(f.written, cmd)
	switch {
	case strings.Contains(cmd, "GETSTATUS"):
		f.out = append(f.out, []byte(f.statusBlock())...)
	case strings.Contains(cmd, "GETCONFIG"):
		f.out = append(f.out, []byte(f.configBlock())...)
	case strings.Contains(cmd, "GETHUBINFO"):
		f.out = append(f.out, []byte("!\r\nHUB INFO\r\nHub FVer = 2.3.9\r\nSleeping = 0\r\nEND\r\n")...)
	case strings.Contains(cmd, "HELLO"):
		f.reply("Foc1")
	case strings.Contains(cmd, "MA"):
		f.pos = parseMA(cmd)
		f.reply("M")
	case strings.Contains(cmd, "HALT"):
		f.reply("HALTED")
	case strings.Contains(cmd, "HOME"):
		f.reply("H")
	case strings.Contains(cmd, "ERM"):
		f.reply("STOPPED")
	case strings.Contains(cmd, "MIR"), strings.Contains(cmd, "MOR"), strings.Contains(cmd, "CENTER"):
		f.reply("M")
	default: // SC* config setters, RESET, …
		f.reply("SET")
	}
	return len(b), nil
}

// reply queues a "!" ack plus the given one-word status line.
func (f *fakeHub) reply(word string) {
	f.out = append(f.out, []byte("!\r\n"+word+"\r\n")...)
}

func (f *fakeHub) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failW {
		return 0, errGone
	}
	if len(f.out) == 0 {
		return 0, nil
	}
	n := copy(p, f.out)
	f.out = f.out[n:]
	return n, nil
}

func (f *fakeHub) Close() error { return nil }

func (f *fakeHub) last() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.written) == 0 {
		return ""
	}
	return f.written[len(f.written)-1]
}

func (f *fakeHub) statusBlock() string {
	mv := "0"
	if f.moving {
		mv = "1"
	}
	return fmt.Sprintf("!\r\nSTATUS1\r\nTemp(C) = +21.7\r\nCurr Pos = %07d\r\nTarg Pos = %07d\r\nIsMoving = %s\r\nIsHoming = 0\r\nIsHomed  = 1\r\nEND\r\n",
		f.pos, f.pos, mv)
}

func (f *fakeHub) configBlock() string {
	return fmt.Sprintf("!\r\nCONFIG1\r\nNickname = Foc1\r\nMax Pos  = %07d\r\nDev Typ  = OE\r\nTComp ON = 0\r\nBLC En   = 0\r\nBLC Stps = +40\r\nLED Brt  = 075\r\nEND\r\n", f.maxStep)
}

func parseMA(cmd string) int {
	i := strings.Index(cmd, "MA")
	if i < 0 {
		return 0
	}
	s := cmd[i+2:]
	s = strings.TrimRight(strings.TrimSuffix(s, ">"), ">")
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

type goneErr struct{}

func (goneErr) Error() string { return "device removed" }

var errGone = goneErr{}

func chan1(f *fakeHub) *Focuser { return New(f, DeviceInfo{Port: "fake"}).Focuser(1) }

func TestEncodeCommands(t *testing.T) {
	cases := []struct {
		name string
		do   func(*Focuser)
		want string
	}{
		{"status", func(fc *Focuser) { fc.Status() }, "<F1GETSTATUS>"},
		{"config", func(fc *Focuser) { fc.Config() }, "<F1GETCONFIG>"},
		{"moveto 5000", func(fc *Focuser) { fc.MoveTo(5000) }, "<F1MA005000>"},
		{"halt", func(fc *Focuser) { fc.Halt() }, "<F1HALT>"},
		{"home", func(fc *Focuser) { fc.Home() }, "<F1HOME>"},
		{"center", func(fc *Focuser) { fc.Center() }, "<F1CENTER>"},
		{"hello", func(fc *Focuser) { fc.Hello() }, "<F1HELLO>"},
		{"set nickname", func(fc *Focuser) { fc.SetNickname("OAG focuser") }, "<F1SCNNOAG focuser>"},
		{"movein high", func(fc *Focuser) { fc.MoveIn(false) }, "<F1MIR0>"},
		{"moveout low", func(fc *Focuser) { fc.MoveOut(true) }, "<F1MOR1>"},
		{"endrelative", func(fc *Focuser) { fc.EndRelative() }, "<F1ERM>"},
		{"sync 1500", func(fc *Focuser) { fc.Sync(1500) }, "<F1SCCP001500>"},
		{"devtype", func(fc *Focuser) { fc.SetDeviceType("oa") }, "<F1SCDTOA>"},
		{"backlash on", func(fc *Focuser) { fc.SetBacklashComp(true) }, "<F1SCBE1>"},
		{"backlash steps", func(fc *Focuser) { fc.SetBacklashSteps(50) }, "<F1SCBS50>"},
		{"tempcomp off", func(fc *Focuser) { fc.SetTempComp(false) }, "<F1SCTE0>"},
		{"tempcomp atstart", func(fc *Focuser) { fc.SetTempCompAtStart(true) }, "<F1SCTS1>"},
		{"tempcomp mode", func(fc *Focuser) { fc.SetTempCompMode('C') }, "<F1SCTMC>"},
		{"tempcoef D+92", func(fc *Focuser) { fc.SetTempCoefficient('D', 92) }, "<F1SCTCD+0092>"},
		{"tempcoef A-5", func(fc *Focuser) { fc.SetTempCoefficient('A', -5) }, "<F1SCTCA-0005>"},
		{"reset", func(fc *Focuser) { fc.FactoryReset() }, "<F1RESET>"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := &fakeHub{maxStep: 125440}
			c.do(chan1(f))
			if got := f.last(); got != c.want {
				t.Errorf("sent %q, want %q", got, c.want)
			}
		})
	}
}

func TestChannel2Prefix(t *testing.T) {
	f := &fakeHub{maxStep: 125440}
	h := New(f, DeviceInfo{})
	h.Focuser(2).Halt()
	if got := f.last(); got != "<F2HALT>" {
		t.Errorf("sent %q, want <F2HALT>", got)
	}
}

func TestDecodeStatusConfig(t *testing.T) {
	f := &fakeHub{pos: 12345, maxStep: 125440}
	fc := chan1(f)
	if p, err := fc.Position(); err != nil || p != 12345 {
		t.Fatalf("Position()=%d,%v want 12345", p, err)
	}
	if temp, err := fc.Temperature(); err != nil || temp != 21.7 {
		t.Fatalf("Temperature()=%v,%v want 21.7", temp, err)
	}
	if mv, err := fc.IsMoving(); err != nil || mv {
		t.Fatalf("IsMoving()=%v,%v want false", mv, err)
	}
	if m, err := fc.MaxStep(); err != nil || m != 125440 {
		t.Fatalf("MaxStep()=%d,%v want 125440", m, err)
	}
	f.moving = true
	if mv, err := fc.IsMoving(); err != nil || !mv {
		t.Fatalf("IsMoving()=%v,%v want true", mv, err)
	}
}

func TestMoveUpdatesPosition(t *testing.T) {
	f := &fakeHub{pos: 0, maxStep: 125440}
	fc := chan1(f)
	if err := fc.MoveTo(5000); err != nil {
		t.Fatal(err)
	}
	if p, _ := fc.Position(); p != 5000 {
		t.Fatalf("Position after move = %d, want 5000", p)
	}
}

func TestTimeout(t *testing.T) {
	old := replyTimeout
	replyTimeout = 50 * time.Millisecond
	defer func() { replyTimeout = old }()
	// silentHub accepts writes but never replies, so Position must time out.
	fc := New(&silentHub{}, DeviceInfo{}).Focuser(1)
	if _, err := fc.Position(); err == nil {
		t.Error("want timeout when hub gives no reply")
	}
}

// silentHub accepts writes but never replies (to exercise the timeout path).
type silentHub struct{}

func (silentHub) Write(p []byte) (int, error) { return len(p), nil }
func (silentHub) Read([]byte) (int, error)    { return 0, nil }
func (silentHub) Close() error                { return nil }

func TestDeviceRemoved(t *testing.T) {
	f := &fakeHub{failW: true}
	if _, err := chan1(f).Position(); err == nil {
		t.Error("want error on removed device")
	}
}

// TestActionKeywordConsumed guards against reply desync: an action's trailing
// status keyword (e.g. "H" after HOME) must be consumed so it can't be misread as
// the next command's reply.
func TestActionKeywordConsumed(t *testing.T) {
	f := &fakeHub{pos: 4321, maxStep: 125440}
	fc := chan1(f)
	if err := fc.Home(); err != nil {
		t.Fatal(err)
	}
	if p, err := fc.Position(); err != nil || p != 4321 {
		t.Fatalf("Position()=%d,%v want 4321 (stale keyword desynced the reply?)", p, err)
	}
}

func TestHelloReturnsNickname(t *testing.T) {
	f := &fakeHub{maxStep: 125440}
	if name, err := chan1(f).Hello(); err != nil || name != "Foc1" {
		t.Fatalf("Hello()=%q,%v want \"Foc1\"", name, err)
	}
}

func TestSetNicknameValidation(t *testing.T) {
	fc := chan1(&fakeHub{maxStep: 125440})
	for _, bad := range []string{"", "this name is far too long", "has<bracket"} {
		if err := fc.SetNickname(bad); err == nil {
			t.Errorf("SetNickname(%q) = nil, want validation error", bad)
		}
	}
	if err := fc.SetNickname("Imager"); err != nil {
		t.Errorf("SetNickname(%q) = %v, want nil", "Imager", err)
	}
}

func TestHubCommands(t *testing.T) {
	f := &fakeHub{maxStep: 125440}
	h := New(f, DeviceInfo{})
	if info, err := h.HubInfo(); err != nil || info["Hub FVer"] != "2.3.9" {
		t.Fatalf("HubInfo()=%v,%v", info, err)
	}
	if err := h.SetLEDBrightness(85); err != nil {
		t.Fatal(err)
	}
	if got := f.last(); got != "<FHSCLB085>" {
		t.Errorf("LED cmd = %q, want <FHSCLB085>", got)
	}
	if err := h.SetLEDBrightness(101); err == nil {
		t.Error("want range error for brightness 101")
	}
}

// errHub replies as the real controller does to a rejected command: "!" ack followed
// by an "ER=<n> <message>" line (not a keyword / not an END block), once.
type errHub struct{ sent bool }

func (e *errHub) Write(p []byte) (int, error) { return len(p), nil }
func (e *errHub) Read(p []byte) (int, error) {
	if e.sent {
		return 0, nil
	}
	e.sent = true
	return copy(p, []byte("!\r\nER=3 Unknown Command Received\r\n")), nil
}
func (e *errHub) Close() error { return nil }

func TestErrorReplySurfaced(t *testing.T) {
	old := replyTimeout
	replyTimeout = 200 * time.Millisecond
	defer func() { replyTimeout = old }()
	if _, err := New(&errHub{}, DeviceInfo{}).Focuser(1).Status(); err == nil {
		t.Error("Query: want error when controller returns an error string")
	}
	if err := New(&errHub{}, DeviceInfo{}).Focuser(1).Halt(); err == nil {
		t.Error("Action: want error when controller returns an error string")
	}
}

func TestConcurrentAccess(t *testing.T) {
	f := &fakeHub{pos: 1000, maxStep: 125440}
	h := New(f, DeviceInfo{})
	var wg sync.WaitGroup
	for i := 0; i < 48; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			fc := h.Focuser(1 + i%2)
			switch i % 3 {
			case 0:
				fc.Position()
			case 1:
				fc.MoveTo(i % 5000)
			case 2:
				fc.IsMoving()
			}
		}(i)
	}
	wg.Wait()
}
