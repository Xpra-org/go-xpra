package ui

import "fmt"

// BytesPerPixel is the size of a pixel once converted. Both backends paint
// through a 32-bit little-endian B,G,R,X buffer: that is the X server's ZPixmap
// layout at depth 24 and the layout of a 32-bit BI_RGB Windows DIB, so the
// formats we ask the server for need no conversion at all on either.
const BytesPerPixel = 4

// Converter copies one row of source pixels into the B,G,R,X layout. dst is
// always BytesPerPixel bytes per pixel.
type Converter func(dst, src []byte)

// ConverterFor returns the row converter and source bytes-per-pixel for an xpra
// rgb_format name.
//
// The format names are literal memory byte orders, and their length is the
// bytes per pixel (xpra/codecs/rgb_transform.py:118). We advertise only BGRX
// and BGRA, which need no conversion; the rest are here so that a server
// ignoring that preference degrades to slow rather than to wrong.
func ConverterFor(format string) (Converter, int, error) {
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

// Convert writes height rows of width pixels, read from src in the named
// format at srcStride bytes per row, into dst at dstStride bytes per row.
//
// The two strides are unrelated: the source one is whatever the server chose,
// and the destination one is the full width of the backing store being painted
// into, so that a damage rectangle lands in the middle of a window.
func Convert(dst []byte, dstStride int, src []byte, srcStride, width, height int, format string) error {
	convert, srcBytesPerPixel, err := ConverterFor(format)
	if err != nil {
		return err
	}
	if srcStride < width*srcBytesPerPixel {
		return fmt.Errorf("rowstride %d is too small for %d pixels of %s", srcStride, width, format)
	}
	if need := srcStride * height; len(src) < need {
		return fmt.Errorf("got %d bytes of %s pixels, need %d for %dx%d at rowstride %d",
			len(src), format, need, width, height, srcStride)
	}
	if need := dstStride*(height-1) + width*BytesPerPixel; len(dst) < need {
		return fmt.Errorf("destination holds %d bytes, need %d for %dx%d", len(dst), need, width, height)
	}
	for row := range height {
		to := row * dstStride
		from := row * srcStride
		convert(dst[to:to+width*BytesPerPixel], src[from:from+width*srcBytesPerPixel])
	}
	return nil
}

// ClipDamage clips a width by height damage rectangle at x,y against a window
// of winWidth by winHeight, and reports the size that is left to paint.
//
// The server can size damage to what it believes the window to be while a
// resize is still in flight, so a rectangle overhanging the window is routine; a
// zero result means it misses the window altogether and there is nothing to do.
func ClipDamage(x, y, width, height, winWidth, winHeight int) (w, h int, err error) {
	if x < 0 || y < 0 {
		return 0, 0, fmt.Errorf("negative damage origin %d,%d", x, y)
	}
	if x >= winWidth || y >= winHeight {
		return 0, 0, nil
	}
	return max(min(width, winWidth-x), 0), max(min(height, winHeight-y), 0), nil
}

// ClampSize keeps window dimensions inside what the platforms allow: a zero
// dimension is an error for both CreateWindow calls, and the X11 wire format
// caps them at 16 bits.
func ClampSize(width, height int) (int, int) {
	return min(max(width, 1), 0xffff), min(max(height, 1), 0xffff)
}

// copy4 handles BGRX and BGRA, which already match the destination layout. The
// X byte of BGRX lands in the unused fourth byte, which an opaque window ignores.
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
