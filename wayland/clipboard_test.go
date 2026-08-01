//go:build linux

package wayland

import "testing"

func TestUnavailableClipboardIsNil(t *testing.T) {
	d := &Display{}
	if clipboard := d.Clipboard(); clipboard != nil {
		t.Fatalf("Clipboard() = %#v, want nil", clipboard)
	}
}

func TestPreferredWaylandMIME(t *testing.T) {
	if got := preferredWaylandMIME([]string{"application/json", "text/plain", "text/plain;charset=utf-8"}); got != "text/plain;charset=utf-8" {
		t.Fatalf("preferred MIME = %q", got)
	}
	if got := preferredWaylandMIME([]string{"application/json"}); got != "" {
		t.Fatalf("unsupported MIME selection = %q", got)
	}
}

func TestWireString(t *testing.T) {
	data := make([]byte, 12)
	// The length includes the NUL, while the event payload is padded to four
	// bytes just like a request string.
	data[0] = 6
	copy(data[4:], "hello")
	if got := wireString(data); got != "hello" {
		t.Fatalf("wire string = %q", got)
	}
	if got := wireString([]byte{100, 0, 0, 0}); got != "" {
		t.Fatalf("malformed wire string = %q", got)
	}
}
