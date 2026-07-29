package protocol

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/coder/websocket"
)

const xpraWebSocketSubprotocol = "binary"

// DialWebSocket connects to an xpra server over WebSocket and starts the
// packet read/write loops. Xpra carries its normal byte stream in binary
// WebSocket messages and requires the "binary" subprotocol.
//
// A TLS configuration is required for wss:// URLs and must be nil for ws://
// URLs. The caller owns the TLS policy, including roots and hostname
// verification.
func DialWebSocket(rawURL string, config *tls.Config) (*Conn, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parsing WebSocket URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "ws" && scheme != "wss" {
		return nil, fmt.Errorf("WebSocket URL must use ws or wss, got %q", u.Scheme)
	}

	var httpClient *http.Client
	if scheme == "wss" {
		if config == nil {
			return nil, fmt.Errorf("TLS configuration is required for wss")
		}
		defaultTransport, ok := http.DefaultTransport.(*http.Transport)
		if !ok {
			return nil, fmt.Errorf("default HTTP transport has type %T, want *http.Transport",
				http.DefaultTransport)
		}
		transport := defaultTransport.Clone()
		transport.TLSClientConfig = config.Clone()
		httpClient = &http.Client{Transport: transport}
	} else if config != nil {
		return nil, fmt.Errorf("TLS configuration cannot be used with ws")
	}

	wsConn, _, err := websocket.Dial(context.Background(), rawURL, &websocket.DialOptions{
		HTTPClient:   httpClient,
		Subprotocols: []string{xpraWebSocketSubprotocol},
	})
	if err != nil {
		return nil, err
	}
	if got := wsConn.Subprotocol(); got != xpraWebSocketSubprotocol {
		_ = wsConn.CloseNow()
		return nil, fmt.Errorf("server selected WebSocket subprotocol %q, want %q",
			got, xpraWebSocketSubprotocol)
	}

	stream := websocket.NetConn(context.Background(), wsConn, websocket.MessageBinary)
	return New(stream), nil
}
