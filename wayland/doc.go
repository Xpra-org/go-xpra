// Package wayland renders xpra windows onto a Wayland compositor.
//
// It is the third telling of what the x11 and win32 backends already do: one
// real top-level surface per forwarded window, painted from raw BGRX pixels,
// with local input translated back into the X11 vocabulary the xpra protocol
// speaks. Everything goes through pure Go bindings to the wire protocol, so
// this too needs no cgo.
//
// Wayland differs from the other two in one way that runs through the whole
// package: a client cannot place its own windows. There is no request for it,
// deliberately, and no way to ask where a window ended up. But xpra deals in
// absolute screen positions — it sends them, and it expects pointer events and
// configure replies back in the same frame of reference — so the coordinates
// the server gives us are kept as bookkeeping and used to answer it, while the
// compositor puts the windows wherever it likes. Pointer positions are
// reported relative to where the server thinks the window is, which keeps the
// remote application's own idea of the pointer consistent with its windows.
//
// Two other requests have no equivalent here and are no-ops: raising a window,
// and restoring a minimized one.
//
// The package has nothing in it on any other platform — cmd/go-xpra selects the
// backend for the system it is built for — so it must keep at least this one
// unconstrained file to remain a buildable package everywhere.
package wayland
