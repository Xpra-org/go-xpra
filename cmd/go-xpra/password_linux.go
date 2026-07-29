//go:build linux

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"unsafe"
)

func promptPassword(username, address, description string) (string, error) {
	password, pinentryErr := promptPasswordPinentry(username, address, description)
	if pinentryErr == nil {
		return password, nil
	}
	password, terminalErr := promptPasswordTerminal(username, address)
	if terminalErr == nil {
		return password, nil
	}
	return "", fmt.Errorf("pinentry failed: %v; terminal prompt failed: %w", pinentryErr, terminalErr)
}

func promptPasswordPinentry(username, address, description string) (password string, err error) {
	path, err := exec.LookPath("pinentry")
	if err != nil {
		return "", err
	}

	command := exec.Command(path)
	command.Env = append(os.Environ(), "GPG_TTY=/dev/tty")
	stdin, err := command.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		return "", err
	}
	defer func() {
		if command.Process != nil {
			_ = command.Process.Kill()
		}
		_ = command.Wait()
	}()

	reader := bufio.NewReader(stdout)
	if _, err := readPinentryResponse(reader); err != nil {
		return "", fmt.Errorf("reading greeting: %w", err)
	}

	// This option lets a curses pinentry use the controlling terminal. GUI
	// implementations may reject it, so its response is deliberately ignored.
	if err := writePinentryCommand(stdin, "OPTION ttyname=/dev/tty"); err == nil {
		_, _ = readPinentryResponse(reader)
	}

	description = strings.TrimSpace(description)
	if description == "" {
		description = "Password"
	}
	context := "Xpra server " + address
	if username != "" {
		context += " requests authentication for " + username
	} else {
		context += " requests authentication"
	}
	for _, setting := range []struct {
		command string
		value   string
	}{
		{"SETTITLE", "Xpra authentication"},
		{"SETDESC", context},
		{"SETPROMPT", description + ":"},
	} {
		if err := writePinentryCommand(stdin, setting.command+" "+escapePinentry(setting.value)); err != nil {
			return "", err
		}
		if _, err := readPinentryResponse(reader); err != nil {
			return "", fmt.Errorf("%s: %w", strings.ToLower(setting.command), err)
		}
	}

	if err := writePinentryCommand(stdin, "GETPIN"); err != nil {
		return "", err
	}
	password, err = readPinentryResponse(reader)
	if err != nil {
		return "", err
	}
	_ = writePinentryCommand(stdin, "BYE")
	return password, nil
}

func writePinentryCommand(w io.Writer, command string) error {
	_, err := fmt.Fprintln(w, command)
	return err
}

// readPinentryResponse consumes one complete Assuan response. Data is
// percent-escaped by pinentry and can be split over more than one D line.
func readPinentryResponse(reader *bufio.Reader) (string, error) {
	var data strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		switch {
		case line == "OK" || strings.HasPrefix(line, "OK "):
			return data.String(), nil
		case line == "D":
			continue
		case strings.HasPrefix(line, "D "):
			decoded, err := url.PathUnescape(strings.TrimPrefix(line, "D "))
			if err != nil {
				return "", fmt.Errorf("invalid escaped data: %w", err)
			}
			data.WriteString(decoded)
		case line == "ERR" || strings.HasPrefix(line, "ERR "):
			return "", errors.New(strings.TrimSpace(strings.TrimPrefix(line, "ERR")))
		case line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "S "):
			// Comments and status lines do not end the response.
		default:
			return "", fmt.Errorf("unexpected response %q", line)
		}
	}
}

func escapePinentry(value string) string {
	replacer := strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A")
	return replacer.Replace(value)
}

func promptPasswordTerminal(username, address string) (string, error) {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return "", fmt.Errorf("opening /dev/tty: %w", err)
	}
	defer tty.Close()

	prompt := "Password"
	if username != "" {
		prompt += " for " + username
	}
	if address != "" {
		prompt += " at " + address
	}
	return readTerminalPassword(tty, prompt+": ")
}

func readTerminalPassword(tty *os.File, prompt string) (string, error) {
	fd := tty.Fd()
	var original syscall.Termios
	if err := ioctlTermios(fd, syscall.TCGETS, &original); err != nil {
		return "", fmt.Errorf("reading terminal settings: %w", err)
	}

	interruptCh := make(chan os.Signal, 1)
	signal.Notify(interruptCh, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer signal.Stop(interruptCh)

	hidden := original
	hidden.Lflag &^= syscall.ECHO | syscall.ECHONL
	if err := ioctlTermios(fd, syscall.TCSETS, &hidden); err != nil {
		return "", fmt.Errorf("disabling terminal echo: %w", err)
	}
	restored := false
	restore := func() {
		if !restored {
			_ = ioctlTermios(fd, syscall.TCSETS, &original)
			restored = true
		}
	}
	defer restore()

	if _, err := io.WriteString(tty, prompt); err != nil {
		return "", err
	}

	type result struct {
		password string
		err      error
	}
	resultCh := make(chan result, 1)
	go func() {
		line, err := bufio.NewReader(tty).ReadString('\n')
		resultCh <- result{strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r"), err}
	}()

	select {
	case result := <-resultCh:
		restore()
		_, _ = io.WriteString(tty, "\n")
		if result.err != nil && !(errors.Is(result.err, io.EOF) && result.password != "") {
			return "", result.err
		}
		return result.password, nil
	case caught := <-interruptCh:
		restore()
		_, _ = io.WriteString(tty, "\n")
		return "", fmt.Errorf("interrupted by %s", caught)
	}
}

func ioctlTermios(fd, request uintptr, termios *syscall.Termios) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(unsafe.Pointer(termios)))
	if errno != 0 {
		return errno
	}
	return nil
}
