package x11

import (
	"fmt"

	"github.com/jezek/xgb/xproto"
	"github.com/jezek/xgbutil/ewmh"
	"github.com/jezek/xgbutil/icccm"
	"github.com/jezek/xgbutil/xwindow"
)

// eventMask is what we ask the X server to report for each forwarded window.
//
// Exposure is deliberately absent: each window's pixmap is installed as its
// background, so the X server repaints damaged regions from it without waking
// us up.
const eventMask = xproto.EventMaskStructureNotify |
	xproto.EventMaskButtonPress |
	xproto.EventMaskButtonRelease |
	xproto.EventMaskPointerMotion |
	xproto.EventMaskKeyPress |
	xproto.EventMaskKeyRelease |
	xproto.EventMaskFocusChange

// bytesPerPixel is the size of a pixel in the X server's 24-depth ZPixmap
// format, which pads each pixel to 32 bits.
const bytesPerPixel = 4

// Window is one forwarded xpra window.
//
// The pixel store is an X pixmap rather than a buffer in this process: it is
// installed as the window's background, so the X server can satisfy exposures
// from it on its own, and painting a damage rectangle is a single PutImage
// followed by a ClearArea over the same region. Nothing needs to be kept
// client-side between draws.
type Window struct {
	d      *Display
	win    *xwindow.Window
	pixmap xproto.Pixmap

	// scratch holds one converted damage rectangle. PutImage needs tightly
	// packed rows, whereas the server sends rows padded to its own rowstride,
	// so a copy is unavoidable; reusing the buffer keeps it off the hot path.
	scratch []byte

	x, y, width, height int
	overrideRedirect    bool
}

// NewWindow creates an unmapped top-level window at the given geometry.
//
// Override-redirect windows (menus, tooltips) bypass the local window manager
// entirely, which is what lets them land exactly where the server put them.
func (d *Display) NewWindow(x, y, width, height int, overrideRedirect bool) (*Window, error) {
	width, height = clampSize(width, height)

	win, err := xwindow.Generate(d.X)
	if err != nil {
		return nil, fmt.Errorf("allocating a window id: %w", err)
	}
	// The value list must be ordered by the CW constants: OverrideRedirect
	// (0x200) comes before EventMask (0x800).
	mask := xproto.CwOverrideRedirect | xproto.CwEventMask
	or := uint32(0)
	if overrideRedirect {
		or = 1
	}
	if err := win.CreateChecked(d.X.RootWin(), x, y, width, height, mask, or, eventMask); err != nil {
		return nil, fmt.Errorf("creating a window: %w", err)
	}

	w := &Window{
		d: d, win: win,
		x: x, y: y, width: width, height: height,
		overrideRedirect: overrideRedirect,
	}
	if err := w.newPixmap(); err != nil {
		win.Destroy()
		return nil, err
	}
	if !overrideRedirect {
		// Let the window manager's close button reach us instead of having it
		// kill the connection.
		if err := icccm.WmProtocolsSet(d.X, win.Id, []string{"WM_DELETE_WINDOW"}); err != nil {
			return nil, fmt.Errorf("setting WM_PROTOCOLS: %w", err)
		}
	}
	return w, nil
}

// ID returns the X window id, used to route incoming events back to this window.
func (w *Window) ID() xproto.Window { return w.win.Id }

// Geometry returns the window's position and size as we last knew it.
func (w *Window) Geometry() (x, y, width, height int) {
	return w.x, w.y, w.width, w.height
}

// SetTitle sets both the modern and legacy window title properties.
func (w *Window) SetTitle(title string) {
	if title == "" {
		return
	}
	ewmh.WmNameSet(w.d.X, w.win.Id, title)
	icccm.WmNameSet(w.d.X, w.win.Id, title)
}

// Map shows the window.
func (w *Window) Map() { w.win.Map() }

// Destroy releases the window and its pixmap.
func (w *Window) Destroy() {
	w.freePixmap()
	w.win.Destroy()
}

// MoveResize applies a server-driven geometry change, reallocating the pixmap
// if the size changed.
func (w *Window) MoveResize(x, y, width, height int) error {
	width, height = clampSize(width, height)
	resized := width != w.width || height != w.height

	w.x, w.y = x, y
	if resized {
		w.width, w.height = width, height
	}
	w.win.MoveResize(x, y, w.width, w.height)
	if resized {
		return w.newPixmap()
	}
	return nil
}

// Resized records a size change the X server has already applied — for instance
// because the local window manager resized us — and reallocates the pixmap.
func (w *Window) Resized(x, y, width, height int) error {
	width, height = clampSize(width, height)
	w.x, w.y = x, y
	if width == w.width && height == w.height {
		return nil
	}
	w.width, w.height = width, height
	return w.newPixmap()
}

// newPixmap allocates a backing pixmap for the current size and installs it as
// the window's background.
//
// The new pixmap starts blank; we rely on the server re-sending damage for the
// window after a resize rather than trying to preserve the old contents.
func (w *Window) newPixmap() error {
	w.freePixmap()

	pixmap, err := xproto.NewPixmapId(w.d.X.Conn())
	if err != nil {
		return fmt.Errorf("allocating a pixmap id: %w", err)
	}
	if err := xproto.CreatePixmapChecked(w.d.X.Conn(), w.d.Depth(), pixmap,
		xproto.Drawable(w.d.X.RootWin()), uint16(w.width), uint16(w.height)).Check(); err != nil {
		return fmt.Errorf("creating a %dx%d pixmap: %w", w.width, w.height, err)
	}
	w.pixmap = pixmap

	// A fresh pixmap holds whatever was in that video memory; paint it black
	// so the window does not flash someone else's pixels before the first draw.
	xproto.PolyFillRectangle(w.d.X.Conn(), xproto.Drawable(pixmap), w.d.blackGC,
		[]xproto.Rectangle{{X: 0, Y: 0, Width: uint16(w.width), Height: uint16(w.height)}})

	xproto.ChangeWindowAttributes(w.d.X.Conn(), w.win.Id,
		xproto.CwBackPixmap, []uint32{uint32(pixmap)})
	return nil
}

