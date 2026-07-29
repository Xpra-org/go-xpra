//go:build linux

package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptPasswordPinentry(t *testing.T) {
	directory := t.TempDir()
	helper := filepath.Join(directory, "pinentry")
	script := `#!/bin/sh
printf 'OK fake pinentry ready\n'
while IFS= read -r command; do
	case "$command" in
		GETPIN)
			printf 'D test%%40password\nOK\n'
			;;
		BYE)
			printf 'OK\n'
			exit 0
			;;
		*)
			printf 'OK\n'
			;;
	esac
done
`
	if err := os.WriteFile(helper, []byte(script), 0o700); err != nil {
		t.Fatalf("writing fake pinentry: %v", err)
	}
	t.Setenv("PATH", directory)

	password, err := promptPasswordPinentry("alice", "example.com:14500", "Password")
	if err != nil {
		t.Fatalf("promptPasswordPinentry: %v", err)
	}
	if password != "test@password" {
		t.Errorf("password = %q, want fake pinentry response", password)
	}
}

func TestReadPinentryResponse(t *testing.T) {
	response := strings.NewReader(
		"# comment\n" +
			"S BUTTON_INFO some-status\n" +
			"D p%40ss%25word%0Aline\n" +
			"D %2Bmore\n" +
			"OK\n")
	got, err := readPinentryResponse(bufio.NewReader(response))
	if err != nil {
		t.Fatalf("readPinentryResponse: %v", err)
	}
	if want := "p@ss%word\nline+more"; got != want {
		t.Errorf("response = %q, want %q", got, want)
	}
}

func TestReadPinentryError(t *testing.T) {
	response := strings.NewReader("ERR 83886179 Operation cancelled <Pinentry>\n")
	if _, err := readPinentryResponse(bufio.NewReader(response)); err == nil ||
		!strings.Contains(err.Error(), "Operation cancelled") {
		t.Errorf("error = %v, want cancellation", err)
	}
}

func TestEscapePinentry(t *testing.T) {
	if got, want := escapePinentry("100%\r\nready"), "100%25%0D%0Aready"; got != want {
		t.Errorf("escapePinentry = %q, want %q", got, want)
	}
}
