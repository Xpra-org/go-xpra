//go:build !linux && !windows && !darwin

package main

import (
	"fmt"
	"runtime"

	"github.com/Xpra-org/go-xpra/ui"
)

// openDisplay fails: there is no backend for this platform. The file exists so
// that such a platform gets this message rather than a build error.
func openDisplay() (ui.Display, error) {
	return nil, fmt.Errorf("no display backend for %s: this client supports linux, windows and darwin", runtime.GOOS)
}

// runMain runs fn directly: only darwin needs the indirection of running the
// display backend's own run loop on a specific thread first.
func runMain(fn func() error) error { return fn() }
