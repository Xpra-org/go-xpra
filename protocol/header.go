// Package protocol implements xpra's packet framing: the 8-byte header, the
// read/write loops over a stream connection, and typed access to decoded packets.
package protocol

import (
	"encoding/binary"
	"fmt"
)

// HeaderSize is the fixed size of an xpra packet header.
//
// The layout, from xpra/net/protocol/header.py, is struct '!BBBBL':
//
//	0  magic 'P'
//	1  protocol flags (which packet encoder, plus cipher/flush bits)
//	2  compression: low nibble is the level, high bits select the algorithm
//	3  packet index (0 for the main packet, >0 for an out-of-band chunk)
//	4  payload length, big-endian uint32
const HeaderSize = 8

// magic is the first header byte of every xpra packet.
const magic = 'P'

// Protocol flags (header byte 1). Only one encoder bit may be set at a time.
const (
	FlagRencode     = 0x01
	FlagCipher      = 0x02 // AES; we never negotiate encryption
	FlagYAML        = 0x04
	FlagFlush       = 0x08 // "no more packets follow immediately"
	FlagRencodeplus = 0x10
)

// Compression algorithm bits (the high bits of header byte 2). A zero byte
// means the payload is not compressed; otherwise the low nibble is the level.
const (
	compressionLZ4    = 0x10
	compressionBrotli = 0x40
	compressionZstd   = 0x80
	compressionLevel  = 0x0f
)

// maxPayloadSize bounds what we will allocate for a single packet. xpra's own
// limit is 256MB (SocketProtocol.abs_max_packet_size); a full-screen 4K RGB
// frame is ~33MB, so 64MB is generous for a client that never enables mmap or
// video encodings, and it keeps a corrupt length field from exhausting memory.
const maxPayloadSize = 64 << 20

// header is a parsed packet header.
type header struct {
	flags       byte
	compression byte
	index       byte
	length      uint32
}

// encodeHeader writes the 8-byte header for a payload of the given length.
// We only ever send uncompressed, unchunked, rencodeplus-encoded packets.
func encodeHeader(flags byte, payloadLen int) []byte {
	h := make([]byte, HeaderSize)
	h[0] = magic
	h[1] = flags
	h[2] = 0 // compression level: outbound packets are small, never compressed
	h[3] = 0 // packet index: we negotiate chunks=false, so always the main packet
	binary.BigEndian.PutUint32(h[4:], uint32(payloadLen))
	return h
}

// parseHeader validates and decodes an 8-byte header.
func parseHeader(b []byte) (header, error) {
	if len(b) < HeaderSize {
		return header{}, fmt.Errorf("short header: %d bytes", len(b))
	}
	if b[0] != magic {
		return header{}, fmt.Errorf("bad magic byte %#x, want %#x", b[0], magic)
	}
	h := header{
		flags:       b[1],
		compression: b[2],
		index:       b[3],
		length:      binary.BigEndian.Uint32(b[4:]),
	}
	if h.length == 0 {
		// xpra treats a zero-length payload as a protocol error, since every
		// packet is at least a one-element list.
		return header{}, fmt.Errorf("zero-length payload")
	}
	if h.length > maxPayloadSize {
		return header{}, fmt.Errorf("payload of %d bytes exceeds the %d byte limit", h.length, maxPayloadSize)
	}
	return h, nil
}
