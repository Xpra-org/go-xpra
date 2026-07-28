//go:build linux

package wayland

import (
	"fmt"
	"log"
	"os"
	"strconv"

	"github.com/rajveermalviya/go-wayland/wayland/client"
	"github.com/rajveermalviya/go-wayland/wayland/cursor"

	"github.com/Xpra-org/go-xpra/ui"
)

// defaultCursorSize is the theme size to load when the desktop does not say,
// which is the size every cursor theme is expected to ship.
const defaultCursorSize = 24

// shownCursor is the image currently handed to the pointer.
//
// It is kept because the compositor forgets the cursor whenever the pointer
// leaves our surfaces, so every enter has to set it again.
type shownCursor struct {
	buffer             *client.Buffer
	width, height      int
	hotspotX, hotspotY int
}

// ownedCursor is the shared memory behind a cursor the server sent us, as
// opposed to one that came out of the local cursor theme and belongs to it.
type ownedCursor struct {
	memory *sharedMemory
	pool   *client.ShmPool
	buffer *client.Buffer
}

func (c *ownedCursor) release() {
	if c == nil {
		return
	}
	if c.buffer != nil {
		c.buffer.Destroy()
	}
	if c.pool != nil {
		c.pool.Destroy()
	}
	c.memory.close()
}

// SetCursor applies one pointer image to every forwarded window.
//
// Wayland has no cursor attribute on a window. Instead a client owns the
// pointer image for as long as the pointer is over one of its surfaces, and
// says what it should be by attaching a buffer to a surface of its own. So
// there is nothing per-window to do here — one surface serves the whole
// session, which is exactly the scope the server sets cursors at.
func (d *Display) SetCursor(image *ui.Cursor) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.pointer == nil {
		// No pointer on this seat, so no cursor to speak of.
		return nil
	}
	if image == nil {
		return d.showThemeCursor()
	}
	if err := image.Validate(); err != nil {
		return err
	}

	owned, err := d.newCursorBuffer(image)
	if err != nil {
		return err
	}
	err = d.showCursor(&shownCursor{
		buffer: owned.buffer,
		width:  image.Width, height: image.Height,
		hotspotX: image.HotspotX, hotspotY: image.HotspotY,
	})
	if err != nil {
		owned.release()
		return err
	}
	// Only once the new image is up is the old one safe to let go of.
	d.ownedCursor.release()
	d.ownedCursor = owned
	return nil
}

// newCursorBuffer copies a cursor into memory shared with the compositor.
//
// ui.Cursor is alpha-premultiplied BGRA, which is wl_shm's argb8888 — a
// little-endian 32-bit A:R:G:B — so the pixels go across as they are, the same
// way window pixels do.
func (d *Display) newCursorBuffer(image *ui.Cursor) (*ownedCursor, error) {
	stride := image.Width * ui.BytesPerPixel
	size := stride * image.Height

	memory, err := newSharedMemory(size)
	if err != nil {
		return nil, err
	}
	owned := &ownedCursor{memory: memory}
	copy(memory.bytes, image.Pixels)

	if owned.pool, err = d.shm.CreatePool(int(memory.file.Fd()), int32(size)); err != nil {
		owned.release()
		return nil, fmt.Errorf("sharing a cursor with the compositor: %w", err)
	}
	owned.buffer, err = owned.pool.CreateBuffer(0, int32(image.Width), int32(image.Height),
		int32(stride), uint32(client.ShmFormatArgb8888))
	if err != nil {
		owned.release()
		return nil, fmt.Errorf("creating a %dx%d cursor buffer: %w", image.Width, image.Height, err)
	}
	return owned, nil
}

// showThemeCursor puts the desktop's own pointer back, which is what the server
// asks for when the remote application wants no cursor of its own.
//
// A client that has never set a cursor is already showing the compositor's, and
// has nothing to do. One that has cannot simply stop: there is no request that
// hands the pointer back, so the default has to be loaded from the local cursor
// theme and set like any other.
func (d *Display) showThemeCursor() error {
	if d.shown == nil {
		return nil
	}
	image := d.themeCursor()
	if image == nil {
		// Leaving the last cursor up is less unhelpful than hiding the pointer.
		return nil
	}
	buffer, err := image.GetBuffer()
	if err != nil {
		return fmt.Errorf("reading the default cursor: %w", err)
	}
	if err := d.showCursor(&shownCursor{
		buffer: buffer,
		width:  int(image.Width), height: int(image.Height),
		hotspotX: int(image.HotspotX), hotspotY: int(image.HotspotY),
	}); err != nil {
		return err
	}
	d.ownedCursor.release()
	d.ownedCursor = nil
	return nil
}

// themeCursor returns the local theme's ordinary arrow, loading the theme the
// first time it is wanted. A desktop with no cursor theme installed is reported
// once and then left alone.
func (d *Display) themeCursor() *cursor.Image {
	if d.theme == nil {
		if d.themeMissing {
			return nil
		}
		theme, err := cursor.LoadTheme(os.Getenv("XCURSOR_THEME"), cursorSize(), d.shm)
		if err != nil {
			log.Printf("wayland: loading the cursor theme: %v", err)
			d.themeMissing = true
			return nil
		}
		d.theme = theme
	}
	arrow := d.theme.GetCursor(cursor.LeftPtr)
	if arrow == nil || len(arrow.Images) == 0 {
		return nil
	}
	return &arrow.Images[0]
}

// showCursor attaches a cursor image to our cursor surface and hands that
// surface to the pointer.
func (d *Display) showCursor(next *shownCursor) error {
	if d.cursorSurface == nil {
		surface, err := d.compositor.CreateSurface()
		if err != nil {
			return fmt.Errorf("creating a cursor surface: %w", err)
		}
		d.cursorSurface = surface
	}
	for _, err := range []error{
		d.cursorSurface.Attach(next.buffer, 0, 0),
		d.cursorSurface.DamageBuffer(0, 0, int32(next.width), int32(next.height)),
		d.cursorSurface.Commit(),
		// The serial has to be the one from the enter that gave us the pointer;
		// the compositor rejects a cursor set on the strength of anything else.
		d.pointer.SetCursor(d.enterSerial, d.cursorSurface,
			int32(next.hotspotX), int32(next.hotspotY)),
	} {
		if err != nil {
			return fmt.Errorf("setting the cursor: %w", err)
		}
	}
	d.shown = next
	return nil
}

// restoreCursor re-applies the current cursor after the pointer has entered one
// of our windows, which is when the compositor expects to be told again.
func (d *Display) restoreCursor() {
	if d.shown == nil {
		return
	}
	if err := d.showCursor(d.shown); err != nil {
		log.Printf("wayland: %v", err)
	}
}

// releaseCursor lets go of everything the cursor holds. The theme owns its own
// buffers and is destroyed as a whole.
func (d *Display) releaseCursor() {
	d.ownedCursor.release()
	d.ownedCursor = nil
	d.shown = nil
	if d.theme != nil {
		d.theme.Destroy()
		d.theme = nil
	}
	if d.cursorSurface != nil {
		destroySurface(d.cursorSurface)
		d.cursorSurface = nil
	}
}

// cursorSize reads the size the desktop wants its cursors at, which is how the
// toolkits agree on one.
func cursorSize() int {
	if size, err := strconv.Atoi(os.Getenv("XCURSOR_SIZE")); err == nil && size > 0 {
		return size
	}
	return defaultCursorSize
}
