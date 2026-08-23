package protocol

import (
	"bufio"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pierrec/lz4/v4"

	"github.com/Xpra-org/go-xpra/rencodeplus"
)

// writeQueue bounds how many outbound packets may be buffered. Input events
// can be produced faster than a stalled socket drains them; a bounded queue
// applies back-pressure to the sender instead of growing without limit.
const writeQueue = 256

// outbound is one item in the write queue: a frame to write, or — when data is
// nil — a flush marker whose done channel the write loop closes once every
// frame queued ahead of it has reached the socket.
type outbound struct {
	data []byte
	done chan struct{}
}

// Xpra rejects a fourth pending raw chunk. Chunks are top-level binary packet
// elements sent before the encoded main packet that names their indexes.
const maxRawChunks = 3

type rawChunkState struct {
	packets map[byte][]byte
	size    int
}

func (s *rawChunkState) add(index byte, payload []byte) error {
	if len(payload) == 0 {
		return fmt.Errorf("raw chunk at index %d is empty", index)
	}
	if _, duplicate := s.packets[index]; duplicate {
		return fmt.Errorf("duplicate raw chunk at index %d", index)
	}
	if len(s.packets) >= maxRawChunks {
		return fmt.Errorf("too many raw chunks: maximum is %d", maxRawChunks)
	}
	if len(payload) > maxPayloadSize-s.size {
		return fmt.Errorf("raw chunks exceed the %d byte packet limit", maxPayloadSize)
	}
	if s.packets == nil {
		s.packets = make(map[byte][]byte, maxRawChunks)
	}
	s.packets[index] = payload
	s.size += len(payload)
	return nil
}

func (s *rawChunkState) apply(packet []any, mainSize int) error {
	if len(s.packets) == 0 {
		return nil
	}
	if mainSize > maxPayloadSize-s.size {
		return fmt.Errorf("reassembled packet exceeds the %d byte limit", maxPayloadSize)
	}
	for index := byte(1); index <= maxPacketIndex; index++ {
		payload, ok := s.packets[index]
		if !ok {
			continue
		}
		if int(index) >= len(packet) {
			return fmt.Errorf("raw chunk index %d is outside a %d-element packet", index, len(packet))
		}
		switch placeholder := packet[index].(type) {
		case []byte:
			if len(placeholder) != 0 {
				return fmt.Errorf("raw chunk index %d has a non-empty byte placeholder", index)
			}
		case string:
			if placeholder != "" {
				return fmt.Errorf("raw chunk index %d has a non-empty string placeholder", index)
			}
		default:
			return fmt.Errorf("raw chunk index %d has a %T placeholder", index, packet[index])
		}
		packet[index] = payload
	}
	clear(s.packets)
	s.size = 0
	return nil
}

// Conn is a framed xpra connection over a bidirectional stream.
//
// Incoming packets are decoded on a reader goroutine and delivered on Packets;
// outgoing packets are serialized by a writer goroutine, so Send is safe to
// call from any goroutine. Both loops stop on the first error, which Err
// reports once Packets has been closed.
type Conn struct {
	conn    io.ReadWriteCloser
	packets chan Packet
	writes  chan outbound
	closing chan struct{} // closed by Close, which stops the write loop
	written chan struct{} // closed when the write loop has stopped

	mu     sync.Mutex
	err    error
	closed bool
}

// Dial connects to an xpra server over TCP and starts the read/write loops.
func Dial(address string) (*Conn, error) {
	netConn, err := net.Dial("tcp", address)
	if err != nil {
		return nil, err
	}
	return New(netConn), nil
}

// DialUnix connects to an xpra server over a Unix-domain socket and starts
// the read/write loops.
func DialUnix(path string) (*Conn, error) {
	netConn, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return New(netConn), nil
}

// DialTLS connects to an xpra server over TLS and starts the read/write loops.
// The caller owns the TLS policy, including roots and hostname verification.
func DialTLS(address string, config *tls.Config) (*Conn, error) {
	if config == nil {
		return nil, fmt.Errorf("TLS configuration is required")
	}
	netConn, err := tls.Dial("tcp", address, config)
	if err != nil {
		return nil, err
	}
	return New(netConn), nil
}

// New frames an existing bidirectional stream and starts the read/write loops.
//
// The framing is the same in both directions, so this also serves the accepted
// side of a connection, which is how the mock server stands in for a real one.
func New(stream io.ReadWriteCloser) *Conn {
	c := newConn(stream)
	go c.readLoop()
	go c.writeLoop()
	return c
}

// newConn builds a connection without starting its loops.
func newConn(stream io.ReadWriteCloser) *Conn {
	return &Conn{
		conn:    stream,
		packets: make(chan Packet, 64),
		writes:  make(chan outbound, writeQueue),
		closing: make(chan struct{}),
		written: make(chan struct{}),
	}
}

// Packets returns the channel of incoming packets. It is closed when the
// connection ends, after which Err reports why.
func (c *Conn) Packets() <-chan Packet { return c.packets }

// Err returns the error that ended the connection, or nil for a clean close.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Send encodes and queues a packet. The first element must be the packet type.
//
// It returns an error only if the packet cannot be encoded; a failure to write
// it surfaces through Packets closing, since by then the connection is gone.
func (c *Conn) Send(packet ...any) error {
	payload, err := rencodeplus.Encode(packet)
	if err != nil {
		return fmt.Errorf("encoding %v: %w", packetName(packet), err)
	}
	// One buffer for header and payload: writing them separately would put an
	// 8-byte segment on the wire ahead of every packet.
	frame := append(encodeHeader(FlagRencodeplus|FlagFlush, len(payload)), payload...)

	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil
	}
	select {
	case c.writes <- outbound{data: frame}:
	default:
		// The queue is full, which means the peer has stopped reading. Drop
		// rather than block: the caller is the UI goroutine, and the
		// connection is about to fail anyway.
		return fmt.Errorf("write queue full, dropping %s", packetName(packet))
	}
	return nil
}

