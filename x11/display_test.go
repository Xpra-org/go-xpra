//go:build linux

package x11

import "testing"

func TestUnavailableClipboardIsNil(t *testing.T) {
	d := &Display{}
	if clipboard := d.Clipboard(); clipboard != nil {
		t.Fatalf("Clipboard() = %#v, want nil", clipboard)
	}
}
