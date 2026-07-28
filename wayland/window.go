//go:build linux

package wayland

import (
	"fmt"
	"log"

	"github.com/rajveermalviya/go-wayland/wayland/client"
	xdg_shell "github.com/rajveermalviya/go-wayland/wayland/stable/xdg-shell"
	xdg_decoration "github.com/rajveermalviya/go-wayland/wayland/unstable/xdg-decoration-v1"

	"github.com/Xpra-org/go-xpra/ui"
)

// Window is one forwarded xpra window.
//
// Its pixels live in memory shared with the compositor, which is the middle
// ground between the other two backends: like the Win32 buffer it is ordinary
// memory that a damage rectangle is copied into, and like the X11 pixmap the
// compositor reads it directly, so a repaint needs nothing from us beyond
// saying which part changed.
//
// x and y are what the server last said and are never learnt from the
// compositor, which does not report a position and would not accept one. They
// exist so that pointer events and configure replies can be expressed in the
// coordinates the server placed this window at.
type Window struct {
	d       *Display
	surface *client.Surface
	xdg     *xdg_shell.Surface

	// Exactly one of these is set: an ordinary window is a toplevel, an
	// override-redirect one is a popup.
	toplevel *xdg_shell.Toplevel
	popup    *xdg_shell.Popup

	decoration *xdg_decoration.ToplevelDecoration

	memory *sharedMemory
	pool   *client.ShmPool
	buffer *client.Buffer

	x, y, width, height int
	bufWidth, bufHeight int
	overrideRedirect    bool

	// A surface may not show anything until the compositor has configured it,
	// and the client only wants it shown once it has called Map, so the first
	// paint waits for both.
	mapped     bool
	configured bool
}

var _ ui.Window = (*Window)(nil)

// NewWindow creates an unmapped top-level window whose content area is the
// given geometry.
//
// The position is recorded rather than applied. Wayland has no request that
// places a window, so where it lands is the compositor's decision.
func (d *Display) NewWindow(x, y, width, height int, overrideRedirect bool) (ui.Window, error) {
	width, height = ui.ClampSize(width, height)

	d.mu.Lock()
	defer d.mu.Unlock()

	w := &Window{
		d: d,
		x: x, y: y, width: width, height: height,
		overrideRedirect: overrideRedirect,
	}
	if err := w.create(); err != nil {
		w.destroy()
		return nil, err
	}
	d.windows[w.surface.ID()] = w
	return w, nil
}

// create builds the surface and its shell role. It runs under the display lock.
func (w *Window) create() error {
	surface, err := w.d.compositor.CreateSurface()
	if err != nil {
		return fmt.Errorf("creating a surface: %w", err)
	}
	w.surface = surface

	if w.xdg, err = w.d.wmBase.GetXdgSurface(surface); err != nil {
		return fmt.Errorf("giving a surface a shell role: %w", err)
	}
	w.xdg.SetConfigureHandler(w.configure)

	if w.overrideRedirect {
		err = w.createPopup()
	} else {
		err = w.createToplevel()
	}
	if err != nil {
		return err
	}
	if err := w.newBuffer(w.width, w.height); err != nil {
		return err
	}
	// A commit with nothing attached is what asks for the first configure, and
	// a surface may not show anything before it has one.
	if err := surface.Commit(); err != nil {
		return fmt.Errorf("committing a new surface: %w", err)
	}
	return nil
}

func (w *Window) createToplevel() error {
	toplevel, err := w.xdg.GetToplevel()
	if err != nil {
		return fmt.Errorf("creating a toplevel: %w", err)
	}
	w.toplevel = toplevel
	toplevel.SetConfigureHandler(w.configureToplevel)
	toplevel.SetCloseHandler(func(xdg_shell.ToplevelCloseEvent) {
		// The server owns the decision, as on the other backends: it closes the
		// window by sending a window-destroy packet back.
		w.d.emit(ui.CloseRequest{Window: w.ID()})
	})
	if err := stringRequest(toplevel, opcodeToplevelSetAppID, appID); err != nil {
		return fmt.Errorf("naming a toplevel: %w", err)
	}
	w.requestDecoration()
	return nil
}

