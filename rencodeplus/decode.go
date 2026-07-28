package rencodeplus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strconv"
)

// maxDepth caps container nesting while decoding. Packets come straight off the
// network, so a hostile peer could otherwise nest lists deeply enough to blow
// the stack. Real xpra packets are two or three levels deep.
const maxDepth = 64

// Decode deserializes a complete rencodeplus value. It is an error for the
// input to hold trailing bytes beyond the first value.
//
// Wire types map to Go as: text to string, binary to []byte, integers to int64,
// floats to float64, lists to []any, dicts to map[string]any, none to nil.
func Decode(data []byte) (any, error) {
	d := &decoder{data: data}
	v, err := d.value(0)
	if err != nil {
		return nil, err
	}
	if d.pos != len(data) {
		return nil, fmt.Errorf("rencodeplus: %d trailing bytes after value", len(data)-d.pos)
	}
	return v, nil
}

type decoder struct {
	data []byte
	pos  int
}

var errTruncated = errors.New("rencodeplus: truncated input")

// next returns the following n bytes and advances past them. The returned slice
// aliases the input buffer, so callers that keep it (binary payloads) must copy.
func (d *decoder) next(n int) ([]byte, error) {
	if n < 0 || len(d.data)-d.pos < n {
		return nil, errTruncated
	}
	b := d.data[d.pos : d.pos+n]
	d.pos += n
	return b, nil
}

func (d *decoder) value(depth int) (any, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("rencodeplus: nesting deeper than %d", maxDepth)
	}
	if d.pos >= len(d.data) {
		return nil, errTruncated
	}
	tag := d.data[d.pos]
	d.pos++

	switch {
	case tag < intPosFixedStart+intPosFixedCount: // 0..43
		return int64(tag), nil

	case tag >= intNegFixedStart && tag < intNegFixedStart+intNegFixedCount: // 70..101
		return -int64(tag-intNegFixedStart) - 1, nil

	case tag >= strFixedStart && tag < strFixedStart+strFixedCount: // 128..191
		b, err := d.next(int(tag - strFixedStart))
		if err != nil {
			return nil, err
		}
		return string(b), nil

	case tag >= listFixedStart: // 192..255
		return d.list(int(tag-listFixedStart), false, depth)

	case tag >= dictFixedStart && tag < dictFixedStart+dictFixedCount: // 102..126
		return d.dict(int(tag-dictFixedStart), false, depth)

	case tag >= '0' && tag <= '9': // length-prefixed string or bytes
		d.pos-- // the digit is part of the length
		return d.sized()
	}

	switch tag {
	case chrNone:
		return nil, nil
	case chrTrue:
		return true, nil
	case chrFalse:
		return false, nil

	case chrInt1:
		b, err := d.next(1)
		if err != nil {
			return nil, err
		}
		return int64(int8(b[0])), nil
	case chrInt2:
		b, err := d.next(2)
		if err != nil {
			return nil, err
		}
		return int64(int16(binary.BigEndian.Uint16(b))), nil
	case chrInt4:
		b, err := d.next(4)
		if err != nil {
			return nil, err
		}
		return int64(int32(binary.BigEndian.Uint32(b))), nil
	case chrInt8:
		b, err := d.next(8)
		if err != nil {
			return nil, err
		}
		return int64(binary.BigEndian.Uint64(b)), nil
	case chrInt:
		return d.bigInt()

	case chrFloat32:
		b, err := d.next(4)
		if err != nil {
			return nil, err
		}
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b))), nil
	case chrFloat64:
		b, err := d.next(8)
		if err != nil {
			return nil, err
		}
		return math.Float64frombits(binary.BigEndian.Uint64(b)), nil

	case chrList:
		return d.list(0, true, depth)
	case chrDict:
		return d.dict(0, true, depth)
	}
	return nil, fmt.Errorf("rencodeplus: invalid type tag %d at offset %d", tag, d.pos-1)
}

// sized reads the "<decimal length><separator><payload>" form. The separator
// decides the Go type: ':' yields a string, '/' yields a []byte.
func (d *decoder) sized() (any, error) {
	start := d.pos
	for d.pos < len(d.data) && d.data[d.pos] != sepText && d.data[d.pos] != sepBinary {
		d.pos++
	}
	if d.pos >= len(d.data) {
		return nil, errTruncated
	}
	n, err := strconv.Atoi(string(d.data[start:d.pos]))
	if err != nil {
		return nil, fmt.Errorf("rencodeplus: bad length prefix at offset %d: %w", start, err)
	}
	binaryPayload := d.data[d.pos] == sepBinary
	d.pos++ // separator

	b, err := d.next(n)
	if err != nil {
		return nil, err
	}
	if binaryPayload {
		// Copy: b aliases the packet buffer, and pixel data outlives it.
		// Allocate explicitly rather than appending to nil so that a
		// zero-length payload still decodes to a non-nil []byte.
		out := make([]byte, n)
		copy(out, b)
		return out, nil
	}
	return string(b), nil
}

// bigInt reads the ASCII-decimal bignum form. xpra never emits one for a value
// that fits in an int64, so anything that does not fit is a protocol error
// rather than something to represent.
func (d *decoder) bigInt() (any, error) {
	start := d.pos
	for d.pos < len(d.data) && d.data[d.pos] != chrTerm {
		d.pos++
	}
	if d.pos >= len(d.data) {
		return nil, errTruncated
	}
	s := string(d.data[start:d.pos])
	d.pos++ // chrTerm
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("rencodeplus: bignum %q does not fit in int64: %w", s, err)
	}
	return n, nil
}

// list decodes either the fixed-length form (n items) or the terminated form.
func (d *decoder) list(n int, terminated bool, depth int) ([]any, error) {
	out := []any{}
	for i := 0; terminated || i < n; i++ {
		if terminated {
			if d.pos >= len(d.data) {
				return nil, errTruncated
			}
			if d.data[d.pos] == chrTerm {
				d.pos++
				break
			}
		}
		v, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

// dict decodes either the fixed-length form (n pairs) or the terminated form.
func (d *decoder) dict(n int, terminated bool, depth int) (map[string]any, error) {
	out := map[string]any{}
	for i := 0; terminated || i < n; i++ {
		if terminated {
			if d.pos >= len(d.data) {
				return nil, errTruncated
			}
			if d.data[d.pos] == chrTerm {
				d.pos++
				break
			}
		}
		k, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		v, err := d.value(depth + 1)
		if err != nil {
			return nil, err
		}
		// Keys are normally text, but a peer is free to send them as binary
		// (xpra's own server does for a few legacy keys), so accept both.
		switch key := k.(type) {
		case string:
			out[key] = v
		case []byte:
			out[string(key)] = v
		default:
			return nil, fmt.Errorf("rencodeplus: dict key has type %T, want string", k)
		}
	}
	return out, nil
}
