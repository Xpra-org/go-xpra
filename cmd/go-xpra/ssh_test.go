package main

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestMakeSSHInvocation(t *testing.T) {
	paths := map[string]string{
		"ssh":     "/test/bin/ssh",
		"sshpass": "/test/bin/sshpass",
	}
	lookup := func(name string) (string, error) {
		if path := paths[name]; path != "" {
			return path, nil
		}
		return "", exec.ErrNotFound
	}

	t.Run("agent or interactive authentication", func(t *testing.T) {
		invocation, err := makeSSHInvocation(connectionURL{
			transport:  transportSSH,
			serverName: "example.com",
			port:       defaultSSHPort,
			username:   "alice",
			display:    "100",
		}, lookup)
		if err != nil {
			t.Fatalf("makeSSHInvocation: %v", err)
		}
		if invocation.executable != paths["ssh"] {
			t.Errorf("executable = %q, want %q", invocation.executable, paths["ssh"])
		}
		wantPrefix := []string{"-x", "-T", "-l", "alice", "example.com"}
		if !slices.Equal(invocation.args[:len(wantPrefix)], wantPrefix) {
			t.Errorf("args prefix = %q, want %q", invocation.args[:len(wantPrefix)], wantPrefix)
		}
		if slices.Contains(invocation.args, "-p") {
			t.Errorf("default SSH port unexpectedly produced -p: %q", invocation.args)
		}
		if got, want := invocation.args[len(invocation.args)-1], sshRemoteCommand("100"); got != want {
			t.Errorf("remote command = %q, want %q", got, want)
		}
		if invocation.usesSSHPass {
			t.Error("usesSSHPass = true without a password")
		}
	})

	t.Run("password through environment", func(t *testing.T) {
		const password = "top secret"
		invocation, err := makeSSHInvocation(connectionURL{
			transport:  transportSSH,
			serverName: "example.com",
			port:       "2222",
			username:   "alice",
			password:   password,
		}, lookup)
		if err != nil {
			t.Fatalf("makeSSHInvocation: %v", err)
		}
		if invocation.executable != paths["sshpass"] {
			t.Errorf("executable = %q, want %q", invocation.executable, paths["sshpass"])
		}
		wantPrefix := []string{"-e", paths["ssh"], "-x", "-T", "-p", "2222", "-l", "alice", "example.com"}
		if !slices.Equal(invocation.args[:len(wantPrefix)], wantPrefix) {
			t.Errorf("args prefix = %q, want %q", invocation.args[:len(wantPrefix)], wantPrefix)
		}
		if strings.Contains(invocation.executable, password) ||
			strings.Contains(strings.Join(invocation.args, " "), password) {
			t.Error("SSH password appears in the process command line")
		}
		if got := environmentValue(invocation.env, "SSHPASS"); got != password {
			t.Errorf("SSHPASS = %q, want supplied password", got)
		}
		if !invocation.usesSSHPass {
			t.Error("usesSSHPass = false with a password")
		}
	})

	t.Run("missing sshpass", func(t *testing.T) {
		delete(paths, "sshpass")
		_, err := makeSSHInvocation(connectionURL{
			serverName: "example.com", port: defaultSSHPort, password: "secret",
		}, lookup)
		if err == nil || !strings.Contains(err.Error(), "sshpass was not found") {
			t.Fatalf("error = %v, want missing sshpass error", err)
		}
		if strings.Contains(err.Error(), "secret") {
			t.Errorf("error exposes password: %v", err)
		}
	})

	t.Run("missing ssh", func(t *testing.T) {
		delete(paths, "ssh")
		_, err := makeSSHInvocation(connectionURL{serverName: "example.com"}, lookup)
		if err == nil || !strings.Contains(err.Error(), "finding ssh in PATH") {
			t.Fatalf("error = %v, want missing ssh error", err)
		}
	})
}

func TestSetEnvironmentReplacesExistingValue(t *testing.T) {
	got := setEnvironment([]string{"A=1", "SSHPASS=old", "B=2", "SSHPASS=older"}, "SSHPASS", "new")
	if count := countEnvironment(got, "SSHPASS"); count != 1 {
		t.Errorf("SSHPASS entries = %d, want 1: %q", count, got)
	}
	if value := environmentValue(got, "SSHPASS"); value != "new" {
		t.Errorf("SSHPASS = %q, want new", value)
	}
}