// requestDecoration asks the compositor to draw the window frame, which is the
// only way to have one: Wayland leaves decorations to the client, and this
// client draws none. A compositor without the extension — GNOME — leaves the
// windows bare, and the close button then lives in whatever gesture or overview
// it offers instead.
func (w *Window) requestDecoration() {
	if w.d.decoration == nil {
		return
	}
	decoration, err := w.d.decoration.GetToplevelDecoration(w.toplevel)
	if err != nil {
		log.Printf("wayland: requesting a window frame: %v", err)
		return
	}
	w.decoration = decoration
	if err := decoration.SetMode(uint32(xdg_decoration.ToplevelDecorationModeServerSide)); err != nil {
		log.Printf("wayland: requesting a window frame: %v", err)
	}
}

// createPopup gives an override-redirect window the one shape Wayland has for
// a menu or a tooltip.
//
// A popup must name a parent surface and is placed relative to it, whereas the
// server sends its menus as free-standing windows at absolute positions. The
// two are reconciled by anchoring to the most recent ordinary window and
// turning the absolute position into an offset from it, which lands the menu
// where the server meant it whenever the compositor has honoured the parent's
// own geometry — and near enough when it has not.
func (w *Window) createPopup() error {
	parent := w.d.lastToplevel
	if parent == nil || !parent.mapped || !parent.configured {
		// A popup with nothing to hang off is a protocol error. A plain window
		// in the wrong place beats no window at all.
		return w.createToplevel()
	}

	positioner, err := w.d.wmBase.CreatePositioner()
	if err != nil {
		return fmt.Errorf("creating a popup positioner: %w", err)
	}
	defer positioner.Destroy()

	for _, err := range []error{
		positioner.SetSize(int32(w.width), int32(w.height)),
		// A one-pixel anchor rectangle at the offset, with the popup growing
		// down and to the right from its top left corner, puts the popup's
		// corner exactly where the server asked for it.
		positioner.SetAnchorRect(int32(w.x-parent.x), int32(w.y-parent.y), 1, 1),
		positioner.SetAnchor(uint32(xdg_shell.PositionerAnchorTopLeft)),
		positioner.SetGravity(uint32(xdg_shell.PositionerGravityBottomRight)),
		// Slide a menu that would fall off the screen back on, rather than
		// letting the compositor flip it to the other side of the anchor.
		positioner.SetConstraintAdjustment(uint32(
			xdg_shell.PositionerConstraintAdjustmentSlideX |
				xdg_shell.PositionerConstraintAdjustmentSlideY)),
	} {
		if err != nil {
			return fmt.Errorf("placing a popup: %w", err)
		}
	}

	popup, err := w.xdg.GetPopup(parent.xdg, positioner)
	if err != nil {
		return fmt.Errorf("creating a popup: %w", err)
	}
	w.popup = popup
	popup.SetConfigureHandler(w.configurePopup)
	popup.SetPopupDoneHandler(func(xdg_shell.PopupPopupDoneEvent) {
		// The compositor has dismissed the popup. Nothing is closed locally:
		// the server is told, and the window goes when it says so.
		w.d.emit(ui.CloseRequest{Window: w.ID()})
	})
	return nil
}

// configure acknowledges a compositor configure. Until the first one arrives a
// surface may show nothing, so this is also where a window that the client has
// already mapped first appears.
func (w *Window) configure(e xdg_shell.SurfaceConfigureEvent) {
	if err := w.xdg.AckConfigure(e.Serial); err != nil {
		log.Printf("wayland: acknowledging a configure: %v", err)
		return
	}
	w.configured = true
	w.present()
}

// configureToplevel takes the size the compositor has chosen — the user
// dragging an edge, or a tiling layout — and tells the client, which passes it
// on to the server so the remote application reflows.
//
// The position reported alongside it is the one we already hold: the compositor
// has none to give, and the server has to keep hearing its own.
func (w *Window) configureToplevel(e xdg_shell.ToplevelConfigureEvent) {
	// Zero means the compositor is leaving the size to us, which it does for
	// the first configure of a floating window.
	if e.Width <= 0 || e.Height <= 0 {
		return
	}
	w.resize(int(e.Width), int(e.Height))
}

// configurePopup does the same for a popup, whose position the compositor also
// reports — relative to the parent, and possibly slid to keep it on screen. It
// is ignored for the same reason a toplevel's absent position is: the server's
// coordinates are the ones both ends have to agree on.
func (w *Window) configurePopup(e xdg_shell.PopupConfigureEvent) {
	if e.Width <= 0 || e.Height <= 0 {
		return
	}
	w.resize(int(e.Width), int(e.Height))
}

