//go:build !darwin

package focuslynx

import (
	"fmt"
	"strings"

	"go.bug.st/serial/enumerator"
)

// USB IDs per variant, as the enumerator reports them (hex strings, matched
// case-insensitively): FocusLynx is an FTDI bridge; ThirdLynx is a Microchip CDC
// device pinned by both VID and PID (so a generic Microchip device isn't claimed).
const (
	vidFTDI      = "0403" // FocusLynx (FTDI)
	vidMicrochip = "04D8" // ThirdLynx (Microchip CDC)
	pidThirdLynx = "ED77" // ThirdLynx product ID
)

// enumeratePorts lists FocusLynx/ThirdLynx ports via go.bug.st/serial's enumerator,
// which is pure Go on every non-darwin OS and reports USB VID/PID, tagging each with
// its variant baud.
func enumeratePorts() ([]DeviceInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, fmt.Errorf("focuslynx: enumerate ports: %w", err)
	}
	var out []DeviceInfo
	for _, p := range ports {
		switch {
		case !p.IsUSB:
			continue
		case strings.EqualFold(p.VID, vidFTDI):
			out = append(out, DeviceInfo{Port: p.Name, Baud: baudFocusLynx})
		case strings.EqualFold(p.VID, vidMicrochip) && strings.EqualFold(p.PID, pidThirdLynx):
			out = append(out, DeviceInfo{Port: p.Name, Baud: baudThirdLynx})
		}
	}
	return out, nil
}
