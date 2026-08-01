//go:build linux

package wayland

import (
	"log"
	"syscall"

	"github.com/rajveermalviya/go-wayland/wayland/client"

	"github.com/Xpra-org/go-xpra/ui"
)

// The pointer buttons wl_pointer reports, which are Linux evdev codes, and the
// X11 numbering xpra forwards: 1 left, 2 middle, 3 right, 8 back, 9 forward.
var pointerButtons = map[uint32]int{
	0x110: 1, // BTN_LEFT
	0x112: 2, // BTN_MIDDLE
	0x111: 3, // BTN_RIGHT
	0x113: 8, // BTN_SIDE
	0x114: 9, // BTN_EXTRA
}

// axisStep is how far a wl_pointer.axis event scrolls for one wheel notch on a
// compositor too old to count the notches for us. It is the value libinput uses
// for a discrete wheel.
const axisStep = 10

// maxScrollClicks caps how much one wheel event may turn into, so that a fast
// flick cannot flood the server.
const maxScrollClicks = 10

func (d *Display) watchPointer(pointer *client.Pointer) {
	pointer.SetEnterHandler(d.pointerEnter)
	pointer.SetLeaveHandler(d.pointerLeave)
	pointer.SetMotionHandler(d.pointerMotion)
	pointer.SetButtonHandler(d.pointerButton)
	pointer.SetAxisHandler(d.pointerAxis)
	pointer.SetAxisDiscreteHandler(d.pointerAxisDiscrete)
}

func (d *Display) watchKeyboard(keyboard *client.Keyboard) {
	keyboard.SetKeymapHandler(d.keyboardKeymap)
	keyboard.SetEnterHandler(d.keyboardEnter)
	keyboard.SetLeaveHandler(d.keyboardLeave)
	keyboard.SetKeyHandler(d.keyboardKey)
	keyboard.SetModifiersHandler(d.keyboardModifiers)
}

// pointerEnter records which window the pointer is over and where in it.
//
// Wayland only ever reports a position within a surface, whereas the server
// places the pointer in its own screen space, so every position has to be read
// together with the window it belongs to.
func (d *Display) pointerEnter(e client.PointerEnterEvent) {
	d.setInputSerial(e.Serial)
	w := d.window(e.Surface)
	if w == nil {
		return
	}
	d.pointerOver = w
	d.pointerX, d.pointerY = int(e.SurfaceX), int(e.SurfaceY)

	// The cursor is only ours while the pointer is over one of our surfaces,
	// and this serial is what proves it, so both are taken while we have them.
	d.enterSerial = e.Serial
	d.restoreCursor()

	d.emit(ui.Motion{Window: w.ID(), X: w.x + d.pointerX, Y: w.y + d.pointerY})
}

func (d *Display) pointerLeave(client.PointerLeaveEvent) {
	d.pointerOver = nil
}

func (d *Display) pointerMotion(e client.PointerMotionEvent) {
	w := d.pointerOver
	if w == nil {
		return
	}
	d.pointerX, d.pointerY = int(e.SurfaceX), int(e.SurfaceY)
	d.emit(ui.Motion{Window: w.ID(), X: w.x + d.pointerX, Y: w.y + d.pointerY})
}

// pointerButton forwards a button. Wayland carries no position with it, so the
// one from the last motion is used — which is where the pointer is, motion
// being reported before the button that follows it.
func (d *Display) pointerButton(e client.PointerButtonEvent) {
	d.setInputSerial(e.Serial)
	w := d.pointerOver
	if w == nil {
		return
	}
	button, ok := pointerButtons[e.Button]
	if !ok {
		return
	}
	d.emit(ui.Button{
		Window:  w.ID(),
		Button:  button,
		Pressed: e.State == uint32(client.PointerButtonStatePressed),
		X:       w.x + d.pointerX, Y: w.y + d.pointerY,
	})
}

// pointerAxis turns a scroll into button presses: X11, and so xpra, has no
// scroll axis, and a notch is a press and release of buttons 4 to 7.
//
// This is the fallback path. A compositor on wl_seat 5 or later also sends
// axis_discrete with the notch count, which arrives first and leaves nothing
// here to do.
func (d *Display) pointerAxis(e client.PointerAxisEvent) {
	if d.seatBound >= 5 {
		return
	}
	d.scroll(e.Axis, int(e.Value/axisStep))
}

