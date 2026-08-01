package main

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestValidateOptionSpelling(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Bool("v", false, "")
	flags.String("backend", "auto", "")
	flags.String("ssl-ca-cert", "", "")
	flags.Bool("ssl-insecure", false, "")
	flags.Bool("version", false, "")

	valid := [][]string{
		{"-v", "tcp://example.com/"},
		{"-v=true", "tcp://example.com/"},
		{"--backend", "wayland", "tcp://example.com/"},
		{"--ssl-ca-cert", "ca.pem", "ssl://example.com/"},
		{"--ssl-ca-cert", "-private-ca.pem", "ssl://example.com/"},
		{"--ssl-ca-cert=ca.pem", "ssl://example.com/"},
		{"--ssl-insecure", "ssl://example.com/"},
		{"--version"},
		{"--help"},
		{"--", "-ssl-insecure"},
		{"tcp://example.com/", "-ssl-insecure"},
	}
	for _, args := range valid {
		if err := validateOptionSpelling(args, flags); err != nil {
			t.Errorf("validateOptionSpelling(%q): %v", args, err)
		}
	}

	invalid := [][]string{
		{"-backend", "wayland"},
		{"-ssl-ca-cert=ca.pem"},
		{"-ssl-insecure"},
		{"-version"},
		{"-help"},
		{"-h"},
		{"--v"},
		{"--v=true"},
	}
	for _, args := range invalid {
		if err := validateOptionSpelling(args, flags); err == nil {
			t.Errorf("validateOptionSpelling(%q) succeeded", args)
		}
	}
}

