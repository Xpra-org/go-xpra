//go:build linux

package x11

import (
	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"

	"github.com/Xpra-org/go-xpra/keysym"
	"github.com/Xpra-org/go-xpra/ui"
)

// translate converts an X event into the neutral event the client works in, and
// reports false for the events we have nothing to say about.
//
// Which events arrive at all is decided by the mask in window.go, so the
// default case is only reached by the unsolicited ones — MappingNotify, say,
// which keybind handles for us.
func (d *Display) translate(event xgb.Event) (ui.Event, bool) {
	switch e := event.(type) {
	case xproto.ConfigureNotifyEvent:
		return ui.Configure{
			Window: ui.WindowID(e.Window),
			X:      int(e.X), Y: int(e.Y),
			Width: int(e.Width), Height: int(e.Height),
		}, true

	case xproto.ClientMessageEvent:
		if e.Type != d.wmProtocols || e.Format != 32 {
			return nil, false
		}
		data := e.Data.Data32
		if len(data) == 0 || xproto.Atom(data[0]) != d.wmDeleteWindow {
			return nil, false
		}
		return ui.CloseRequest{Window: ui.WindowID(e.Window)}, true

	case xproto.MotionNotifyEvent:
		return ui.Motion{
			Window: ui.WindowID(e.Event),
			X:      int(e.RootX), Y: int(e.RootY),
		}, true

	case xproto.ButtonPressEvent:
		return d.button(e.Event, e.Detail, true, e.RootX, e.RootY), true
	case xproto.ButtonReleaseEvent:
		return d.button(e.Event, e.Detail, false, e.RootX, e.RootY), true

	case xproto.KeyPressEvent:
		return d.key(e.Event, e.Detail, e.State, true), true
	case xproto.KeyReleaseEvent:
		return d.key(e.Event, e.Detail, e.State, false), true

	case xproto.FocusInEvent:
		return ui.Focus{Window: ui.WindowID(e.Event)}, true
	}
	return nil, false
}

// button reports a pointer button. X11 numbers buttons the way xpra forwards
// them, including the wheel as buttons 4 to 7, so nothing needs mapping.
func (d *Display) button(xid xproto.Window, button xproto.Button, pressed bool, rootX, rootY int16) ui.Event {
	return ui.Button{
		Window:  ui.WindowID(xid),
		Button:  int(button),
		Pressed: pressed,
		X:       int(rootX), Y: int(rootY),
	}
}

// key resolves a keycode and modifier state into the naming the server expects.
// The keycode is passed along as-is: the server is an X server too, so its
// keymap may well agree with ours.
func (d *Display) key(xid xproto.Window, keycode xproto.Keycode, state uint16, pressed bool) ui.Event {
	sym := effectiveKeysym(d.X, keycode, state)
	return ui.Key{
		Window:    ui.WindowID(xid),
		Pressed:   pressed,
		Name:      keysym.Name(uint32(sym)),
		Keysym:    int(sym),
		Text:      keyText(sym),
		Keycode:   int(keycode),
		Modifiers: ui.ModifierNames(uint32(state)),
	}
}
