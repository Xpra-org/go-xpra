//go:build linux

package wayland

import (
	"testing"

	"github.com/rajveermalviya/go-wayland/wayland/client"

	"github.com/Xpra-org/go-xpra/ui"
)

func TestOutputGeometryAccountsForScaleAndRotation(t *testing.T) {
	output := outputInfo{
		monitor:    ui.Monitor{ScaleFactor: 2},
		modeWidth:  3840,
		modeHeight: 2160,
		transform:  int32(client.OutputTransform90),
	}
	output.updateGeometry()
	if got := output.monitor.Geometry; got.Width != 1080 || got.Height != 1920 {
		t.Errorf("geometry = %+v, want 1080x1920", got)
	}
}

func TestSubpixelNamesMatchXpraVocabulary(t *testing.T) {
	if got := subpixelName(int32(client.OutputSubpixelHorizontalRgb)); got != "horizontal-rgb" {
		t.Errorf("subpixel name = %q", got)
	}
}

func TestDesktopSizeBoundsOutputs(t *testing.T) {
	display := &Display{outputs: map[uint32]*outputInfo{
		1: {monitor: ui.Monitor{Geometry: ui.Rectangle{X: -1920, Width: 1920, Height: 1080}}},
		2: {monitor: ui.Monitor{Geometry: ui.Rectangle{X: 0, Y: -240, Width: 2560, Height: 1440}}},
	}}
	width, height, ok := display.DesktopSize()
	if !ok || width != 4480 || height != 1440 {
		t.Errorf("DesktopSize() = %dx%d, %v; want 4480x1440, true", width, height, ok)
	}
}