func TestPrintOptionDefaultsUsesCommandSpelling(t *testing.T) {
	flags := flag.NewFlagSet("test", flag.ContinueOnError)
	flags.Bool("v", false, "verbose output")
	flags.String("backend", "auto", "display backend")
	flags.Bool("ssl-insecure", false, "disable certificate verification")

	var output bytes.Buffer
	printOptionDefaults(&output, flags)
	got := output.String()
	for _, want := range []string{"  -v", "  --backend string", "  --ssl-insecure"} {
		if !strings.Contains(got, want) {
			t.Errorf("option help does not contain %q:\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"  --v", "  -backend", "  -ssl-insecure"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("option help contains old spelling %q:\n%s", unwanted, got)
		}
	}
}

func TestParseConnectionURL(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		address    string
		serverName string
		username   string
		password   string
		display    string
		path       string
		rawPath    string
		rawQuery   string
		port       string
		transport  connectionTransport
	}{
		{
			name:       "host with defaults",
			raw:        "tcp://example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
			port:       defaultTCPPort,
			transport:  transportTCP,
		},
		{
			name:       "credentials and port",
			raw:        "tcp://alice:secret@example.com:12345/",
			address:    "example.com:12345",
			serverName: "example.com",
			username:   "alice",
			password:   "secret",
			port:       "12345",
			transport:  transportTCP,
		},
		{
			name:       "username only",
			raw:        "tcp://alice@example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
			username:   "alice",
			port:       defaultTCPPort,
			transport:  transportTCP,
		},
		{
			name:       "password only",
			raw:        "tcp://:secret@example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
			password:   "secret",
			port:       defaultTCPPort,
			transport:  transportTCP,
		},
		{
			name:       "escaped credentials",
			raw:        "tcp://first%20last:p%40ss@example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
			username:   "first last",
			password:   "p@ss",
			port:       defaultTCPPort,
			transport:  transportTCP,
		},
		{
			name:       "IPv6",
			raw:        "tcp://[2001:db8::1]:15000/",
			address:    "[2001:db8::1]:15000",
			serverName: "2001:db8::1",
			port:       "15000",
			transport:  transportTCP,
		},
		{
			name:       "no trailing slash",
			raw:        "tcp://example.com",
			address:    "example.com:14500",
			serverName: "example.com",
			port:       defaultTCPPort,
			transport:  transportTCP,
		},
		{
			name:       "case-insensitive protocol",
			raw:        "TCP://example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
			port:       defaultTCPPort,
			transport:  transportTCP,
		},
		{
			name:       "SSL with defaults",
			raw:        "ssl://secure.example.com/",
			address:    "secure.example.com:14500",
			serverName: "secure.example.com",
			port:       defaultTCPPort,
			transport:  transportSSL,
		},
		{
			name:       "case-insensitive SSL with credentials",
			raw:        "SSL://alice:secret@secure.example.com:15000/",
			address:    "secure.example.com:15000",
			serverName: "secure.example.com",
			username:   "alice",
			password:   "secret",
			port:       "15000",
			transport:  transportSSL,
		},
		{
			name:       "WebSocket with defaults",
			raw:        "ws://example.com/",
			address:    "example.com:80",
			serverName: "example.com",
			path:       "/",
			port:       defaultWSPort,
			transport:  transportWS,
		},
		{
			name:       "WebSocket path query and credentials",
			raw:        "WS://alice:secret@example.com:8080/xpra%2Fsession?token=abc%20123",
			address:    "example.com:8080",
			serverName: "example.com",
			username:   "alice",
			password:   "secret",
			path:       "/xpra/session",
			rawPath:    "/xpra%2Fsession",
			rawQuery:   "token=abc%20123",
			port:       "8080",
			transport:  transportWS,
		},
		{
			name:       "secure WebSocket with defaults",
			raw:        "wss://secure.example.com",
			address:    "secure.example.com:443",
			serverName: "secure.example.com",
			path:       "/",
			port:       defaultWSSPort,
			transport:  transportWSS,
		},
		{
			name:       "secure WebSocket IPv6",
			raw:        "wss://[2001:db8::1]:14430/xpra/",
			address:    "[2001:db8::1]:14430",
			serverName: "2001:db8::1",
			path:       "/xpra/",
			port:       "14430",
			transport:  transportWSS,
		},
		{
			name:       "SSH with defaults",
			raw:        "ssh://alice@example.com/",
			address:    "example.com:22",
			serverName: "example.com",
			username:   "alice",
			port:       defaultSSHPort,
			transport:  transportSSH,
		},
		{
			name:       "SSH password port and display",
			raw:        "SSH://alice:p%40ss@example.com:2222/100",
			address:    "example.com:2222",
			serverName: "example.com",
			username:   "alice",
			password:   "p@ss",
			display:    "100",
			port:       "2222",
			transport:  transportSSH,
		},
		{
			name:       "SSH escaped display",
			raw:        "ssh://example.com/session%20name",
			address:    "example.com:22",
			serverName: "example.com",
			display:    "session name",
			port:       defaultSSHPort,
			transport:  transportSSH,
		},
		{
			name:       "SSH IPv6",
			raw:        "ssh://alice@[2001:db8::1]:2222/:100",
			address:    "[2001:db8::1]:2222",
			serverName: "2001:db8::1",
			username:   "alice",
			display:    ":100",
			port:       "2222",
			transport:  transportSSH,
		},
		{
			name:      "Unix socket",
			raw:       "socket:///run/user/1000/xpra/100",
			address:   "/run/user/1000/xpra/100",
			transport: transportSocket,
		},
		{
			name:      "Unix alias with credentials and escaped path",
			raw:       "UNIX://alice:p%40ss@/tmp/xpra%20socket",
			address:   "/tmp/xpra socket",
			username:  "alice",
			password:  "p@ss",
			transport: transportSocket,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseConnectionURL(tt.raw)
			if err != nil {
				t.Fatalf("parseConnectionURL(%q): %v", tt.raw, err)
			}
			if got.address != tt.address {
				t.Errorf("address = %q, want %q", got.address, tt.address)
			}
			if got.serverName != tt.serverName {
				t.Errorf("serverName = %q, want %q", got.serverName, tt.serverName)
			}
			if got.username != tt.username {
				t.Errorf("username = %q, want %q", got.username, tt.username)
			}
			if got.password != tt.password {
				t.Errorf("password = %q, want %q", got.password, tt.password)
			}
			if got.display != tt.display {
				t.Errorf("display = %q, want %q", got.display, tt.display)
			}
			if got.path != tt.path {
				t.Errorf("path = %q, want %q", got.path, tt.path)
			}
			if got.rawPath != tt.rawPath {
				t.Errorf("rawPath = %q, want %q", got.rawPath, tt.rawPath)
			}
			if got.rawQuery != tt.rawQuery {
				t.Errorf("rawQuery = %q, want %q", got.rawQuery, tt.rawQuery)
			}
			if got.port != tt.port {
				t.Errorf("port = %q, want %q", got.port, tt.port)
			}
			if got.transport != tt.transport {
				t.Errorf("transport = %q, want %q", got.transport, tt.transport)
			}
		})
	}
}

