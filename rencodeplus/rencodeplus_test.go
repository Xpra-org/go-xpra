package rencodeplus

import (
	"bytes"
	"encoding/hex"
	"math"
	"reflect"
	"strings"
	"testing"
)

// The expected byte strings below were produced by the reference
// implementation, by feeding the same values to xpra.net.rencodeplus.dumps.
// They are ground truth: if these pass, the encoder is on the wire correctly.
var goldenEncode = []struct {
	name string
	in   any
	hex  string
}{
	{"ping", []any{"ping", 1234}, "c28470696e673f04d2"},
	{
		"hello",
		[]any{"hello", Dict{
			{"version", "6.6"},
			{"encoders", []string{"rencodeplus"}},
			{"chunks", false},
		}},
		"c28568656c6c6f698776657273696f6e83362e3688656e636f64657273c18b" +
			"72656e636f6465706c7573866368756e6b7344",
	},
	{
		"draw-shaped",
		[]any{"draw", 1, Dict{{"rgb_format", "BGRX"}, {"flush", 0}}, []byte{0xde, 0xad, 0xbe, 0xef}},
		"c4846472617701688a7267625f666f726d6174844247525885666c75736800342fdeadbeef",
	},

	// Every tag boundary, in both directions.
	{"str63", strings.Repeat("a", 63), "bf" + strings.Repeat("61", 63)},
	{"str64", strings.Repeat("a", 64), "36343a" + strings.Repeat("61", 64)},
	{"empty-list", []any{}, "c0"},
	{"empty-dict", Dict{}, "66"},
	{"empty-str", "", "80"},
	{"empty-bytes", []byte{}, "302f"},
	{"int-min", int64(math.MinInt64), "418000000000000000"},
	{
		"int-edges",
		[]any{127, 128, -128, -129, 32767, 32768, -32768, -32769,
			2147483647, int64(2147483648), -2147483648, int64(-2147483649)},
		"cc3e7f3f00803e803fff7f3f7fff40000080003f800040ffff7fff407fffffff" +
			"410000000080000000408000000041ffffffff7fffffff",
	},
	{
		"fixed-int-edges",
		// 43 is the last value that fits in a tag byte, 44 is the first that
		// needs chrInt1; -32 is the last negative fixed int, -33 the first
		// that does not fit.
		[]any{43, 44, -32, -33},
		"c42b3e2c653edf",
	},
	{"floats", []any{0.0, -1.5, 1e300}, "c32c00000000000000002cbff80000000000002c7e37e43c8800759c"},
	{"specials", []any{true, false, nil}, "c3434445"},
}

func TestEncodeGolden(t *testing.T) {
	for _, tc := range goldenEncode {
		t.Run(tc.name, func(t *testing.T) {
			want, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatalf("bad test fixture: %v", err)
			}
			got, err := Encode(tc.in)
			if err != nil {
				t.Fatalf("Encode(%v): %v", tc.in, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("Encode(%v)\n got %x\nwant %x", tc.in, got, want)
			}
		})
	}
}

// Dicts of 24 and 25 pairs straddle the switch from the fixed-length tag to the
// terminated form; they get their own test because building the fixture inline
// would be unreadable.
func TestEncodeDictLengthBoundary(t *testing.T) {
	for _, n := range []int{24, 25} {
		var d Dict
		for i := 0; i < n; i++ {
			d.Set(string(rune('a'+i)), i)
		}
		got, err := Encode(d)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if n < dictFixedCount {
			if got[0] != byte(dictFixedStart+n) {
				t.Errorf("%d pairs: tag = %d, want %d", n, got[0], dictFixedStart+n)
			}
			if got[len(got)-1] == chrTerm {
				t.Errorf("%d pairs: fixed-length dict must not be terminated", n)
			}
		} else {
			if got[0] != chrDict {
				t.Errorf("%d pairs: tag = %d, want chrDict", n, got[0])
			}
			if got[len(got)-1] != chrTerm {
				t.Errorf("%d pairs: terminated dict must end with chrTerm", n)
			}
		}
		roundTrip(t, got, n)
	}
}

func TestEncodeListLengthBoundary(t *testing.T) {
	for _, n := range []int{63, 64} {
		list := make([]any, n)
		for i := range list {
			list[i] = i
		}
		got, err := Encode(list)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if n < listFixedCount {
			if got[0] != byte(listFixedStart+n) {
				t.Errorf("%d items: tag = %d, want %d", n, got[0], listFixedStart+n)
			}
		} else if got[0] != chrList {
			t.Errorf("%d items: tag = %d, want chrList", n, got[0])
		}
		decoded, err := Decode(got)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if l, ok := decoded.([]any); !ok || len(l) != n {
			t.Errorf("%d items: round trip gave %T of len %d", n, decoded, len(decoded.([]any)))
		}
	}
}

func roundTrip(t *testing.T, encoded []byte, wantPairs int) {
	t.Helper()
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	m, ok := decoded.(map[string]any)
	if !ok {
		t.Fatalf("Decode gave %T, want map[string]any", decoded)
	}
	if len(m) != wantPairs {
		t.Errorf("decoded %d pairs, want %d", len(m), wantPairs)
	}
}

