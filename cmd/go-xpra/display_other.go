//go:build !linux && !windows

package main

import (
	"fmt"
	"runtime"

	"github.com/Xpra-org/go-xpra/ui"
)

// openDisplay fails: there is no backend for this platform. The file exists so
// that such a platform gets this message rather than a build error.
func openDisplay() (ui.Display, error) {
	return nil, fmt.Errorf("no display backend for %s: this client supports linux and windows", runtime.GOOS)
}
