package client

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"

	"github.com/Xpra-org/go-xpra/ui"
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

// decodeImage decodes one complete JPEG or PNG damage rectangle into the BGRX
// layout consumed by both desktop backends.
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
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			// RGBA returns alpha-premultiplied channels. Since the hello says
			// this client has no transparent backing store, that naturally
			// composites an unexpected translucent PNG over black.
			r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := y*stride + x*ui.BytesPerPixel
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] =
				byte(b>>8), byte(g>>8), byte(r>>8), 0xff
		}
	}
	return pixels, stride, nil
}
