package protocol

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type webSocketServerResult struct {
	path     string
	rawQuery string
	message  []byte
	err      error
}

func TestDialWebSocket(t *testing.T) {
	result := make(chan webSocketServerResult, 1)
	serverPayload := bytes.Repeat([]byte("pixels"), 12_000)
	serverFrame := frame(t, []any{"draw", 7, 0, 0, 200, 100, "rgb24", serverPayload}, false)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{xpraWebSocketSubprotocol},
		})
		if err != nil {
			result <- webSocketServerResult{err: err}
			return
		}
		defer wsConn.CloseNow()

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		messageType, message, err := wsConn.Read(ctx)
		if err == nil && messageType != websocket.MessageBinary {
			err = &websocket.CloseError{
				Code:   websocket.StatusUnsupportedData,
				Reason: "client message was not binary",
			}
		}
		if err == nil {
			err = wsConn.Write(ctx, websocket.MessageBinary, serverFrame)
		}
		result <- webSocketServerResult{
			path: r.URL.EscapedPath(), rawQuery: r.URL.RawQuery, message: message, err: err,
		}
	}))
	defer server.Close()

	rawURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/xpra%2Fsession?token=abc%20123"
	conn, err := DialWebSocket(rawURL, nil)
	if err != nil {
		t.Fatalf("DialWebSocket: %v", err)
	}
	defer conn.Close()
	if err := conn.Send("ping", 1234); err != nil {
		t.Fatalf("Send: %v", err)
	}

	packet, ok := recvPacket(t, conn)
	if !ok {
		t.Fatalf("connection closed: %v", conn.Err())
	}
	if packet.Type() != "draw" || packet.Int(1) != 7 {
		t.Errorf("packet = %v, want draw for window 7", packet)
	}
	pixels, ok := packet.Bytes(7)
	if !ok || !bytes.Equal(pixels, serverPayload) {
		t.Errorf("pixel payload = %d bytes (ok=%v), want %d", len(pixels), ok, len(serverPayload))
	}

	got := <-result
	if got.err != nil {
		t.Fatalf("WebSocket server: %v", got.err)
	}
	if got.path != "/xpra%2Fsession" || got.rawQuery != "token=abc%20123" {
		t.Errorf("request endpoint = %q?%s", got.path, got.rawQuery)
	}
	wantMessage := frame(t, []any{"ping", 1234}, false)
	wantMessage[1] |= FlagFlush
	if !bytes.Equal(got.message, wantMessage) {
		t.Errorf("client message = %x, want %x", got.message, wantMessage)
	}
}

func TestDialSecureWebSocket(t *testing.T) {
	accepted := make(chan error, 1)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			Subprotocols: []string{xpraWebSocketSubprotocol},
		})
		if err != nil {
			accepted <- err
			return
		}
		accepted <- nil
		wsConn.CloseNow()
	}))
	defer server.Close()

	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: "example.com",
	}
	rawURL := "wss" + strings.TrimPrefix(server.URL, "https") + "/"
	conn, err := DialWebSocket(rawURL, config)
	if err != nil {
		t.Fatalf("DialWebSocket: %v", err)
	}
	conn.Close()
	if err := <-accepted; err != nil {
		t.Fatalf("WebSocket server: %v", err)
	}

	if conn, err := DialWebSocket(rawURL, &tls.Config{MinVersion: tls.VersionTLS12}); err == nil {
		conn.Close()
		t.Fatal("DialWebSocket trusted an unknown certificate")
	}
}

func TestDialWebSocketRequiresBinarySubprotocol(t *testing.T) {
	accepted := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := websocket.Accept(w, r, nil)
		accepted <- err
		if err == nil {
			wsConn.CloseNow()
		}
	}))
	defer server.Close()

	rawURL := "ws" + strings.TrimPrefix(server.URL, "http")
	if conn, err := DialWebSocket(rawURL, nil); err == nil {
		conn.Close()
		t.Fatal("DialWebSocket accepted a server without the binary subprotocol")
	} else if !strings.Contains(err.Error(), "subprotocol") {
		t.Fatalf("DialWebSocket error = %v, want subprotocol error", err)
	}
	if err := <-accepted; err != nil {
		t.Fatalf("WebSocket server: %v", err)
	}
}

func TestDialWebSocketValidatesSchemeAndTLS(t *testing.T) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	tests := []struct {
		name   string
		rawURL string
		config *tls.Config
	}{
		{name: "wrong scheme", rawURL: "http://example.com/", config: nil},
		{name: "TLS with plain WebSocket", rawURL: "ws://example.com/", config: config},
		{name: "missing WSS TLS", rawURL: "wss://example.com/", config: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if conn, err := DialWebSocket(tt.rawURL, tt.config); err == nil {
				conn.Close()
				t.Fatal("DialWebSocket succeeded")
			}
		})
	}
}
