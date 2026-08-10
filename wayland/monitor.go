//go:build linux

package wayland

import (
	"sort"

	"github.com/rajveermalviya/go-wayland/wayland/client"

	"github.com/Xpra-org/go-xpra/ui"
)

type outputInfo struct {
	output                *client.Output
	monitor               ui.Monitor
	modeWidth, modeHeight int
	transform             int32
}

var _ ui.MonitorProvider = (*Display)(nil)
var _ ui.DesktopSizeProvider = (*Display)(nil)

// addOutput binds a wl_output and records every property useful to Xpra's
// per-monitor capability dictionary. It runs under the display lock.
func (d *Display) addOutput(e client.RegistryGlobalEvent) {
	output := client.NewOutput(d.ctx)
	d.bind(e, output, outputVersion)
	info := &outputInfo{output: output}
	info.monitor.ScaleFactor = 1
	d.outputs[e.Name] = info

	output.SetGeometryHandler(func(e client.OutputGeometryEvent) {
		info.monitor.Geometry.X = int(e.X)
		info.monitor.Geometry.Y = int(e.Y)
		info.monitor.WidthMM = int(e.PhysicalWidth)
		info.monitor.HeightMM = int(e.PhysicalHeight)
		info.monitor.Manufacturer = e.Make
		info.monitor.Model = e.Model
		info.monitor.SubpixelLayout = subpixelName(e.Subpixel)
		info.transform = e.Transform
		info.updateGeometry()
	})
	output.SetModeHandler(func(e client.OutputModeEvent) {
		if e.Flags&uint32(client.OutputModeCurrent) == 0 {
			return
		}
		info.modeWidth, info.modeHeight = int(e.Width), int(e.Height)
		info.monitor.RefreshRate = int(e.Refresh)
		info.updateGeometry()
	})
	output.SetScaleHandler(func(e client.OutputScaleEvent) {
		if e.Factor > 0 {
			info.monitor.ScaleFactor = int(e.Factor)
			info.updateGeometry()
		}
	})
	output.SetNameHandler(func(e client.OutputNameEvent) {
		info.monitor.Name = e.Name
	})
}

func (o *outputInfo) updateGeometry() {
	width, height := o.modeWidth, o.modeHeight
	if o.transform == int32(client.OutputTransform90) ||
		o.transform == int32(client.OutputTransform270) ||
		o.transform == int32(client.OutputTransformFlipped90) ||
		o.transform == int32(client.OutputTransformFlipped270) {
		width, height = height, width
	}
	scale := max(o.monitor.ScaleFactor, 1)
	o.monitor.Geometry.Width = width / scale
	o.monitor.Geometry.Height = height / scale
}

func subpixelName(value int32) string {
	switch client.OutputSubpixel(value) {
	case client.OutputSubpixelNone:
		return "none"
	case client.OutputSubpixelHorizontalRgb:
		return "horizontal-rgb"
	case client.OutputSubpixelHorizontalBgr:
		return "horizontal-bgr"
	case client.OutputSubpixelVerticalRgb:
		return "vertical-rgb"
	case client.OutputSubpixelVerticalBgr:
		return "vertical-bgr"
	default:
		return "unknown"
	}
}

// Monitors returns a snapshot of the compositor's active outputs.
func (d *Display) Monitors() []ui.Monitor {
	d.mu.Lock()
	defer d.mu.Unlock()
	names := make([]uint32, 0, len(d.outputs))
	for name := range d.outputs {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	monitors := make([]ui.Monitor, 0, len(names))
	for _, name := range names {
		output := d.outputs[name]
		if output.monitor.Geometry.Valid() {
			monitor := output.monitor
			// Wayland has no primary-output property. Treat the compositor's
			// first output as primary so the server still receives one stable
			// anchor for layouts whose coordinates do not identify it.
			monitor.Primary = len(monitors) == 0
			monitors = append(monitors, monitor)
		}
	}
	return monitors
}

// DesktopSize returns the bounding rectangle of all active outputs in
// compositor coordinates, the same coordinate space used by monitor geometry.
func (d *Display) DesktopSize() (int, int, bool) {
	monitors := d.Monitors()
	if len(monitors) == 0 {
		return 0, 0, false
	}
	first := monitors[0].Geometry
	minX, minY := first.X, first.Y
	maxX, maxY := first.X+first.Width, first.Y+first.Height
	for _, monitor := range monitors[1:] {
		geometry := monitor.Geometry
		minX, minY = min(minX, geometry.X), min(minY, geometry.Y)
		maxX = max(maxX, geometry.X+geometry.Width)
		maxY = max(maxY, geometry.Y+geometry.Height)
	}
	return maxX - minX, maxY - minY, true
}
