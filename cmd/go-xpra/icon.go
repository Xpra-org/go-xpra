package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image/png"

	"github.com/Xpra-org/go-xpra/ui"
)

// Regenerate the multi-resolution icon and its Windows resource.
//go:generate sh generate-icon.sh

// dialogIconSize is the size of the icon carried by the windows go-xpra opens
// on its own behalf. 64 pixels is what the desktops ask for at the largest
// scale they draw an icon at; Win32 downsamples it for the title bar.
const dialogIconSize = 64

// dialogIconPNG is one of the sizes generate-icon.sh renders from xpra.png. It
// is embedded rather than read from disk so that a single executable carries
// its own icon on every platform, including the X11 and Wayland backends which
// have no equivalent of a Windows resource section.
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
		return nil, fmt.Errorf("decoding dialog icon: %w", err)
	}
	bounds := img.Bounds()
	if bounds.Dx() != dialogIconSize || bounds.Dy() != dialogIconSize {
		return nil, fmt.Errorf("dialog icon is %dx%d, want %dx%d",
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
	return &ui.Icon{Pixels: pixels, Width: dialogIconSize, Height: dialogIconSize}, nil
}
