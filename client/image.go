package client

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/Xpra-org/go-xpra/ui"
	"golang.org/x/image/webp"
)

// maxImageDimension matches xpra's own limit for dimensions read from an
// untrusted image stream (xpra/codecs/constants.py). Checking the encoded
// header before decoding prevents a small, malicious payload from causing an
// unreasonable allocation.
const maxImageDimension = 16384

// maxDecodedImageBytes still permits an 8K frame while bounding the expansion
// ratio of a highly compressible image. The packet itself is bounded
// separately by the protocol reader.
const maxDecodedImageBytes = 256 << 20

// Cursor images are tiny in normal use. Keeping a separate bound prevents a
// malicious peer from turning a cursor update into a very large native desktop
// resource while still allowing unusually large HiDPI cursors.
const maxCursorDimension = 1024

// decodeImage decodes one complete JPEG, PNG or WebP damage rectangle into the
// BGRX layout consumed by both desktop backends.
//
// PNG's palette and grayscale variants differ only in the coding name on the
// wire; the encoded payload is an ordinary PNG in every case.
func decodeImage(coding string, data []byte, width, height int) ([]byte, int, error) {
	var (
		decode       func(io.Reader) (image.Image, error)
		decodeConfig func(io.Reader) (image.Config, error)
	)
	switch coding {
	case "jpeg":
		decode, decodeConfig = jpeg.Decode, jpeg.DecodeConfig
	case "png", "png/P", "png/L":
		decode, decodeConfig = png.Decode, png.DecodeConfig
	case "webp":
		decode, decodeConfig = webp.Decode, webp.DecodeConfig
	default:
		return nil, 0, fmt.Errorf("unsupported image encoding %q", coding)
	}

	config, err := decodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("decoding %s header: %w", coding, err)
	}
	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > maxImageDimension || config.Height > maxImageDimension {
		return nil, 0, fmt.Errorf("%s image size %dx%d is outside the 1..%d limit",
			coding, config.Width, config.Height, maxImageDimension)
	}
	if config.Width != width || config.Height != height {
		return nil, 0, fmt.Errorf("%s image is %dx%d, draw packet says %dx%d",
			coding, config.Width, config.Height, width, height)
	}
	if decodedBytes := config.Width * config.Height * ui.BytesPerPixel; decodedBytes > maxDecodedImageBytes {
		return nil, 0, fmt.Errorf("%s image expands to %d bytes, over the %d byte limit",
			coding, decodedBytes, maxDecodedImageBytes)
	}

	img, err := decode(bytes.NewReader(data))
	if err != nil {
		return nil, 0, fmt.Errorf("decoding %s pixels: %w", coding, err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != width || bounds.Dy() != height {
		return nil, 0, fmt.Errorf("decoded %s image is %dx%d, draw packet says %dx%d",
			coding, bounds.Dx(), bounds.Dy(), width, height)
	}

	stride := width * ui.BytesPerPixel
	pixels := make([]byte, stride*height)
	var webpYCbCr *image.YCbCr
	var webpAlpha *image.NYCbCrA
	if coding == "webp" {
		switch img := img.(type) {
		case *image.YCbCr:
			webpYCbCr = img
		case *image.NYCbCrA:
			webpYCbCr = &img.YCbCr
			webpAlpha = img
		}
	}
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			i := y*stride + x*ui.BytesPerPixel
			if webpYCbCr != nil {
				px, py := bounds.Min.X+x, bounds.Min.Y+y
				yi, ci := webpYCbCr.YOffset(px, py), webpYCbCr.COffset(px, py)
				r, g, b := webpYCbCrToRGB(
					webpYCbCr.Y[yi], webpYCbCr.Cb[ci], webpYCbCr.Cr[ci])
				if webpAlpha != nil {
					a := uint16(webpAlpha.A[webpAlpha.AOffset(px, py)])
					r, g, b = byte(uint16(r)*a/0xff),
						byte(uint16(g)*a/0xff), byte(uint16(b)*a/0xff)
				}
				pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = b, g, r, 0xff
				continue
			}
			// RGBA returns alpha-premultiplied channels. Since the hello says
			// this client has no transparent backing store, that naturally
			// composites an unexpected translucent image over black.
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] =
				byte(b>>8), byte(g>>8), byte(r>>8), 0xff
		}
	}
	return pixels, stride, nil
}

// webpYCbCrToRGB converts WebP's video-range BT.601 samples. Go's
// image.YCbCr conversion implements full-range JPEG YCbCr instead, which maps
// WebP's black luma value of 16 to RGB 16 rather than RGB 0.
//
// These fixed-point constants and operations match libwebp's scalar decoder.
func webpYCbCrToRGB(y, cb, cr byte) (byte, byte, byte) {
	multHi := func(value, coefficient int) int {
		return value * coefficient >> 8
	}
	clip := func(value int) byte {
		if value < 0 {
			return 0
		}
		value >>= 6
		if value > 0xff {
			return 0xff
		}
		return byte(value)
	}
	yi, cbi, cri := int(y), int(cb), int(cr)
	r := clip(multHi(yi, 19077) + multHi(cri, 26149) - 14234)
	g := clip(multHi(yi, 19077) - multHi(cbi, 6419) - multHi(cri, 13320) + 8708)
	b := clip(multHi(yi, 19077) + multHi(cbi, 33050) - 17685)
	return r, g, b
}

// decodeCursorPNG preserves the PNG alpha channel, unlike ordinary window
// damage which is deliberately made opaque. RGBA reports premultiplied colour
// components, yielding the BGRA representation both native backends consume.
func decodeCursorPNG(data []byte) (*ui.Cursor, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding png header: %w", err)
	}
	if config.Width <= 0 || config.Height <= 0 ||
		config.Width > maxCursorDimension || config.Height > maxCursorDimension {
		return nil, fmt.Errorf("png cursor size %dx%d is outside the 1..%d limit",
			config.Width, config.Height, maxCursorDimension)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding png pixels: %w", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != config.Width || bounds.Dy() != config.Height {
		return nil, fmt.Errorf("decoded png cursor is %dx%d, header says %dx%d",
			bounds.Dx(), bounds.Dy(), config.Width, config.Height)
	}
	pixels := make([]byte, config.Width*config.Height*ui.BytesPerPixel)
	for y := range config.Height {
		for x := range config.Width {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := (y*config.Width + x) * ui.BytesPerPixel
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] =
				byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8)
		}
	}
	return &ui.Cursor{
		Pixels: pixels,
		Width:  config.Width,
		Height: config.Height,
	}, nil
}
