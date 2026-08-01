//go:build windows

package win32

import (
	"fmt"
	"log"
	"syscall"
	"unicode/utf8"
	"unsafe"

	"github.com/Xpra-org/go-xpra/ui"
)

const maxClipboardBytes = 16 * 1024 * 1024

var _ ui.Clipboard = (*Display)(nil)

// Clipboard returns the Win32 Unicode text clipboard.
func (d *Display) Clipboard() ui.Clipboard {
	if !d.clipboardAvailable {
		return nil
	}
	return d
}

// SetText publishes UTF-16 CF_UNICODETEXT on the window thread.
func (d *Display) SetText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("clipboard text is not valid UTF-8")
	}
	if len(text) > maxClipboardBytes {
		return fmt.Errorf("clipboard text is larger than %d bytes", maxClipboardBytes)
	}
	units, err := syscall.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("encoding clipboard text: %w", err)
	}
	var clipboardErr error
	if err := d.call(func() {
		if err := openClipboard(d.helper); err != nil {
			clipboardErr = err
			return
		}
		defer closeClipboard()
		if err := emptyClipboard(); err != nil {
			clipboardErr = err
			return
		}
		memory, err := globalAlloc(gmemMoveable|gmemZeroInit, uintptr(len(units)*2))
		if err != nil {
			clipboardErr = err
			return
		}
		ptr, err := globalLock(memory)
		if err != nil {
			globalFree(memory)
			clipboardErr = err
			return
		}
		copy(unsafe.Slice((*uint16)(ptr), len(units)), units)
		globalUnlock(memory)
		if err := setClipboardData(cfUnicodeText, memory); err != nil {
			globalFree(memory)
			clipboardErr = err
			return
		}
		// SetClipboardData owns memory now. Remember its sequence so the
		// listener does not report our remote update back to the server.
		d.clipboardSequence = clipboardSequenceNumber()
	}); err != nil {
		return err
	}
	return clipboardErr
}

func (d *Display) clipboardChanged() {
	sequence := clipboardSequenceNumber()
	if sequence != 0 && sequence == d.clipboardSequence {
		d.clipboardSequence = 0
		return
	}
	text, available, err := d.readClipboardText()
	if err != nil {
		log.Printf("windows: reading clipboard: %v", err)
		return
	}
	if !available {
		text = ""
	}
	if len(text) > maxClipboardBytes || !utf8.ValidString(text) {
		log.Printf("windows: ignoring invalid or oversized clipboard text")
		return
	}
	d.emit(ui.ClipboardChange{Text: text})
}

// readClipboardText runs only on the window thread.
func (d *Display) readClipboardText() (string, bool, error) {
	if !clipboardFormatAvailable(cfUnicodeText) {
		return "", false, nil
	}
	if err := openClipboard(d.helper); err != nil {
		return "", false, err
	}
	defer closeClipboard()
	memory, err := getClipboardData(cfUnicodeText)
	if err != nil {
		return "", false, err
	}
	bytes := globalSize(memory)
	if bytes == 0 || bytes > uintptr(maxClipboardBytes*2+2) {
		return "", false, fmt.Errorf("clipboard allocation has invalid size %d", bytes)
	}
	ptr, err := globalLock(memory)
	if err != nil {
		return "", false, err
	}
	defer globalUnlock(memory)
	units := unsafe.Slice((*uint16)(ptr), int(bytes/2))
	end := 0
	for end < len(units) && units[end] != 0 {
		end++
	}
	return syscall.UTF16ToString(units[:end]), true, nil
}
