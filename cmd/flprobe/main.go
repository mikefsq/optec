// Command flprobe exercises the pure-Go Optec FocusLynx / ThirdLynx driver against a
// real device: it opens a hub (by port, nickname, or first found), dumps the decoded
// read surface, and can move or validate the config-write surface.
//
//	flprobe                          # read-only: identity, status, config
//	flprobe -list                    # list candidate serial ports
//	flprobe -port /dev/cu.usbmodem101
//	flprobe -nickname "QuickSync FTX40"   # open by protocol nickname (scans ports)
//	flprobe -ch 2                    # operate on channel F2 (FocusLynx)
//	flprobe -moveto 12000            # absolute move, then watch it settle
//	flprobe -in 200  / -out 200      # relative move by N steps (in=−, out=+)
//	flprobe -reltest                 # continuous MoveOut+EndRelative, report Δ
//	flprobe -stoptest 30000          # MoveTo far, run ~1s, Halt mid-flight
//	flprobe -home / -center / -stop  # home / center-of-travel / halt
//	flprobe -sync 1500               # set reported position without moving (SCCP)
//	flprobe -setnick "OAG focuser"   # set channel nickname, read back
//	flprobe -led 50                  # set hub LED brightness, read back
//	flprobe -backlashsteps 40        # set backlash steps, read back
//	flprobe -tempcomp on             # enable/disable temp compensation, read back
//	flprobe -cfgtest                 # safe round-trip of every config setter (reverted)
//	flprobe -watch                   # poll position+moving repeatedly
package main

import (
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mikefsq/optec-astro/focuslynx"
)

