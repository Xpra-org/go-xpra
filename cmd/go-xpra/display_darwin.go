//go:build darwin

package main

import (
	"github.com/Xpra-org/go-xpra/darwin"
	"github.com/Xpra-org/go-xpra/ui"
)

// openDisplay connects to AppKit.
func openDisplay() (ui.Display, error) {
	display, err := darwin.Open()
	if err != nil {
		return nil, err
	}
	return display, nil
}

// runMain hands control to darwin.RunMain, which spins the AppKit run loop on
// the real OS main thread while fn runs in the background. See
// darwin.RunMain's doc comment for why this indirection exists only on
// darwin: Cocoa requires the run loop on that specific thread, unlike Win32's
// message loop, which any thread may own.
func runMain(fn func() error) error {
	return darwin.RunMain(fn)
}
