//go:build linux

package protocol

import (
	"net"
	"path/filepath"
	"testing"
)

func TestDialUnix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "xpra.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("listening on Unix socket: %v", err)
	}
	t.Cleanup(func() { listener.Close() })

	data := frame(t, []any{"ping", 4321}, false)
	serverErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverErr <- err
			return
		}
		defer conn.Close()
		_, err = conn.Write(data)
		serverErr <- err
	}()

	conn, err := DialUnix(path)
	if err != nil {
		t.Fatalf("DialUnix: %v", err)
	}
	defer conn.Close()
	packet, ok := recvPacket(t, conn)
	if !ok {
		t.Fatalf("connection closed: %v", conn.Err())
	}
	if packet.Type() != "ping" || packet.Int(1) != 4321 {
		t.Errorf("got %v, want [ping 4321]", packet)
	}
	if err := <-serverErr; err != nil {
		t.Errorf("Unix server: %v", err)
	}
}