func TestParseConnectionURLRejectsUnsupportedURLs(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "bare address", raw: "example.com:14500"},
		{name: "unsupported protocol", raw: "quic://example.com/"},
		{name: "missing host", raw: "tcp:///"},
		{name: "display path", raw: "tcp://example.com/100"},
		{name: "SSH multiple path segments", raw: "ssh://example.com/100/child"},
		{name: "SSH escaped slash", raw: "ssh://example.com/100%2Fchild"},
		{name: "SSH empty nested path", raw: "ssh://example.com//"},
		{name: "SSH control character", raw: "ssh://example.com/%0Acommand"},
		{name: "SSH option as host", raw: "ssh://-oProxyCommand=bad/"},
		{name: "query", raw: "tcp://example.com/?encoding=rgb"},
		{name: "SSH query", raw: "ssh://example.com/?proxy=other"},
		{name: "fragment", raw: "tcp://example.com/#session"},
		{name: "zero port", raw: "tcp://example.com:0/"},
		{name: "empty port", raw: "tcp://example.com:/"},
		{name: "large port", raw: "tcp://example.com:65536/"},
		{name: "named port", raw: "tcp://example.com:xpra/"},
		{name: "socket missing path", raw: "socket:///"},
		{name: "socket missing authority marker", raw: "socket:/tmp/xpra"},
		{name: "socket host", raw: "socket://localhost/tmp/xpra"},
		{name: "socket query", raw: "socket:///tmp/xpra?session=100"},
		{name: "socket fragment", raw: "unix:///tmp/xpra#session"},
		{name: "socket NUL", raw: "socket:///tmp/xpra%00bad"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseConnectionURL(tt.raw); err == nil {
				t.Fatalf("parseConnectionURL(%q) succeeded", tt.raw)
			}
		})
	}
}

func TestParseConnectionURLErrorsDoNotExposePassword(t *testing.T) {
	const password = "top-secret-password"
	_, err := parseConnectionURL("tcp://user:" + password + "@example.com:bad/")
	if err == nil {
		t.Fatal("parseConnectionURL succeeded")
	}
	if strings.Contains(err.Error(), password) {
		t.Errorf("error exposes password: %v", err)
	}
}

func TestWebSocketURLDoesNotExposeCredentials(t *testing.T) {
	target, err := parseConnectionURL("wss://alice:top-secret@example.com/xpra/?token=abc")
	if err != nil {
		t.Fatalf("parseConnectionURL: %v", err)
	}
	got := target.webSocketURL()
	if strings.Contains(got, "alice") || strings.Contains(got, "top-secret") {
		t.Errorf("WebSocket URL exposes credentials: %q", got)
	}
	if got != "wss://example.com:443/xpra/?token=abc" {
		t.Errorf("WebSocket URL = %q", got)
	}
}

func TestConnectionLabel(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"TCP default port", "tcp://example.com/", "xpra @ example.com:14500"},
		{"WebSocket default port", "wss://example.com/", "xpra @ example.com:443"},
		{"socket path", "socket:///run/user/1000/xpra/100", "xpra @ /run/user/1000/xpra/100"},
		{"credentials omitted", "tcp://alice:top-secret@example.com:15000/", "xpra @ example.com:15000"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			target, err := parseConnectionURL(test.raw)
			if err != nil {
				t.Fatalf("parseConnectionURL: %v", err)
			}
			got := connectionLabel(target)
			if got != test.want {
				t.Errorf("connectionLabel = %q, want %q", got, test.want)
			}
			if strings.Contains(got, "top-secret") {
				t.Errorf("connection label exposes password: %q", got)
			}
		})
	}
}

