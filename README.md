# optec

Go driver for Optec **FocusLynx** / **ThirdLynx** focuser hubs — package `focuslynx/`.

The FocusLynx is a **two-channel** focuser hub: one USB-serial connection drives two
focuser ports (`F1`, `F2`) plus hub-level commands (`FH`). The **ThirdLynx** is the
single-channel variant (`F1` only). Both speak the same ASCII protocol; the driver is
Go over [`go.bug.st/serial`](https://pkg.go.dev/go.bug.st/serial).

## Protocol

Commands are ASCII wrapped in angle brackets, e.g. `<F1GETSTATUS>`; the destination is
`F1`/`F2` (focuser channel) or `FH` (hub). 

Every reply opens with a `!` acknowledgement, then:

- an **action** adds a one-word status line — `M` (moving), `H` (homing), `HALTED`,
  `STOPPED`, or `SET` (config saved);
- a **query** adds a `key = value` block (after a `STATUSn` / `CONFIGn` / `HUB INFO`
  header) terminated by `END`;
- a **rejected** command adds an `ER=<n> <message>` line in place of the keyword.

| Command | Meaning | Reply |
|---|---|---|
| `<F1GETSTATUS>` | status block | `Temp(C)`, `Curr Pos`, `Targ Pos`, `IsMoving`, `IsHoming`, `IsHomed`, `TmpProbe`, … `END` |
| `<F1GETCONFIG>` | config block | `Nickname`, `Max Pos`, `Dev Typ`, `TComp ON`, `TempCo A`–`E`, `TC Mode`, `BLC En`, `BLC Stps`, `LED Brt`, `TC@Start` `END` |
| `<FHGETHUBINFO>` | hub info | `Hub FVer`, `Sleeping`, `Wired IP`, `WF …` `END` |
| `<F1HELLO>` | report nickname | `!` + nickname |
| `<F1MA012345>` | move absolute (6-digit) | `!` `M` |
| `<F1MIR0>` `<F1MOR1>` `<F1ERM>` | relative move in/out (`z`: 0 high / 1 low), end relative | `!` `M` / `STOPPED` |
| `<F1HALT>` `<F1HOME>` `<F1CENTER>` | halt / home / center-of-travel | `!` `HALTED` / `H` / `M` |
| `<F1SCNN…>` `<F1SCDT**>` `<F1SCCP012345>` | set nickname / device type / sync position | `!` `SET` |
| `<F1SCBE1>` `<F1SCBS50>` | backlash comp enable / steps (2-digit) | `!` `SET` |
| `<F1SCTE1>` `<F1SCTMC>` `<F1SCTCD+0092>` `<F1SCTS1>` | temp-comp enable / mode A–E / coefficient / at-start | `!` `SET` |
| `<FHSCLB085>` | hub LED brightness (3-digit, 0–100) | `!` `SET` |
| `<F1RESET>` | factory reset channel | `!` `SET` |

## Usage

```go
// Open by nickname (returns the hub and the channel it resolved to). Alternatively
// focuslynx.OpenFirst() or focuslynx.OpenPort("/dev/ttyUSB0"), then hub.Focuser(1|2).
hub, ch, err := focuslynx.OpenByNickname("focuser")
if err != nil {
	log.Fatal(err)
}
defer hub.Close()

f := hub.Focuser(ch)
pos, _ := f.Position()
_ = f.MoveTo(12000) // absolute
temp, _ := f.Temperature()
_ = f.SetTempComp(true)
```

A hub is identified by its **nickname**, read over the protocol (`HELLO`) — a stable,
platform-independent identity that survives replug and port renumbering and
disambiguates several FTDI devices sharing one VID/PID (only a real FocusLynx answers
`HELLO`). Assign one with `Focuser.SetNickname`.

## flprobe

`cmd/flprobe` is a CLI to inspect and exercise a hub:

```sh
flprobe -list                    # candidate serial ports
flprobe -nickname "OAG focuser"  # open by nickname (scans ports)
flprobe                          # read-only: hub / status / config dump
flprobe -moveto 12000            # absolute move, watch settle
flprobe -reltest                 # continuous relative move in/out
flprobe -stoptest 40000          # move far, then halt mid-flight
flprobe -cfgtest                 # safe round-trip of every config setter (reverted)
flprobe -setnick "focuser"       # label this unit
flprobe -raw "<F1GETSTATUS>"     # send a raw command, show the reply
```

## Layout

```
focuslynx/
  focuslynx.go        Hub + per-channel Focuser; <…> framing, !/keyword/END parsing,
                      error (ER=) detection, OpenByNickname
  transport.go        serial Transport seam (Write/Read/Close) + DeviceInfo + Baud
  serial.go           go.bug.st/serial port open (pure Go, all OSes)
  enum_other.go       !darwin — USB VID/PID via the pure-Go enumerator
  enum_darwin.go      darwin  — device-name match (cgo-free)
  focuslynx_test.go   protocol tests over a fake hub transport
cmd/flprobe/          inspect / drive / validate a focuser channel
```

## Variants & portability

| Variant | Bridge | VID / PID | Baud | Channels |
|---|---|---|---|---|
| FocusLynx | FTDI | 0x0403 | 115200 | F1, F2 |
| ThirdLynx | Microchip CDC | 0x04D8 / 0xED77 | 19200 | F1 |

The sole dependency is `go.bug.st/serial`, used only on its Go paths, so the driver
compiles for any target with `CGO_ENABLED=0` (linux/darwin/windows × amd64/arm64/arm).
macOS device discovery is name-based to avoid the enumerator's lone cgo (IOKit) path.

An ASCOM **Alpaca** server over this driver lives in a separate module,
`goalpaca_devices/focuslynx` (one Alpaca Focuser device per channel, bound by nickname).

## Build & test

```sh
go test -race ./focuslynx/
GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -o flprobe ./cmd/flprobe   # e.g. Raspberry Pi
```

## License

[MIT](LICENSE) © 2026 mikefsq
