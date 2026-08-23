//go:build darwin

package darwin

import (
	"fmt"
	"log"
	"time"
	"unicode/utf8"

	"github.com/ebitengine/purego/objc"

	"github.com/Xpra-org/go-xpra/ui"
)

const maxClipboardBytes = 16 * 1024 * 1024

// clipboardPollInterval is how often this backend checks NSPasteboard's
// change count. AppKit has no clipboard-change notification — a deliberate
// omission — so polling a cheap scalar property is the standard technique
// every cross-platform toolkit on macOS uses in its place.
const clipboardPollInterval = 300 * time.Millisecond

var (
	class_NSPasteboard = objc.GetClass("NSPasteboard")

	sel_generalPasteboard = objc.RegisterName("generalPasteboard")
	sel_clearContents     = objc.RegisterName("clearContents")
	sel_setStringForType  = objc.RegisterName("setString:forType:")
	sel_stringForType     = objc.RegisterName("stringForType:")
	sel_changeCount       = objc.RegisterName("changeCount")
)

var _ ui.Clipboard = (*Display)(nil)

// Clipboard returns the AppKit text clipboard. Unlike win32, where listener
// setup can fail, NSPasteboard is always available, so this never returns
// nil.
func (d *Display) Clipboard() ui.Clipboard { return d }

// nsPasteboardTypeString builds an NSString equal to the real
// NSPasteboardTypeString constant ("public.utf8-plain-text" since macOS
// 10.6). Pasteboard type matching is by string equality, not pointer
// identity, so constructing an equal string here works exactly like the real
// constant would, without needing to resolve AppKit's exported global symbol
// for it.
func nsPasteboardTypeString() objc.ID {
	return nsString("public.utf8-plain-text")
}

// SetText publishes UTF-8 text on the general pasteboard.
func (d *Display) SetText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("clipboard text is not valid UTF-8")
	}
	if len(text) > maxClipboardBytes {
		return fmt.Errorf("clipboard text is larger than %d bytes", maxClipboardBytes)
	}
	return d.call(func() {
		pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
		pasteboard.Send(sel_clearContents)
		pasteboard.Send(sel_setStringForType, nsString(text), nsPasteboardTypeString())
		// Remember the change count this produced, so the poll loop below
		// does not echo our own write back to the server as though the user
		// had copied it — the same role win32's clipboardSequence field
		// plays for its own listener.
		d.clipboardOwnChangeCount = objc.Send[int64](pasteboard, sel_changeCount)
	})
}

func (d *Display) readClipboardText() (string, bool) {
	pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
	value := pasteboard.Send(sel_stringForType, nsPasteboardTypeString())
	if value == 0 {
		return "", false
	}
	return goString(value), true
}

// startClipboardPoll begins watching the pasteboard's change count for
// updates made outside this client.
func (d *Display) startClipboardPoll() {
	d.clipboardStop = make(chan struct{})
	go d.pollClipboard(d.clipboardStop)
}

func (d *Display) pollClipboard(stop chan struct{}) {
	ticker := time.NewTicker(clipboardPollInterval)
	defer ticker.Stop()

	lastSeenChangeCount := int64(-1)
	for {
		select {
		case <-ticker.C:
			var text string
			var available, changed bool
			if err := d.call(func() {
				pasteboard := objc.ID(class_NSPasteboard).Send(sel_generalPasteboard)
				count := objc.Send[int64](pasteboard, sel_changeCount)
				if count == lastSeenChangeCount {
					return
				}
				lastSeenChangeCount = count
				if count == d.clipboardOwnChangeCount {
					return
				}
				text, available = d.readClipboardText()
				changed = true
			}); err != nil {
				return
			}
			if !changed || !available {
				continue
			}
			if len(text) > maxClipboardBytes || !utf8.ValidString(text) {
				log.Printf("darwin: ignoring invalid or oversized clipboard text")
				continue
			}
			d.emit(ui.ClipboardChange{Text: text})
		case <-stop:
			return
		}
	}
}

func (d *Display) stopClipboardPoll() {
	if d.clipboardStop != nil {
		close(d.clipboardStop)
		d.clipboardStop = nil
	}
}
