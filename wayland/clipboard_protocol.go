//go:build linux

package wayland

import (
	"io"
	"os"
	"reflect"
	"syscall"
	"unsafe"

	"github.com/rajveermalviya/go-wayland/wayland/client"
)

// The pinned bindings mishandle nullable object ids in wl_data_device events
// and do not register server-created wl_data_offer ids. These compact proxies
// implement just the clipboard subset correctly; the Context registration
// shim is safe for the pinned Context layout and can disappear when the
// dependency grows a public server-id registration API.
type dataDeviceManager struct{ client.BaseProxy }

func newDataDeviceManager(ctx *client.Context) *dataDeviceManager {
	m := &dataDeviceManager{}
	ctx.Register(m)
	return m
}

func (*dataDeviceManager) Dispatch(uint32, int, []byte) {}

func (m *dataDeviceManager) createSource() (*dataSource, error) {
	s := &dataSource{}
	m.Context().Register(s)
	request := newRequest(m, 0, 4)
	client.PutUint32(request[requestHeader:], s.ID())
	if err := m.Context().WriteMsg(request, nil); err != nil {
		m.Context().Unregister(s)
		return nil, err
	}
	return s, nil
}

func (m *dataDeviceManager) getDevice(seat *client.Seat) (*dataDevice, error) {
	d := &dataDevice{}
	m.Context().Register(d)
	request := newRequest(m, 1, 8)
	client.PutUint32(request[requestHeader:requestHeader+4], d.ID())
	client.PutUint32(request[requestHeader+4:], seat.ID())
	if err := m.Context().WriteMsg(request, nil); err != nil {
		m.Context().Unregister(d)
		return nil, err
	}
	return d, nil
}

type dataDevice struct {
	client.BaseProxy
	onOffer     func(*dataOffer)
	onSelection func(*dataOffer)
}

func (d *dataDevice) setSelection(source *dataSource, serial uint32) error {
	request := newRequest(d, 1, 8)
	if source != nil {
		client.PutUint32(request[requestHeader:requestHeader+4], source.ID())
	}
	client.PutUint32(request[requestHeader+4:], serial)
	return d.Context().WriteMsg(request, nil)
}

func (d *dataDevice) Dispatch(opcode uint32, _ int, data []byte) {
	if len(data) < 4 {
		return
	}
	id := client.Uint32(data[:4])
	switch opcode {
	case 0: // data_offer(new_id)
		offer := &dataOffer{}
		registerServerProxy(d.Context(), id, offer)
		if d.onOffer != nil {
			d.onOffer(offer)
		}
	case 5: // selection(nullable data_offer)
		var offer *dataOffer
		if id != 0 {
			offer, _ = d.Context().GetProxy(id).(*dataOffer)
		}
		if d.onSelection != nil {
			d.onSelection(offer)
		}
	}
}

type dataOffer struct {
	client.BaseProxy
	mimeTypes []string
}

func (o *dataOffer) Dispatch(opcode uint32, fd int, data []byte) {
	if fd >= 0 {
		syscall.Close(fd)
	}
	if opcode == 0 {
		o.mimeTypes = append(o.mimeTypes, wireString(data))
	}
}

func (o *dataOffer) receive(mimeType string, fd int) error {
	request := newRequest(o, 1, stringSize(mimeType))
	putString(request[requestHeader:], mimeType)
	return o.Context().WriteMsg(request, syscall.UnixRights(fd))
}

func (o *dataOffer) destroy() error {
	defer o.Context().Unregister(o)
	return o.Context().WriteMsg(newRequest(o, 2, 0), nil)
}

type dataSource struct {
	client.BaseProxy
	text        string
	onCancelled func()
}

func (s *dataSource) offer(mimeType string) error {
	return stringRequest(s, 0, mimeType)
}

func (s *dataSource) Dispatch(opcode uint32, fd int, data []byte) {
	switch opcode {
	case 1: // send(mime_type, fd)
		if fd < 0 {
			return
		}
		text := s.text
		go func() {
			file := os.NewFile(uintptr(fd), "wayland-clipboard")
			if file == nil {
				syscall.Close(fd)
				return
			}
			_, _ = io.WriteString(file, text)
			_ = file.Close()
		}()
	case 2: // cancelled
		if fd >= 0 {
			syscall.Close(fd)
		}
		if s.onCancelled != nil {
			s.onCancelled()
		}
	default:
		if fd >= 0 {
			syscall.Close(fd)
		}
	}
}

func (s *dataSource) destroy() error {
	defer s.Context().Unregister(s)
	return s.Context().WriteMsg(newRequest(s, 1, 0), nil)
}

func wireString(data []byte) string {
	if len(data) < 4 {
		return ""
	}
	n := int(client.Uint32(data[:4]))
	if n <= 0 || n > len(data)-4 {
		return ""
	}
	return string(data[4 : 4+n-1])
}

func registerServerProxy(ctx *client.Context, id uint32, proxy client.Proxy) {
	proxy.SetID(id)
	proxy.SetContext(ctx)
	objectsField := reflect.ValueOf(ctx).Elem().FieldByName("objects")
	objects := *(*map[uint32]client.Proxy)(unsafe.Pointer(objectsField.UnsafeAddr()))
	objects[id] = proxy
}
