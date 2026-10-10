//go:build windows

package win32

import (
	"syscall"
	"unsafe"

	"github.com/Xpra-org/go-xpra/ui"
)

// createNativeCursor builds a colour HCURSOR from a top-down 32-bit DIB.
// CreateIconIndirect copies both bitmaps, so they can be deleted immediately
// after it returns.
//
// Pixels of the previous cursor can remain visible outside of a smaller one
// (seen with VirtualBox), so the cursor is pasted in the top-left corner of a
// transparent image at least as big as the system cursors, which keeps the
// hotspot where it is.
func createNativeCursor(cursor *ui.Cursor) (syscall.Handle, error) {
	if err := cursor.Validate(); err != nil {
		return 0, err
	}
	width := max(cursor.Width, systemMetrics(smCxCursor))
	height := max(cursor.Height, systemMetrics(smCyCursor))
	info := bitmapV5Header{
		Size:        uint32(unsafe.Sizeof(bitmapV5Header{})),
		Width:       int32(width),
		Height:      -int32(height),
		Planes:      1,
		BitCount:    32,
		Compression: biBitfields,
		RedMask:     0x00ff0000,
		GreenMask:   0x0000ff00,
		BlueMask:    0x000000ff,
		AlphaMask:   0xff000000,
	}
	var bits unsafe.Pointer
	colorBitmap, err := createDIBSection(unsafe.Pointer(&info), &bits)
	if err != nil {
		return 0, err
	}
	defer deleteObject(colorBitmap)
	padded := unsafe.Slice((*byte)(bits), width*height*ui.BytesPerPixel)
	clear(padded)
	stride := cursor.Width * ui.BytesPerPixel
	for y := range cursor.Height {
		copy(padded[y*width*ui.BytesPerPixel:], cursor.Pixels[y*stride:(y+1)*stride])
	}

	maskBitmap, err := createClearMask(width, height)
	if err != nil {
		return 0, err
	}
	defer deleteObject(maskBitmap)

	native, err := createIconIndirect(&iconInfo{
		HotspotX: uint32(cursor.HotspotX),
		HotspotY: uint32(cursor.HotspotY),
		Mask:     maskBitmap,
		Color:    colorBitmap,
	})
	return native, err
}
