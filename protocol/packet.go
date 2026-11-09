package protocol

// Packet is a decoded xpra packet: a list whose first element is the packet
// type string, followed by that packet type's positional arguments.
//
// Indices match the protocol specification directly, so p.Int(1) is "the first
// argument" exactly as the xpra source describes it. Accessors are lenient and
// return the zero value for a missing or wrongly-typed element, mirroring
// xpra's own typedict accessors; callers validate the fields they care about.
type Packet []any

// Type returns the packet type, or "" for a malformed packet.
func (p Packet) Type() string {
	return p.Str(0)
}

// Int returns element i as an integer. The decoder produces int64 for every
// integer tag, so this covers all of them.
func (p Packet) Int(i int) int64 {
	if v, ok := p.at(i).(int64); ok {
		return v
	}
	return 0
}

// Str returns element i as a string. Binary elements are converted, since some
// xpra versions send a few string-valued fields as bytes.
func (p Packet) Str(i int) string {
	switch v := p.at(i).(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	return ""
}

// Bytes returns element i as a binary payload. It reports false when the
// element is absent or is not binary — pixel data is worth checking.
func (p Packet) Bytes(i int) ([]byte, bool) {
	switch v := p.at(i).(type) {
	case []byte:
		return v, true
	case string:
		// rencodeplus distinguishes the two, but be forgiving on receive.
		return []byte(v), true
	}
	return nil, false
}

// Bool returns element i as a boolean. Integers are treated as truthy when
// non-zero, matching how xpra encodes some legacy flags.
func (p Packet) Bool(i int) bool {
	switch v := p.at(i).(type) {
	case bool:
		return v
	case int64:
		return v != 0
	}
	return false
}

// Dict returns element i as a dictionary, or nil.
func (p Packet) Dict(i int) Dict {
	if v, ok := p.at(i).(map[string]any); ok {
		return Dict(v)
	}
	return nil
}

func (p Packet) at(i int) any {
	if i < 0 || i >= len(p) {
		return nil
	}
	return p[i]
}

// Dict is a decoded dictionary, with the same lenient accessors as Packet.
// The xpra capability dictionaries are nested, so being able to chain
// d.Dict("remote-logging").Bool("receive") keeps the reading code flat.
type Dict map[string]any

// Str returns key k as a string, or "" if absent or of another type.
func (d Dict) Str(k string) string {
	switch v := d[k].(type) {
	case string:
		return v
	case []byte:
		return string(v)
	}
	return ""
}

// Int returns key k as an integer, or 0.
func (d Dict) Int(k string) int64 {
	if v, ok := d[k].(int64); ok {
		return v
	}
	return 0
}

// Bool returns key k as a boolean, or false.
func (d Dict) Bool(k string) bool {
	switch v := d[k].(type) {
	case bool:
		return v
	case int64:
		return v != 0
	}
	return false
}

// Has reports whether key k is present.
func (d Dict) Has(k string) bool {
	_, ok := d[k]
	return ok
}

// Dict returns key k as a nested dictionary, or nil.
func (d Dict) Dict(k string) Dict {
	if v, ok := d[k].(map[string]any); ok {
		return Dict(v)
	}
	return nil
}

// Bytes returns key k as a binary payload, or nil.
func (d Dict) Bytes(k string) []byte {
	switch v := d[k].(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	}
	return nil
}
