package client

import (
	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/rencodeplus"
	"github.com/Xpra-org/go-xpra/ui"
)

// deviceID identifies the input device a pointer event came from. A negative
// value tells the server not to track per-device event sequence numbers
// (xpra/server/subsystem/pointer.py:590), which is what we want since we do not
// number our events.
const deviceID = -1

// handleMotion forwards pointer movement.
//
// Coordinates are absolute (screen-relative), matching the geometry we reported
// in window-map, so the server can place the pointer in its own screen space.
func (c *Client) handleMotion(e ui.Motion) {
	wid, _, ok := c.lookup(e.Window)
	if !ok {
		return
	}
	c.send("pointer-motion", deviceID, 0, wid,
		[]any{e.X, e.Y}, rencodeplus.Dict{})
}

// handleButton forwards a button press or release.
//
// Backends represent each wheel notch as a press and release of buttons 4 to 7.
// Xpra's wheel packet represents the whole notch, so only the press is sent.
func (c *Client) handleButton(e ui.Button) {
	wid, _, ok := c.lookup(e.Window)
	if !ok {
		return
	}
	if e.Button >= 4 && e.Button <= 7 {
		if !e.Pressed {
			return
		}
		packetType := "pointer-wheel"
		if protocol.BackwardsCompatible {
			packetType = "wheel-motion"
		}
		distance := 1000
		if e.Button == 5 || e.Button == 7 {
			distance = -distance
		}
		c.send(packetType, wid, e.Button, distance,
			[]any{e.X, e.Y}, []string{}, []int{}, rencodeplus.Dict{})
		return
	}
	c.send("pointer-button", deviceID, 0, wid, e.Button, e.Pressed,
		[]any{e.X, e.Y}, rencodeplus.Dict{})
}

// handleKey forwards a key press or release.
//
// The backend has already done the platform's half of the work: naming the key
// the way the server's find_matching_keycode expects. A key it could not name
// is dropped, since sending the server a name it cannot resolve would have it
// guess from the keycode instead.
func (c *Client) handleKey(e ui.Key) {
	wid, _, ok := c.lookup(e.Window)
	if !ok {
		return
	}
	if e.Name == "" {
		c.debugf("ignoring key with unmapped keysym %#x (keycode %d)", e.Keysym, e.Keycode)
		return
	}
	modifiers := e.Modifiers
	if modifiers == nil {
		// An absent modifier list has to encode as an empty list, not as a null.
		modifiers = []string{}
	}
	// group 0: we do not track keyboard layout groups.
	c.send("keyboard-event", wid, e.Name, e.Pressed, rencodeplus.Dict{
		{Key: "modifiers", Value: modifiers},
		{Key: "keyval", Value: e.Keysym},
		{Key: "string", Value: e.Text},
		{Key: "keycode", Value: e.Keycode},
		{Key: "group", Value: 0},
	})
}

// handleFocus tells the server which window should receive keyboard input.
func (c *Client) handleFocus(e ui.Focus) {
	wid, _, ok := c.lookup(e.Window)
	if !ok {
		return
	}
	if c.focused == wid {
		return
	}
	c.focused = wid
	c.send("window-focus", wid, []string{})
}
