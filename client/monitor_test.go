package client

import (
	"testing"

	"github.com/Xpra-org/go-xpra/ui"
)

func TestMonitorRelativePosition(t *testing.T) {
	monitors := []ui.Monitor{
		{Geometry: ui.Rectangle{X: -1920, Width: 1920, Height: 1080}},
		{Geometry: ui.Rectangle{Y: 200, Width: 2560, Height: 1440}},
	}
	tests := []struct {
		name             string
		x, y             int
		index, relativeX int
		relativeY        int
		ok               bool
	}{
		{"left origin", -1920, 0, 0, 0, 0, true},
		{"left last pixel", -1, 1079, 0, 1919, 1079, true},
		{"right origin", 0, 200, 1, 0, 0, true},
		{"gap", 0, 100, 0, 0, 0, false},
		{"exclusive right edge", 2560, 200, 0, 0, 0, false},
		{"exclusive bottom edge", 0, 1640, 0, 0, 0, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			index, x, y, ok := monitorRelativePosition(monitors, test.x, test.y)
			if index != test.index || x != test.relativeX || y != test.relativeY || ok != test.ok {
				t.Errorf("monitorRelativePosition(%d, %d) = (%d, %d, %d, %v), want (%d, %d, %d, %v)",
					test.x, test.y, index, x, y, ok,
					test.index, test.relativeX, test.relativeY, test.ok)
			}
		})
	}
}

func TestUsableMonitorsCompactsIndexes(t *testing.T) {
	monitors := usableMonitors([]ui.Monitor{
		{},
		{Name: "DP-1", Geometry: ui.Rectangle{Width: 1920, Height: 1080}},
	})
	if len(monitors) != 1 || monitors[0].Name != "DP-1" {
		t.Fatalf("usableMonitors() = %#v", monitors)
	}
}
