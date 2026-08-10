//go:build linux

package x11

import (
	"log"

	"github.com/jezek/xgb/randr"
	"github.com/jezek/xgbutil/xprop"

	"github.com/Xpra-org/go-xpra/ui"
)

var _ ui.MonitorProvider = (*Display)(nil)

// Monitors returns the active RandR monitor layout. RandR 1.5 monitor objects
// preserve the geometry of tiled displays better than treating every output as
// a separate monitor; on an older X server the capability is simply omitted.
func (d *Display) Monitors() []ui.Monitor {
	conn := d.X.Conn()
	if err := randr.Init(conn); err != nil {
		log.Printf("x11: RandR monitor information is unavailable: %v", err)
		return nil
	}
	version, err := randr.QueryVersion(conn, 1, 5).Reply()
	if err != nil {
		log.Printf("x11: querying the RandR version: %v", err)
		return nil
	}
	if version == nil || version.MajorVersion < 1 ||
		(version.MajorVersion == 1 && version.MinorVersion < 5) {
		return nil
	}
	reply, err := randr.GetMonitors(conn, d.X.RootWin(), true).Reply()
	if err != nil {
		log.Printf("x11: reading RandR monitors: %v", err)
		return nil
	}
	if reply == nil {
		return nil
	}
	monitors := make([]ui.Monitor, 0, len(reply.Monitors))
	for _, info := range reply.Monitors {
		geometry := ui.Rectangle{
			X: int(info.X), Y: int(info.Y),
			Width: int(info.Width), Height: int(info.Height),
		}
		if !geometry.Valid() {
			continue
		}
		name, err := xprop.AtomName(d.X, info.Name)
		if err != nil {
			name = ""
		}
		monitors = append(monitors, ui.Monitor{
			Name: name, Geometry: geometry,
			WidthMM: int(info.WidthInMillimeters), HeightMM: int(info.HeightInMillimeters),
			ScaleFactor: 1, Primary: info.Primary,
		})
	}
	return monitors
}