func (w *Window) freePixmap() {
	if w.pixmap != 0 {
		xproto.FreePixmap(w.d.X.Conn(), w.pixmap)
		w.pixmap = 0
	}
}

// Paint uploads a decoded RGB rectangle to the window.
//
// rowstride is the number of bytes between the start of consecutive rows in the
// source, which the server computes as roundup(width*bytesPerPixel, 4) and may
// make larger still — so it is never safe to assume the payload is tightly
// packed.
func (w *Window) Paint(x, y, width, height int, pixels []byte, rowstride int, format string) error {
	if w.pixmap == 0 {
		return fmt.Errorf("window has no pixmap")
	}
	conv, srcBytesPerPixel, err := converterFor(format)
	if err != nil {
		return err
	}
	if rowstride < width*srcBytesPerPixel {
		return fmt.Errorf("rowstride %d is too small for %d pixels of %s", rowstride, width, format)
	}
	if need := rowstride * height; len(pixels) < need {
		return fmt.Errorf("got %d bytes of %s pixels, need %d for %dx%d at rowstride %d",
			len(pixels), format, need, width, height, rowstride)
	}

	// Clip against the window: the server can send damage sized to what it
	// believes the window to be while a resize is still in flight.
	if x < 0 || y < 0 {
		return fmt.Errorf("negative damage origin %d,%d", x, y)
	}
	if x >= w.width || y >= w.height {
		return nil
	}
	width = min(width, w.width-x)
	height = min(height, w.height-y)
	if width <= 0 || height <= 0 {
		return nil
	}

	dstStride := width * bytesPerPixel
	if cap(w.scratch) < dstStride*height {
		w.scratch = make([]byte, dstStride*height)
	}
	dst := w.scratch[:dstStride*height]
	for row := 0; row < height; row++ {
		conv(dst[row*dstStride:(row+1)*dstStride],
			pixels[row*rowstride:row*rowstride+width*srcBytesPerPixel])
	}

	w.putImage(x, y, width, height, dst)
	// Repainting the window from its background pixmap is what makes the new
	// pixels visible.
	xproto.ClearArea(w.d.X.Conn(), false, w.win.Id,
		int16(x), int16(y), uint16(width), uint16(height))
	return nil
}

// putImage uploads tightly packed pixels to the backing pixmap, splitting the
// transfer into as many requests as the X server's maximum request size needs.
func (w *Window) putImage(x, y, width, height int, data []byte) {
	stride := width * bytesPerPixel
	rowsPerRequest := max(1, w.d.maxImageBytes/stride)

	for row := 0; row < height; row += rowsPerRequest {
		rows := min(rowsPerRequest, height-row)
		xproto.PutImage(w.d.X.Conn(), xproto.ImageFormatZPixmap,
			xproto.Drawable(w.pixmap), w.d.blackGC,
			uint16(width), uint16(rows), int16(x), int16(y+row),
			0, w.d.Depth(), data[row*stride:(row+rows)*stride])
	}
}

// converter copies one row of source pixels into the X server's B,G,R,X layout.
// dst is always 4 bytes per pixel.
type converter func(dst, src []byte)

// converterFor returns the row converter and source bytes-per-pixel for an xpra
// rgb_format name.
//
// The format names are literal memory byte orders, and their length is the
// bytes per pixel (xpra/codecs/rgb_transform.py:118). We advertise only BGRX
// and BGRA, which need no conversion at all on a little-endian X server; the
// rest are here so that a server ignoring that preference degrades to slow
// rather than to wrong.
func converterFor(format string) (converter, int, error) {
	switch format {
	case "BGRX", "BGRA":
		return copy4, 4, nil
	case "RGBX", "RGBA":
		return swapRB4, 4, nil
	case "BGR":
		return expandBGR, 3, nil
	case "RGB":
		return expandRGB, 3, nil
	}
	return nil, 0, fmt.Errorf("unsupported pixel format %q", format)
}

// copy4 handles BGRX and BGRA, which already match the X server's layout. The
// X byte of BGRX lands in the unused fourth byte, which a depth-24 window ignores.
func copy4(dst, src []byte) { copy(dst, src) }

func swapRB4(dst, src []byte) {
	for i := 0; i+3 < len(src); i += 4 {
		dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], src[i+3]
	}
}

func expandBGR(dst, src []byte) {
	for i, j := 0, 0; i+2 < len(src); i, j = i+3, j+4 {
		dst[j], dst[j+1], dst[j+2], dst[j+3] = src[i], src[i+1], src[i+2], 0xff
	}
}

func expandRGB(dst, src []byte) {
	for i, j := 0, 0; i+2 < len(src); i, j = i+3, j+4 {
		dst[j], dst[j+1], dst[j+2], dst[j+3] = src[i+2], src[i+1], src[i], 0xff
	}
}

// clampSize keeps window dimensions inside what the X protocol allows: zero is
// a CreateWindow error, and the wire format caps them at 16 bits.
func clampSize(width, height int) (int, int) {
	return min(max(width, 1), 0xffff), min(max(height, 1), 0xffff)
}
