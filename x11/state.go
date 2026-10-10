//go:build linux

package x11

import (
	"sort"

	"github.com/jezek/xgbutil/ewmh"

	"github.com/Xpra-org/go-xpra/ui"
)

var _ ui.WindowStateProvider = (*Display)(nil)
var _ ui.StatefulWindow = (*Window)(nil)

// netWMStates maps the xpra window states to the EWMH _NET_WM_STATE atoms a
// window manager acts on. Maximizing takes both axes.
var netWMStates = map[string][]string{
	"maximized":    {"_NET_WM_STATE_MAXIMIZED_VERT", "_NET_WM_STATE_MAXIMIZED_HORZ"},
	"fullscreen":   {"_NET_WM_STATE_FULLSCREEN"},
	"above":        {"_NET_WM_STATE_ABOVE"},
	"below":        {"_NET_WM_STATE_BELOW"},
	"sticky":       {"_NET_WM_STATE_STICKY"},
	"shaded":       {"_NET_WM_STATE_SHADED"},
	"skip-taskbar": {"_NET_WM_STATE_SKIP_TASKBAR"},
	"skip-pager":   {"_NET_WM_STATE_SKIP_PAGER"},
}

// WindowStates names every state with an EWMH equivalent. Whether the window
// manager honours one is up to it, as it is for a local application.
func (d *Display) WindowStates() []string {
	states := make([]string, 0, len(netWMStates))
	for state := range netWMStates {
		states = append(states, state)
	}
	sort.Strings(states)
	return states
}

// SetState asks the window manager for a _NET_WM_STATE change. Before the
// window is mapped there is no window manager involvement yet, and EWMH has
// the client write the property itself instead, for Map to publish.
func (w *Window) SetState(state string, enabled bool) {
	atoms, ok := netWMStates[state]
	if !ok || w.overrideRedirect {
		return
	}
	if !w.mapped {
		if w.initialStates == nil {
			w.initialStates = map[string]bool{}
		}
		w.initialStates[state] = enabled
		return
	}
	action := ewmh.StateRemove
	if enabled {
		action = ewmh.StateAdd
	}
	second := ""
	if len(atoms) > 1 {
		second = atoms[1]
	}
	_ = ewmh.WmStateReqExtra(w.d.X, w.win.Id, action, atoms[0], second, 1)
}

// publishInitialNetWMState writes the states asked of the window before it was
// mapped into the _NET_WM_STATE property a window manager reads when it maps
// it.
func (w *Window) publishInitialNetWMState() {
	var atoms []string
	for state, enabled := range w.initialStates {
		if enabled {
			atoms = append(atoms, netWMStates[state]...)
		}
	}
	w.initialStates = nil
	if len(atoms) > 0 {
		sort.Strings(atoms)
		_ = ewmh.WmStateSet(w.d.X, w.win.Id, atoms)
	}
}
