//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego/objc"

	"github.com/Xpra-org/go-xpra/ui"
)

var (
	class_NSImage  = objc.GetClass("NSImage")
	class_NSCursor = objc.GetClass("NSCursor")

	sel_initWithImageHotSpot = objc.RegisterName("initWithImage:hotSpot:")
)

// SetCursor applies one session-wide pointer image to every forwarded
// window, including windows created later — the same contract win32's
// SetCursor keeps, achieved here through AppKit's cursor-rect mechanism (see
// resetCursorRects in display.go) rather than a per-move WM_SETCURSOR call.
func (d *Display) SetCursor(image *ui.Cursor) error {
	var cursorErr error
	if err := d.call(func() {
		var next objc.ID
		if image != nil {
			next, cursorErr = d.makeCursor(image)
			if cursorErr != nil {
				return
			}
		}
		old := d.cursor
		d.cursor = next
		// Force every window to re-evaluate its cursor rects, which is what
		// applies the change immediately to a window the pointer is already
		// over — AppKit's documented way to update a cursor dynamically.
		for _, w := range d.windows {
			w.window.Send(sel_invalidateCursorRectsForView, w.view)
		}
		if old != 0 {
			old.Send(sel_release)
		}
	}); err != nil {
		return err
	}
	return cursorErr
}

// makeCursor builds an NSCursor from a top-down, alpha-premultiplied BGRA
// image, reusing the same CGImage construction Window.blit paints with.
func (d *Display) makeCursor(cursor *ui.Cursor) (objc.ID, error) {
	if err := cursor.Validate(); err != nil {
		return 0, err
	}
	cgImage, err := cgImageFromBGRA(cursor.Pixels, cursor.Width, cursor.Height)
	if err != nil {
		return 0, err
	}
	defer cgImageRelease(cgImage)

	nsImage := objc.ID(class_NSImage).Send(sel_alloc)
	nsImage = nsImage.Send(sel_initWithCGImageSize, objc.ID(cgImage),
		nsSize{Width: float64(cursor.Width), Height: float64(cursor.Height)})
	defer nsImage.Send(sel_release)

	// NSCursor's hotSpot is in the image's own coordinate space, which for a
	// plain NSImage is bottom-left/Y-up — the opposite of ui.Cursor's
	// top-left/Y-down HotspotX/Y. This is the one Y flip that belongs to the
	// cursor's own image space rather than to coords.go, which is about
	// screen and window space.
	hotspot := nsPoint{X: float64(cursor.HotspotX), Y: float64(cursor.Height - cursor.HotspotY)}

	native := objc.ID(class_NSCursor).Send(sel_alloc)
	native = native.Send(sel_initWithImageHotSpot, nsImage, hotspot)
	return native, nil
}
