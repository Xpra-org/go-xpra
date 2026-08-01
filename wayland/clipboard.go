//go:build linux

package wayland

import (
	"fmt"
	"io"
	"log"
	"os"
	"unicode/utf8"

	"github.com/rajveermalviya/go-wayland/wayland/client"

	"github.com/Xpra-org/go-xpra/ui"
)

const maxClipboardBytes = 16 * 1024 * 1024

var waylandTextMIMEs = []string{"text/plain;charset=utf-8", "text/plain", "UTF8_STRING"}

type clipboard struct {
	d       *Display
	manager *dataDeviceManager
	device  *dataDevice
	offer   *dataOffer
	source  *dataSource
	pending *string
}

var _ ui.Clipboard = (*clipboard)(nil)

func newClipboard(d *Display, manager *dataDeviceManager, seat *client.Seat) (*clipboard, error) {
	device, err := manager.getDevice(seat)
	if err != nil {
		return nil, fmt.Errorf("creating data device: %w", err)
	}
	c := &clipboard{d: d, manager: manager, device: device}
	device.onSelection = c.selection
	return c, nil
}

func (c *clipboard) SetText(text string) error {
	if !utf8.ValidString(text) {
		return fmt.Errorf("clipboard text is not valid UTF-8")
	}
	if len(text) > maxClipboardBytes {
		return fmt.Errorf("clipboard text is larger than %d bytes", maxClipboardBytes)
	}
	c.d.mu.Lock()
	defer c.d.mu.Unlock()
	if c.d.inputSerial == 0 {
		pending := text
		c.pending = &pending
		return nil
	}
	return c.publish(text)
}

func (c *clipboard) publish(text string) error {
	source, err := c.manager.createSource()
	if err != nil {
		return err
	}
	source.text = text
	for _, mimeType := range waylandTextMIMEs {
		if err := source.offer(mimeType); err != nil {
			_ = source.destroy()
			return err
		}
	}
	source.onCancelled = func() {
		if c.source == source {
			c.source = nil
		}
		_ = source.destroy()
	}
	if err := c.device.setSelection(source, c.d.inputSerial); err != nil {
		_ = source.destroy()
		return err
	}
	c.source = source
	c.pending = nil
	return nil
}

func (c *clipboard) selection(offer *dataOffer) {
	if offer == nil && c.source != nil {
		// Compositors do not create an offer for the client which supplied the
		// current source. This is our own SetText, not an empty local copy.
		return
	}
	if c.offer != nil && c.offer != offer {
		_ = c.offer.destroy()
	}
	c.offer = offer
	if offer == nil {
		c.d.emit(ui.ClipboardChange{Text: ""})
		return
	}
	mimeType := preferredWaylandMIME(offer.mimeTypes)
	if mimeType == "" {
		return
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		log.Printf("wayland: creating clipboard pipe: %v", err)
		return
	}
	if err := offer.receive(mimeType, int(writer.Fd())); err != nil {
		reader.Close()
		writer.Close()
		log.Printf("wayland: requesting clipboard text: %v", err)
		return
	}
	writer.Close()
	go c.readOffer(reader)
}

func (c *clipboard) readOffer(reader *os.File) {
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, maxClipboardBytes+1))
	if err != nil {
		log.Printf("wayland: reading clipboard text: %v", err)
		return
	}
	if len(data) > maxClipboardBytes || !utf8.Valid(data) {
		log.Printf("wayland: ignoring invalid or oversized clipboard text")
		return
	}
	c.d.emit(ui.ClipboardChange{Text: string(data)})
}

func preferredWaylandMIME(offered []string) string {
	for _, preferred := range waylandTextMIMEs {
		for _, mimeType := range offered {
			if mimeType == preferred {
				return preferred
			}
		}
	}
	return ""
}

func (d *Display) setInputSerial(serial uint32) {
	if serial == 0 {
		return
	}
	d.inputSerial = serial
	if d.clipboard != nil && d.clipboard.pending != nil {
		if err := d.clipboard.publish(*d.clipboard.pending); err != nil {
			log.Printf("wayland: publishing queued clipboard text: %v", err)
		}
	}
}

func (d *Display) releaseClipboard() {
	if d.clipboard == nil {
		return
	}
	if d.clipboard.offer != nil {
		_ = d.clipboard.offer.destroy()
	}
	if d.clipboard.source != nil {
		_ = d.clipboard.source.destroy()
	}
}