// resize reports a size the compositor has chosen.
//
// Nothing is applied here. The client compares what it is told against
// Geometry() to decide whether the server needs to hear about it, so recording
// the new size now would leave it with nothing to notice: it would send no
// configure-window, the server would go on believing the old size, and the
// window would sit there showing a buffer nothing ever damages. The size is
// taken in Resized instead, which is the client answering.
func (w *Window) resize(width, height int) {
	width, height = ui.ClampSize(width, height)
	if width == w.width && height == w.height {
		return
	}
	w.d.emit(ui.Configure{Window: w.ID(), X: w.x, Y: w.y, Width: width, Height: height})
}

// ID returns the surface id, which is what events for this window carry. A
// destroyed window has none, and nothing routes to it any more.
func (w *Window) ID() ui.WindowID {
	if w.surface == nil {
		return 0
	}
	return ui.WindowID(w.surface.ID())
}

// Geometry returns the window's size as the compositor last set it, at the
// position the server last asked for.
func (w *Window) Geometry() (x, y, width, height int) {
	w.d.mu.Lock()
	defer w.d.mu.Unlock()
	return w.x, w.y, w.width, w.height
}

func (w *Window) SetTitle(title string) {
	if title == "" {
		return
	}
	w.d.mu.Lock()
	defer w.d.mu.Unlock()
	if w.toplevel == nil {
		// A popup has no title: nothing shows one.
		return
	}
	if err := stringRequest(w.toplevel, opcodeToplevelSetTitle, title); err != nil {
		log.Printf("wayland: setting a window title: %v", err)
	}
}

// Map shows the window, or arranges for it to appear as soon as the compositor
// has configured it.
func (w *Window) Map() {
	w.d.mu.Lock()
	defer w.d.mu.Unlock()
	w.mapped = true
	if w.toplevel != nil {
		// Remember the newest ordinary window: the next menu the server opens
		// will be anchored to it.
		w.d.lastToplevel = w
	}
	w.present()
}

// Raise does nothing. A Wayland client cannot bring itself to the front —
// stacking belongs entirely to the compositor, and the only way to ask is
// xdg-activation, which needs a token from a real user interaction that a
// server-driven raise does not have.
func (w *Window) Raise() {}

// Minimize hides the window. There is no way back: xdg_toplevel can be
// minimized but not restored, so the compositor's own taskbar or overview is
// what brings a window back, and a restore from the server is ignored.
func (w *Window) Minimize(minimized bool) {
	if !minimized {
		return
	}
	w.d.mu.Lock()
	defer w.d.mu.Unlock()
	if w.toplevel == nil {
		return
	}
	if err := w.toplevel.SetMinimized(); err != nil {
		log.Printf("wayland: minimizing a window: %v", err)
	}
}

// Destroy releases the window, its shared memory and everything made from it.
func (w *Window) Destroy() {
	w.d.mu.Lock()
	defer w.d.mu.Unlock()

	if w.surface != nil {
		delete(w.d.windows, w.surface.ID())
	}
	if w.d.lastToplevel == w {
		w.d.lastToplevel = nil
	}
	if w.d.pointerOver == w {
		w.d.pointerOver = nil
	}
	if w.d.keyboardFocus == w {
		w.d.keyboardFocus = nil
	}
	w.destroy()
}

// destroy tears the window down in the order the protocol wants: the role
// before the surface it was given to, and the surface before the pixels it was
// showing. Each part may be unset, either because this window never had one or
// because create failed partway through. It runs under the display lock.
func (w *Window) destroy() {
	release := func(part func() error) {
		if err := part(); err != nil {
			log.Printf("wayland: destroying a window: %v", err)
		}
	}
	if w.decoration != nil {
		release(w.decoration.Destroy)
		w.decoration = nil
	}
	if w.toplevel != nil {
		release(w.toplevel.Destroy)
		w.toplevel = nil
	}
	if w.popup != nil {
		release(w.popup.Destroy)
		w.popup = nil
	}
	if w.xdg != nil {
		release(w.xdg.Destroy)
		w.xdg = nil
	}
	if w.surface != nil {
		// Not the bindings' Destroy: see destroySurface in wire.go.
		release(func() error { return destroySurface(w.surface) })
		w.surface = nil
	}
	w.releaseBuffer()
}

// MoveResize applies a server-driven geometry change. Only the size can be
// applied; the position is remembered so that everything reported back to the
// server stays in the coordinates it chose.
func (w *Window) MoveResize(x, y, width, height int) error {
	width, height = ui.ClampSize(width, height)

	w.d.mu.Lock()
	defer w.d.mu.Unlock()

	w.x, w.y = x, y
	if width == w.width && height == w.height {
		return nil
	}
	w.width, w.height = width, height
	if err := w.newBuffer(width, height); err != nil {
		return err
	}
	w.setGeometry()
	w.present()
	return nil
}

