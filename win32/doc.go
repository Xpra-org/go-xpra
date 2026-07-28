// Package win32 renders xpra windows onto the Windows desktop.
//
// It is a direct translation of the same ideas the X11 backend implements: one
// real top-level window per forwarded window, placed in absolute screen
// coordinates, painted from raw BGRX pixels, with local input translated back
// into the X11 vocabulary the xpra protocol speaks. Everything goes through the
// syscall package, so this is still pure Go with no cgo.
//
// The package has nothing in it on any other platform — cmd/go-xpra selects the
// backend for the system it is built for — so it must keep at least this one
// unconstrained file to remain a buildable package everywhere.
package win32
