package protocol

import (
	"encoding/binary"
	"encoding/hex"
	"net"
	"testing"
	"time"

	"github.com/pierrec/lz4/v4"

	"github.com/Xpra-org/go-xpra/rencodeplus"
)

func TestEncodeHeader(t *testing.T) {
	got := encodeHeader(FlagRencodeplus|FlagFlush, 9)
	// 'P', rencodeplus|flush, no compression, main packet, length 9.
	want, _ := hex.DecodeString("5018000000000009")
	if string(got) != string(want) {
		t.Errorf("encodeHeader = %x, want %x", got, want)
	}
}

func TestParseHeader(t *testing.T) {
	h, err := parseHeader([]byte{'P', FlagRencodeplus, 0x11, 0, 0, 0, 0x01, 0x00})
	if err != nil {
		t.Fatalf("parseHeader: %v", err)
	}
	if h.flags != FlagRencodeplus {
		t.Errorf("flags = %#x", h.flags)
	}
	if h.compression != 0x11 { // lz4 at level 1
		t.Errorf("compression = %#x", h.compression)
	}
	if h.length != 256 {
		t.Errorf("length = %d, want 256", h.length)
	}
}

func TestParseHeaderRejectsBadInput(t *testing.T) {
	cases := map[string][]byte{
		"short":       {'P', 0, 0, 0},
		"bad magic":   {'X', FlagRencodeplus, 0, 0, 0, 0, 0, 1},
		"zero length": {'P', FlagRencodeplus, 0, 0, 0, 0, 0, 0},
		"oversized":   {'P', FlagRencodeplus, 0, 0, 0xff, 0xff, 0xff, 0xff},
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseHeader(b); err == nil {
				t.Errorf("parseHeader(%x) succeeded, want error", b)
			}
		})
	}
}

// frame builds a complete packet the way a server would, so the reader can be
// exercised without one.
func frame(t *testing.T, packet []any, compress bool) []byte {
	t.Helper()
	payload, err := rencodeplus.Encode(packet)
	if err != nil {
		t.Fatalf("encoding fixture: %v", err)
	}
	compression := byte(0)
	if compress {
		// xpra's framing: 4-byte little-endian uncompressed size, then a raw
		// LZ4 block.
		buf := make([]byte, 4+lz4.CompressBlockBound(len(payload)))
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(payload)))
		var c lz4.Compressor
		n, err := c.CompressBlock(payload, buf[4:])
		if err != nil {
			t.Fatalf("compressing fixture: %v", err)
		}
		if n == 0 {
			t.Skip("payload is incompressible; nothing to test")
		}
		payload = buf[:4+n]
		compression = compressionLZ4 | 1
	}
	header := encodeHeader(FlagRencodeplus, len(payload))
	header[2] = compression
	return append(header, payload...)
}

// readerHarness runs a Conn against an in-memory pipe and feeds it bytes.
func readerHarness(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	c := &Conn{
		conn:    client,
		packets: make(chan Packet, 8),
		writes:  make(chan []byte, 8),
	}
	go c.readLoop()
	go c.writeLoop()
	t.Cleanup(func() {
		c.Close()
		server.Close()
	})
	return c, server
}

func recvPacket(t *testing.T, c *Conn) (Packet, bool) {
	t.Helper()
	select {
	case p, ok := <-c.Packets():
		return p, ok
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a packet")
		return nil, false
	}
}

func TestReadPacket(t *testing.T) {
	for _, compress := range []bool{false, true} {
		name := "plain"
		if compress {
			name = "lz4"
		}
		t.Run(name, func(t *testing.T) {
			c, server := readerHarness(t)
			// Repeated text so the lz4 case actually compresses.
			fixture := []any{"draw", 3, 0, 0, 64, 48, "rgb32",
				[]byte("pixelpixelpixelpixelpixelpixelpixelpixelpixelpixel"),
				7, 256, map[string]any{"rgb_format": "BGRX"}}
			go func() {
				server.Write(frame(t, fixture, compress))
			}()

			p, ok := recvPacket(t, c)
			if !ok {
				t.Fatalf("connection closed: %v", c.Err())
			}
			if p.Type() != "draw" {
				t.Errorf("Type = %q, want draw", p.Type())
			}
			if p.Int(1) != 3 {
				t.Errorf("wid = %d, want 3", p.Int(1))
			}
			if p.Str(6) != "rgb32" {
				t.Errorf("coding = %q, want rgb32", p.Str(6))
			}
			pixels, ok := p.Bytes(7)
			if !ok || string(pixels) != "pixelpixelpixelpixelpixelpixelpixelpixelpixelpixel" {
				t.Errorf("pixels = %q (ok=%v)", pixels, ok)
			}
			if p.Int(9) != 256 {
				t.Errorf("rowstride = %d, want 256", p.Int(9))
			}
			if got := p.Dict(10).Str("rgb_format"); got != "BGRX" {
				t.Errorf("rgb_format = %q, want BGRX", got)
			}
		})
	}
}