// Resized records a size change the compositor has already made — the user
// dragging an edge, or a tiling layout — and reallocates the buffer to match.
//
// The new buffer starts blank, and stays blank until the server sends damage
// for the size it has just been told about.
func (w *Window) Resized(x, y, width, height int) error {
	width, height = ui.ClampSize(width, height)

	w.d.mu.Lock()
	defer w.d.mu.Unlock()

	w.x, w.y = x, y
	if width == w.width && height == w.height {
		return nil
	}
	w.width, w.height = width, height
	if err := w.newBuffer(width, height); err != nil {
		return err
	}
	w.setGeometry()
	return nil
}

// setGeometry tells the compositor which part of the surface is the window
// proper, so that it can place its own frame around exactly that.
func (w *Window) setGeometry() {
	if w.xdg == nil {
		return
	}
	if err := w.xdg.SetWindowGeometry(0, 0, int32(w.width), int32(w.height)); err != nil {
		log.Printf("wayland: setting a window's geometry: %v", err)
	}
}

// newBuffer replaces the shared memory the window is painted from.
//
// A fresh mapping is zeroed, which in this layout is opaque black, so a window
// never shows uninitialised memory. Nothing is carried over from the old
// buffer: the server re-sends damage for the whole window after a resize.
func (w *Window) newBuffer(width, height int) error {
	stride := width * ui.BytesPerPixel
	size := stride * height

	memory, err := newSharedMemory(size)
	if err != nil {
		return err
	}
	pool, err := w.d.shm.CreatePool(int(memory.file.Fd()), int32(size))
	if err != nil {
		memory.close()
		return fmt.Errorf("sharing %d bytes with the compositor: %w", size, err)
	}
	// xrgb8888 is a little-endian 32-bit x:R:G:B, which is the byte order
	// B,G,R,X that ui.Convert produces — the same coincidence that makes an X11
	// ZPixmap and a Win32 DIB free to paint into.
	buffer, err := pool.CreateBuffer(0, int32(width), int32(height), int32(stride),
		uint32(client.ShmFormatXrgb8888))
	if err != nil {
		pool.Destroy()
		memory.close()
		return fmt.Errorf("creating a %dx%d buffer: %w", width, height, err)
	}

	w.releaseBuffer()
	w.memory, w.pool, w.buffer = memory, pool, buffer
	w.bufWidth, w.bufHeight = width, height
	return nil
}

func (w *Window) releaseBuffer() {
	if w.buffer != nil {
		w.buffer.Destroy()
		w.buffer = nil
	}
	if w.pool != nil {
		w.pool.Destroy()
		w.pool = nil
	}
	w.memory.close()
	w.memory = nil
	w.bufWidth, w.bufHeight = 0, 0
}

// Paint converts a damage rectangle into the shared buffer and tells the
// compositor which part of the window changed.
func (w *Window) Paint(x, y, width, height int, pixels []byte, rowstride int, format string) error {
	w.d.mu.Lock()
	defer w.d.mu.Unlock()

	if w.memory == nil {
		return fmt.Errorf("window has no buffer")
	}
	width, height, err := ui.ClipDamage(x, y, width, height, w.bufWidth, w.bufHeight)
	if err != nil {
		return err
	}
	if width == 0 || height == 0 {
		return nil
	}

	stride := w.bufWidth * ui.BytesPerPixel
	if err := ui.Convert(w.memory.bytes[y*stride+x*ui.BytesPerPixel:], stride,
		pixels, rowstride, width, height, format); err != nil {
		return err
	}
	w.damage(x, y, width, height)
	return nil
}

// present shows the whole window, which is what a fresh buffer or a first
// configure needs.
func (w *Window) present() {
	w.damage(0, 0, w.bufWidth, w.bufHeight)
}

// damage attaches the current buffer and commits one changed rectangle.
//
// A window the client has not mapped, or that the compositor has not yet
// configured, may not show anything; the pixels stay in the buffer and go up
// whole once both are true.
func (w *Window) damage(x, y, width, height int) {
	if w.buffer == nil || !w.mapped || !w.configured {
		return
	}
	for _, err := range []error{
		w.surface.Attach(w.buffer, 0, 0),
		w.surface.DamageBuffer(int32(x), int32(y), int32(width), int32(height)),
		w.surface.Commit(),
	} {
		if err != nil {
			log.Printf("wayland: painting a window: %v", err)
			return
		}
	}
}
