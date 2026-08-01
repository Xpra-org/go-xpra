package client

import (
	"bytes"
	"encoding/binary"
	"math/big"
	"reflect"
	"testing"

	"github.com/Xpra-org/go-xpra/protocol"
)

func TestMmapTokenRoundTrip(t *testing.T) {
	token := new(big.Int).Lsh(big.NewInt(1), 120)
	token.Add(token, big.NewInt(0x1234))
	data := make([]byte, 256)
	if err := writeMmapToken(data, token, 32, mmapTokenBytes); err != nil {
		t.Fatalf("writeMmapToken: %v", err)
	}
	got, err := readMmapToken(data, 32, mmapTokenBytes)
	if err != nil {
		t.Fatalf("readMmapToken: %v", err)
	}
	if got.Cmp(token) != 0 {
		t.Errorf("token = %x, want %x", got, token)
	}
}

func TestMmapEnableFromServerCapabilities(t *testing.T) {
	area := &mmapArea{data: make([]byte, 512)}
	serverToken := new(big.Int).Lsh(big.NewInt(1), 100)
	serverToken.Add(serverToken, big.NewInt(99))
	if err := writeMmapToken(area.data, serverToken, 200, mmapTokenBytes); err != nil {
		t.Fatal(err)
	}
	caps := protocol.Dict{"write": map[string]any{
		"enabled": true, "token": serverToken,
		"token_index": int64(200), "token_bytes": int64(mmapTokenBytes),
	}}
	accepted, err := area.enableFromCaps(caps)
	if err != nil {
		t.Fatalf("enableFromCaps: %v", err)
	}
	if !accepted || !area.enabled {
		t.Error("valid server mmap capabilities were not accepted")
	}

	area.enabled = false
	caps["write"].(map[string]any)["token"] = big.NewInt(1)
	if _, err := area.enableFromCaps(caps); err == nil {
		t.Error("mismatched mmap token was accepted")
	}
}

func TestMmapEnableAcceptsLegacyCapabilities(t *testing.T) {
	area := &mmapArea{data: make([]byte, 256)}
	token := big.NewInt(12345)
	if err := writeMmapToken(area.data, token, 64, 16); err != nil {
		t.Fatal(err)
	}
	accepted, err := area.enableFromCaps(protocol.Dict{
		"token": token, "token_index": int64(64), "token_bytes": int64(16),
	})
	if err != nil || !accepted {
		t.Fatalf("legacy capabilities: accepted=%v, err=%v", accepted, err)
	}
}

func TestMmapReadChunks(t *testing.T) {
	area := &mmapArea{data: make([]byte, 64), enabled: true}
	copy(area.data[20:24], []byte{1, 2, 3, 4})
	pixels, release, err := area.readChunks([]any{[]any{int64(20), int64(4)}})
	if err != nil {
		t.Fatalf("readChunks: %v", err)
	}
	if !bytes.Equal(pixels, []byte{1, 2, 3, 4}) {
		t.Errorf("pixels = %v", pixels)
	}
	if got := binary.NativeEndian.Uint32(area.data[:4]); got != 0 {
		t.Errorf("read cursor advanced before release: %d", got)
	}
	release()
	if got := binary.NativeEndian.Uint32(area.data[:4]); got != 24 {
		t.Errorf("read cursor = %d, want 24", got)
	}

	copy(area.data[60:64], []byte{5, 6, 7, 8})
	copy(area.data[8:11], []byte{9, 10, 11})
	pixels, release, err = area.readChunks([]any{
		[]any{int64(60), int64(4)}, []any{int64(8), int64(3)},
	})
	if err != nil {
		t.Fatalf("read wrapped chunks: %v", err)
	}
	if !bytes.Equal(pixels, []byte{5, 6, 7, 8, 9, 10, 11}) {
		t.Errorf("wrapped pixels = %v", pixels)
	}
	release()
	if got := binary.NativeEndian.Uint32(area.data[:4]); got != 11 {
		t.Errorf("wrapped read cursor = %d, want 11", got)
	}
}

