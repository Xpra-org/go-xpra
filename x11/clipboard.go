//go:build linux

package x11

import (
	"fmt"
	"log"
	"sync"
	"unicode/utf8"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xfixes"
	"github.com/jezek/xgb/xproto"

	"github.com/Xpra-org/go-xpra/ui"
)

const maxClipboardBytes = 16 * 1024 * 1024

type clipboard struct {
	d                                                                 *Display
	window                                                            xproto.Window
	selection, property                                               xproto.Atom
	targets, utf8, plainUTF8, plain, textAtom, stringAtom, atom, incr xproto.Atom

	mu          sync.Mutex
	text        string
	requested   xproto.Atom
	receiving   bool
	receiveData []byte
	transfers   map[clipboardTransferKey]*clipboardTransfer
}

type clipboardTransferKey struct {
	window   xproto.Window
	property xproto.Atom
}

type clipboardTransfer struct {
	target xproto.Atom
	data   []byte
	offset int
}

var _ ui.Clipboard = (*clipboard)(nil)

func newClipboard(d *Display) (*clipboard, error) {
	if err := xfixes.Init(d.X.Conn()); err != nil {
		return nil, fmt.Errorf("initializing XFixes: %w", err)
	}
	window, err := xproto.NewWindowId(d.X.Conn())
	if err != nil {
		return nil, fmt.Errorf("allocating helper window: %w", err)
	}
	screen := d.X.Screen()
	if err := xproto.CreateWindowChecked(d.X.Conn(), screen.RootDepth, window, screen.Root,
		0, 0, 1, 1, 0, xproto.WindowClassInputOutput, screen.RootVisual,
		xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check(); err != nil {
		return nil, fmt.Errorf("creating helper window: %w", err)
	}
	c := &clipboard{d: d, window: window, transfers: map[clipboardTransferKey]*clipboardTransfer{}}
	for name, dst := range map[string]*xproto.Atom{
		"CLIPBOARD":                &c.selection,
		"GO_XPRA_CLIPBOARD":        &c.property,
		"TARGETS":                  &c.targets,
		"UTF8_STRING":              &c.utf8,
		"text/plain;charset=utf-8": &c.plainUTF8,
		"text/plain":               &c.plain,
		"TEXT":                     &c.textAtom,
		"STRING":                   &c.stringAtom,
		"ATOM":                     &c.atom,
		"INCR":                     &c.incr,
	} {
		reply, err := xproto.InternAtom(d.X.Conn(), false, uint16(len(name)), name).Reply()
		if err != nil {
			xproto.DestroyWindow(d.X.Conn(), window)
			return nil, fmt.Errorf("interning %s: %w", name, err)
		}
		*dst = reply.Atom
	}
	if err := xfixes.SelectSelectionInputChecked(d.X.Conn(), window, c.selection,
		xfixes.SelectionEventMaskSetSelectionOwner|
			xfixes.SelectionEventMaskSelectionWindowDestroy|
			xfixes.SelectionEventMaskSelectionClientClose).Check(); err != nil {
		xproto.DestroyWindow(d.X.Conn(), window)
		return nil, fmt.Errorf("watching CLIPBOARD ownership: %w", err)
	}
	c.request(c.utf8, xproto.TimeCurrentTime)
	return c, nil
}

func (c *clipboard) SetText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("clipboard text is not valid UTF-8")
	}
	if len(text) > maxClipboardBytes {
		return fmt.Errorf("clipboard text is larger than %d bytes", maxClipboardBytes)
	}
	c.mu.Lock()
	c.text = text
	c.mu.Unlock()
	if err := xproto.SetSelectionOwnerChecked(c.d.X.Conn(), c.window, c.selection,
		xproto.TimeCurrentTime).Check(); err != nil {
		return fmt.Errorf("claiming CLIPBOARD: %w", err)
	}
	return nil
}

func (c *clipboard) ownerChanged(e xfixes.SelectionNotifyEvent) (ui.Event, bool) {
	if e.Selection != c.selection || e.Owner == c.window {
		return nil, false
	}
	c.receiving = false
	c.receiveData = nil
	if e.Owner == xproto.WindowNone {
		return ui.ClipboardChange{Text: ""}, true
	}
	c.request(c.utf8, e.Timestamp)
	return nil, false
}

func (c *clipboard) request(target xproto.Atom, timestamp xproto.Timestamp) {
	c.requested = target
	xproto.ConvertSelection(c.d.X.Conn(), c.window, c.selection, target, c.property, timestamp)
}

func (c *clipboard) selectionNotify(e xproto.SelectionNotifyEvent) (ui.Event, bool) {
	if e.Requestor != c.window || e.Selection != c.selection {
		return nil, false
	}
	if owner, err := xproto.GetSelectionOwner(c.d.X.Conn(), c.selection).Reply(); err == nil && owner.Owner == c.window {
		return nil, false
	}
	if e.Property == xproto.AtomNone {
		if c.requested != c.stringAtom {
			c.request(c.stringAtom, e.Time)
		}
		return nil, false
	}
	reply, err := xproto.GetProperty(c.d.X.Conn(), true, c.window, c.property,
		xproto.GetPropertyTypeAny, 0, maxClipboardBytes/4+1).Reply()
	if err != nil {
		log.Printf("x11: reading CLIPBOARD: %v", err)
		return nil, false
	}
	if reply.Type == c.incr {
		c.receiving = true
		c.receiveData = nil
		return nil, false
	}
	if reply.Format != 8 || len(reply.Value) > maxClipboardBytes {
		return nil, false
	}
	data := reply.Value
	if c.requested == c.stringAtom && !utf8.Valid(data) {
		runes := make([]rune, len(data))
		for i, b := range data {
			runes[i] = rune(b)
		}
		return ui.ClipboardChange{Text: string(runes)}, true
	}
	if !utf8.Valid(data) {
		return nil, false
	}
	return ui.ClipboardChange{Text: string(data)}, true
}

