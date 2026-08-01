// Package ui is the boundary between the xpra client and the local desktop.
//
// The client state machine deals only in the types declared here, so that the
// windowing system it is driving — X11 or Wayland on Linux, Win32 on Windows —
// is chosen at run time and is otherwise invisible to it. A backend implements
// Display and Window, and translates its native event queue into the small set
// of events in event.go.
//
// The pixel and keysym helpers live here because every backend needs them: the
// pixel converters because BGRX happens to be the native layout of an X11
// framebuffer, a Win32 DIB and a wl_shm buffer alike, and the keysym names
// because they are really part of the xpra protocol vocabulary rather than of
// any one platform.
package ui

import "fmt"

// WindowID identifies a window created by a backend. It is only ever compared
// and used as a map key; an X11 window id and a Win32 window handle both fit.
type WindowID uintptr

// Cursor is one session-wide pointer image.
//
// Pixels are tightly packed, alpha-premultiplied BGRA. That maps directly to
// Win32's 32-bit cursor bitmaps and to X Render's usual little-endian ARGB32
// representation; other X11 byte orders are converted by that backend.
type Cursor struct {
	Pixels             []byte
	Width, Height      int
	HotspotX, HotspotY int
}

// Icon is one application-provided window icon.
//
// Pixels are tightly packed, alpha-premultiplied BGRA. This is the native
// representation used by Win32 and Wayland; X11 converts it to the straight
// ARGB CARDINAL values required by _NET_WM_ICON.
type Icon struct {
	Pixels        []byte
	Width, Height int
}

// Validate reports whether an icon is one the backends can carry.
func (i *Icon) Validate() error {
	if i.Width <= 0 || i.Height <= 0 ||
		i.Width > int(^uint16(0)) || i.Height > int(^uint16(0)) {
		return fmt.Errorf("invalid icon size %dx%d", i.Width, i.Height)
	}
	if len(i.Pixels) != i.Width*i.Height*BytesPerPixel {
		return fmt.Errorf("icon has %d pixel bytes, want %d",
			len(i.Pixels), i.Width*i.Height*BytesPerPixel)
	}
	return nil
}

// Validate reports whether a cursor is one the backends can carry.
//
// The pixels come from the server by way of a PNG decoder, so the dimensions
// and the hotspot are only as trustworthy as the packet was. The 16-bit cap is
// X11's wire limit for a cursor and Win32's for a bitmap; no real cursor comes
// close to it.
func (c *Cursor) Validate() error {
	if c.Width <= 0 || c.Height <= 0 ||
		c.Width > int(^uint16(0)) || c.Height > int(^uint16(0)) {
		return fmt.Errorf("invalid cursor size %dx%d", c.Width, c.Height)
	}
	if len(c.Pixels) != c.Width*c.Height*BytesPerPixel {
		return fmt.Errorf("cursor has %d pixel bytes, want %d",
			len(c.Pixels), c.Width*c.Height*BytesPerPixel)
	}
	if c.HotspotX < 0 || c.HotspotX >= c.Width ||
		c.HotspotY < 0 || c.HotspotY >= c.Height {
		return fmt.Errorf("cursor hotspot %d,%d lies outside %dx%d",
			c.HotspotX, c.HotspotY, c.Width, c.Height)
	}
	return nil
}

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

	// SetCursor applies one session-wide pointer image to every forwarded
	// window, including windows created later. A nil cursor restores the
	// platform default.
	SetCursor(cursor *Cursor) error

	// Close tears down the connection and every window on it.
	Close()
}

// Clipboard is the text clipboard exposed by a desktop backend.
//
// It is deliberately smaller than Display because clipboard support is an
// optional desktop capability: a Wayland compositor may omit the data-device
// manager, and an unusually old X server may have no XFixes extension.
type Clipboard interface {
	// SetText makes text the current contents of the ordinary desktop
	// clipboard. Local changes arrive through Display.Events as
	// ClipboardChange values.
	SetText(text string) error
}

// ClipboardProvider is implemented by displays which know how to expose a
// native clipboard. Clipboard returns nil when the desktop service needed for
// it was not available at startup.
type ClipboardProvider interface {
	Clipboard() Clipboard
}

// SystemTrayProvider is implemented by displays which can expose a native
// session control in the desktop's notification area. ShowTray is optional:
// clients continue normally when a backend does not implement it or setup
// fails.
type SystemTrayProvider interface {
	ShowTray(title string) error
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

	// SetIcon applies an application-provided icon. A nil icon restores the
	// platform default.
	SetIcon(icon *Icon) error

	// Map shows the window.
	Map()

	// Raise brings the window to the front of the local desktop.
	Raise()

	// Minimize hides or restores the window using the local desktop's native
	// minimized state.
	Minimize(minimized bool)

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
