// Package focuslynx is a pure-Go driver for Optec FocusLynx / ThirdLynx
// focusers over their USB-serial link.
//
// FocusLynx is a two-channel focuser hub (FTDI bridge, VID 0x0403, 115200 baud)
// driving ports F1 and F2 plus hub commands (FH). ThirdLynx is a single-channel
// controller (Microchip CDC bridge, VID 0x04D8, 19200 baud) speaking the same
// protocol on F1. Both use ASCII commands wrapped in angle brackets
// ("<F1GETSTATUS>"). Every reply opens with a "!" acknowledgement; an action then
// adds a one-word status line ("M", "H", "HALTED", "STOPPED", "SET") and a query
// adds a "key = value" block terminated by "END". A malformed command returns an
// error string in place of the "!".
//
// The transport opens the OS serial port at the variant's baud (8N1) and does line
// I/O via go.bug.st/serial — no vendor library. It uses only the library's pure-Go
// paths (port I/O everywhere; the USB-VID/PID enumerator off macOS, device-name
// matching on macOS), so it builds for any target with CGO_ENABLED=0.
package focuslynx

// Line speeds per variant. The matching USB IDs live in enum_other.go (as the hex
// strings the enumerator reports).
const (
	baudFocusLynx = 115200
	baudThirdLynx = 19200
)

// Transport is a byte-level serial channel (satisfied by a go.bug.st/serial port);
// the protocol logic frames the ASCII commands over it. Read should block up to a
// short timeout and return 0 bytes (not an error) when nothing is available, so the
// line reader can poll to a deadline.
type Transport interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	Close() error
}

// DeviceInfo identifies an opened serial port and the baud its variant uses.
type DeviceInfo struct {
	Port string // e.g. /dev/cu.usbserial-XXXX, /dev/ttyUSB0, COM3
	Baud int    // 115200 (FocusLynx) or 19200 (ThirdLynx)
}
