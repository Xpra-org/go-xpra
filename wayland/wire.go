//go:build linux

package wayland

import "github.com/rajveermalviya/go-wayland/wayland/client"

// The three requests this backend sends that carry a string are built here
// rather than through the bindings, which get the encoding wrong.
//
// A string on the wire is a length, the bytes, a terminating NUL, and padding
// up to the next four-byte boundary. The length counts the bytes and the NUL;
// the bindings write the padded size there instead, and libwayland rejects the
// whole message as "invalid arguments" and closes the connection — which for
// wl_registry.bind means the very first request a client sends. Only the
// framing is redone here: the object ids, the opcodes and everything the
// bindings decode are their own.
//
// The opcodes are an interface's request numbers counted from zero, and are
// fixed for as long as the interface is: xdg-shell has been stable since 2018.
const (
	opcodeSurfaceDestroy   = 0 // wl_surface.destroy
	opcodeRegistryBind     = 0 // wl_registry.bind
	opcodeToplevelSetTitle = 2 // xdg_toplevel.set_title
	opcodeToplevelSetAppID = 3 // xdg_toplevel.set_app_id
)

// destroySurface destroys a surface without dropping it from the object table,
// which the bindings' own Destroy would do at once.
//
// An input event names the surface it happened on, and one can still be on its
// way when the surface goes: closing a window the user was typing into always
// produces a keyboard leave for it. The bindings resolve that name in the
// object table and assume they will find something, so an entry taken away too
// early is not a dropped event but a panic. The object stays until the
// compositor says it has seen the destroy, which is what wl_display.delete_id
// is for and what deleteID in display.go acts on.
func destroySurface(surface *client.Surface) error {
	return surface.Context().WriteMsg(newRequest(surface, opcodeSurfaceDestroy, 0), nil)
}

// bindGlobal asks the registry for one of the globals it announced: which one,
// the interface and version wanted from it, and the id the new object takes.
func bindGlobal(registry *client.Registry, name uint32, iface string, version uint32, proxy client.Proxy) error {
	request := newRequest(registry, opcodeRegistryBind, 4+stringSize(iface)+4+4)
	at := requestHeader
	client.PutUint32(request[at:at+4], name)
	at += 4
	at += putString(request[at:], iface)
	client.PutUint32(request[at:at+4], version)
	at += 4
	client.PutUint32(request[at:at+4], proxy.ID())

	return registry.Context().WriteMsg(request, nil)
}

// stringRequest sends a request whose only argument is a string, which is the
// shape of both the toplevel requests used here.
func stringRequest(proxy client.Proxy, opcode uint32, value string) error {
	request := newRequest(proxy, opcode, stringSize(value))
	putString(request[requestHeader:], value)
	return proxy.Context().WriteMsg(request, nil)
}

// requestHeader is the object id and the packed size and opcode that every
// request begins with.
const requestHeader = 8

// newRequest allocates a request of argBytes arguments, with its header filled
// in and the arguments left to the caller.
func newRequest(proxy client.Proxy, opcode uint32, argBytes int) []byte {
	size := requestHeader + argBytes
	request := make([]byte, size)
	client.PutUint32(request[0:4], proxy.ID())
	client.PutUint32(request[4:8], uint32(size)<<16|opcode)
	return request
}

// putString writes one string argument and reports how much of dst it used.
func putString(dst []byte, value string) int {
	client.PutUint32(dst[0:4], uint32(len(value)+1))
	// The NUL and the padding are already there: the buffer starts zeroed.
	copy(dst[4:], value)
	return stringSize(value)
}

func stringSize(value string) int {
	return 4 + client.PaddedLen(len(value)+1)
}
