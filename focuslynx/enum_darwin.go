//go:build darwin

package focuslynx

import (
	"fmt"
	"strings"

	bugst "go.bug.st/serial"
)

// enumeratePorts finds candidate serial ports by macOS device names, without cgo.
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
