//go:build !linux

package client

import (
	"errors"
	"os"
)

func mapMmapFile(_ *os.File, _ int) ([]byte, error) {
	return nil, errors.New("mmap screen updates are only supported on Linux")
}

func unmapMmapFile(_ []byte) error {
	return nil
}