func TestDecodeGolden(t *testing.T) {
	// Decoding normalizes: integers become int64, dicts become plain maps.
	cases := []struct {
		name string
		hex  string
		want any
	}{
		{"ping", "c28470696e673f04d2", []any{"ping", int64(1234)}},
		{"specials", "c3434445", []any{true, false, nil}},
		{"fixed-int-edges", "c42b3e2c653edf", []any{int64(43), int64(44), int64(-32), int64(-33)}},
		{"empty-bytes", "302f", []byte{}},
		{"text-vs-binary", "c283666f6f332f666f6f", []any{"foo", []byte("foo")}},
		{"float32", "2a", nil}, // placeholder, replaced below
	}
	// chrFloat32 is decode-only (the reference encoder never emits it), so it
	// gets a hand-built fixture: tag 66 followed by big-endian float32 1.5.
	cases[len(cases)-1] = struct {
		name string
		hex  string
		want any
	}{"float32", "423fc00000", 1.5}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := hex.DecodeString(tc.hex)
			if err != nil {
				t.Fatalf("bad test fixture: %v", err)
			}
			got, err := Decode(data)
			if err != nil {
				t.Fatalf("Decode(%s): %v", tc.hex, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Decode(%s) = %#v, want %#v", tc.hex, got, tc.want)
			}
		})
	}
}

// The separator byte is the only thing distinguishing text from binary, and
// mixing them up is silent on the wire but breaks every dictionary lookup on
// the far side.
func TestTextAndBinaryStayDistinct(t *testing.T) {
	encoded, err := Encode([]any{"foo", []byte("foo")})
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if !bytes.Contains(encoded, []byte("3/foo")) {
		t.Errorf("binary payload not length-prefixed with '/': %x", encoded)
	}
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	list := decoded.([]any)
	if _, ok := list[0].(string); !ok {
		t.Errorf("text decoded as %T, want string", list[0])
	}
	if _, ok := list[1].([]byte); !ok {
		t.Errorf("binary decoded as %T, want []byte", list[1])
	}
}

// Decoded binary must not alias the packet buffer: pixel data outlives the
// read buffer it arrived in.
func TestDecodedBytesAreCopied(t *testing.T) {
	data, err := hex.DecodeString("332fdeadbe")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got := decoded.([]byte)
	for i := range data {
		data[i] = 0
	}
	if !bytes.Equal(got, []byte{0xde, 0xad, 0xbe}) {
		t.Errorf("decoded bytes aliased the input buffer: got %x", got)
	}
}

func TestRoundTrip(t *testing.T) {
	values := []any{
		"hello", []byte{0, 1, 2}, true, false, nil,
		int64(0), int64(43), int64(44), int64(-1), int64(-32), int64(-33),
		int64(math.MaxInt64), int64(math.MinInt64), 3.5,
		[]any{int64(1), "two", []any{int64(3)}},
	}
	for _, v := range values {
		encoded, err := Encode(v)
		if err != nil {
			t.Fatalf("Encode(%#v): %v", v, err)
		}
		got, err := Decode(encoded)
		if err != nil {
			t.Fatalf("Decode(%x): %v", encoded, err)
		}
		if !reflect.DeepEqual(got, v) {
			t.Errorf("round trip of %#v gave %#v", v, got)
		}
	}
}

// Packets arrive from the network, so malformed input must be an error rather
// than a panic or a hang.
func TestDecodeRejectsMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":              "",
		"truncated int":      "3f01",
		"truncated string":   "8461626 3",
		"truncated sized":    "31303a6162",
		"unterminated list":  "3b0102",
		"unterminated dict":  "3c8161",
		"dict missing value": "678161",
		"invalid tag":        "2d",
		"trailing bytes":     "0000",
		"bad length prefix":  "392f",
		"non-string key":     "670001",
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := hex.DecodeString(strings.ReplaceAll(h, " ", ""))
			if err != nil {
				t.Skipf("fixture %q is not valid hex", h)
			}
			if _, err := Decode(data); err == nil {
				t.Errorf("Decode(%x) succeeded, want error", data)
			}
		})
	}
}

func TestDecodeRejectsDeepNesting(t *testing.T) {
	// A list-of-list-of-... deeper than maxDepth.
	data := bytes.Repeat([]byte{listFixedStart + 1}, maxDepth+2)
	data = append(data, listFixedStart) // innermost empty list
	if _, err := Decode(data); err == nil {
		t.Error("deeply nested input decoded without error")
	}
}

func TestEncodeRejectsUnsupportedType(t *testing.T) {
	if _, err := Encode(struct{ A int }{1}); err == nil {
		t.Error("Encode(struct) succeeded, want error")
	}
}

// map[string]any is encoded in sorted key order so output stays deterministic.
func TestEncodeMapIsDeterministic(t *testing.T) {
	m := map[string]any{"b": 2, "a": 1, "c": 3}
	first, err := Encode(m)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for i := 0; i < 20; i++ {
		again, err := Encode(m)
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("map encoding is not deterministic:\n%x\n%x", first, again)
		}
	}
}