func main() {
	list := flag.Bool("list", false, "list candidate serial ports and exit")
	port := flag.String("port", "", "serial port to open (default: first found)")
	nickname := flag.String("nickname", "", "open the focuser with this nickname (scans all ports; channel auto-discovered)")
	ch := flag.Int("ch", 1, "focuser channel (1 or 2)")

	moveTo := flag.Int("moveto", -1, "absolute move to step, then watch; -1 = skip")
	in := flag.Int("in", -1, "relative move IN by N steps (decreasing), then watch; -1 = skip")
	out := flag.Int("out", -1, "relative move OUT by N steps (increasing), then watch; -1 = skip")
	relTest := flag.Bool("reltest", false, "continuous MoveOut(low) ~800ms then EndRelative; report Δ position")
	stopTest := flag.Int("stoptest", -1, "MoveTo this far target, let it run ~1s, then Halt; report halted position")
	home := flag.Bool("home", false, "home the focuser")
	center := flag.Bool("center", false, "move to center of travel (MaxPos/2)")
	stop := flag.Bool("stop", false, "halt motion")
	sync := flag.Int("sync", -1, "set reported position to N without moving (SCCP); -1 = skip")

	setNick := flag.String("setnick", "", "set the channel's nickname (1-16 chars), then read back via Hello")
	led := flag.Int("led", -1, "set hub LED brightness 0-100, then read back; -1 = skip")
	backlashSteps := flag.Int("backlashsteps", -1, "set backlash steps 0-99, then read back; -1 = skip")
	tempComp := flag.String("tempcomp", "", "enable/disable temperature compensation (on|off), then read back")
	cfgTest := flag.Bool("cfgtest", false, "validate every config setter with a safe round-trip (read, change, confirm, restore)")

	watch := flag.Bool("watch", false, "poll position+moving repeatedly")
	raw := flag.String("raw", "", "send a raw bracketed command (e.g. <F1SCTA+0050>) and print its ack/keyword")
	flag.Parse()

	if *list {
		ports, err := focuslynx.Enumerate()
		if err != nil {
			fmt.Fprintln(os.Stderr, "enumerate:", err)
			os.Exit(1)
		}
		for _, p := range ports {
			fmt.Printf("%s (baud %d)\n", p.Port, p.Baud)
		}
		return
	}

	var (
		hub *focuslynx.Hub
		err error
	)
	switch {
	case *nickname != "":
		var found int
		hub, found, err = focuslynx.OpenByNickname(*nickname)
		if err == nil {
			*ch = found // drive the channel the nickname resolved to
		}
	case *port != "":
		hub, err = focuslynx.OpenPort(*port)
	default:
		hub, err = focuslynx.OpenFirst()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer hub.Close()
	fc := hub.Focuser(*ch)
	fmt.Printf("opened %s (channel F%d)\n", hub.Info().Port, *ch)

	if *raw != "" {
		word, err := hub.Action(*raw)
		if err != nil {
			fmt.Fprintf(os.Stderr, "raw %s: %v\n", *raw, err)
			os.Exit(1)
		}
		fmt.Printf("raw %s -> %q\n", *raw, word)
		return
	}

	dumpAll(hub, fc)

	switch {
	case *cfgTest:
		runCfgTest(hub, fc)
	case *stopTest >= 0:
		p0, _ := fc.Position()
		fmt.Printf("\nstoptest: at %d, MoveTo %d then Halt mid-flight...\n", p0, *stopTest)
		report("MoveTo", fc.MoveTo(*stopTest))
		for i := 0; i < 40; i++ { // wait until it's actually moving
			if m, _ := fc.IsMoving(); m {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		time.Sleep(1 * time.Second)
		report("Halt", fc.Halt())
		time.Sleep(300 * time.Millisecond)
		p, _ := fc.Position()
		m, _ := fc.IsMoving()
		fmt.Printf("after halt: position=%d moving=%v (target was %d; halted short = Halt works)\n", p, m, *stopTest)
	case *relTest:
		p0, _ := fc.Position()
		fmt.Printf("\nreltest: at %d — MoveOut(low) ~800ms, EndRelative...\n", p0)
		report("MoveOut", fc.MoveOut(true))
		time.Sleep(800 * time.Millisecond)
		report("EndRelative", fc.EndRelative())
		watchSettle(fc)
		p1, _ := fc.Position()
		fmt.Printf("  out: %d -> %d (Δ%+d)\n", p0, p1, p1-p0)
		fmt.Printf("reltest: at %d — MoveIn(low) ~800ms, EndRelative...\n", p1)
		report("MoveIn", fc.MoveIn(true))
		time.Sleep(800 * time.Millisecond)
		report("EndRelative", fc.EndRelative())
		watchSettle(fc)
		p2, _ := fc.Position()
		fmt.Printf("  in:  %d -> %d (Δ%+d)\n", p1, p2, p2-p1)
	case *out >= 0:
		p0, _ := fc.Position()
		tgt := clamp(p0+*out, 0, maxStep(fc))
		fmt.Printf("\nat %d, out %d -> MoveTo %d...\n", p0, *out, tgt)
		report("MoveTo", fc.MoveTo(tgt))
		watchSettle(fc)
	case *in >= 0:
		p0, _ := fc.Position()
		tgt := clamp(p0-*in, 0, maxStep(fc))
		fmt.Printf("\nat %d, in %d -> MoveTo %d...\n", p0, *in, tgt)
		report("MoveTo", fc.MoveTo(tgt))
		watchSettle(fc)
	case *moveTo >= 0:
		fmt.Printf("\nmoving to %d...\n", *moveTo)
		report("MoveTo", fc.MoveTo(*moveTo))
		watchSettle(fc)
	case *home:
		report("Home", fc.Home())
		watchSettle(fc)
	case *center:
		report("Center", fc.Center())
		watchSettle(fc)
	case *stop:
		report("Halt", fc.Halt())
	case *sync >= 0:
		report("Sync", fc.Sync(*sync))
		p, _ := fc.Position()
		fmt.Printf("position now %d\n", p)
	case *setNick != "":
		report("SetNickname", fc.SetNickname(*setNick))
		name, _ := fc.Hello()
		fmt.Printf("nickname now %q\n", name)
	case *led >= 0:
		report("SetLEDBrightness", hub.SetLEDBrightness(*led))
		fmt.Printf("LED Brt = %s\n", cfgStr(fc, "LED Brt"))
	case *backlashSteps >= 0:
		report("SetBacklashSteps", fc.SetBacklashSteps(*backlashSteps))
		fmt.Printf("BLC Stps = %s\n", cfgStr(fc, "BLC Stps"))
	case *tempComp != "":
		report("SetTempComp", fc.SetTempComp(onoff(*tempComp)))
		fmt.Printf("TComp ON = %s\n", cfgStr(fc, "TComp ON"))
	case *watch:
		fmt.Println("\nwatching (Ctrl-C to stop)...")
		for {
			p, _ := fc.Position()
			m, _ := fc.IsMoving()
			fmt.Printf("position=%d moving=%v\n", p, m)
			time.Sleep(500 * time.Millisecond)
		}
	}
}

func dumpAll(hub *focuslynx.Hub, fc *focuslynx.Focuser) {
	fmt.Println("\n-- hub --")
	if hi, err := hub.HubInfo(); err == nil {
		fmt.Printf("%v\n", hi)
	} else {
		fmt.Fprintln(os.Stderr, "hubinfo:", err)
	}

	fmt.Println("\n-- identity --")
	if name, err := fc.Hello(); err == nil {
		fmt.Printf("nickname    : %q\n", name)
	}

	fmt.Println("\n-- status --")
	if st, err := fc.Status(); err == nil {
		fmt.Printf("%v\n", st)
	} else {
		fmt.Fprintln(os.Stderr, "status:", err)
	}
	if p, err := fc.Position(); err == nil {
		fmt.Printf("position    : %d\n", p)
	}
	if m, err := fc.IsMoving(); err == nil {
		fmt.Printf("moving      : %v\n", m)
	}
	if temp, err := fc.Temperature(); err == nil {
		fmt.Printf("temperature : %.1f °C\n", temp)
	}

	fmt.Println("\n-- config --")
	if cfg, err := fc.Config(); err == nil {
		fmt.Printf("%v\n", cfg)
	} else {
		fmt.Fprintln(os.Stderr, "config:", err)
	}
}

// runCfgTest validates every config setter with a safe round-trip: read the original
// value, change it, confirm the readback, then restore the original. Every change is
// reverted; nothing is left modified. Device type is only re-set to its CURRENT value
// (a real change can damage the focuser), and FactoryReset is never issued.
func runCfgTest(hub *focuslynx.Hub, fc *focuslynx.Focuser) {
	fmt.Println("\n-- config round-trip validation (all changes reverted) --")

	// Backlash steps.
	bs0 := cfgInt(fc, "BLC Stps")
	bsN := pick(bs0, 25, 30)
	chk("SetBacklashSteps", fc.SetBacklashSteps(bsN), cfgInt(fc, "BLC Stps"), bsN)
	report("  restore", fc.SetBacklashSteps(bs0))

	// Backlash enable (toggle).
	be0 := cfgInt(fc, "BLC En")
	chk("SetBacklashComp", fc.SetBacklashComp(be0 == 0), cfgInt(fc, "BLC En"), 1-be0)
	report("  restore", fc.SetBacklashComp(be0 == 1))

	// Temp-comp enable / at-start (toggle).
	te0 := cfgInt(fc, "TComp ON")
	chk("SetTempComp", fc.SetTempComp(te0 == 0), cfgInt(fc, "TComp ON"), 1-te0)
	report("  restore", fc.SetTempComp(te0 == 1))
	ts0 := cfgInt(fc, "TC@Start")
	chk("SetTempCompAtStart", fc.SetTempCompAtStart(ts0 == 0), cfgInt(fc, "TC@Start"), 1-ts0)
	report("  restore", fc.SetTempCompAtStart(ts0 == 1))

	// Temp-comp mode A-E.
	tm0 := cfgStr(fc, "TC Mode")
	tmN := byte('B')
	if tm0 == "B" {
		tmN = 'C'
	}
	chk("SetTempCompMode", fc.SetTempCompMode(tmN), cfgStr(fc, "TC Mode"), string(tmN))
	if tm0 != "" {
		report("  restore", fc.SetTempCompMode(tm0[0]))
	}

	// Temp coefficient for slot A.
	tc0 := cfgInt(fc, "TempCo A")
	tcN := pick(tc0, 50, 60)
	chk("SetTempCoefficient(A)", fc.SetTempCoefficient('A', tcN), cfgInt(fc, "TempCo A"), tcN)
	report("  restore", fc.SetTempCoefficient('A', tc0))

	// Hub LED brightness.
	led0 := cfgInt(fc, "LED Brt")
	ledN := pick(led0, 30, 40)
	chk("SetLEDBrightness", hub.SetLEDBrightness(ledN), cfgInt(fc, "LED Brt"), ledN)
	report("  restore", hub.SetLEDBrightness(led0))

	// Device type: re-set the CURRENT value (a real change could damage the focuser).
	dt0 := cfgStr(fc, "Dev Typ")
	chk("SetDeviceType(no-op)", fc.SetDeviceType(dt0), cfgStr(fc, "Dev Typ"), dt0)

	// Nickname.
	nk0, _ := fc.Hello()
	if len(nk0) > 0 && len(nk0) <= 16 {
		setErr := fc.SetNickname("hwtest")
		got, _ := fc.Hello()
		chk("SetNickname", setErr, got, "hwtest")
		report("  restore", fc.SetNickname(nk0))
	}

	fmt.Println("FactoryReset: skipped (destructive)")
}

func report(label string, err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", label, err)
		return
	}
	fmt.Printf("%s: ok\n", label)
}

// chk prints PASS/FAIL for a setter: setErr is the write result, got is the readback,
// want is the expected value (compared as strings).
func chk(label string, setErr error, got, want any) {
	if setErr != nil {
		fmt.Printf("%-24s FAIL (set: %v)\n", label, setErr)
		return
	}
	status := "PASS"
	if fmt.Sprint(got) != fmt.Sprint(want) {
		status = "FAIL"
	}
	fmt.Printf("%-24s %s (readback %v, want %v)\n", label, status, got, want)
}

func watchSettle(fc *focuslynx.Focuser) {
	// The hub can report IsMoving=false for a moment right after accepting a move, so
	// wait briefly for motion to actually begin before watching it finish — otherwise
	// we'd report "settled" before the focuser has moved a single step.
	for i := 0; i < 20; i++ {
		if m, _ := fc.IsMoving(); m {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for i := 0; i < 600; i++ {
		m, err := fc.IsMoving()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ismoving:", err)
			return
		}
		if !m {
			p, _ := fc.Position()
			fmt.Printf("settled at %d\n", p)
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	fmt.Println("gave up waiting for the focuser to settle")
}

func cfgStr(fc *focuslynx.Focuser, key string) string {
	cfg, err := fc.Config()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cfg[key])
}

func cfgInt(fc *focuslynx.Focuser, key string) int {
	n, _ := strconv.Atoi(cfgStr(fc, key))
	return n
}

func maxStep(fc *focuslynx.Focuser) int {
	m, _ := fc.MaxStep()
	if m <= 0 {
		return 1 << 30
	}
	return m
}

func onoff(s string) bool {
	switch strings.ToLower(s) {
	case "on", "1", "true", "yes":
		return true
	}
	return false
}

// pick returns a if it differs from cur, else b (so the probe value is never a no-op).
func pick(cur, a, b int) int {
	if cur == a {
		return b
	}
	return a
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
