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

// Enumerate lists candidate FocusLynx and ThirdLynx ports with their baud rates.
func Enumerate() ([]DeviceInfo, error) { return enumeratePorts() }

// openFirst opens the first attached port that IDENTIFIES as a FocusLynx, not merely the first
// port with a matching USB vendor id.
//
// FTDI's 0403 is a generic USB-serial bridge shared by mounts, focusers, sky-quality meters and
// anything else that needed a UART. Taking ports[0] on the strength of the VID alone binds
// whichever such device the OS happened to enumerate first, holds its port against its real
// driver, and then talks a protocol it does not speak. OpenByNickname already scans this way and
// says why: only a real FocusLynx answers HELLO.
func openFirst() (Transport, DeviceInfo, error) {
	ports, err := enumeratePorts()
	if err != nil {
		return nil, DeviceInfo{}, err
	}
	if len(ports) == 0 {
		return nil, DeviceInfo{}, errors.New("focuslynx: no FocusLynx/ThirdLynx serial port found")
	}
	for _, d := range ports {
		t, info, err := openPort(d.Port, d.Baud)
		if err != nil {
			continue // busy (its real driver holds it) or not openable: says nothing about what it is
		}
		if speaksFocusLynx(New(t, info)) {
			return t, info, nil
		}
		t.Close()
	}
	return nil, DeviceInfo{}, fmt.Errorf("focuslynx: none of %d candidate port(s) answered as a FocusLynx/ThirdLynx", len(ports))
}

// speaksFocusLynx reports whether the hub answers the protocol.
//
// HELLO on channel 1 is the test: every unit has a channel 1, and a device that is not a FocusLynx
// stays silent rather than replying, so a parsed reply is positive identification rather than
// "something was listening". Channel 2 is not tried — a ThirdLynx has none, and a hub that answers
// on 1 is already identified.
func speaksFocusLynx(h *Hub) bool {
	_, err := h.Focuser(1).Hello()
	return err == nil
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
