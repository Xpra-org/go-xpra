//go:build darwin

package darwin

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"

	"github.com/Xpra-org/go-xpra/ui"
)

// AppKit window style masks and levels this backend uses.
const (
	nsWindowStyleMaskBorderless         = 0
	nsWindowStyleMaskTitled             = 1 << 0
	nsWindowStyleMaskClosable           = 1 << 1
	nsWindowStyleMaskMiniaturizable     = 1 << 2
	nsWindowStyleMaskResizable          = 1 << 3
	nsWindowStyleMaskNonactivatingPanel = 1 << 7
	nsBackingStoreBuffered              = 2
	nsPopUpMenuWindowLevel              = 101
)

// Window is one forwarded xpra window.
//
// Like win32's Window, the pixel store is an ordinary Go buffer rather than a
// native surface: it costs no native handles, needs no thread affinity to
// hold, and a damage rectangle is a copy into the middle of it. Unlike
// win32's SetDIBitsToDevice, which the desktop pulls from on WM_PAINT, this
// backend pushes the whole buffer to the desktop on every Paint (see blit),
// wrapped as a CGImage and handed to the view's backing CALayer — the
// trade-off is a full re-upload per damage rectangle rather than a true
// partial blit, accepted for the lower purego call-shape risk it buys (see
// darwin/doc.go).
type Window struct {
	d      *Display
	window objc.ID // NSWindow or NSPanel
	view   objc.ID // the content view, of the registered view class

	// mu guards the pixels against blit() reading them while the client
	// goroutine is painting into them. The dimensions travel with the buffer,
	// since the client's idea of the window size changes a moment before the
	// buffer does.
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

// create runs on the main thread, which is where AppKit requires a window and
// its view to be made.
func (w *Window) create() error {
	d := w.d
	content := topLeftToCocoa(w.x, w.y, w.width, w.height, d.referenceHeight)

	// A server commonly uses 0,0 as a new window's initial position. For a
	// decorated window AppKit adds the title bar above the content rect,
	// which would push it off the top of the screen when the content is
	// already flush against it; nudge the content down by the estimated
	// decoration height first, the same problem win32's placeInitialFrame
	// solves for the opposite (top-left growing) direction.
	style := uint32(nsWindowStyleMaskTitled | nsWindowStyleMaskClosable |
		nsWindowStyleMaskMiniaturizable | nsWindowStyleMaskResizable)
	if !w.overrideRedirect && w.x == 0 && w.y == 0 {
		estimate := objc.Send[nsRect](objc.ID(objc.GetClass("NSWindow")),
			sel_frameRectForContentRectStyleMask, content, style)
		if decoration := estimate.Size.Height - content.Size.Height; decoration > 0 {
			content.Origin.Y -= decoration
		}
	}

	view := objc.ID(viewClass).Send(sel_alloc)
	view = view.Send(sel_initWithFrame, makeRect(0, 0, content.Size.Width, content.Size.Height))
	view.Send(sel_setWantsLayer, true)

	var win objc.ID
	if w.overrideRedirect {
		// The closest AppKit equivalent of X11/Win32's override-redirect: no
		// frame, no activation, floats above ordinary windows — the same role
		// WS_EX_NOACTIVATE|WS_EX_TOOLWINDOW|WS_EX_TOPMOST plays for win32.
		win = objc.ID(objc.GetClass("NSPanel")).Send(sel_alloc)
		win = win.Send(sel_initWithContentRectStyleMaskBackingDefer, content,
			uint32(nsWindowStyleMaskBorderless|nsWindowStyleMaskNonactivatingPanel), nsBackingStoreBuffered, false)
		win.Send(sel_setBecomesKeyOnlyIfNeeded, true)
		win.Send(sel_setLevel, nsPopUpMenuWindowLevel)
		win.Send(sel_setHidesOnDeactivate, false)
	} else {
		win = objc.ID(objc.GetClass("NSWindow")).Send(sel_alloc)
		win = win.Send(sel_initWithContentRectStyleMaskBackingDefer, content, style, nsBackingStoreBuffered, false)
	}
	if win == 0 {
		return fmt.Errorf("darwin: creating the window failed")
	}
	win.Send(sel_setContentView, view)
	win.Send(sel_setDelegate, d.coordinator)
	win.Send(sel_setAcceptsMouseMovedEvents, true)
	// This process owns the window's lifetime through d.windows, not AppKit's
	// own close-releases-it default.
	win.Send(sel_setReleasedWhenClosed, false)

	w.window = win
	w.view = view
	d.windows[win] = w
	return nil
}

// ID returns the window's id, which is what events for this window carry.
func (w *Window) ID() ui.WindowID { return ui.WindowID(w.window) }

// Geometry returns the window's position and size as we last knew it.
func (w *Window) Geometry() (x, y, width, height int) {
	return w.x, w.y, w.width, w.height
}

func (w *Window) SetTitle(title string) {
	if title == "" {
		return
	}
	w.d.post(func() { w.window.Send(sel_setTitle, nsString(title)) })
}

// SetIcon sets the process-wide Dock icon. AppKit has no per-window
// title-bar or task-switcher icon the way win32 does — a forwarded window's
// icon is the closest approximation available, applied to whichever window
// set one most recently.
func (w *Window) SetIcon(icon *ui.Icon) error {
	if icon == nil {
		return w.d.call(func() { nsApp.Send(sel_setApplicationIconImage, objc.ID(0)) })
	}
	if err := icon.Validate(); err != nil {
		return err
	}
	image, err := cgImageFromBGRA(icon.Pixels, icon.Width, icon.Height)
	if err != nil {
		return err
	}
	defer cgImageRelease(image)

	var setErr error
	if err := w.d.call(func() {
		nsImage := objc.ID(objc.GetClass("NSImage")).Send(sel_alloc)
		nsImage = nsImage.Send(sel_initWithCGImageSize, objc.ID(image),
			nsSize{Width: float64(icon.Width), Height: float64(icon.Height)})
		if nsImage == 0 {
			setErr = fmt.Errorf("darwin: building the icon image failed")
			return
		}
		nsApp.Send(sel_setApplicationIconImage, nsImage)
		nsImage.Send(sel_release)
	}); err != nil {
		return err
	}
	return setErr
}

// Map shows the window.
func (w *Window) Map() {
	w.d.post(func() {
		if w.overrideRedirect {
			// No activation, matching an override-redirect window taking no
			// focus from whatever the server placed it over.
			w.window.Send(sel_orderFrontRegardless)
		} else {
			w.window.Send(sel_makeKeyAndOrderFront, objc.ID(0))
		}
	})
}

// Raise brings the window to the front of the local desktop.
func (w *Window) Raise() {
	w.d.post(func() { w.window.Send(sel_orderFrontRegardless) })
}

// Minimize changes the native minimized state without affecting geometry.
func (w *Window) Minimize(minimized bool) {
	w.d.post(func() {
		if minimized {
			w.window.Send(sel_miniaturize, objc.ID(0))
		} else {
			w.window.Send(sel_deminiaturize, objc.ID(0))
		}
	})
}

// Destroy releases the window. Its pixels go with it, being ordinary memory.
func (w *Window) Destroy() {
	w.d.post(func() {
		delete(w.d.windows, w.window)
		w.window.Send(sel_close)
	})
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
	targetWidth, targetHeight, overrideRedirect := w.width, w.height, w.overrideRedirect
	d := w.d
	w.d.post(func() {
		content := topLeftToCocoa(x, y, targetWidth, targetHeight, d.referenceHeight)
		frame := content
		if !overrideRedirect {
			frame = objc.Send[nsRect](w.window, sel_frameRectForContentRect, content)
		}
		w.window.Send(sel_setFrameDisplay, frame, true)
	})
	return nil
}

// Resized records a size change AppKit has already made — the user dragging
// an edge — and reallocates the buffer to match.
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

// Paint converts a damage rectangle into the window's buffer and pushes the
// whole buffer to the desktop.
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
	w.d.post(func() { w.blit() })
	return nil
}

