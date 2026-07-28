package main

import (
	"strings"
	"testing"
)

func TestParseConnectionURL(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		address  string
		username string
		password string
	}{
		{
			name:    "host with defaults",
			raw:     "tcp://example.com/",
			address: "example.com:14500",
		},
		{
			name:     "credentials and port",
			raw:      "tcp://alice:secret@example.com:12345/",
			address:  "example.com:12345",
			username: "alice",
			password: "secret",
		},
		{
			name:     "username only",
			raw:      "tcp://alice@example.com/",
			address:  "example.com:14500",
			username: "alice",
		},
		{
			name:     "password only",
			raw:      "tcp://:secret@example.com/",
			address:  "example.com:14500",
			password: "secret",
		},
		{
			name:     "escaped credentials",
			raw:      "tcp://first%20last:p%40ss@example.com/",
			address:  "example.com:14500",
			username: "first last",
			password: "p@ss",
		},
		{
			name:    "IPv6",
			raw:     "tcp://[2001:db8::1]:15000/",
			address: "[2001:db8::1]:15000",
		},
		{
			name:    "no trailing slash",
			raw:     "tcp://example.com",
			address: "example.com:14500",
		},
		{
			name:    "case-insensitive protocol",
			raw:     "TCP://example.com/",
			address: "example.com:14500",
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
			if got.username != tt.username {
				t.Errorf("username = %q, want %q", got.username, tt.username)
			}
			if got.password != tt.password {
				t.Errorf("password = %q, want %q", got.password, tt.password)
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
		{name: "SSL", raw: "ssl://example.com/"},
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
