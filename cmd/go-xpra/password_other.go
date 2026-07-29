//go:build !linux && !windows

package main

import (
	"errors"
)

func promptPassword(username, address, description string) (string, error) {
	return "", errors.New("interactive password prompting is only supported on Linux and Windows")
}
