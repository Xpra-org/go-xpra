package protocol

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/pierrec/lz4/v4"

	"github.com/Xpra-org/go-xpra/rencodeplus"
)

// writeQueue bounds how many outbound packets may be buffered. Input events
// can be produced faster than a stalled socket drains them; a bounded queue
// applies back-pressure to the sender instead of growing without limit.
const writeQueue = 256

// Conn is a framed xpra connection over a stream socket.
//
// Incoming packets are decoded on a reader goroutine and delivered on Packets;
// outgoing packets are serialized by a writer goroutine, so Send is safe to
// call from any goroutine. Both loops stop on the first error, which Err
// reports once Packets has been closed.
type Conn struct {
	conn    net.Conn
	packets chan Packet
	writes  chan []byte

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
	c := &Conn{
		conn:    netConn,
		packets: make(chan Packet, 64),
		writes:  make(chan []byte, writeQueue),
	}
	go c.readLoop()
	go c.writeLoop()
	return c, nil
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
	case c.writes <- frame:
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

// Close shuts the connection down. It is safe to call more than once.
func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
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
	for {
		if _, err := io.ReadFull(reader, headerBuf); err != nil {
			if err != io.EOF {
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

		// Chunked packets carry a binary element out of band, to be spliced
		// into the main packet by index. We negotiate chunks=false in the
		// hello, so receiving one means the server ignored that capability and
		// the stream is no longer something we can interpret.
		if h.index != 0 {
			c.fail(fmt.Errorf("server sent a chunked packet (index %d) despite chunks=false", h.index))
			return
		}
		if h.compression != 0 {
			if payload, err = decompress(h.compression, payload); err != nil {
				c.fail(err)
				return
			}
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
		c.packets <- Packet(list)
	}
}

func (c *Conn) writeLoop() {
	for frame := range c.writes {
		if _, err := c.conn.Write(frame); err != nil {
			c.fail(fmt.Errorf("writing packet: %w", err))
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
