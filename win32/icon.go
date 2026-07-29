//go:build windows

package win32

import (
	"syscall"
	"unsafe"

	"github.com/Xpra-org/go-xpra/ui"
)

// createNativeIcon builds a colour HICON from a top-down 32-bit DIB.
// CreateIconIndirect copies both bitmaps, so they can be deleted immediately.
func createNativeIcon(icon *ui.Icon) (syscall.Handle, error) {
	if err := icon.Validate(); err != nil {
		return 0, err
	}
	info := bitmapV5Header{
		Size:        uint32(unsafe.Sizeof(bitmapV5Header{})),
		Width:       int32(icon.Width),
		Height:      -int32(icon.Height),
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
	copy(unsafe.Slice((*byte)(bits), len(icon.Pixels)), icon.Pixels)

	maskBitmap, err := createBitmap(int32(icon.Width), int32(icon.Height), 1, 1, nil)
	if err != nil {
		return 0, err
	}
	defer deleteObject(maskBitmap)

	return createIconIndirect(&iconInfo{
		Icon:  1,
		Mask:  maskBitmap,
		Color: colorBitmap,
	})
}
