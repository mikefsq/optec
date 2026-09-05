// Package focuslynx controls Optec FocusLynx and ThirdLynx focuser channels over USB-serial.
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

// DeviceInfo contains serial-port discovery metadata.
type DeviceInfo struct {
	Port string // e.g. /dev/cu.usbserial-XXXX, /dev/ttyUSB0, COM3
	Baud int    // 115200 (FocusLynx) or 19200 (ThirdLynx)
}