func packetName(packet []any) string {
	if len(packet) > 0 {
		if s, ok := packet[0].(string); ok {
			return s
		}
	}
	return "?"
}

// Flush waits for the packets queued so far to reach the socket, so that a
// last packet — the disconnect we send on exit, above all — is not lost to the
// Close that follows it. It gives up after timeout, and returns as soon as the
// connection ends, since a queue that will never drain is not worth waiting
// for. The error it returns is informational: the caller is on its way out.
func (c *Conn) Flush(timeout time.Duration) error {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return c.flushErr()
	}

	done := make(chan struct{})
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case c.writes <- outbound{done: done}:
	case <-c.written:
		return c.flushErr()
	case <-timer.C:
		return fmt.Errorf("timed out after %s queueing the flush marker", timeout)
	}
	select {
	case <-done:
		return nil
	case <-c.written:
		return c.flushErr()
	case <-timer.C:
		return fmt.Errorf("timed out after %s waiting for queued packets to be sent", timeout)
	}
}

// flushErr describes a connection that stopped writing before the flush
// completed, naming the underlying failure when there was one.
func (c *Conn) flushErr() error {
	if err := c.Err(); err != nil {
		return fmt.Errorf("connection ended before the queued packets were sent: %w", err)
	}
	return errors.New("connection closed before the queued packets were sent")
}

// Close shuts the connection down. It is safe to call more than once.
//
// Anything still queued is abandoned; call Flush first to give it a chance to
// go out.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	close(c.closing)
	return c.conn.Close()
}

// fail records the first error and tears the connection down.
func (c *Conn) fail(err error) {
	c.mu.Lock()
	if c.err == nil && !c.closed {
		c.err = err
	}
	c.mu.Unlock()
	c.Close()
}

func (c *Conn) readLoop() {
	defer close(c.packets)

	reader := bufio.NewReaderSize(c.conn, 64*1024)
	headerBuf := make([]byte, HeaderSize)
	var chunks rawChunkState
	for {
		if _, err := io.ReadFull(reader, headerBuf); err != nil {
			if err == io.EOF && len(chunks.packets) != 0 {
				c.fail(fmt.Errorf("connection ended with %d pending raw chunk(s)", len(chunks.packets)))
			} else if err != io.EOF {
				c.fail(fmt.Errorf("reading packet header: %w", err))
			}
			return
		}
		h, err := parseHeader(headerBuf)
		if err != nil {
			c.fail(fmt.Errorf("invalid packet header: %w", err))
			return
		}
		payload := make([]byte, h.length)
		if _, err := io.ReadFull(reader, payload); err != nil {
			c.fail(fmt.Errorf("reading %d byte payload: %w", h.length, err))
			return
		}

		if h.compression != 0 {
			if payload, err = decompress(h.compression, payload); err != nil {
				c.fail(err)
				return
			}
		}
		if h.index != 0 {
			if err := chunks.add(h.index, payload); err != nil {
				c.fail(err)
				return
			}
			continue
		}
		if h.flags&FlagRencodeplus == 0 {
			c.fail(fmt.Errorf("packet is not rencodeplus-encoded (flags %#x)", h.flags))
			return
		}
		decoded, err := rencodeplus.Decode(payload)
		if err != nil {
			c.fail(fmt.Errorf("decoding packet: %w", err))
			return
		}
		list, ok := decoded.([]any)
		if !ok {
			c.fail(fmt.Errorf("packet decoded to %T, want a list", decoded))
			return
		}
		if err := chunks.apply(list, len(payload)); err != nil {
			c.fail(err)
			return
		}
		c.packets <- Packet(list)
	}
}

func (c *Conn) writeLoop() {
	defer close(c.written)

	for {
		select {
		case item := <-c.writes:
			if item.data == nil {
				close(item.done)
				continue
			}
			if _, err := c.conn.Write(item.data); err != nil {
				c.fail(fmt.Errorf("writing packet: %w", err))
				return
			}
		case <-c.closing:
			return
		}
	}
}

// decompress inflates a payload according to the header's compression byte.
//
// We only advertise lz4, so that is the only algorithm a well-behaved server
// will use. xpra frames it as a raw LZ4 block behind a 4-byte little-endian
// uncompressed size — note the endianness, which is the opposite of every
// other length field in the protocol (the Python side uses struct's native
// byte order here, not network order).
func decompress(compression byte, payload []byte) ([]byte, error) {
	switch {
	case compression&compressionLZ4 != 0:
	case compression&compressionZstd != 0:
		return nil, fmt.Errorf("server used zstd compression, which we did not advertise")
	case compression&compressionBrotli != 0:
		return nil, fmt.Errorf("server used brotli compression, which we did not advertise")
	default:
		// No algorithm bit set but a non-zero level means zlib.
		return nil, fmt.Errorf("server used zlib compression, which we did not advertise")
	}
	if len(payload) < 4 {
		return nil, fmt.Errorf("lz4 payload of %d bytes is too short for its size prefix", len(payload))
	}
	size := binary.LittleEndian.Uint32(payload[:4])
	if size > maxPayloadSize {
		return nil, fmt.Errorf("lz4 payload expands to %d bytes, over the %d byte limit", size, maxPayloadSize)
	}
	out := make([]byte, size)
	n, err := lz4.UncompressBlock(payload[4:], out)
	if err != nil {
		return nil, fmt.Errorf("lz4 decompression: %w", err)
	}
	if uint32(n) != size {
		return nil, fmt.Errorf("lz4 produced %d bytes, header said %d", n, size)
	}
	return out, nil
}
