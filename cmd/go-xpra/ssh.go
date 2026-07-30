package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

type sshInvocation struct {
	executable  string
	args        []string
	env         []string
	usesSSHPass bool
}

// dialSSH starts the system SSH client and exposes its stdin and stdout as one
// bidirectional stream. SSH owns authentication, host verification and config.
func dialSSH(target connectionURL) (io.ReadWriteCloser, error) {
	invocation, err := makeSSHInvocation(target, exec.LookPath)
	if err != nil {
		return nil, err
	}
	return startSSHInvocation(invocation)
}

func makeSSHInvocation(target connectionURL, lookPath func(string) (string, error)) (sshInvocation, error) {
	sshPath, err := lookPath("ssh")
	if err != nil {
		return sshInvocation{}, fmt.Errorf("finding ssh in PATH: %w", err)
	}

	args := []string{"-x", "-T"}
	if target.port != "" && target.port != defaultSSHPort {
		args = append(args, "-p", target.port)
	}
	if target.username != "" {
		args = append(args, "-l", target.username)
	}
	args = append(args, target.serverName, sshRemoteCommand(target.display))

	invocation := sshInvocation{
		executable: sshPath,
		args:       args,
		env:        os.Environ(),
	}
	if target.password == "" {
		return invocation, nil
	}

	sshpassPath, err := lookPath("sshpass")
	if err != nil {
		return sshInvocation{}, fmt.Errorf(
			"an SSH password was supplied, but sshpass was not found in PATH: %w", err)
	}
	invocation.executable = sshpassPath
	invocation.args = append([]string{"-e", sshPath}, args...)
	invocation.env = setEnvironment(invocation.env, "SSHPASS", target.password)
	invocation.usesSSHPass = true
	return invocation, nil
}

func setEnvironment(environment []string, name, value string) []string {
	prefix := name + "="
	result := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		if !strings.HasPrefix(entry, prefix) {
			result = append(result, entry)
		}
	}
	return append(result, prefix+value)
}

func sshRemoteCommand(display string) string {
	proxy := "xpra _proxy"
	if display != "" {
		proxy += " " + shellQuote(display)
	}
	inner := `if command -v "xpra" > /dev/null 2>&1; then ` + proxy +
		`; else echo "no xpra command found" 1>&2; exit 1; fi`
	return "sh -c " + shellQuote(inner)
}

// shellQuote returns one POSIX shell word. OpenSSH joins the remote command
// arguments into a shell command, so the complete command must be quoted even
// though exec.Command itself does not invoke a local shell.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func startSSHInvocation(invocation sshInvocation) (*sshStream, error) {
	command := exec.Command(invocation.executable, invocation.args...)
	command.Env = invocation.env
	command.Stderr = os.Stderr

	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening ssh stdin: %w", err)
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("opening ssh stdout: %w", err)
	}
	if err := command.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, fmt.Errorf("starting ssh: %w", err)
	}
	return &sshStream{
		stdin:       stdin,
		stdout:      stdout,
		command:     command,
		usesSSHPass: invocation.usesSSHPass,
	}, nil
}

type sshStream struct {
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	command     *exec.Cmd
	usesSSHPass bool

	waitOnce sync.Once
	waitErr  error

	closeOnce sync.Once
	closeErr  error
}

func (s *sshStream) Read(buffer []byte) (int, error) {
	n, err := s.stdout.Read(buffer)
	if n != 0 || !errors.Is(err, io.EOF) {
		return n, err
	}
	if waitErr := s.wait(); waitErr != nil {
		return 0, sshProcessError(waitErr, s.usesSSHPass)
	}
	return 0, io.EOF
}

func (s *sshStream) Write(buffer []byte) (int, error) {
	return s.stdin.Write(buffer)
}

func (s *sshStream) Close() error {
	s.closeOnce.Do(func() {
		stdinErr := s.stdin.Close()
		stdoutErr := s.stdout.Close()
		if s.command.Process != nil && s.command.ProcessState == nil {
			if err := s.command.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				s.closeErr = errors.Join(s.closeErr, fmt.Errorf("stopping ssh: %w", err))
			}
		}
		// Always reap the child, but do not turn the expected exit caused by
		// Close into a connection error. Read reports spontaneous failures.
		_ = s.wait()
		if stdinErr != nil && !errors.Is(stdinErr, os.ErrClosed) {
			s.closeErr = errors.Join(s.closeErr, stdinErr)
		}
		if stdoutErr != nil && !errors.Is(stdoutErr, os.ErrClosed) {
			s.closeErr = errors.Join(s.closeErr, stdoutErr)
		}
	})
	return s.closeErr
}

func (s *sshStream) wait() error {
	s.waitOnce.Do(func() {
		s.waitErr = s.command.Wait()
	})
	return s.waitErr
}

func sshProcessError(err error, usedSSHPass bool) error {
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return fmt.Errorf("ssh process failed: %w", err)
	}
	code := exitErr.ExitCode()
	if usedSSHPass {
		descriptions := map[int]string{
			1: "invalid command line",
			2: "conflicting arguments",
			3: "runtime error",
			4: "unrecognized SSH response",
			5: "invalid password",
			6: "unknown host key",
		}
		if description := descriptions[code]; description != "" {
			return fmt.Errorf("sshpass failed: %s (exit status %d)", description, code)
		}
	}
	return fmt.Errorf("ssh process exited with status %d", code)
}
