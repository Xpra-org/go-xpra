package rencodeplus

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
)

// Encode serializes v into rencodeplus form.
//
// Go types map to the wire as follows:
//
//	string           text  (":" separator)
//	[]byte           bytes ("/" separator)
//	bool             true/false
//	int, int8..int64 integer
//	uint, uint8..uint64 integer (must fit in an int64)
//	big.Int, *big.Int arbitrary precision integer
//	float32, float64 64-bit float
//	nil              none
//	[]any, []string  list
//	Dict             dict, in insertion order
//	map[string]any   dict, in sorted key order
func Encode(v any) ([]byte, error) {
	var buf []byte
	buf, err := encode(buf, v)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func encode(buf []byte, v any) ([]byte, error) {
	// bool has to be tested before the integer cases: Go would happily let a
	// bool fall through a numeric type switch arm in a hand-written version of
	// this, and the Python encoder has the same ordering requirement.
	switch x := v.(type) {
	case nil:
		return append(buf, chrNone), nil
	case bool:
		if x {
			return append(buf, chrTrue), nil
		}
		return append(buf, chrFalse), nil

	case string:
		return encodeString(buf, x), nil
	case []byte:
		return encodeBytes(buf, x), nil

	case int:
		return encodeInt(buf, int64(x))
	case int8:
		return encodeInt(buf, int64(x))
	case int16:
		return encodeInt(buf, int64(x))
	case int32:
		return encodeInt(buf, int64(x))
	case int64:
		return encodeInt(buf, x)
	case uint8:
		return encodeInt(buf, int64(x))
	case uint16:
		return encodeInt(buf, int64(x))
	case uint32:
		return encodeInt(buf, int64(x))
	case uint:
		if uint64(x) > math.MaxInt64 {
			return nil, fmt.Errorf("rencodeplus: uint %d overflows int64", x)
		}
		return encodeInt(buf, int64(x))
	case uint64:
		if x > math.MaxInt64 {
			return nil, fmt.Errorf("rencodeplus: uint64 %d overflows int64", x)
		}
		return encodeInt(buf, int64(x))
	case big.Int:
		return encodeBigInt(buf, &x)
	case *big.Int:
		if x == nil {
			return nil, fmt.Errorf("rencodeplus: cannot encode a nil *big.Int")
		}
		return encodeBigInt(buf, x)

	case float32:
		return encodeFloat(buf, float64(x)), nil
	case float64:
		return encodeFloat(buf, x), nil

	case []any:
		return encodeList(buf, len(x), func(buf []byte, i int) ([]byte, error) {
			return encode(buf, x[i])
		})
	case []string:
		return encodeList(buf, len(x), func(buf []byte, i int) ([]byte, error) {
			return encodeString(buf, x[i]), nil
		})
	case []int:
		return encodeList(buf, len(x), func(buf []byte, i int) ([]byte, error) {
			return encodeInt(buf, int64(x[i]))
		})

	case Dict:
		return encodeDict(buf, len(x), func(buf []byte, i int) ([]byte, error) {
			buf = encodeString(buf, x[i].Key)
			return encode(buf, x[i].Value)
		})
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return encodeDict(buf, len(keys), func(buf []byte, i int) ([]byte, error) {
			buf = encodeString(buf, keys[i])
			return encode(buf, x[keys[i]])
		})
	}
	return nil, fmt.Errorf("rencodeplus: cannot encode %T", v)
}

func encodeBigInt(buf []byte, n *big.Int) ([]byte, error) {
	if n.IsInt64() {
		return encodeInt(buf, n.Int64())
	}
	buf = append(buf, chrInt)
	buf = n.Append(buf, 10)
	return append(buf, chrTerm), nil
}

func encodeString(buf []byte, s string) []byte {
	if len(s) < strFixedCount {
		return append(append(buf, byte(strFixedStart+len(s))), s...)
	}
	buf = strconv.AppendInt(buf, int64(len(s)), 10)
	return append(append(buf, sepText), s...)
}

// encodeBytes always uses the length-prefixed form: unlike text, binary has no
// fixed-length tag range.
func encodeBytes(buf []byte, b []byte) []byte {
	buf = strconv.AppendInt(buf, int64(len(b)), 10)
	return append(append(buf, sepBinary), b...)
}

func encodeInt(buf []byte, n int64) ([]byte, error) {
	switch {
	case n >= intPosFixedStart && n < intPosFixedStart+intPosFixedCount:
		return append(buf, byte(intPosFixedStart+n)), nil
	case n >= -intNegFixedCount && n < 0:
		return append(buf, byte(intNegFixedStart-1-n)), nil
	case n >= math.MinInt8 && n <= math.MaxInt8:
		return append(buf, chrInt1, byte(int8(n))), nil
	case n >= math.MinInt16 && n <= math.MaxInt16:
		return binary.BigEndian.AppendUint16(append(buf, chrInt2), uint16(int16(n))), nil
	case n >= math.MinInt32 && n <= math.MaxInt32:
		return binary.BigEndian.AppendUint32(append(buf, chrInt4), uint32(int32(n))), nil
	default:
		return binary.BigEndian.AppendUint64(append(buf, chrInt8), uint64(n)), nil
	}
}

func encodeFloat(buf []byte, f float64) []byte {
	return binary.BigEndian.AppendUint64(append(buf, chrFloat64), math.Float64bits(f))
}

func encodeList(buf []byte, n int, item func([]byte, int) ([]byte, error)) ([]byte, error) {
	var err error
	if n < listFixedCount {
		buf = append(buf, byte(listFixedStart+n))
	} else {
		buf = append(buf, chrList)
	}
	for i := 0; i < n; i++ {
		if buf, err = item(buf, i); err != nil {
			return nil, err
		}
	}
	if n >= listFixedCount {
		buf = append(buf, chrTerm)
	}
	return buf, nil
}

func encodeDict(buf []byte, n int, pair func([]byte, int) ([]byte, error)) ([]byte, error) {
	var err error
	if n < dictFixedCount {
		buf = append(buf, byte(dictFixedStart+n))
	} else {
		buf = append(buf, chrDict)
	}
	for i := 0; i < n; i++ {
		if buf, err = pair(buf, i); err != nil {
			return nil, err
		}
	}
	if n >= dictFixedCount {
		buf = append(buf, chrTerm)
	}
	return buf, nil
}
