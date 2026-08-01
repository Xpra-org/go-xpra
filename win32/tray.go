//go:build windows

package win32

import (
	"errors"
	"fmt"
	"log"
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/Xpra-org/go-xpra/ui"
)

const (
	xpraIconResource = 1
	trayIconID       = 1
	trayExitCommand  = 1

	imageIcon     = 1
	lrDefaultSize = 0x0040

	nimAdd        = 0x00000000
	nimDelete     = 0x00000002
	nimSetVersion = 0x00000004
	nifMessage    = 0x00000001
	nifIcon       = 0x00000002
	nifTip        = 0x00000004
	nifShowTip    = 0x00000080
	notifyVersion = 4

	mfGrayed    = 0x00000001
	mfDisabled  = 0x00000002
	mfString    = 0x00000000
	mfSeparator = 0x00000800

	tpmRightButton = 0x0002
	tpmNonotify    = 0x0080
	tpmReturnCmd   = 0x0100

	ninSelect    = wmUser
	ninKeySelect = wmUser + 1
)

// ShowTray installs a notification-area icon on the existing window thread.
// It is safe to call only once per session; a repeated call simply keeps the
// first icon and title.
func (d *Display) ShowTray(title string) error {
	if title == "" {
		return errors.New("tray title is empty")
	}
	if _, err := syscall.UTF16PtrFromString(title); err != nil {
		return fmt.Errorf("tray title: %w", err)
	}

	var trayErr error
	if err := d.call(func() {
		if d.tray == 0 {
			trayErr = d.createTray(title)
		}
	}); err != nil {
		return err
	}
	return trayErr
}

func (d *Display) createTray(title string) error {
	taskbarCreated, err := syscall.UTF16PtrFromString("TaskbarCreated")
	if err != nil {
		return err
	}
	d.taskbarCreated, err = registerWindowMessage(taskbarCreated)
	if err != nil {
		return err
	}

	titleUTF16, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return err
	}
	d.tray, err = createWindowEx(wsExToolWindow, d.class, titleUTF16, wsPopup,
		0, 0, 0, 0, 0, d.instance)
	if err != nil {
		return fmt.Errorf("creating tray window: %w", err)
	}
	d.trayTitle = title

	d.trayIcon, err = loadIconImage(d.instance, xpraIconResource)
	if err == nil {
		d.trayIconOwned = true
	} else {
		d.trayIcon = loadIcon(0, idiApplication)
		if d.trayIcon == 0 {
			d.destroyTray()
			return fmt.Errorf("loading tray icon: %w", err)
		}
	}
	if err := d.addTrayIcon(); err != nil {
		d.destroyTray()
		return err
	}
	return nil
}

func (d *Display) trayIconData() notifyIconData {
	data := notifyIconData{
		Hwnd:     d.tray,
		ID:       trayIconID,
		Flags:    nifMessage | nifIcon | nifTip | nifShowTip,
		Callback: wmTrayCallback,
		Icon:     d.trayIcon,
	}
	data.Size = uint32(unsafe.Sizeof(data))
	setFixedUTF16(data.Tip[:], d.trayTitle)
	return data
}

func (d *Display) addTrayIcon() error {
	data := d.trayIconData()
	if !shellNotifyIcon(nimAdd, &data) {
		return errors.New("Shell_NotifyIconW could not add the tray icon")
	}
	d.trayAdded = true

	data.Version = notifyVersion
	if !shellNotifyIcon(nimSetVersion, &data) {
		shellNotifyIcon(nimDelete, &data)
		d.trayAdded = false
		return errors.New("Shell_NotifyIconW could not set tray callback version")
	}
	return nil
}

func (d *Display) restoreTray() {
	// Explorer discarded the old icon when its taskbar went away, even though
	// our own bookkeeping still considered it added.
	d.trayAdded = false
	if err := d.addTrayIcon(); err != nil {
		log.Printf("windows: restoring system tray icon: %v", err)
	}
}

func (d *Display) destroyTray() {
	if d.trayAdded {
		data := d.trayIconData()
		shellNotifyIcon(nimDelete, &data)
		d.trayAdded = false
	}
	if d.tray != 0 {
		hwnd := d.tray
		d.tray = 0
		destroyWindow(hwnd)
	}
	if d.trayIconOwned && d.trayIcon != 0 {
		destroyIcon(d.trayIcon)
	}
	d.trayIcon = 0
	d.trayIconOwned = false
	d.trayTitle = ""
}

func (d *Display) handleTrayCallback(notification uint32) {
	if trayActivated(notification) {
		d.openTrayMenu()
	}
}

func trayActivated(notification uint32) bool {
	switch notification & 0xffff {
	case wmLButtonUp, wmRButtonUp, wmContextMenu, ninSelect, ninKeySelect:
		return true
	default:
		return false
	}
}

func (d *Display) openTrayMenu() {
	menu, err := createPopupMenu()
	if err != nil {
		log.Printf("windows: opening system tray menu: %v", err)
		return
	}
	defer destroyMenu(menu)

	header, err := syscall.UTF16PtrFromString(d.trayTitle)
	if err != nil {
		return
	}
	exit, err := syscall.UTF16PtrFromString("Exit")
	if err != nil {
		return
	}
	if err := appendMenu(menu, mfString|mfGrayed|mfDisabled, 0, header); err != nil {
		log.Printf("windows: building system tray menu: %v", err)
		return
	}
	if err := appendMenu(menu, mfSeparator, 0, nil); err != nil {
		log.Printf("windows: building system tray menu: %v", err)
		return
	}
	if err := appendMenu(menu, mfString, trayExitCommand, exit); err != nil {
		log.Printf("windows: building system tray menu: %v", err)
		return
	}

	var position point
	if !getCursorPos(&position) {
		return
	}
	// A notification icon is not itself foreground-capable. Giving its hidden
	// top-level window the foreground before tracking is what makes the menu
	// dismiss normally when the user clicks elsewhere.
	setForegroundWindow(d.tray)
	command := trackPopupMenu(menu, tpmRightButton|tpmNonotify|tpmReturnCmd,
		position.X, position.Y, d.tray)
	postMessage(d.tray, wmNull, 0, 0)
	if command == trayExitCommand {
		d.emit(ui.ExitRequest{})
	}
}

// setFixedUTF16 writes a NUL-terminated string into a Win32 fixed-size field.
// Truncation never leaves half of a surrogate pair at the end.
func setFixedUTF16(destination []uint16, value string) {
	clear(destination)
	if len(destination) == 0 {
		return
	}
	encoded := utf16.Encode([]rune(value))
	limit := min(len(encoded), len(destination)-1)
	if limit > 0 && encoded[limit-1] >= 0xd800 && encoded[limit-1] <= 0xdbff {
		limit--
	}
	copy(destination, encoded[:limit])
}
