//go:build linux

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"github.com/Xpra-org/go-xpra/ui"
	"github.com/Xpra-org/go-xpra/wayland"
	"github.com/Xpra-org/go-xpra/x11"
)

// backend chooses between the two display backends Linux has. The flag is
// declared here rather than in main.go because Linux is the only platform with
// a choice to make; flag.Parse finds it all the same.
var backend = flag.String("backend", "auto", "display backend: auto, x11 or wayland")

// openDisplay connects to the local desktop.
//
// X11 comes first when both are available. A session running XWayland is still
// an X session as far as this client is concerned, and it is the better path
// through one: xpra deals in absolute window positions, which an X server
// honours and a Wayland compositor cannot.
func openDisplay() (ui.Display, error) {
	switch *backend {
	case "x11":
		return openX11()
	case "wayland":
		return openWayland()
	case "auto":
	default:
		return nil, fmt.Errorf("unknown display backend %q: use auto, x11 or wayland", *backend)
	}

	var failures []error
	for _, candidate := range []struct {
		env  string
		open func() (ui.Display, error)
	}{
		{"DISPLAY", openX11},
		{"WAYLAND_DISPLAY", openWayland},
	} {
		if os.Getenv(candidate.env) == "" {
			continue
		}
		display, err := candidate.open()
		if err == nil {
			return display, nil
		}
		failures = append(failures, err)
	}
	if len(failures) == 0 {
		return nil, errors.New("no local desktop: neither DISPLAY nor WAYLAND_DISPLAY is set")
	}
	return nil, errors.Join(failures...)
}

// openX11 connects to the X server named by $DISPLAY.
func openX11() (ui.Display, error) {
	display, err := x11.Open()
	if err != nil {
		return nil, err
	}
	return display, nil
}

// openWayland connects to the compositor named by $WAYLAND_DISPLAY.
func openWayland() (ui.Display, error) {
	display, err := wayland.Open()
	if err != nil {
		return nil, err
	}
	return display, nil
}
