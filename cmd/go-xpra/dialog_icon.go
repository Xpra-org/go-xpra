package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image/png"

	"github.com/Xpra-org/go-xpra/ui"
)

// dialogIconSize is large enough for HiDPI title bars and task switchers
// without publishing the full 1024-pixel source as a multi-megabyte X11
// property or native Windows resource.
const dialogIconSize = 64

// dialogIconPNG is one of the sizes generate-icon.sh renders from xpra.png,
// embedded so that a single executable carries its own icon on every platform,
// including the X11 and Wayland backends which have no equivalent of a Windows
// resource section.
//
// Rendering it ahead of time rather than downscaling the source at run time is
// what keeps this icon identical to the 64 pixel image inside xpra.ico: the
// generator crops the source's transparent margin before resizing, so the logo
// fills the same 60 pixels here as it does in the executable's own icon.
//
//go:embed xpra-64.png
var dialogIconPNG []byte

// loadDialogIcon decodes the embedded icon into the alpha-premultiplied BGRA
// layout the backends consume.
//
// Unlike a window icon arriving from a server, the payload is part of the
// build, so an error here means the embedded asset and this package have gone
// out of step rather than that a peer sent something unusable.
func loadDialogIcon() (*ui.Icon, error) {
	img, err := png.Decode(bytes.NewReader(dialogIconPNG))
	if err != nil {
		return nil, fmt.Errorf("decoding the embedded Xpra icon: %w", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != dialogIconSize || bounds.Dy() != dialogIconSize {
		return nil, fmt.Errorf("embedded Xpra icon is %dx%d, want %dx%d",
			bounds.Dx(), bounds.Dy(), dialogIconSize, dialogIconSize)
	}
	pixels := make([]byte, dialogIconSize*dialogIconSize*ui.BytesPerPixel)
	for y := range dialogIconSize {
		for x := range dialogIconSize {
			r, g, b, a := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			i := (y*dialogIconSize + x) * ui.BytesPerPixel
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] =
				byte(b>>8), byte(g>>8), byte(r>>8), byte(a>>8)
		}
	}
	return &ui.Icon{
		Pixels: pixels,
		Width:  dialogIconSize,
		Height: dialogIconSize,
	}, nil
}
