//go:build linux

package wayland

import (
	"github.com/rajveermalviya/go-wayland/wayland/client"
	xdg_shell "github.com/rajveermalviya/go-wayland/wayland/stable/xdg-shell"
)

// These two small proxies implement the requests from
// xdg-toplevel-icon-v1. The dependency predates that protocol, and carrying
// just the fixed wire messages avoids replacing all of its generated Wayland
// bindings.
type toplevelIconManager struct {
	client.BaseProxy
}

func newToplevelIconManager(ctx *client.Context) *toplevelIconManager {
	manager := &toplevelIconManager{}
	ctx.Register(manager)
	return manager
}

// Dispatch consumes the optional icon_size and done events. The server already
// scales Xpra icons to a compact size, so the compositor's preferences do not
// require a response here.
func (*toplevelIconManager) Dispatch(_ uint32, _ int, _ []byte) {}

func (m *toplevelIconManager) createIcon() (*toplevelIcon, error) {
	icon := &toplevelIcon{}
	m.Context().Register(icon)
	request := newRequest(m, 1, 4) // create_icon(new_id)
	client.PutUint32(request[requestHeader:], icon.ID())
	if err := m.Context().WriteMsg(request, nil); err != nil {
		m.Context().Unregister(icon)
		return nil, err
	}
	return icon, nil
}

func (m *toplevelIconManager) setIcon(toplevel *xdg_shell.Toplevel, icon *toplevelIcon) error {
	request := newRequest(m, 2, 8) // set_icon(toplevel, icon)
	client.PutUint32(request[requestHeader:requestHeader+4], toplevel.ID())
	var iconID uint32
	if icon != nil {
		iconID = icon.ID()
	}
	client.PutUint32(request[requestHeader+4:], iconID)
	return m.Context().WriteMsg(request, nil)
}

type toplevelIcon struct {
	client.BaseProxy
}

func (i *toplevelIcon) addBuffer(buffer *client.Buffer, scale int32) error {
	request := newRequest(i, 2, 8) // add_buffer(buffer, scale)
	client.PutUint32(request[requestHeader:requestHeader+4], buffer.ID())
	client.PutUint32(request[requestHeader+4:], uint32(scale))
	return i.Context().WriteMsg(request, nil)
}

func (i *toplevelIcon) destroy() error {
	defer i.Context().Unregister(i)
	return i.Context().WriteMsg(newRequest(i, 0, 0), nil)
}
