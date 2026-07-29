package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"

	goxpra "github.com/Xpra-org/go-xpra"
	"github.com/Xpra-org/go-xpra/ui"
	"golang.org/x/image/draw"
)

// dialogIconSize is large enough for HiDPI title bars and task switchers
// without publishing the full 1024-pixel source as a multi-megabyte X11
// property or native Windows resource.
const dialogIconSize = 64

func loadDialogIcon() (*ui.Icon, error) {
	source, err := png.Decode(bytes.NewReader(goxpra.XpraPNG))
	if err != nil {
		return nil, fmt.Errorf("decoding the embedded Xpra icon: %w", err)
	}
	scaled := image.NewRGBA(image.Rect(0, 0, dialogIconSize, dialogIconSize))
	draw.CatmullRom.Scale(scaled, scaled.Bounds(), source, source.Bounds(), draw.Over, nil)

	pixels := make([]byte, len(scaled.Pix))
	for i := 0; i < len(scaled.Pix); i += ui.BytesPerPixel {
		// image.RGBA stores alpha-premultiplied RGBA; ui.Icon uses the same
		// channels in the native little-endian BGRA order.
		pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] =
			scaled.Pix[i+2], scaled.Pix[i+1], scaled.Pix[i], scaled.Pix[i+3]
	}
	return &ui.Icon{
		Pixels: pixels,
		Width:  dialogIconSize,
		Height: dialogIconSize,
	}, nil
}
