//go:build linux

package wayland

import (
	"fmt"
	"log"

	"github.com/rajveermalviya/go-wayland/wayland/client"

	"github.com/Xpra-org/go-xpra/ui"
)

type ownedWindowIcon struct {
	memory *sharedMemory
	pool   *client.ShmPool
	buffer *client.Buffer
}

func (i *ownedWindowIcon) release() {
	if i == nil {
		return
	}
	if i.buffer != nil {
		i.buffer.Destroy()
	}
	if i.pool != nil {
		i.pool.Destroy()
	}
	i.memory.close()
}

// SetIcon uses xdg-toplevel-icon-v1 when the compositor offers it. Compositors
// without the extension retain the default icon derived from appID.
func (w *Window) SetIcon(image *ui.Icon) error {
	if image != nil {
		if err := image.Validate(); err != nil {
			return err
		}
	}

	w.d.mu.Lock()
	defer w.d.mu.Unlock()
	if w.d.iconManager == nil || w.toplevel == nil {
		return nil
	}
	if image == nil {
		if err := w.d.iconManager.setIcon(w.toplevel, nil); err != nil {
			return fmt.Errorf("resetting the toplevel icon: %w", err)
		}
		return w.surface.Commit()
	}

	owned, err := w.newWindowIconBuffer(image)
	if err != nil {
		return err
	}
	defer owned.release()

	native, err := w.d.iconManager.createIcon()
	if err != nil {
		return fmt.Errorf("creating a toplevel icon: %w", err)
	}
	defer func() {
		if err := native.destroy(); err != nil {
			log.Printf("wayland: destroying a toplevel icon: %v", err)
		}
	}()
	if err := native.addBuffer(owned.buffer, 1); err != nil {
		return fmt.Errorf("adding a toplevel icon buffer: %w", err)
	}
	if err := w.d.iconManager.setIcon(w.toplevel, native); err != nil {
		return fmt.Errorf("setting a toplevel icon: %w", err)
	}
	if err := w.surface.Commit(); err != nil {
		return fmt.Errorf("committing a toplevel icon: %w", err)
	}
	return nil
}

// newWindowIconBuffer copies an icon to a square wl_shm buffer, padding a
// rectangular source transparently as required by xdg-toplevel-icon-v1.
func (w *Window) newWindowIconBuffer(image *ui.Icon) (*ownedWindowIcon, error) {
	dimension := max(image.Width, image.Height)
	stride := dimension * ui.BytesPerPixel
	size := stride * dimension
	memory, err := newSharedMemory(size)
	if err != nil {
		return nil, err
	}
	owned := &ownedWindowIcon{memory: memory}

	x := (dimension - image.Width) / 2
	y := (dimension - image.Height) / 2
	srcStride := image.Width * ui.BytesPerPixel
	for row := range image.Height {
		copy(memory.bytes[(y+row)*stride+x*ui.BytesPerPixel:],
			image.Pixels[row*srcStride:(row+1)*srcStride])
	}

	if owned.pool, err = w.d.shm.CreatePool(int(memory.file.Fd()), int32(size)); err != nil {
		owned.release()
		return nil, fmt.Errorf("sharing a window icon with the compositor: %w", err)
	}
	owned.buffer, err = owned.pool.CreateBuffer(0, int32(dimension), int32(dimension),
		int32(stride), uint32(client.ShmFormatArgb8888))
	if err != nil {
		owned.release()
		return nil, fmt.Errorf("creating a %dx%d window icon buffer: %w",
			dimension, dimension, err)
	}
	return owned, nil
}
