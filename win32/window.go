//go:build windows

package win32

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/Xpra-org/go-xpra/ui"
)

// Window is one forwarded xpra window.
//
// The pixel store is an ordinary Go buffer rather than a GDI bitmap: it costs
// no handles, needs no thread affinity, and a damage rectangle is a copy into
// the middle of it. The window thread then hands it to the desktop as a
// device-independent bitmap when Windows asks for a repaint, which it does for
// every region it wants back — so the buffer is both what we paint into and
// what the window is restored from.
type Window struct {
	d    *Display
	hwnd syscall.Handle

	// mu guards the pixels against the window thread blitting them while the
	// client goroutine is painting into them. The dimensions travel with the
	// buffer, since the client's idea of the window size changes a moment
	// before the buffer does.
	mu                  sync.Mutex
	pixels              []byte
	bufWidth, bufHeight int

	x, y, width, height int
	overrideRedirect    bool
}

var _ ui.Window = (*Window)(nil)

// NewWindow creates an unmapped top-level window whose content area is the
// given geometry.
func (d *Display) NewWindow(x, y, width, height int, overrideRedirect bool) (ui.Window, error) {
	width, height = ui.ClampSize(width, height)
	w := &Window{
		d: d,
		x: x, y: y, width: width, height: height,
		overrideRedirect: overrideRedirect,
	}
	w.newBuffer(width, height)

	var err error
	if callErr := d.call(func() { err = w.create() }); callErr != nil {
		return nil, callErr
	}
	if err != nil {
		return nil, err
	}
	return w, nil
}

// create runs on the window thread, which is where a window has to be made if
// that thread is to receive its messages.
func (w *Window) create() error {
	style, exStyle := styles(w.overrideRedirect)
	outer, err := frame(w.x, w.y, w.width, w.height, style, exStyle)
	if err != nil {
		return err
	}
	hwnd, err := createWindowEx(exStyle, w.d.class, nil, style,
		outer.Left, outer.Top, outer.Right-outer.Left, outer.Bottom-outer.Top,
		0, w.d.instance)
	if err != nil {
		return err
	}
	w.hwnd = hwnd
	w.d.windows[hwnd] = w
	return nil
}

// styles picks the window's decoration.
func styles(overrideRedirect bool) (style, exStyle uint32) {
	if overrideRedirect {
		// A menu or a tooltip: no frame to displace it, no taskbar button, and
		// no focus taken from the window it belongs to. That is as close as
		// Windows gets to what override-redirect means on X11.
		return wsPopup, wsExNoActivate | wsExToolWindow | wsExTopMost
	}
	return wsOverlappedWindow, 0
}

// frame grows a content rectangle into the outer rectangle Windows positions a
// window by, which for a decorated window includes its border and title bar.
func frame(x, y, width, height int, style, exStyle uint32) (rect, error) {
	r := rect{
		Left: int32(x), Top: int32(y),
		Right: int32(x + width), Bottom: int32(y + height),
	}
	if err := adjustWindowRect(&r, style, exStyle); err != nil {
		return rect{}, err
	}
	return r, nil
}

// ID returns the window handle, which is what events for this window carry.
func (w *Window) ID() ui.WindowID { return ui.WindowID(w.hwnd) }

// Geometry returns the window's position and size as we last knew it.
func (w *Window) Geometry() (x, y, width, height int) {
	return w.x, w.y, w.width, w.height
}

func (w *Window) SetTitle(title string) {
	if title == "" {
		return
	}
	text, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		// The title came from the server and can hold anything, including the
		// NUL that has no place in a C string. A window keeps its old title
		// rather than the client giving up over it.
		return
	}
	w.d.post(func() { setWindowText(w.hwnd, text) })
}

// Map shows the window.
func (w *Window) Map() {
	command := int32(swShow)
	if w.overrideRedirect {
		command = swShowNoActivate
	}
	w.d.post(func() { showWindow(w.hwnd, command) })
}

// Raise brings the window to the top of the normal z-order. Omitting
// SWP_NOACTIVATE matches Xpra's "present" semantics as closely as Win32
// permits; Windows may still reject foreground activation for another process.
func (w *Window) Raise() {
	w.d.post(func() {
		setWindowPos(w.hwnd, hwndTop, 0, 0, 0, 0, swpNoMove|swpNoSize)
	})
}