func (d *Display) pointerAxisDiscrete(e client.PointerAxisDiscreteEvent) {
	d.scroll(e.Axis, int(e.Discrete))
}

// scroll emits one press and release per notch. A positive value is down or to
// the right, which X11 numbers 5 and 7; the other way is 4 and 6.
func (d *Display) scroll(axis uint32, notches int) {
	w := d.pointerOver
	if w == nil || notches == 0 {
		return
	}
	positive, negative := 5, 4
	if axis == uint32(client.PointerAxisHorizontalScroll) {
		positive, negative = 7, 6
	}
	button := positive
	if notches < 0 {
		button, notches = negative, -notches
	}
	notches = min(notches, maxScrollClicks)

	for range notches {
		for _, pressed := range []bool{true, false} {
			d.emit(ui.Button{
				Window: w.ID(), Button: button, Pressed: pressed,
				X: w.x + d.pointerX, Y: w.y + d.pointerY,
			})
		}
	}
}

// keyboardKeymap reads the layout the compositor is using, which is what names
// the keys for the server.
//
// The keymap has to be mapped rather than read. The descriptor arrives over the
// socket, and a descriptor passed that way shares its file offset with the one
// the compositor kept: compositors that send every client the same open file
// have already read to the end of it, so a read here returns nothing at all.
// Mapping it privately is both what the protocol asks for and the only way to
// see the bytes.
func (d *Display) keyboardKeymap(e client.KeyboardKeymapEvent) {
	if e.Fd < 0 {
		return
	}
	// The descriptor is ours once it has been mapped; the mapping outlives it.
	defer syscall.Close(e.Fd)

	if e.Format != uint32(client.KeyboardKeymapFormatXkbV1) {
		log.Printf("wayland: ignoring a keymap in unknown format %d", e.Format)
		return
	}
	if e.Size == 0 {
		return
	}
	text, err := syscall.Mmap(e.Fd, 0, int(e.Size), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		log.Printf("wayland: mapping the keymap: %v", err)
		return
	}
	defer syscall.Munmap(text)

	// The keymap is handed over NUL-terminated, and the size includes it.
	if end := len(text) - 1; end >= 0 && text[end] == 0 {
		text = text[:end]
	}
	d.keys = parseKeymap(string(text))
}

func (d *Display) keyboardEnter(e client.KeyboardEnterEvent) {
	d.setInputSerial(e.Serial)
	w := d.window(e.Surface)
	if w == nil {
		return
	}
	d.keyboardFocus = w
	if w.toplevel != nil {
		// The window the user is typing into is the one a menu opened next
		// belongs to.
		d.lastToplevel = w
	}
	d.emit(ui.Focus{Window: w.ID()})
}

func (d *Display) keyboardLeave(client.KeyboardLeaveEvent) {
	d.keyboardFocus = nil
}

// keyboardModifiers records the modifier state the next key events are read
// under. Latched modifiers count as held: the server cares which of them
// applies to the key, not how it came to.
func (d *Display) keyboardModifiers(e client.KeyboardModifiersEvent) {
	d.mods = e.ModsDepressed | e.ModsLatched | e.ModsLocked
}

// keyboardKey forwards a key, named the way the server's own keymap lookup
// expects. A key the compositor's keymap does not describe gets no name, and
// the client drops it rather than have the server guess from the keycode.
func (d *Display) keyboardKey(e client.KeyboardKeyEvent) {
	d.setInputSerial(e.Serial)
	w := d.keyboardFocus
	if w == nil {
		return
	}
	name := d.keys.name(e.Key, d.mods)
	keysym, text := keyDetails(name)
	d.emit(ui.Key{
		Window:  w.ID(),
		Pressed: e.State == uint32(client.KeyboardKeyStatePressed),
		Name:    name,
		Keysym:  keysym,
		Text:    text,
		// The server is an X server, which numbers keycodes from 8 where
		// Wayland numbers them from 0.
		Keycode:   int(e.Key) + keycodeOffset,
		Modifiers: ui.ModifierNames(d.mods),
	})
}

// window finds the forwarded window a surface belongs to. An event can name a
// surface that is not one of ours, or one we have just destroyed.
func (d *Display) window(surface *client.Surface) *Window {
	if surface == nil {
		return nil
	}
	return d.windows[surface.ID()]
}
