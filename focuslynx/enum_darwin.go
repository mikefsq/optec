//go:build darwin

package focuslynx

import (
	"fmt"
	"strings"

	bugst "go.bug.st/serial"
)

// enumeratePorts lists FocusLynx/ThirdLynx ports on macOS by the device-name
// convention: FTDI (FocusLynx) → /dev/cu.usbserial-* @115200; Microchip CDC
// (ThirdLynx) → /dev/cu.usbmodem* @19200. Reading the USB VID on macOS would
// require the enumerator's cgo (IOKit) path, which has no CGO_ENABLED=0 fallback and
// so would break cross-compilation to darwin; GetPortsList is pure Go, so discovery
// here is name-based.
func enumeratePorts() ([]DeviceInfo, error) {
	names, err := bugst.GetPortsList()
	if err != nil {
		return nil, fmt.Errorf("focuslynx: list ports: %w", err)
	}
	var out []DeviceInfo
	for _, n := range names {
		switch {
		case strings.HasPrefix(n, "/dev/cu.") && strings.Contains(n, "usbserial"):
			out = append(out, DeviceInfo{Port: n, Baud: baudFocusLynx})
		case strings.HasPrefix(n, "/dev/cu.") && strings.Contains(n, "usbmodem"):
			out = append(out, DeviceInfo{Port: n, Baud: baudThirdLynx})
		}
	}
	return out, nil
}
