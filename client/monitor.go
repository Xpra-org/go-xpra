package client

import (
	"github.com/Xpra-org/go-xpra/rencodeplus"
	"github.com/Xpra-org/go-xpra/ui"
)

// usableMonitors filters the backend snapshot to the exact compact order sent
// in hello. Keeping this slice means later descriptors use the same indexes as
// the server, even when the backend returned an invalid output before one that
// was advertised.
func usableMonitors(monitors []ui.Monitor) []ui.Monitor {
	usable := make([]ui.Monitor, 0, len(monitors))
	for _, monitor := range monitors {
		if monitor.Geometry.Valid() {
			usable = append(usable, monitor)
		}
	}
	return usable
}

// monitorRelativePosition identifies the monitor containing an absolute point
// and rebases the point against that monitor's origin. Right and bottom edges
// are exclusive, matching Xpra's MonitorLayout.relative_position.
func monitorRelativePosition(monitors []ui.Monitor, x, y int) (index, relativeX, relativeY int, ok bool) {
	for index, monitor := range monitors {
		geometry := monitor.Geometry
		x64, y64 := int64(x), int64(y)
		left, top := int64(geometry.X), int64(geometry.Y)
		right := left + int64(geometry.Width)
		bottom := top + int64(geometry.Height)
		if x64 >= left && x64 < right && y64 >= top && y64 < bottom {
			return index, x - geometry.X, y - geometry.Y, true
		}
	}
	return 0, 0, 0, false
}

func monitorDescriptor(index, x, y int, monitor ui.Monitor) rencodeplus.Dict {
	geometry := monitor.Geometry
	return rencodeplus.Dict{
		{Key: "index", Value: index},
		{Key: "position", Value: []any{x - geometry.X, y - geometry.Y}},
	}
}

// pointMonitorDescriptor describes a point only when it lies on a known
// monitor. Dead space between outputs keeps using the absolute fallback.
func (c *Client) pointMonitorDescriptor(x, y int) (rencodeplus.Dict, bool) {
	index, _, _, ok := monitorRelativePosition(c.monitors, x, y)
	if !ok {
		return nil, false
	}
	return monitorDescriptor(index, x, y, c.monitors[index]), true
}

// windowMonitorDescriptor also handles a window origin outside every output,
// as happens when a window overlaps a monitor's left or top edge. Any monitor
// is a valid anchor because its raw origin and the offset are changed by equal
// and opposite amounts; use the first advertised monitor as a stable fallback.
func (c *Client) windowMonitorDescriptor(x, y int) (rencodeplus.Dict, bool) {
	if descriptor, ok := c.pointMonitorDescriptor(x, y); ok {
		return descriptor, true
	}
	if len(c.monitors) == 0 {
		return nil, false
	}
	return monitorDescriptor(0, x, y, c.monitors[0]), true
}

func (c *Client) pointerProperties(x, y int) rencodeplus.Dict {
	properties := rencodeplus.Dict{}
	if monitor, ok := c.pointMonitorDescriptor(x, y); ok {
		properties.Set("monitor", monitor)
	}
	return properties
}