func TestShellQuote(t *testing.T) {
	tests := map[string]string{
		"":      "''",
		"plain": "'plain'",
		"a b":   "'a b'",
		"a'b":   `'a'\''b'`,
	}
	for input, want := range tests {
		if got := shellQuote(input); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", input, got, want)
		}
	}

	command := sshRemoteCommand(`100'; echo injected`)
	if !strings.HasPrefix(command, "sh -c ") ||
		!strings.Contains(command, "command -v") ||
		!strings.Contains(command, "xpra _proxy") {
		t.Errorf("remote command is incomplete: %q", command)
	}
}

func TestSSHRemoteCommandQuotesDisplay(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("sh is unavailable: %v", err)
	}
	directory := t.TempDir()
	xpra := filepath.Join(directory, "xpra")
	if err := os.WriteFile(xpra, []byte("#!/bin/sh\nprintf '%s' \"$2\"\n"), 0o700); err != nil {
		t.Fatalf("writing fake xpra: %v", err)
	}

	display := "100'; printf injected"
	command := exec.Command(shell, "-c", sshRemoteCommand(display))
	command.Env = setEnvironment(os.Environ(), "PATH",
		directory+string(os.PathListSeparator)+filepath.Dir(shell))
	output, err := command.Output()
	if err != nil {
		t.Fatalf("running remote command: %v", err)
	}
	if got := string(output); got != display {
		t.Errorf("display argument = %q, want %q", got, display)
	}
}

func TestSSHStreamBidirectionalAndReaped(t *testing.T) {
	stream := startSSHHelper(t, false, "echo")
	if _, err := stream.Write([]byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(stream, reply); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if string(reply) != "pong" {
		t.Errorf("reply = %q, want pong", reply)
	}
	buffer := make([]byte, 1)
	if n, err := stream.Read(buffer); n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("final Read = %d, %v, want 0, EOF", n, err)
	}
	if stream.command.ProcessState == nil {
		t.Error("SSH helper was not reaped")
	}
	if err := stream.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

func TestSSHStreamReportsSSHPassExit(t *testing.T) {
	stream := startSSHHelper(t, true, "exit5")
	defer stream.Close()

	_, err := stream.Read(make([]byte, 1))
	if err == nil || !strings.Contains(err.Error(), "invalid password") {
		t.Fatalf("Read error = %v, want sshpass invalid-password error", err)
	}
}

func TestSSHStreamCloseReapsProcess(t *testing.T) {
	stream := startSSHHelper(t, false, "wait")
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if stream.command.ProcessState == nil {
		t.Error("SSH helper was not reaped")
	}
}

func TestStartSSHInvocationFailure(t *testing.T) {
	_, err := startSSHInvocation(sshInvocation{
		executable: filepath.Join(t.TempDir(), "missing-ssh"),
		env:        os.Environ(),
	})
	if err == nil || !strings.Contains(err.Error(), "starting ssh") {
		t.Fatalf("error = %v, want process start error", err)
	}
}

func startSSHHelper(t *testing.T, usesSSHPass bool, mode string) *sshStream {
	t.Helper()
	environment := setEnvironment(os.Environ(), "GO_XPRA_SSH_HELPER", "1")
	stream, err := startSSHInvocation(sshInvocation{
		executable:  os.Args[0],
		args:        []string{"-test.run=TestSSHHelperProcess", "--", mode},
		env:         environment,
		usesSSHPass: usesSSHPass,
	})
	if err != nil {
		t.Fatalf("startSSHInvocation: %v", err)
	}
	return stream
}

func TestSSHHelperProcess(t *testing.T) {
	if os.Getenv("GO_XPRA_SSH_HELPER") != "1" {
		return
	}
	mode := ""
	for index, argument := range os.Args {
		if argument == "--" && index+1 < len(os.Args) {
			mode = os.Args[index+1]
			break
		}
	}
	switch mode {
	case "echo":
		request := make([]byte, 4)
		_, _ = io.ReadFull(os.Stdin, request)
		_, _ = os.Stdout.Write([]byte("pong"))
	case "exit5":
		os.Exit(5)
	case "wait":
		_, _ = io.Copy(io.Discard, os.Stdin)
	default:
		os.Exit(2)
	}
	os.Exit(0)
}

func environmentValue(environment []string, name string) string {
	prefix := name + "="
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

func countEnvironment(environment []string, name string) int {
	prefix := name + "="
	count := 0
	for _, entry := range environment {
		if strings.HasPrefix(entry, prefix) {
			count++
		}
	}
	return count
}

func TestSSHProcessError(t *testing.T) {
	got := sshProcessError(errors.New("wait failed"), false)
	if !strings.Contains(got.Error(), "wait failed") {
		t.Errorf("error = %v", got)
	}
}
