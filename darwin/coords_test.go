//go:build darwin

package darwin

import "testing"

func TestCocoaToTopLeftRoundTrip(t *testing.T) {
	cases := []struct {
		name                           string
		x, y, width, height, refHeight float64
	}{
		{"primary screen origin", 0, 0, 1920, 1080, 1080},
		{"window near top of primary screen", 100, 1000, 640, 50, 1080},
		{"window near bottom of primary screen", 100, 0, 640, 50, 1080},
		{"secondary monitor above primary", -200, 1080, 1920, 1080, 1080},
		{"secondary monitor to the left, negative X", -1920, 200, 1920, 1080, 1080},
		{"secondary monitor below primary, negative Y region", 0, -600, 1920, 600, 1080},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx, ty, tw, th := cocoaToTopLeft(tc.x, tc.y, tc.width, tc.height, tc.refHeight)
			if tw != int(tc.width) || th != int(tc.height) {
				t.Errorf("cocoaToTopLeft size = %d,%d, want %d,%d", tw, th, int(tc.width), int(tc.height))
			}

			back := topLeftToCocoa(tx, ty, tw, th, tc.refHeight)
			if back.Origin.X != tc.x || back.Origin.Y != tc.y {
				t.Errorf("round trip origin = %+v, want %g,%g", back.Origin, tc.x, tc.y)
			}
			if back.Size.Width != tc.width || back.Size.Height != tc.height {
				t.Errorf("round trip size = %+v, want %gx%g", back.Size, tc.width, tc.height)
			}
		})
	}
}

// A window flush against the top of the primary screen must come back at
// top-left Y=0, and one flush against the bottom at Y = referenceHeight -
// windowHeight: the two ends of the axis this conversion flips.
func TestCocoaToTopLeftKnownPositions(t *testing.T) {
	const refHeight = 1080

	// Window whose Cocoa origin.Y + height reaches the very top of the screen.
	_, ty, _, _ := cocoaToTopLeft(0, 1080-200, 640, 200, refHeight)
	if ty != 0 {
		t.Errorf("window at the top of the screen: top-left Y = %d, want 0", ty)
	}

	// Window whose Cocoa origin.Y is 0 sits at the bottom of the screen, so
	// its top-left Y should be referenceHeight-height.
	_, ty, _, _ = cocoaToTopLeft(0, 0, 640, 200, refHeight)
	if want := refHeight - 200; ty != want {
		t.Errorf("window at the bottom of the screen: top-left Y = %d, want %d", ty, want)
	}
}

func TestTopLeftToCocoaKnownPositions(t *testing.T) {
	const refHeight = 1080

	// A window requested at the server's top-left origin (0,0) should end up
	// flush against the top of the screen in Cocoa's space.
	r := topLeftToCocoa(0, 0, 640, 200, refHeight)
	if r.Origin.Y != refHeight-200 {
		t.Errorf("topLeftToCocoa(0,0,...).Origin.Y = %g, want %g", r.Origin.Y, float64(refHeight-200))
	}
}
