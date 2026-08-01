//go:build linux

package client

import (
	"os"
	"syscall"
)

func mapMmapFile(file *os.File, size int) ([]byte, error) {
	return syscall.Mmap(int(file.Fd()), 0, size,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
}

func unmapMmapFile(data []byte) error {
	return syscall.Munmap(data)
}