// Destroy releases the window. Its pixels go with it, being ordinary memory.
func (w *Window) Destroy() {
	w.d.post(func() { destroyWindow(w.hwnd) })
}

// MoveResize applies a server-driven geometry change.
func (w *Window) MoveResize(x, y, width, height int) error {
	width, height = ui.ClampSize(width, height)
	resized := width != w.width || height != w.height

	w.x, w.y = x, y
	if resized {
		w.width, w.height = width, height
		w.newBuffer(width, height)
	}
	style, exStyle := styles(w.overrideRedirect)
	outer, err := frame(x, y, w.width, w.height, style, exStyle)
	if err != nil {
		return err
	}
	w.d.post(func() {
		setWindowPos(w.hwnd, 0, outer.Left, outer.Top,
			outer.Right-outer.Left, outer.Bottom-outer.Top, swpNoZOrder|swpNoActivate)
	})
	return nil
}

// Resized records a size change Windows has already made — the user dragging an
// edge — and reallocates the buffer to match.
func (w *Window) Resized(x, y, width, height int) error {
	width, height = ui.ClampSize(width, height)
	w.x, w.y = x, y
	if width == w.width && height == w.height {
		return nil
	}
	w.width, w.height = width, height
	w.newBuffer(width, height)
	return nil
}

// newBuffer replaces the pixel buffer. A fresh one is zeroed, which in this
// layout is opaque black, so a window never shows uninitialised memory.
func (w *Window) newBuffer(width, height int) {
	pixels := make([]byte, width*height*ui.BytesPerPixel)

	w.mu.Lock()
	defer w.mu.Unlock()
	w.pixels = pixels
	w.bufWidth, w.bufHeight = width, height
}

// Paint converts a damage rectangle into the window's buffer and asks Windows
// to show it.
func (w *Window) Paint(x, y, width, height int, pixels []byte, rowstride int, format string) error {
	w.mu.Lock()
	width, height, err := ui.ClipDamage(x, y, width, height, w.bufWidth, w.bufHeight)
	if err == nil && width > 0 && height > 0 {
		stride := w.bufWidth * ui.BytesPerPixel
		err = ui.Convert(w.pixels[y*stride+x*ui.BytesPerPixel:], stride,
			pixels, rowstride, width, height, format)
	}
	w.mu.Unlock()

	if err != nil {
		return err
	}
	if width <= 0 || height <= 0 {
		return nil
	}
	// Posting rather than invalidating from here keeps every window call on the
	// window thread; the repaint itself happens when the desktop gets round to
	// the WM_PAINT this produces.
	damaged := rect{
		Left: int32(x), Top: int32(y),
		Right: int32(x + width), Bottom: int32(y + height),
	}
	w.d.post(func() { invalidateRect(w.hwnd, &damaged) })
	return nil
}

// print draws the whole window into a device context of someone else's
// choosing, which is what WM_PRINTCLIENT asks for.
func (w *Window) print(hdc syscall.Handle) {
	w.mu.Lock()
	whole := rect{Right: int32(w.bufWidth), Bottom: int32(w.bufHeight)}
	w.mu.Unlock()
	w.blit(hdc, whole)
}

// blit copies the requested part of the buffer onto the window, and runs on the
// window thread inside WM_PAINT.
func (w *Window) blit(hdc syscall.Handle, r rect) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// The update rectangle is whatever Windows wants back, which after a resize
	// can reach past the buffer we still hold.
	left, top := max(int(r.Left), 0), max(int(r.Top), 0)
	right, bottom := min(int(r.Right), w.bufWidth), min(int(r.Bottom), w.bufHeight)
	if right <= left || bottom <= top {
		return
	}
	rows := bottom - top
	stride := w.bufWidth * ui.BytesPerPixel

	info := bitmapInfoHeader{
		Width: int32(w.bufWidth),
		// A negative height means the rows are stored top down, the way both
		// the server and this buffer have them.
		Height:      int32(-rows),
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}
	info.Size = uint32(unsafe.Sizeof(info))

	// The bitmap handed over starts at the first row of the update rectangle
	// and is exactly as tall as it, so every row of it is copied and there is
	// no question of which end of the buffer the source rows are counted from.
	setDIBitsToDevice(hdc, int32(left), int32(top), int32(right-left), int32(rows),
		int32(left), 0, 0, uint32(rows), &w.pixels[top*stride], &info)
}