// blit uploads the whole pixel buffer as the view's backing layer contents.
// It runs on the main thread.
func (w *Window) blit() {
	w.mu.Lock()
	width, height := w.bufWidth, w.bufHeight
	pixels := w.pixels
	w.mu.Unlock()
	if width <= 0 || height <= 0 {
		return
	}

	image, err := cgImageFromBGRA(pixels, width, height)
	if err != nil {
		return
	}
	defer cgImageRelease(image)

	layer := w.view.Send(sel_layer)
	if layer != 0 {
		layer.Send(sel_setContents, objc.ID(image))
	}
}

// bgraBitmapInfo describes the tightly packed, alpha-premultiplied BGRA
// layout ui.Cursor, ui.Icon and this backend's own pixel buffers all share:
// alpha first with 32-bit little-endian byte order together mean the byte
// sequence in memory is B,G,R,A, matching ui.BytesPerPixel's documented
// layout exactly.
const bgraBitmapInfo = 2 /* kCGImageAlphaPremultipliedFirst */ | (2 << 12) /* kCGBitmapByteOrder32Little */

// cgDataReleaseCallback frees a CGImage's backing buffer once CoreGraphics is
// done with it. It is created once at package initialization — like
// runMainTrampoline in run.go, purego.NewCallback's function pointers are
// never deallocated, so a fresh one per image would leak without bound.
var cgDataReleaseCallback = purego.NewCallback(func(_ unsafe.Pointer, data unsafe.Pointer, _ uintptr) {
	cFree(data)
})