func (c *clipboard) selectionRequest(e xproto.SelectionRequestEvent) {
	if e.Owner != c.window || e.Selection != c.selection {
		return
	}
	property := e.Property
	if property == xproto.AtomNone {
		property = e.Target
	}
	succeeded := false
	switch e.Target {
	case c.targets:
		atoms := []xproto.Atom{c.targets, c.utf8, c.plainUTF8, c.plain, c.textAtom, c.stringAtom}
		data := make([]byte, len(atoms)*4)
		for i, atom := range atoms {
			xgb.Put32(data[i*4:], uint32(atom))
		}
		succeeded = xproto.ChangePropertyChecked(c.d.X.Conn(), xproto.PropModeReplace,
			e.Requestor, property, c.atom, 32, uint32(len(atoms)), data).Check() == nil
	case c.utf8, c.plainUTF8, c.plain, c.textAtom, c.stringAtom:
		c.mu.Lock()
		data := []byte(c.text)
		c.mu.Unlock()
		if len(data) <= c.d.maxImageBytes {
			succeeded = xproto.ChangePropertyChecked(c.d.X.Conn(), xproto.PropModeReplace,
				e.Requestor, property, e.Target, 8, uint32(len(data)), data).Check() == nil
		} else {
			size := make([]byte, 4)
			xgb.Put32(size, uint32(len(data)))
			if err := xproto.ChangeWindowAttributesChecked(c.d.X.Conn(), e.Requestor,
				xproto.CwEventMask, []uint32{xproto.EventMaskPropertyChange}).Check(); err == nil {
				succeeded = xproto.ChangePropertyChecked(c.d.X.Conn(), xproto.PropModeReplace,
					e.Requestor, property, c.incr, 32, 1, size).Check() == nil
				if succeeded {
					key := clipboardTransferKey{window: e.Requestor, property: property}
					c.transfers[key] = &clipboardTransfer{target: e.Target, data: data}
				}
			}
		}
	}
	if !succeeded {
		property = xproto.AtomNone
	}
	notify := xproto.SelectionNotifyEvent{
		Time: e.Time, Requestor: e.Requestor, Selection: e.Selection,
		Target: e.Target, Property: property,
	}
	xproto.SendEvent(c.d.X.Conn(), false, e.Requestor, 0, string(notify.Bytes()))
}

func (c *clipboard) propertyNotify(e xproto.PropertyNotifyEvent) (ui.Event, bool) {
	if e.Window == c.window && e.Atom == c.property && c.receiving && e.State == xproto.PropertyNewValue {
		reply, err := xproto.GetProperty(c.d.X.Conn(), true, c.window, c.property,
			xproto.GetPropertyTypeAny, 0, uint32(c.d.maxImageBytes/4+1)).Reply()
		if err != nil {
			log.Printf("x11: reading incremental CLIPBOARD data: %v", err)
			c.receiving = false
			c.receiveData = nil
			return nil, false
		}
		if len(reply.Value) == 0 {
			data := c.receiveData
			c.receiving = false
			c.receiveData = nil
			if c.requested == c.stringAtom && !utf8.Valid(data) {
				runes := make([]rune, len(data))
				for i, b := range data {
					runes[i] = rune(b)
				}
				return ui.ClipboardChange{Text: string(runes)}, true
			}
			if utf8.Valid(data) {
				return ui.ClipboardChange{Text: string(data)}, true
			}
			return nil, false
		}
		if reply.Format != 8 || len(c.receiveData)+len(reply.Value) > maxClipboardBytes {
			c.receiving = false
			c.receiveData = nil
			return nil, false
		}
		c.receiveData = append(c.receiveData, reply.Value...)
		return nil, false
	}

	key := clipboardTransferKey{window: e.Window, property: e.Atom}
	transfer := c.transfers[key]
	if transfer == nil || e.State != xproto.PropertyDelete {
		return nil, false
	}
	remaining := len(transfer.data) - transfer.offset
	if remaining == 0 {
		xproto.ChangeProperty(c.d.X.Conn(), xproto.PropModeReplace, e.Window, e.Atom,
			transfer.target, 8, 0, nil)
		delete(c.transfers, key)
		return nil, false
	}
	size := min(remaining, c.d.maxImageBytes)
	chunk := transfer.data[transfer.offset : transfer.offset+size]
	transfer.offset += size
	xproto.ChangeProperty(c.d.X.Conn(), xproto.PropModeReplace, e.Window, e.Atom,
		transfer.target, 8, uint32(len(chunk)), chunk)
	return nil, false
}
