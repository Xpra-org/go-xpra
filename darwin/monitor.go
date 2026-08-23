//go:build darwin

package darwin

import (
	"github.com/ebitengine/purego/objc"

	"github.com/Xpra-org/go-xpra/ui"
)

var (
	sel_visibleFrame       = objc.RegisterName("visibleFrame")
	sel_backingScaleFactor = objc.RegisterName("backingScaleFactor")
	sel_localizedName      = objc.RegisterName("localizedName")
)

// Monitors returns each active NSScreen's full rectangle, usable work area,
// scale factor and name, in NSScreen.screens' own order — the platform's
// native monitor order, as ui.MonitorProvider documents.
func (d *Display) Monitors() []ui.Monitor {
	var monitors []ui.Monitor
	d.call(func() {
		screens := objc.ID(objc.GetClass("NSScreen")).Send(sel_screens)
		count := objc.Send[int](screens, sel_count)
		monitors = make([]ui.Monitor, 0, count)
		for i := 0; i < count; i++ {
			screen := screens.Send(sel_objectAtIndex, i)
			frame := objc.Send[nsRect](screen, sel_frame)
			visible := objc.Send[nsRect](screen, sel_visibleFrame)
			scale := objc.Send[float64](screen, sel_backingScaleFactor)
			name := goString(screen.Send(sel_localizedName))

			gx, gy, gw, gh := cocoaToTopLeft(
				frame.Origin.X, frame.Origin.Y, frame.Size.Width, frame.Size.Height, d.referenceHeight)
			wx, wy, ww, wh := cocoaToTopLeft(
				visible.Origin.X, visible.Origin.Y, visible.Size.Width, visible.Size.Height, d.referenceHeight)

			monitors = append(monitors, ui.Monitor{
				Name:        name,
				Geometry:    ui.Rectangle{X: gx, Y: gy, Width: gw, Height: gh},
				WorkArea:    ui.Rectangle{X: wx, Y: wy, Width: ww, Height: wh},
				ScaleFactor: int(scale),
				// index 0 is, by Apple's own guarantee, the screen this
				// package's referenceHeight is anchored to — the same test
				// used as the flip anchor in coords.go.
				Primary: i == 0,
			})
		}
	})
	return monitors
}

// DesktopSize returns the bounding rectangle of every monitor, in the same
// top-left/Y-down space Monitors reports geometry in — mirroring how win32's
// virtual-screen system metrics can have a negative origin too.
func (d *Display) DesktopSize() (width, height int, ok bool) {
	monitors := d.Monitors()
	if len(monitors) == 0 {
		return 0, 0, false
	}
	minX, minY := monitors[0].Geometry.X, monitors[0].Geometry.Y
	maxX, maxY := minX+monitors[0].Geometry.Width, minY+monitors[0].Geometry.Height
	for _, m := range monitors[1:] {
		minX = min(minX, m.Geometry.X)
		minY = min(minY, m.Geometry.Y)
		maxX = max(maxX, m.Geometry.X+m.Geometry.Width)
		maxY = max(maxY, m.Geometry.Y+m.Geometry.Height)
	}
	return maxX - minX, maxY - minY, true
}
