// Package x11 renders xpra windows onto the local X server.
//
// It is deliberately Linux/X11 specific: xpra forwards real top-level windows
// with absolute positions, override-redirect popups and X11 keycodes, all of
// which map one-to-one onto Xlib concepts and awkwardly onto a GUI toolkit.
// Everything here is pure Go via xgb, with no cgo.
package x11

import (
	"fmt"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
	"github.com/jezek/xgbutil/xprop"
)

// Display is a connection to the X server plus the bits of global state the
// windows need.
type Display struct {
	X *xgbutil.XUtil

	// WMProtocols and WMDeleteWindow identify the "please close" message the
	// window manager sends when the user clicks a titlebar close button.
	WMProtocols    xproto.Atom
	WMDeleteWindow xproto.Atom

	// blackGC is the graphics context every window draws through. Pixel
	// uploads do not consult the GC's colours, so one shared context suffices
	// for all windows of the root depth; the black foreground only matters
	// when clearing a freshly allocated pixmap.
	blackGC xproto.Gcontext

	// maxImageBytes is how much pixel data fits in a single PutImage request.
	maxImageBytes int

	events chan xgb.Event
	done   chan struct{}
}

// Open connects to the X server named by $DISPLAY.
func Open() (*Display, error) {
	X, err := xgbutil.NewConn()
	if err != nil {
		return nil, fmt.Errorf("connecting to the X server: %w", err)
	}
	// keybind builds the keycode/keysym tables that key events are translated
	// through, and keeps them current across MappingNotify.
	keybind.Initialize(X)

	d := &Display{
		X:      X,
		events: make(chan xgb.Event, 256),
		done:   make(chan struct{}),
	}
	if d.WMProtocols, err = xprop.Atm(X, "WM_PROTOCOLS"); err != nil {
		return nil, fmt.Errorf("interning WM_PROTOCOLS: %w", err)
	}
	if d.WMDeleteWindow, err = xprop.Atm(X, "WM_DELETE_WINDOW"); err != nil {
		return nil, fmt.Errorf("interning WM_DELETE_WINDOW: %w", err)
	}
	if err := d.createGC(); err != nil {
		return nil, err
	}

	// The server states its request limit in 4-byte units. Everything but the
	// pixel payload of a PutImage fits in 24 bytes; leaving a little more than
	// that spare keeps us clear of the limit without needing to be exact.
	d.maxImageBytes = int(xproto.Setup(X.Conn()).MaximumRequestLength)*4 - 64

	go d.readLoop()
	return d, nil
}

func (d *Display) createGC() error {
	gc, err := xproto.NewGcontextId(d.X.Conn())
	if err != nil {
		return fmt.Errorf("allocating a graphics context id: %w", err)
	}
	screen := d.X.Screen()
	err = xproto.CreateGCChecked(d.X.Conn(), gc, xproto.Drawable(d.X.RootWin()),
		xproto.GcForeground|xproto.GcBackground,
		[]uint32{screen.BlackPixel, screen.BlackPixel}).Check()
	if err != nil {
		return fmt.Errorf("creating a graphics context: %w", err)
	}
	d.blackGC = gc
	return nil
}

// Events returns the channel of incoming X events. It is closed when the
// connection to the X server ends.
func (d *Display) Events() <-chan xgb.Event { return d.events }

// Depth returns the root window's depth, which is what all our windows use.
func (d *Display) Depth() byte { return d.X.Screen().RootDepth }

// Close tears down the X connection, which also stops the event loop.
func (d *Display) Close() {
	select {
	case <-d.done:
		return
	default:
		close(d.done)
	}
	d.X.Conn().Close()
}

func (d *Display) readLoop() {
	defer close(d.events)
	for {
		event, err := d.X.Conn().WaitForEvent()
		if event == nil && err == nil {
			// Both nil means the connection is gone.
			return
		}
		if err != nil {
			// Protocol errors are reported the same way as events. They are
			// not fatal on their own — an X error for a window we already
			// destroyed is routine — so log nothing here and carry on.
			continue
		}
		select {
		case d.events <- event:
		case <-d.done:
			return
		}
	}
}
