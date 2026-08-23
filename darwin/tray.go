//go:build darwin

package darwin

import (
	"errors"

	"github.com/ebitengine/purego/objc"

	"github.com/Xpra-org/go-xpra/ui"
)

const nsVariableStatusItemLength = -1

var (
	sel_systemStatusBar                  = objc.RegisterName("systemStatusBar")
	sel_statusItemWithLength             = objc.RegisterName("statusItemWithLength:")
	sel_button                           = objc.RegisterName("button")
	sel_setMenu                          = objc.RegisterName("setMenu:")
	sel_initWithTitleActionKeyEquivalent = objc.RegisterName("initWithTitle:action:keyEquivalent:")
	sel_setTarget                        = objc.RegisterName("setTarget:")
	sel_addItem                          = objc.RegisterName("addItem:")
	sel_setEnabled                       = objc.RegisterName("setEnabled:")
	sel_trayExit                         = objc.RegisterName("trayExit:")
	sel_removeStatusItem                 = objc.RegisterName("removeStatusItem:")
)

var _ ui.SystemTrayProvider = (*Display)(nil)

// ShowTray installs a notification-area icon. It is safe to call only once
// per session; a repeated call simply keeps the first one, matching win32's
// ShowTray.
func (d *Display) ShowTray(title string) error {
	if title == "" {
		return errors.New("tray title is empty")
	}
	var trayErr error
	if err := d.call(func() {
		if d.tray != 0 {
			return
		}
		trayErr = d.createTray(title)
	}); err != nil {
		return err
	}
	return trayErr
}

func (d *Display) createTray(title string) error {
	statusBar := objc.ID(objc.GetClass("NSStatusBar")).Send(sel_systemStatusBar)
	item := statusBar.Send(sel_statusItemWithLength, nsVariableStatusItemLength)
	if item == 0 {
		return errors.New("darwin: creating the status item failed")
	}
	// The status bar retains the items it hands out — unlike almost every
	// other AppKit factory method — so no extra retain is needed here, and
	// removeStatusItem: in destroyTray is what releases it.
	if button := item.Send(sel_button); button != 0 {
		// No custom xpra glyph is bundled for macOS, so a short label stands
		// in for the icon a real asset would give win32's ShowTray, the same
		// fall-back spirit as win32 loading a system icon when its own
		// resource is unavailable.
		button.Send(sel_setTitle, nsString("xpra"))
	}

	menu := objc.ID(objc.GetClass("NSMenu")).Send(sel_alloc).Send(sel_init)

	header := objc.ID(objc.GetClass("NSMenuItem")).Send(sel_alloc)
	header = header.Send(sel_initWithTitleActionKeyEquivalent, nsString(title), objc.SEL(0), nsString(""))
	header.Send(sel_setEnabled, false)
	menu.Send(sel_addItem, header)

	exit := objc.ID(objc.GetClass("NSMenuItem")).Send(sel_alloc)
	exit = exit.Send(sel_initWithTitleActionKeyEquivalent, nsString("Exit"), sel_trayExit, nsString(""))
	exit.Send(sel_setTarget, d.coordinator)
	menu.Send(sel_addItem, exit)

	item.Send(sel_setMenu, menu)
	d.tray = item
	return nil
}

// trayExit is the coordinator's action method for the tray menu's Exit item.
func (d *Display) trayExit(_ objc.ID, _ objc.SEL, _ objc.ID) {
	d.emit(ui.ExitRequest{})
}

func (d *Display) destroyTray() {
	if d.tray == 0 {
		return
	}
	statusBar := objc.ID(objc.GetClass("NSStatusBar")).Send(sel_systemStatusBar)
	statusBar.Send(sel_removeStatusItem, d.tray)
	d.tray = 0
}
