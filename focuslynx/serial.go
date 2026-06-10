package focuslynx

import (
	"errors"
	"fmt"
	"time"

	bugst "go.bug.st/serial"
)

// readTimeout is the per-read timeout set on the port. With it, Read returns
// promptly when bytes arrive and returns (0, nil) once idle past the timeout,
// which the line reader's deadline loop handles.
const readTimeout = 100 * time.Millisecond

// openPort opens dev at the given baud (8N1) as a Transport. The go.bug.st/serial
// port already satisfies Transport (Read/Write/Close) and its port I/O is pure Go
// on every OS, so the driver cross-compiles to any target.
func openPort(dev string, baud int) (Transport, DeviceInfo, error) {
	port, err := bugst.Open(dev, &bugst.Mode{
		BaudRate: baud,
		DataBits: 8,
		Parity:   bugst.NoParity,
		StopBits: bugst.OneStopBit,
	})
	if err != nil {
		return nil, DeviceInfo{}, fmt.Errorf("focuslynx: open %s @ %d: %w", dev, baud, err)
	}
	if err := port.SetReadTimeout(readTimeout); err != nil {
		port.Close()
		return nil, DeviceInfo{}, fmt.Errorf("focuslynx: set read timeout on %s: %w", dev, err)
	}
	return port, DeviceInfo{Port: dev, Baud: baud}, nil
}

// Enumerate lists attached FocusLynx (FTDI 0x0403 → 115200) and ThirdLynx
// (Microchip 0x04D8 & 0xED77 → 19200) ports, each tagged with its baud. Matching is
// per-OS: enum_other.go uses USB VID/PID via the pure-Go enumerator; enum_darwin.go
// matches the device-name convention, deliberately avoiding the enumerator's macOS
// cgo (IOKit) path so the driver builds for any target with CGO_ENABLED=0.
func Enumerate() ([]DeviceInfo, error) { return enumeratePorts() }

func openFirst() (Transport, DeviceInfo, error) {
	ports, err := enumeratePorts()
	if err != nil {
		return nil, DeviceInfo{}, err
	}
	if len(ports) == 0 {
		return nil, DeviceInfo{}, errors.New("focuslynx: no FocusLynx/ThirdLynx serial port found")
	}
	return openPort(ports[0].Port, ports[0].Baud)
}

// baudForPort resolves the baud for a named port from enumeration, defaulting to
// FocusLynx (115200) when the port isn't recognized.
func baudForPort(port string) int {
	ports, _ := enumeratePorts()
	for _, d := range ports {
		if d.Port == port {
			return d.Baud
		}
	}
	return baudFocusLynx
}
