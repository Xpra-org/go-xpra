//go:build linux

package x11

import (
	"fmt"
	"log"

	"github.com/jezek/xgb/render"
	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil"
	"github.com/jezek/xgbutil/keybind"
	"github.com/jezek/xgbutil/xprop"

	"github.com/Xpra-org/go-xpra/ui"
)

// Display is a connection to the X server plus the bits of global state the
// windows need.
type Display struct {
	X *xgbutil.XUtil

	// wmProtocols and wmDeleteWindow identify the "please close" message the
	// window manager sends when the user clicks a titlebar close button.
	wmProtocols    xproto.Atom
	wmDeleteWindow xproto.Atom

	// blackGC is the graphics context every window draws through. Pixel
	// uploads do not consult the GC's colours, so one shared context suffices
	// for all windows of the root depth; the black foreground only matters
	// when clearing a freshly allocated pixmap.
	blackGC xproto.Gcontext

	// maxImageBytes is how much pixel data fits in a single PutImage request.
	maxImageBytes int

	// cursorFormat is the X Render ARGB32 format used to build server-provided
	// colour cursors. cursor is kept alive so newly created windows can inherit
	// the same session-wide shape.
	cursorFormat render.Pictformat
	cursor       xproto.Cursor
	windows      map[xproto.Window]*Window

	events    chan ui.Event
	done      chan struct{}
	clipboard *clipboard
}

var _ ui.Display = (*Display)(nil)

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
		X:       X,
		windows: map[xproto.Window]*Window{},
		events:  make(chan ui.Event, 256),
		done:    make(chan struct{}),
	}
	if d.wmProtocols, err = xprop.Atm(X, "WM_PROTOCOLS"); err != nil {
		return nil, fmt.Errorf("interning WM_PROTOCOLS: %w", err)
	}
	if d.wmDeleteWindow, err = xprop.Atm(X, "WM_DELETE_WINDOW"); err != nil {
		return nil, fmt.Errorf("interning WM_DELETE_WINDOW: %w", err)
	}
	if err := d.createGC(); err != nil {
		return nil, err
	}
	if err := d.initCursorFormat(); err != nil {
		return nil, err
	}
	if clipboard, err := newClipboard(d); err != nil {
		log.Printf("x11: clipboard is unavailable: %v", err)
	} else {
		d.clipboard = clipboard
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

// Events returns the channel of incoming events. It is closed when the
// connection to the X server ends.
func (d *Display) Events() <-chan ui.Event { return d.events }

// Clipboard returns the X11 CLIPBOARD selection adapter, or nil when the
// server does not provide the XFixes extension needed to watch ownership.
func (d *Display) Clipboard() ui.Clipboard {
	if d.clipboard == nil {
		return nil
	}
	return d.clipboard
}

// Bell rings the X server's keyboard bell. The core request can vary the
// configured volume, but pitch and duration remain properties of the local X
// server rather than parameters of an individual bell request.
func (d *Display) Bell(percent, _pitch, _duration int64, _name string) {
	volume := int8(min(max(percent, -100), 100))
	xproto.Bell(d.X.Conn(), volume)
}

// Depth returns the root window's depth, which is what all our windows use.
func (d *Display) Depth() byte { return d.X.Screen().RootDepth }

// DesktopSize returns the X screen's root dimensions in physical pixels.
func (d *Display) DesktopSize() (int, int, bool) {
	screen := d.X.Screen()
	return int(screen.WidthInPixels), int(screen.HeightInPixels), true
}

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
		translated, ok := d.translate(event)
		if !ok {
			continue
		}
		select {
		case d.events <- translated:
		case <-d.done:
			return
		}
	}
}