func TestMmapReadChunksRejectsMalformedDescriptors(t *testing.T) {
	area := &mmapArea{data: make([]byte, 64), enabled: true}
	tests := []any{
		nil,
		[]any{},
		[]any{[]any{int64(4), int64(1)}},
		[]any{[]any{int64(60), int64(8)}},
		[]any{[]any{int64(8), int64(-1)}},
		[]any{[]any{int64(8), int64(0)}},
		[]any{[]any{int64(8)}},
		[]any{[]any{int64(8), int64(1)}, []any{int64(9), int64(1)}, []any{int64(10), int64(1)}},
	}
	for _, raw := range tests {
		if _, _, err := area.readChunks(raw); err == nil {
			t.Errorf("readChunks(%#v) succeeded", raw)
		}
	}
}

func TestHelloAdvertisesMmapReadArea(t *testing.T) {
	setBackwardsCompatible(t, true)
	client, server, _ := outboundHarness(t)
	client.mmap = &mmapArea{
		data: make([]byte, 1024), filename: "/tmp/test.mmap",
		token: big.NewInt(12345), tokenIndex: 64, tokenBytes: mmapTokenBytes,
	}
	if err := client.sendHello("", nil); err != nil {
		t.Fatalf("sendHello: %v", err)
	}
	hello := receiveOutbound(t, server, "hello").Dict(1)
	mmapCaps := hello.Dict("mmap")
	read := mmapCaps.Dict("read")
	if read == nil || read.Str("file") != "/tmp/test.mmap" || read.Int("size") != 1024 {
		t.Fatalf("mmap.read capabilities = %#v", read)
	}
	if mmapCaps.Str("file") != read.Str("file") {
		t.Error("legacy unprefixed mmap capabilities are missing")
	}
	if token, ok := mmapInteger(read["token"]); !ok || token.Cmp(big.NewInt(12345)) != 0 {
		t.Errorf("mmap token = %#v", read["token"])
	}
}

func TestMmapDrawAndRelease(t *testing.T) {
	setBackwardsCompatible(t, true)
	client, server, window := outboundHarness(t)
	client.windows[7] = window
	client.mmap = &mmapArea{data: make([]byte, 64), enabled: true}
	copy(client.mmap.data[16:24], []byte{1, 2, 3, 4, 5, 6, 7, 8})

	client.handleDraw(protocol.Packet{
		"window-draw", int64(7), int64(0), int64(0), int64(2), int64(1),
		"mmap", []byte{}, int64(42), int64(8), map[string]any{
			"rgb_format": "BGRX",
			"chunks":     []any{[]any{int64(16), int64(8)}},
		},
	})
	ack := receiveOutbound(t, server, "window-draw-ack")
	if ack.Int(5) <= 0 {
		t.Errorf("mmap draw failed: %v", ack)
	}
	if !reflect.DeepEqual(window.painted, []byte{1, 2, 3, 4, 5, 6, 7, 8}) ||
		window.paintStride != 8 || window.paintFormat != "BGRX" {
		t.Errorf("paint = %v, stride=%d, format=%q", window.painted, window.paintStride, window.paintFormat)
	}
	if got := binary.NativeEndian.Uint32(client.mmap.data[:4]); got != 24 {
		t.Errorf("read cursor = %d, want 24", got)
	}
}

func TestMmapDrawForMissingWindowStillReleasesData(t *testing.T) {
	setBackwardsCompatible(t, true)
	client, server, _ := outboundHarness(t)
	client.mmap = &mmapArea{data: make([]byte, 64), enabled: true}

	client.handleDraw(protocol.Packet{
		"window-draw", int64(99), int64(0), int64(0), int64(1), int64(1),
		"mmap", []any{[]any{int64(32), int64(4)}}, int64(7), int64(4),
		map[string]any{"rgb_format": "BGRX"},
	})
	ack := receiveOutbound(t, server, "window-draw-ack")
	if ack.Int(5) != decodeNotFound {
		t.Errorf("decode time = %d, want %d", ack.Int(5), decodeNotFound)
	}
	if got := binary.NativeEndian.Uint32(client.mmap.data[:4]); got != 36 {
		t.Errorf("read cursor = %d, want 36", got)
	}
}
