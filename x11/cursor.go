//go:build linux

package x11

import (
	"fmt"

	"github.com/jezek/xgb/render"
	"github.com/jezek/xgb/xproto"

	"github.com/Xpra-org/go-xpra/ui"
)

// initCursorFormat locates the standard 32-bit premultiplied ARGB format.
func (d *Display) initCursorFormat() error {
	if err := render.Init(d.X.Conn()); err != nil {
		return fmt.Errorf("initializing the X Render extension for cursors: %w", err)
	}
	formats, err := render.QueryPictFormats(d.X.Conn()).Reply()
	if err != nil {
		return fmt.Errorf("querying X Render formats: %w", err)
	}
	if formats == nil {
		return fmt.Errorf("querying X Render formats returned no reply")
	}
	for _, format := range formats.Formats {
		direct := format.Direct
		if format.Type == render.PictTypeDirect && format.Depth == 32 &&
			direct.RedShift == 16 && direct.RedMask == 0xff &&
			direct.GreenShift == 8 && direct.GreenMask == 0xff &&
			direct.BlueShift == 0 && direct.BlueMask == 0xff &&
			direct.AlphaShift == 24 && direct.AlphaMask == 0xff {
			d.cursorFormat = format.Id
			return nil
		}
	}
	return fmt.Errorf("the X server has no ARGB32 Render format for colour cursors")
}

// SetCursor changes the cursor attribute on every forwarded window. Keeping
// the native cursor resource on Display also makes windows created later pick
// it up in NewWindow.
func (d *Display) SetCursor(image *ui.Cursor) error {
	var cursor xproto.Cursor
	var err error
	if image != nil {
		cursor, err = d.createCursor(image)
		if err != nil {
			return err
		}
	}

	for id := range d.windows {
		xproto.ChangeWindowAttributes(d.X.Conn(), id,
			xproto.CwCursor, []uint32{uint32(cursor)})
	}
	old := d.cursor
	d.cursor = cursor
	if old != 0 {
		xproto.FreeCursor(d.X.Conn(), old)
	}
	return nil
}

func (d *Display) createCursor(image *ui.Cursor) (xproto.Cursor, error) {
	if err := image.Validate(); err != nil {
		return 0, err
	}
	conn := d.X.Conn()
	pixmap, err := xproto.NewPixmapId(conn)
	if err != nil {
		return 0, fmt.Errorf("allocating a cursor pixmap: %w", err)
	}
	if err := xproto.CreatePixmapChecked(conn, 32, pixmap,
		xproto.Drawable(d.X.RootWin()), uint16(image.Width), uint16(image.Height)).Check(); err != nil {
		return 0, fmt.Errorf("creating a cursor pixmap: %w", err)
	}
	defer xproto.FreePixmap(conn, pixmap)

	gc, err := xproto.NewGcontextId(conn)
	if err != nil {
		return 0, fmt.Errorf("allocating a cursor graphics context: %w", err)
	}
	if err := xproto.CreateGCChecked(conn, gc, xproto.Drawable(pixmap), 0, nil).Check(); err != nil {
		return 0, fmt.Errorf("creating a cursor graphics context: %w", err)
	}
	defer xproto.FreeGC(conn, gc)

	pixels := image.Pixels
	if xproto.Setup(conn).ImageByteOrder == xproto.ImageOrderMSBFirst {
		// ui.Cursor is BGRA in memory, which is ARGB32 on little-endian X
		// servers. Big-endian servers expect those bytes in numeric order.
		pixels = make([]byte, len(image.Pixels))
		for i := 0; i < len(pixels); i += ui.BytesPerPixel {
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] =
				image.Pixels[i+3], image.Pixels[i+2], image.Pixels[i+1], image.Pixels[i]
		}
	}
	stride := image.Width * ui.BytesPerPixel
	rowsPerRequest := max(1, d.maxImageBytes/stride)
	for row := 0; row < image.Height; row += rowsPerRequest {
		rows := min(rowsPerRequest, image.Height-row)
		if err := xproto.PutImageChecked(conn, xproto.ImageFormatZPixmap,
			xproto.Drawable(pixmap), gc, uint16(image.Width), uint16(rows),
			0, int16(row), 0, 32, pixels[row*stride:(row+rows)*stride]).Check(); err != nil {
			return 0, fmt.Errorf("uploading cursor pixels: %w", err)
		}
	}

	picture, err := render.NewPictureId(conn)
	if err != nil {
		return 0, fmt.Errorf("allocating a cursor picture: %w", err)
	}
	if err := render.CreatePictureChecked(conn, picture, xproto.Drawable(pixmap),
		d.cursorFormat, 0, nil).Check(); err != nil {
		return 0, fmt.Errorf("creating a cursor picture: %w", err)
	}
	defer render.FreePicture(conn, picture)

	cursor, err := xproto.NewCursorId(conn)
	if err != nil {
		return 0, fmt.Errorf("allocating a cursor: %w", err)
	}
	if err := render.CreateCursorChecked(conn, cursor, picture,
		uint16(image.HotspotX), uint16(image.HotspotY)).Check(); err != nil {
		return 0, fmt.Errorf("creating a cursor: %w", err)
	}
	return cursor, nil
}
