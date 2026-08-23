package protocol

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"math/big"
	"net"
	"strings"
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
		"large index": {'P', 0, 0, maxPacketIndex + 1, 0, 0, 0, 1},
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := parseHeader(b); err == nil {
				t.Errorf("parseHeader(%x) succeeded, want error", b)
			}
		})
	}
}

func rawChunkFrame(t *testing.T, index byte, payload []byte, compress bool) []byte {
	t.Helper()
	compression := byte(0)
	if compress {
		buf := make([]byte, 4+lz4.CompressBlockBound(len(payload)))
		binary.LittleEndian.PutUint32(buf[:4], uint32(len(payload)))
		var compressor lz4.Compressor
		n, err := compressor.CompressBlock(payload, buf[4:])
		if err != nil {
			t.Fatalf("compressing raw chunk: %v", err)
		}
		if n == 0 {
			t.Fatal("raw chunk fixture is not compressible")
		}
		payload = buf[:4+n]
		compression = compressionLZ4 | 1
	}
	header := encodeHeader(0, len(payload))
	header[2] = compression
	header[3] = index
	return append(header, payload...)
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
	c := newConn(client)
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

func TestReadReassemblesRawChunk(t *testing.T) {
	c, server := readerHarness(t)
	pixels := []byte("out-of-band pixel data")
	raw := rawChunkFrame(t, 7, pixels, false)
	main := frame(t, []any{"draw", 3, 0, 0, 2, 1, "rgb24", []byte{}}, false)
	next := frame(t, []any{"ping", 99}, false)
	data := append(append(raw, main...), next...)
	go func() { server.Write(data) }()

	packet, ok := recvPacket(t, c)
	if !ok {
		t.Fatalf("connection closed: %v", c.Err())
	}
	got, ok := packet.Bytes(7)
	if !ok || !bytes.Equal(got, pixels) {
		t.Errorf("reassembled pixels = %q (ok=%v), want %q", got, ok, pixels)
	}
	packet, ok = recvPacket(t, c)
	if !ok || packet.Type() != "ping" || packet.Int(1) != 99 {
		t.Errorf("packet after chunks = %v (ok=%v), want [ping 99]", packet, ok)
	}
}

func TestReadReassemblesMultipleAndCompressedRawChunks(t *testing.T) {
	c, server := readerHarness(t)
	first := []byte("first raw value")
	third := bytes.Repeat([]byte("compressed raw value"), 100)
	rawThird := rawChunkFrame(t, 3, third, true)
	rawFirst := rawChunkFrame(t, 1, first, false)
	main := frame(t, []any{"multi", []byte{}, "inline", ""}, false)
	data := append(append(rawThird, rawFirst...), main...)
	go func() { server.Write(data) }()

	packet, ok := recvPacket(t, c)
	if !ok {
		t.Fatalf("connection closed: %v", c.Err())
	}
	if got, ok := packet.Bytes(1); !ok || !bytes.Equal(got, first) {
		t.Errorf("chunk 1 = %q (ok=%v)", got, ok)
	}
	if got, ok := packet.Bytes(3); !ok || !bytes.Equal(got, third) {
		t.Errorf("chunk 3 = %d bytes (ok=%v), want %d", len(got), ok, len(third))
	}
}

func TestReadRejectsMalformedRawChunks(t *testing.T) {
	chunk1 := rawChunkFrame(t, 1, []byte("one"), false)
	tests := []struct {
		name string
		data []byte
		want string
	}{
		{
			name: "duplicate index",
			data: append(append([]byte{}, chunk1...), chunk1...),
			want: "duplicate raw chunk",
		},
		{
			name: "too many",
			data: append(append(append(append([]byte{},
				rawChunkFrame(t, 1, []byte("one"), false)...),
				rawChunkFrame(t, 2, []byte("two"), false)...),
				rawChunkFrame(t, 3, []byte("three"), false)...),
				rawChunkFrame(t, 4, []byte("four"), false)...),
			want: "too many raw chunks",
		},
		{
			name: "index outside packet",
			data: append(rawChunkFrame(t, 7, []byte("seven"), false),
				frame(t, []any{"ping", 1}, false)...),
			want: "outside a 2-element packet",
		},
		{
			name: "non-empty byte placeholder",
			data: append(chunk1, frame(t, []any{"test", []byte("occupied")}, false)...),
			want: "non-empty byte placeholder",
		},
		{
			name: "non-empty string placeholder",
			data: append(chunk1, frame(t, []any{"test", "occupied"}, false)...),
			want: "non-empty string placeholder",
		},
		{
			name: "wrong placeholder type",
			data: append(chunk1, frame(t, []any{"test", 123}, false)...),
			want: "int64 placeholder",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, server := readerHarness(t)
			go func() { server.Write(tt.data) }()
			if _, ok := recvPacket(t, c); ok {
				t.Fatal("malformed chunks produced a packet")
			}
			if err := c.Err(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("connection error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestReadRejectsEOFWithPendingRawChunk(t *testing.T) {
	c, server := readerHarness(t)
	pending := rawChunkFrame(t, 1, []byte("pending"), false)
	go func() {
		server.Write(pending)
		server.Close()
	}()
	if _, ok := recvPacket(t, c); ok {
		t.Fatal("pending chunk produced a packet")
	}
	if err := c.Err(); err == nil || !strings.Contains(err.Error(), "pending raw chunk") {
		t.Fatalf("connection error = %v, want pending chunk error", err)
	}
}

func TestRawChunkStateBoundsReassembledSize(t *testing.T) {
	state := rawChunkState{
		packets: map[byte][]byte{1: {1}},
		size:    maxPayloadSize - 1,
	}
	if err := state.add(2, []byte{2, 3}); err == nil ||
		!strings.Contains(err.Error(), "packet limit") {
		t.Fatalf("add error = %v, want packet size limit", err)
	}
	state.size = maxPayloadSize
	if err := state.apply([]any{"test", []byte{}}, 1); err == nil ||
		!strings.Contains(err.Error(), "reassembled packet") {
		t.Fatalf("apply error = %v, want reassembled packet size limit", err)
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

// Flush is what keeps the disconnect packet alive: the caller sends it and
// closes immediately afterwards, so the write must be complete first.
func TestFlushWaitsForTheQueuedPacket(t *testing.T) {
	c, server := readerHarness(t)

	if err := c.Send("connection-close", "client exit"); err != nil {
		t.Fatalf("Send: %v", err)
	}
	flushed := make(chan error, 1)
	go func() { flushed <- c.Flush(2 * time.Second) }()

	// net.Pipe is unbuffered, so the write cannot complete until the peer
	// reads it, and neither can the flush.
	select {
	case err := <-flushed:
		t.Fatalf("Flush returned %v before the peer read anything", err)
	case <-time.After(50 * time.Millisecond):
	}

	buf := make([]byte, 128)
	n, err := server.Read(buf)
	if err != nil {
		t.Fatalf("reading the packet: %v", err)
	}
	if n == 0 {
		t.Fatal("read an empty packet")
	}
	select {
	case err := <-flushed:
		if err != nil {
			t.Errorf("Flush: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Flush")
	}
}

func TestFlushTimesOutOnAStalledPeer(t *testing.T) {
	c, _ := readerHarness(t)

	if err := c.Send("ping", 1234); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Nothing reads from the pipe, so the write never completes.
	if err := c.Flush(50 * time.Millisecond); err == nil {
		t.Error("Flush returned nil on a peer that read nothing")
	}
}

func TestFlushReportsAClosedConnection(t *testing.T) {
	c, _ := readerHarness(t)
	c.Close()

	if err := c.Flush(2 * time.Second); err == nil {
		t.Error("Flush returned nil on a closed connection")
	}
}

func testTLSConfigs(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating TLS key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true,
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating TLS certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("loading TLS certificate: %v", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parsing TLS certificate: %v", err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cert},
		}, &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: "localhost",
			RootCAs:    roots,
		}
}

func tlsListener(t *testing.T, config *tls.Config) net.Listener {
	t.Helper()
	listener, err := tls.Listen("tcp", "127.0.0.1:0", config)
	if err != nil {
		t.Fatalf("listening with TLS: %v", err)
	}
	t.Cleanup(func() { listener.Close() })
	return listener
}

func TestDialTLS(t *testing.T) {
	serverConfig, clientConfig := testTLSConfigs(t)
	listener := tlsListener(t, serverConfig)
	data := frame(t, []any{"ping", 4321}, false)
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		_, err = conn.Write(data)
		serverErr <- err
	}()

	c, err := DialTLS(listener.Addr().String(), clientConfig)
	if err != nil {
		t.Fatalf("DialTLS: %v", err)
	}
	defer c.Close()
	packet, ok := recvPacket(t, c)
	if !ok {
		t.Fatalf("connection closed: %v", c.Err())
	}
	if packet.Type() != "ping" || packet.Int(1) != 4321 {
		t.Errorf("got %v, want [ping 4321]", packet)
	}
	if err := <-serverErr; err != nil {
		t.Errorf("TLS server: %v", err)
	}
}

func TestDialTLSRejectsInvalidPeer(t *testing.T) {
	tests := []struct {
		name   string
		config func(*tls.Config) *tls.Config
	}{
		{
			name: "untrusted certificate",
			config: func(config *tls.Config) *tls.Config {
				config.RootCAs = x509.NewCertPool()
				return config
			},
		},
		{
			name: "hostname mismatch",
			config: func(config *tls.Config) *tls.Config {
				config.ServerName = "wrong.example.com"
				return config
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serverConfig, clientConfig := testTLSConfigs(t)
			listener := tlsListener(t, serverConfig)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				if tlsConn, ok := conn.(*tls.Conn); ok {
					_ = tlsConn.Handshake()
				}
			}()
			if conn, err := DialTLS(listener.Addr().String(), tt.config(clientConfig)); err == nil {
				conn.Close()
				t.Fatal("DialTLS succeeded, want verification error")
			}
		})
	}
}

func TestDialTLSRequiresConfig(t *testing.T) {
	if conn, err := DialTLS("127.0.0.1:1", nil); err == nil {
		conn.Close()
		t.Fatal("DialTLS succeeded without a configuration")
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
	old := BackwardsCompatible
	BackwardsCompatible = true
	t.Cleanup(func() { BackwardsCompatible = old })

	cases := map[string]string{
		"draw":                  "window-draw",
		"window-draw":           "window-draw",
		"new-window":            "window-create",
		"lost-window":           "window-destroy",
		"notify_show":           "notification-show",
		"notify_close":          "notification-close",
		"window-metadata":       "window-metadata",
		"new-override-redirect": "new-override-redirect", // must stay distinct
	}
	for in, want := range cases {
		if got := Canonical(in); got != want {
			t.Errorf("Canonical(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalNamesDisabled(t *testing.T) {
	old := BackwardsCompatible
	BackwardsCompatible = false
	t.Cleanup(func() { BackwardsCompatible = old })

	for _, packetType := range []string{"draw", "new-window", "lost-window", "notify_show"} {
		if got := Canonical(packetType); got != packetType {
			t.Errorf("Canonical(%q) = %q with compatibility disabled", packetType, got)
		}
	}
	if got := Canonical("window-draw"); got != "window-draw" {
		t.Errorf("Canonical modern name = %q", got)
	}
}
