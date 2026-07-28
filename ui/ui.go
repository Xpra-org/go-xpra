// Package ui is the boundary between the xpra client and the local desktop.
//
// The client state machine deals only in the types declared here, so that the
// windowing system it is driving — X11 on Linux, Win32 on Windows — is chosen
// at build time and is otherwise invisible to it. A backend implements Display
// and Window, and translates its native event queue into the small set of
// events in event.go.
//
// The pixel and keysym helpers live here because both backends need them: the
// pixel converters because BGRX happens to be the native layout of an X11
// framebuffer and of a Win32 DIB alike, and the keysym names because they are
// really part of the xpra protocol vocabulary rather than of any one platform.
package ui

// WindowID identifies a window created by a backend. It is only ever compared
// and used as a map key; an X11 window id and a Win32 window handle both fit.
type WindowID uintptr

// Display is a connection to the local desktop.
type Display interface {
	// NewWindow creates an unmapped top-level window whose content area is
	// width by height pixels at screen position x,y.
	//
	// Override-redirect windows — menus, tooltips — must escape any local
	// window management, so that they appear exactly where the server put them
	// and take no focus.
	NewWindow(x, y, width, height int, overrideRedirect bool) (Window, error)

	// Events returns the stream of local events. It is closed when the
	// connection to the desktop ends.
	Events() <-chan Event

	// Bell rings the local desktop bell for a remote application. Backends use
	// whichever bell properties their platform can represent.
	Bell(percent, pitch, duration int64, name string)

	// Close tears down the connection and every window on it.
	Close()
}

// Window is one forwarded xpra window shown on the local desktop.
//
// Coordinates are always those of the content area, in screen space: that is
// what the server sends and what it expects back, and it is the only frame of
// reference a window manager and a Win32 window frame agree on.
type Window interface {
	// ID returns the identifier that events for this window carry.
	ID() WindowID

	// Geometry returns the position and size the window was last known to have.
	Geometry() (x, y, width, height int)

	SetTitle(title string)

	// Map shows the window.
	Map()

	// Raise brings the window to the front of the local desktop.
	Raise()

	// Destroy releases the window and its pixels.
	Destroy()

	// MoveResize applies a server-driven geometry change.
	MoveResize(x, y, width, height int) error

	// Resized records a geometry change the desktop has already made — the
	// local window manager resizing us, say — so that the backing pixels can be
	// reallocated to match.
	Resized(x, y, width, height int) error

	// Paint uploads one decoded damage rectangle at x,y within the window.
	//
	// rowstride is the number of bytes between the start of consecutive rows in
	// pixels, which the server computes as roundup(width*bytesPerPixel, 4) and
	// may make larger still — so it is never safe to assume the payload is
	// tightly packed. format names the source layout, one of those accepted by
	// ConverterFor.
	Paint(x, y, width, height int, pixels []byte, rowstride int, format string) error
}
