//go:build linux

package wayland

import (
	"errors"
	"fmt"
	"os"
	"syscall"
)

// sharedMemory is a block of memory both this process and the compositor can
// see: an anonymous file, mapped here and passed over the socket to be mapped
// there.
//
// It is what a window paints into, so it plays the part the backing pixmap
// plays on X11 and the plain Go buffer plays on Windows. Being ordinary mapped
// memory it behaves like the latter — a damage rectangle is a copy into the
// middle of it — with the difference that the compositor reads the same bytes
// rather than being sent a copy of them.
type sharedMemory struct {
	file  *os.File
	bytes []byte
}

// newSharedMemory allocates size bytes of shared memory.
//
// The file is unlinked as soon as it exists: nothing needs to find it by name,
// and the mapping and the descriptor keep it alive for exactly as long as it is
// wanted.
func newSharedMemory(size int) (*sharedMemory, error) {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return nil, errors.New("XDG_RUNTIME_DIR is not set")
	}
	file, err := os.CreateTemp(dir, "go-xpra-shm-*")
	if err != nil {
		return nil, fmt.Errorf("creating a shared memory file: %w", err)
	}
	if err := os.Remove(file.Name()); err != nil {
		file.Close()
		return nil, fmt.Errorf("unlinking a shared memory file: %w", err)
	}
	if err := file.Truncate(int64(size)); err != nil {
		file.Close()
		return nil, fmt.Errorf("sizing %d bytes of shared memory: %w", size, err)
	}

	bytes, err := syscall.Mmap(int(file.Fd()), 0, size,
		syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("mapping %d bytes of shared memory: %w", size, err)
	}
	return &sharedMemory{file: file, bytes: bytes}, nil
}

// close unmaps the memory and releases the file. The compositor keeps its own
// mapping of anything it still holds, so this is safe to call as soon as the
// buffers made from it are gone.
func (m *sharedMemory) close() {
	if m == nil {
		return
	}
	if m.bytes != nil {
		syscall.Munmap(m.bytes)
		m.bytes = nil
	}
	m.file.Close()
}
