package ui

// Event is one thing the user or the desktop did to a forwarded window.
//
// The set is exactly what the client turns into xpra packets, and no more: a
// backend that has nothing to say about, say, expose events simply never emits
// one, because a window's pixels are held by the platform rather than by us.
type Event interface{ isEvent() }

// Configure reports the geometry a window now has, after the desktop moved or
// resized it. Backends emit it only for changes they made themselves, never to
// echo back a MoveResize the client asked for.
type Configure struct {
	Window              WindowID
	X, Y, Width, Height int
}

// CloseRequest is the user asking for a window to go away, through the frame's
// close button or its equivalent. Nothing is closed locally: the request goes to
// the server, and the window disappears when the server says so.
type CloseRequest struct {
	Window WindowID
}

// Motion is pointer movement over a window. X and Y are absolute screen
// coordinates, which is the space the server places the pointer in.
type Motion struct {
	Window WindowID
	X, Y   int
}

// Button is a pointer button going down or up. The numbering is X11's, which is
// what xpra forwards: 1 left, 2 middle, 3 right, 4 and 5 the vertical wheel, 6
// and 7 the horizontal wheel, 8 and 9 back and forward.
type Button struct {
	Window  WindowID
	Button  int
	Pressed bool
	X, Y    int
}

// Key is a key going down or up, already resolved to the X11 names the server
// matches on.
//
// Name is the load-bearing field. Because this client uploads no keymap, the
// server resolves each key through find_matching_keycode
// (xpra/x11/server/keyboard_config.py:551), which looks the name up in its own
// keymap — so it has to be a real X11 keysym name such as "braceleft", not the
// character the key produces. Keysym, Text and Keycode are hints the server
// falls back on, and a backend that cannot supply one leaves it zero or empty.
type Key struct {
	Window    WindowID
	Pressed   bool
	Name      string
	Keysym    int
	Text      string
	Keycode   int
	Modifiers []string
}

// Focus is a window taking the keyboard focus.
type Focus struct {
	Window WindowID
}

func (Configure) isEvent()    {}
func (CloseRequest) isEvent() {}
func (Motion) isEvent()       {}
func (Button) isEvent()       {}
func (Key) isEvent()          {}
func (Focus) isEvent()        {}