// The reader must handle a packet split across several TCP segments, which is
// what net.Pipe's unbuffered writes simulate well.
func TestReadPacketSplitAcrossWrites(t *testing.T) {
	c, server := readerHarness(t)
	data := frame(t, []any{"ping", 1234}, false)
	go func() {
		for _, b := range data {
			server.Write([]byte{b})
		}
	}()
	p, ok := recvPacket(t, c)
	if !ok {
		t.Fatalf("connection closed: %v", c.Err())
	}
	if p.Type() != "ping" || p.Int(1) != 1234 {
		t.Errorf("got %v, want [ping 1234]", p)
	}
}

// We negotiate chunks=false, so a chunked packet means the stream is no longer
// interpretable and the connection must fail loudly rather than silently
// mis-parsing pixel data.
func TestReadRejectsChunkedPacket(t *testing.T) {
	c, server := readerHarness(t)
	data := frame(t, []any{"ping", 1}, false)
	data[3] = 7 // packet index
	go func() { server.Write(data) }()

	if _, ok := recvPacket(t, c); ok {
		t.Fatal("chunked packet was accepted")
	}
	if c.Err() == nil {
		t.Error("no error recorded for a chunked packet")
	}
}

func TestReadRejectsUnexpectedCompression(t *testing.T) {
	c, server := readerHarness(t)
	data := frame(t, []any{"ping", 1}, false)
	data[2] = compressionZstd | 1
	go func() { server.Write(data) }()

	if _, ok := recvPacket(t, c); ok {
		t.Fatal("zstd packet was accepted")
	}
	if c.Err() == nil {
		t.Error("no error recorded for zstd compression")
	}
}

func TestReadRejectsWrongEncoder(t *testing.T) {
	c, server := readerHarness(t)
	data := frame(t, []any{"ping", 1}, false)
	data[1] = FlagYAML
	go func() { server.Write(data) }()

	if _, ok := recvPacket(t, c); ok {
		t.Fatal("yaml-flagged packet was accepted")
	}
	if c.Err() == nil {
		t.Error("no error recorded for a non-rencodeplus packet")
	}
}

func TestSendWritesOneFrame(t *testing.T) {
	c, server := readerHarness(t)
	done := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 128)
		n, _ := server.Read(buf)
		done <- buf[:n]
	}()

	if err := c.Send("ping", 1234); err != nil {
		t.Fatalf("Send: %v", err)
	}
	select {
	case got := <-done:
		// Header and payload must arrive in a single write.
		want, _ := hex.DecodeString("5018000000000009" + "c28470696e673f04d2")
		if string(got) != string(want) {
			t.Errorf("wrote %x, want %x", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for the write")
	}
}

func TestPacketAccessorsAreLenient(t *testing.T) {
	p := Packet{"draw", int64(3), "text", []byte("bin"), true}
	if p.Int(99) != 0 || p.Str(99) != "" || p.Bool(99) {
		t.Error("out-of-range access should give zero values")
	}
	if _, ok := p.Bytes(99); ok {
		t.Error("out-of-range Bytes should report false")
	}
	if p.Int(2) != 0 {
		t.Error("Int of a string should be 0")
	}
	if p.Str(3) != "bin" {
		t.Error("Str should accept a binary element")
	}
	if !p.Bool(4) {
		t.Error("Bool(true) should be true")
	}
	if p.Dict(1) != nil {
		t.Error("Dict of a non-container should be nil")
	}
}

func TestDictAccessors(t *testing.T) {
	d := Dict{
		"title":  "xterm",
		"depth":  int64(24),
		"iconic": true,
		"salt":   []byte{1, 2, 3},
		"nested": map[string]any{"receive": true},
	}
	if d.Str("title") != "xterm" {
		t.Error("Str")
	}
	if d.Int("depth") != 24 {
		t.Error("Int")
	}
	if !d.Bool("iconic") {
		t.Error("Bool")
	}
	if string(d.Bytes("salt")) != "\x01\x02\x03" {
		t.Error("Bytes")
	}
	if !d.Dict("nested").Bool("receive") {
		t.Error("nested Dict")
	}
	if !d.Has("title") || d.Has("absent") {
		t.Error("Has")
	}
	if d.Str("absent") != "" || d.Int("absent") != 0 || d.Dict("absent") != nil {
		t.Error("absent keys should give zero values")
	}
}

func TestCanonicalNames(t *testing.T) {
	cases := map[string]string{
		"draw":                  "window-draw",
		"window-draw":           "window-draw",
		"new-window":            "window-create",
		"lost-window":           "window-destroy",
		"window-metadata":       "window-metadata",
		"new-override-redirect": "new-override-redirect", // must stay distinct
	}
	for in, want := range cases {
		if got := Canonical(in); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
	}
}
