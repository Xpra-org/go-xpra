//go:build linux

package main

import (
	"github.com/Xpra-org/go-xpra/ui"
	"github.com/Xpra-org/go-xpra/x11"
)

// openDisplay connects to the X server named by $DISPLAY.
func openDisplay() (ui.Display, error) {
	display, err := x11.Open()
	if err != nil {
		return nil, err
	}
	return display, nil
}
