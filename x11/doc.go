// Package x11 renders xpra windows onto the local X server.
//
// It is deliberately X11 specific: xpra forwards real top-level windows with
// absolute positions, override-redirect popups and X11 keycodes, all of which
// map one-to-one onto Xlib concepts and awkwardly onto a GUI toolkit.
// Everything here is pure Go via xgb, with no cgo.
//
// The package has nothing in it on any other platform — cmd/go-xpra selects the
// backend for the system it is built for — so it must keep at least this one
// unconstrained file to remain a buildable package everywhere.
package x11
