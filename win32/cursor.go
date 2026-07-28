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
func createNativeCursor(cursor *ui.Cursor) (syscall.Handle, error) {
	if err := cursor.Validate(); err != nil {
		return 0, err
	}
	info := bitmapV5Header{
		Size:        uint32(unsafe.Sizeof(bitmapV5Header{})),
		Width:       int32(cursor.Width),
		Height:      -int32(cursor.Height),
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
	copy(unsafe.Slice((*byte)(bits), len(cursor.Pixels)), cursor.Pixels)

	maskBitmap, err := createBitmap(int32(cursor.Width), int32(cursor.Height), 1, 1, nil)
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