func TestMakeTLSConfig(t *testing.T) {
	target := connectionURL{
		transport: transportSSL, address: "example.com:14500", serverName: "example.com",
	}

	t.Run("verified defaults", func(t *testing.T) {
		config, err := makeTLSConfig(target, sslOptions{})
		if err != nil {
			t.Fatalf("makeTLSConfig: %v", err)
		}
		if config.ServerName != "example.com" {
			t.Errorf("ServerName = %q, want example.com", config.ServerName)
		}
		if config.MinVersion != tls.VersionTLS12 {
			t.Errorf("MinVersion = %#x, want TLS 1.2", config.MinVersion)
		}
		if config.InsecureSkipVerify {
			t.Error("verification is disabled by default")
		}
	})

	t.Run("insecure override", func(t *testing.T) {
		config, err := makeTLSConfig(target, sslOptions{insecure: true})
		if err != nil {
			t.Fatalf("makeTLSConfig: %v", err)
		}
		if !config.InsecureSkipVerify {
			t.Error("InsecureSkipVerify is false")
		}
	})

	t.Run("custom CA", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.pem")
		if err := os.WriteFile(path, testCAPEM(t), 0o600); err != nil {
			t.Fatalf("writing CA fixture: %v", err)
		}

		config, err := makeTLSConfig(target, sslOptions{caCert: path})
		if err != nil {
			t.Fatalf("makeTLSConfig: %v", err)
		}
		if config.RootCAs == nil {
			t.Fatal("custom CA pool is nil")
		}
		if len(config.RootCAs.Subjects()) == 0 {
			t.Error("custom CA pool contains no subjects")
		}
	})

	t.Run("secure WebSocket", func(t *testing.T) {
		target.transport = transportWSS
		config, err := makeTLSConfig(target, sslOptions{})
		if err != nil {
			t.Fatalf("makeTLSConfig: %v", err)
		}
		if config.ServerName != "example.com" {
			t.Errorf("ServerName = %q, want example.com", config.ServerName)
		}
	})
}

func testCAPEM(t *testing.T) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("creating CA certificate: %v", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func TestMakeTLSConfigRejectsInvalidOptions(t *testing.T) {
	secure := connectionURL{transport: transportSSL, serverName: "example.com"}
	plain := connectionURL{transport: transportTCP, serverName: "example.com"}
	ssh := connectionURL{transport: transportSSH, serverName: "example.com"}
	ws := connectionURL{transport: transportWS, serverName: "example.com"}
	socket := connectionURL{transport: transportSocket, address: "/tmp/xpra"}

	tests := []struct {
		name    string
		target  connectionURL
		options sslOptions
	}{
		{name: "SSL option with TCP", target: plain, options: sslOptions{insecure: true}},
		{name: "SSL option with SSH", target: ssh, options: sslOptions{insecure: true}},
		{name: "SSL option with WS", target: ws, options: sslOptions{insecure: true}},
		{name: "SSL option with socket", target: socket, options: sslOptions{insecure: true}},
		{name: "conflicting SSL options", target: secure, options: sslOptions{caCert: "ca.pem", insecure: true}},
		{name: "missing CA file", target: secure, options: sslOptions{caCert: filepath.Join(t.TempDir(), "missing.pem")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := makeTLSConfig(tt.target, tt.options); err == nil {
				t.Fatal("makeTLSConfig succeeded, want error")
			}
		})
	}

	t.Run("invalid CA PEM", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ca.pem")
		if err := os.WriteFile(path, []byte("not a certificate"), 0o600); err != nil {
			t.Fatalf("writing CA fixture: %v", err)
		}
		if _, err := makeTLSConfig(secure, sslOptions{caCert: path}); err == nil {
			t.Fatal("makeTLSConfig succeeded, want error")
		}
	})
}
