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
		secure     bool
	}{
		{
			name:       "host with defaults",
			raw:        "tcp://example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
		},
		{
			name:       "credentials and port",
			raw:        "tcp://alice:secret@example.com:12345/",
			address:    "example.com:12345",
			serverName: "example.com",
			username:   "alice",
			password:   "secret",
		},
		{
			name:       "username only",
			raw:        "tcp://alice@example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
			username:   "alice",
		},
		{
			name:       "password only",
			raw:        "tcp://:secret@example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
			password:   "secret",
		},
		{
			name:       "escaped credentials",
			raw:        "tcp://first%20last:p%40ss@example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
			username:   "first last",
			password:   "p@ss",
		},
		{
			name:       "IPv6",
			raw:        "tcp://[2001:db8::1]:15000/",
			address:    "[2001:db8::1]:15000",
			serverName: "2001:db8::1",
		},
		{
			name:       "no trailing slash",
			raw:        "tcp://example.com",
			address:    "example.com:14500",
			serverName: "example.com",
		},
		{
			name:       "case-insensitive protocol",
			raw:        "TCP://example.com/",
			address:    "example.com:14500",
			serverName: "example.com",
		},
		{
			name:       "SSL with defaults",
			raw:        "ssl://secure.example.com/",
			address:    "secure.example.com:14500",
			serverName: "secure.example.com",
			secure:     true,
		},
		{
			name:       "case-insensitive SSL with credentials",
			raw:        "SSL://alice:secret@secure.example.com:15000/",
			address:    "secure.example.com:15000",
			serverName: "secure.example.com",
			username:   "alice",
			password:   "secret",
			secure:     true,
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
			if got.secure != tt.secure {
				t.Errorf("secure = %v, want %v", got.secure, tt.secure)
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
		{name: "WebSocket", raw: "ws://example.com/"},
		{name: "SSH", raw: "ssh://example.com/"},
		{name: "missing host", raw: "tcp:///"},
		{name: "display path", raw: "tcp://example.com/100"},
		{name: "query", raw: "tcp://example.com/?encoding=rgb"},
		{name: "fragment", raw: "tcp://example.com/#session"},
		{name: "zero port", raw: "tcp://example.com:0/"},
		{name: "empty port", raw: "tcp://example.com:/"},
		{name: "large port", raw: "tcp://example.com:65536/"},
		{name: "named port", raw: "tcp://example.com:xpra/"},
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

func TestMakeTLSConfig(t *testing.T) {
	target := connectionURL{address: "example.com:14500", serverName: "example.com", secure: true}

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
	secure := connectionURL{serverName: "example.com", secure: true}
	plain := connectionURL{serverName: "example.com"}

	tests := []struct {
		name    string
		target  connectionURL
		options sslOptions
	}{
		{name: "SSL option with TCP", target: plain, options: sslOptions{insecure: true}},
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
