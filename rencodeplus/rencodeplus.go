// Package rencodeplus implements xpra's "rencodeplus" packet serialization.
//
// The format is a base-256 relative of bencode: most values are a single type
// tag byte followed by a payload, and the most common small values (short
// strings, short lists, small integers) fold their length or value into the tag
// itself. Every multi-byte integer and float on the wire is big-endian.
//
// The reference implementation is xpra's xpra/net/rencodeplus/rencodeplus.pyx;
// the tag constants below are taken from it verbatim.
package rencodeplus

// Type tags. Values that are not listed here are one of the four "fixed" ranges
// documented on the range constants below.
const (
	chrFloat64 = 44 // followed by 8 bytes, big-endian IEEE-754
	chrList    = 59 // items, then chrTerm
	chrDict    = 60 // key/value pairs, then chrTerm
	chrInt     = 61 // ASCII decimal digits, then chrTerm (bignum)
	chrInt1    = 62 // 1 signed byte
	chrInt2    = 63 // 2 bytes, big-endian int16
	chrInt4    = 64 // 4 bytes, big-endian int32
	chrInt8    = 65 // 8 bytes, big-endian int64
	chrFloat32 = 66 // 4 bytes, big-endian IEEE-754. Decode only: the Python
	// encoder for this is commented out, so we never emit it.
	chrTrue  = 67
	chrFalse = 68
	chrNone  = 69
	chrTerm  = 127
)

// The four ranges that encode a value or a length directly in the tag byte.
const (
	intPosFixedStart = 0
	intPosFixedCount = 44 // tags 0..43 are the integers 0..43

	intNegFixedStart = 70
	intNegFixedCount = 32 // tags 70..101 are the integers -1..-32

	dictFixedStart = 102
	dictFixedCount = 25 // tags 102..126 are dicts of 0..24 pairs

	strFixedStart = 128
	strFixedCount = 64 // tags 128..191 are strings of 0..63 bytes

	listFixedStart = strFixedStart + strFixedCount // 192
	listFixedCount = 64                            // tags 192..255 are lists of 0..63 items
)

// Length-prefixed strings and bytes share the "<decimal length><sep>" form and
// are told apart only by the separator: ':' means the payload is UTF-8 text,
// '/' means it is opaque binary. Getting this backwards is silent and fatal —
// the peer would see b"title" where it expects "title" and every dictionary
// lookup would miss.
const (
	sepText   = ':'
	sepBinary = '/'
)

// Dict is an ordered mapping, used when building packets to send.
//
// The wire format preserves insertion order (Python dicts do), and while xpra
// itself does not care about key order, an ordered type keeps encoding output
// deterministic — which is what makes byte-exact tests against the reference
// implementation possible. Decoding produces a plain map[string]any instead,
// since nothing on the receiving side needs the order.
type Dict []DictEntry

// DictEntry is a single key/value pair of a Dict.
type DictEntry struct {
	Key   string
	Value any
}

// Set appends or replaces the value for key, preserving first-insertion order.
func (d *Dict) Set(key string, value any) {
	for i := range *d {
		if (*d)[i].Key == key {
			(*d)[i].Value = value
			return
		}
	}
	*d = append(*d, DictEntry{Key: key, Value: value})
}
