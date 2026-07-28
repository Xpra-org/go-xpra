//go:build windows

package main

import (
	"github.com/Xpra-org/go-xpra/ui"
	"github.com/Xpra-org/go-xpra/win32"
)

// openDisplay starts the Win32 window thread that owns the forwarded windows.
func openDisplay() (ui.Display, error) {
	display, err := win32.Open()
	if err != nil {
		return nil, err
	}
	return display, nil
}
