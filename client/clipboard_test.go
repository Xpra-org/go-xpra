package client

import (
	"reflect"
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

type recordingClipboard struct {
	texts []string
	err   error
}

func (c *recordingClipboard) SetText(text string) error {
	c.texts = append(c.texts, text)
	return c.err
}

func readyClipboardClient(t *testing.T, compatible bool) (*Client, *protocol.Conn, *recordingClipboard) {
	t.Helper()
	setBackwardsCompatible(t, compatible)
	c, server, _ := outboundHarness(t)
	clipboard := &recordingClipboard{}
	c.clipboard = clipboard
	c.configureClipboard(protocol.Dict{
		"enabled": true, "direction": "both", "selections": []any{"CLIPBOARD"},
	})
	receiveOutbound(t, server, "clipboard-enable-selections")
	return c, server, clipboard
}

func TestLocalClipboardPackets(t *testing.T) {
	for _, compatible := range []bool{true, false} {
		t.Run(map[bool]string{true: "legacy", false: "modern"}[compatible], func(t *testing.T) {
			c, server, _ := readyClipboardClient(t, compatible)
			c.handleClipboardChange(ui.ClipboardChange{Text: "สวัสดี\nworld"})
			wantType := "clipboard-data"
			if compatible {
				wantType = "clipboard-token"
			}
			packet := receiveOutbound(t, server, wantType)
			if packet.Str(1) != clipboardSelection {
				t.Fatalf("selection = %q", packet.Str(1))
			}
			if compatible {
				if got, _ := packet.Bytes(7); string(got) != "สวัสดี\nworld" {
					t.Errorf("legacy clipboard data = %q", got)
				}
			} else if packet.Dict(2)["data"] == nil {
				t.Error("modern clipboard packet has no eager data")
			}
			// An echo caused by the native SetText notification is suppressed.
			c.handleClipboardChange(ui.ClipboardChange{Text: "สวัสดี\nworld"})
		})
	}
}

func TestRemoteClipboardEagerAndLazy(t *testing.T) {
	c, server, clipboard := readyClipboardClient(t, false)
	c.handleClipboardData(protocol.Packet{
		"clipboard-data", "CLIPBOARD", map[string]any{
			"claim": true,
			"data": map[string]any{
				"UTF8_STRING": []any{"UTF8_STRING", int64(8), "bytes", []byte("remote")},
			},
		},
	})
	if !reflect.DeepEqual(clipboard.texts, []string{"remote"}) {
		t.Fatalf("applied clipboard texts = %q", clipboard.texts)
	}

	c.handleClipboardData(protocol.Packet{
		"clipboard-data", "CLIPBOARD", map[string]any{
			"claim": true, "targets": []any{"UTF8_STRING"},
		},
	})
	request := receiveOutbound(t, server, "clipboard-request")
	if request.Str(2) != "CLIPBOARD" || request.Str(3) != "UTF8_STRING" {
		t.Fatalf("clipboard request = %v", request)
	}
	c.handleClipboardContents(protocol.Packet{
		"clipboard-contents", request.Int(1), "CLIPBOARD", "UTF8_STRING", int64(8), "bytes", []byte("lazy"), int64(0),
	})
	if !reflect.DeepEqual(clipboard.texts, []string{"remote", "lazy"}) {
		t.Fatalf("applied clipboard texts = %q", clipboard.texts)
	}
}

func TestStaleClipboardReplyIsIgnored(t *testing.T) {
	c, server, clipboard := readyClipboardClient(t, true)
	c.handleClipboardToken(protocol.Packet{"clipboard-token", "CLIPBOARD", []any{"UTF8_STRING"}})
	request := receiveOutbound(t, server, "clipboard-request")
	c.handleClipboardToken(protocol.Packet{
		"clipboard-token", "CLIPBOARD", []any{"UTF8_STRING"},
		"UTF8_STRING", "UTF8_STRING", int64(8), "bytes", []byte("new"), true, true,
	})
	c.handleClipboardContents(protocol.Packet{
		"clipboard-contents", request.Int(1), "CLIPBOARD", "UTF8_STRING", int64(8), "bytes", []byte("old"), int64(0),
	})
	if !reflect.DeepEqual(clipboard.texts, []string{"new"}) {
		t.Fatalf("applied clipboard texts = %q", clipboard.texts)
	}
}

func TestClipboardRequestReplies(t *testing.T) {
	c, server, _ := readyClipboardClient(t, false)
	c.handleClipboardChange(ui.ClipboardChange{Text: "local"})
	receiveOutbound(t, server, "clipboard-data")

	c.handleClipboardRequest(protocol.Packet{"clipboard-request", int64(7), "CLIPBOARD", "TARGETS"})
	targets := receiveOutbound(t, server, "clipboard-contents")
	if targets.Int(1) != 7 || targets.Str(3) != "ATOM" || targets.Str(5) != "atoms" {
		t.Fatalf("TARGETS response = %v", targets)
	}
	c.handleClipboardRequest(protocol.Packet{"clipboard-request", int64(8), "CLIPBOARD", "UTF8_STRING"})
	contents := receiveOutbound(t, server, "clipboard-contents")
	if got, _ := contents.Bytes(6); string(got) != "local" {
		t.Fatalf("text response = %q", got)
	}
	c.handleClipboardRequest(protocol.Packet{"clipboard-request", int64(9), "PRIMARY", "UTF8_STRING"})
	receiveOutbound(t, server, "clipboard-contents-none")
}

func TestClipboardDirection(t *testing.T) {
	c, server, clipboard := readyClipboardClient(t, false)
	c.configureClipboard(protocol.Dict{
		"direction": "to-server", "selections": []any{"CLIPBOARD"},
	})
	receiveOutbound(t, server, "clipboard-enable-selections")
	c.handleClipboardData(protocol.Packet{
		"clipboard-data", "CLIPBOARD", map[string]any{
			"data": map[string]any{
				"UTF8_STRING": []any{"UTF8_STRING", int64(8), "bytes", []byte("blocked")},
			},
		},
	})
	if len(clipboard.texts) != 0 {
		t.Errorf("received clipboard despite to-server direction: %q", clipboard.texts)
	}
}

func TestClipboardLatin1StringFallback(t *testing.T) {
	got, ok := clipboardWireText("STRING", 8, "bytes", []byte{0x63, 0x61, 0x66, 0xe9})
	if !ok || got != "café" {
		t.Fatalf("decoded STRING = %q, %v", got, ok)
	}
}