// cgImageFromBGRA builds a CGImage from tightly packed, alpha-premultiplied
// BGRA pixels. The caller owns the returned reference and must eventually
// call cgImageRelease.
//
// CGImage retains its data provider for as long as the image lives, unlike
// win32's synchronous SetDIBitsToDevice, which never keeps the pointer past
// one call — so the pixels can't point directly at Go-GC'd memory. They are
// copied into a malloc'd C allocation instead, freed by cgDataReleaseCallback
// once CoreGraphics releases the data provider holding it.
func cgImageFromBGRA(pixels []byte, width, height int) (uintptr, error) {
	if width <= 0 || height <= 0 {
		return 0, fmt.Errorf("darwin: invalid image size %dx%d", width, height)
	}
	size := uintptr(len(pixels))
	buf := cMalloc(size)
	if buf == nil {
		return 0, fmt.Errorf("darwin: out of memory allocating %d bytes", size)
	}
	copy(unsafe.Slice((*byte)(buf), size), pixels)

	provider := cgDataProviderCreateWithData(nil, buf, size, cgDataReleaseCallback)
	if provider == 0 {
		cFree(buf)
		return 0, fmt.Errorf("darwin: CGDataProviderCreateWithData failed")
	}
	colorSpace := cgColorSpaceCreateDeviceRGB()
	bytesPerRow := uintptr(width * ui.BytesPerPixel)
	image := cgImageCreate(uintptr(width), uintptr(height), 8, 32, bytesPerRow,
		colorSpace, bgraBitmapInfo, provider, 0, false, 0)
	cgColorSpaceRelease(colorSpace)
	cgDataProviderRelease(provider)
	if image == 0 {
		return 0, fmt.Errorf("darwin: CGImageCreate failed")
	}
	return image, nil
}

// Selectors used only in this file.
var (
	sel_initWithFrame                            = objc.RegisterName("initWithFrame:")
	sel_setWantsLayer                            = objc.RegisterName("setWantsLayer:")
	sel_initWithContentRectStyleMaskBackingDefer = objc.RegisterName(
		"initWithContentRect:styleMask:backing:defer:")
	sel_frameRectForContentRect          = objc.RegisterName("frameRectForContentRect:")
	sel_frameRectForContentRectStyleMask = objc.RegisterName("frameRectForContentRect:styleMask:")
	sel_setBecomesKeyOnlyIfNeeded        = objc.RegisterName("setBecomesKeyOnlyIfNeeded:")
	sel_setLevel                         = objc.RegisterName("setLevel:")
	sel_setHidesOnDeactivate             = objc.RegisterName("setHidesOnDeactivate:")
	sel_setContentView                   = objc.RegisterName("setContentView:")
	sel_setDelegate                      = objc.RegisterName("setDelegate:")
	sel_setAcceptsMouseMovedEvents       = objc.RegisterName("setAcceptsMouseMovedEvents:")
	sel_setReleasedWhenClosed            = objc.RegisterName("setReleasedWhenClosed:")
	sel_setTitle                         = objc.RegisterName("setTitle:")
	sel_setApplicationIconImage          = objc.RegisterName("setApplicationIconImage:")
	sel_initWithCGImageSize              = objc.RegisterName("initWithCGImage:size:")
	sel_orderFrontRegardless             = objc.RegisterName("orderFrontRegardless")
	sel_makeKeyAndOrderFront             = objc.RegisterName("makeKeyAndOrderFront:")
	sel_miniaturize                      = objc.RegisterName("miniaturize:")
	sel_deminiaturize                    = objc.RegisterName("deminiaturize:")
	sel_layer                            = objc.RegisterName("layer")
	sel_setContents                      = objc.RegisterName("setContents:")
)
