//go:build linux

package client

import (
	"os"
	"testing"
)

func TestMmapAreaFileLifecycle(t *testing.T) {
	area, err := newMmapArea()
	if err != nil {
		t.Fatalf("newMmapArea: %v", err)
	}
	path := area.filename
	info, err := os.Stat(path)
	if err != nil {
		area.close()
		t.Fatalf("stat mmap file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mmap file mode = %o, want 600", info.Mode().Perm())
	}
	if info.Size() != int64(len(area.data)) || info.Size() < 64*1024*1024 {
		t.Errorf("mmap file size = %d, mapped size = %d", info.Size(), len(area.data))
	}
	if err := area.close(); err != nil {
		t.Fatalf("close mmap area: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("mmap file still exists after close: %v", err)
	}
}
