//go:build linux

package wayland

import (
	"strconv"
	"strings"

	"github.com/Xpra-org/go-xpra/keysym"
	"github.com/Xpra-org/go-xpra/ui"
)

// keycodeOffset is the constant X11 adds to a Linux evdev code. wl_keyboard
// reports evdev codes, an XKB keymap is written in X11 keycodes, and the server
// wants the X11 one back.
const keycodeOffset = 8

// The two modifier bits this package acts on itself. The rest are passed
// through to the server by name; see ui.ModifierNames.
const (
	modShift = 1 << 0
	modLock  = 1 << 1
)

// keymap resolves an evdev keycode to the X11 keysym name the xpra server
// matches on.
//
// The whole table comes from the compositor: wl_keyboard hands over a complete
// XKB keymap as text, and the names in its xkb_symbols section are already X11
// keysym names — "bracketleft", not "[". That is exactly the naming ui.Key
// documents as load-bearing, so no guessing is needed for any layout, which is
// more than either of the other backends manages.
type keymap struct {
	// levels holds the keysym names of an evdev keycode by shift level: index
	// 0 unshifted, index 1 shifted.
	levels map[uint32][]string
}

// name returns the keysym name a key produces under the given modifier mask,
// and "" for a key the keymap does not describe.
//
// Only shift and caps lock are applied, and only to the first layout group —
// the same simplification effectiveKeysym makes in the X11 backend, and enough
// for the layouts a basic client meets.
func (k *keymap) name(keycode, mods uint32) string {
	levels := k.levels[keycode]
	if len(levels) == 0 {
		return ""
	}
	shift := mods&modShift != 0
	// Caps lock only applies to alphabetic keys, and it inverts shift there.
	if mods&modLock != 0 && isAlphaName(levels[0]) {
		shift = !shift
	}
	if shift && len(levels) > 1 && levels[1] != "" {
		return levels[1]
	}
	return levels[0]
}

func isAlphaName(name string) bool {
	if len(name) != 1 {
		return false
	}
	c := name[0]
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// parseKeymap reads an XKB v1 text keymap into the keycode table.
//
// It never fails: a keymap it cannot make sense of yields an empty table, and
// the keys that come from it are then dropped by the client rather than sent to
// the server under a name it would have to guess at.
func parseKeymap(text string) *keymap {
	k := &keymap{levels: map[uint32][]string{}}

	codes := parseKeycodes(sectionBody(text, "xkb_keycodes"))
	if len(codes) == 0 {
		return k
	}
	for _, statement := range splitStatements(sectionBody(text, "xkb_symbols")) {
		name, levels, ok := parseKeyStatement(statement)
		if !ok {
			continue
		}
		code, ok := codes[name]
		if !ok || code < keycodeOffset {
			continue
		}
		k.levels[code-keycodeOffset] = levels
	}
	return k
}

// sectionBody returns what is between the braces of the named XKB section, and
// "" when there is no such section.
func sectionBody(text, keyword string) string {
	start := strings.Index(text, keyword)
	if start < 0 {
		return ""
	}
	open := strings.IndexByte(text[start:], '{')
	if open < 0 {
		return ""
	}
	open += start

	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[open+1 : i]
			}
		}
	}
	return ""
}

// parseKeycodes reads the "<AE01> = 10;" lines of an xkb_keycodes section.
// Everything else there — the bounds, the aliases, the indicator names — does
// not start with a key name and is skipped.
func parseKeycodes(body string) map[string]uint32 {
	codes := map[string]uint32{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<") {
			continue
		}
		close := strings.IndexByte(line, '>')
		if close < 0 {
			continue
		}
		name := line[:close+1]

		rest := strings.TrimSpace(line[close+1:])
		rest, ok := strings.CutPrefix(rest, "=")
		if !ok {
			continue
		}
		rest, _, _ = strings.Cut(rest, ";")
		code, err := strconv.ParseUint(strings.TrimSpace(rest), 10, 32)
		if err != nil {
			continue
		}
		codes[name] = uint32(code)
	}
	return codes
}

// splitStatements cuts a section body at the semicolons that end its top-level
// statements, so that a "key <X> { ... };" block arrives whole however many
// lines the compositor spread it over.
func splitStatements(body string) []string {
	var statements []string
	depth, quoted, start := 0, false, 0

	for i := 0; i < len(body); i++ {
		switch c := body[i]; {
		case c == '"':
			quoted = !quoted
		case quoted:
		case c == '{' || c == '[':
			depth++
		case c == '}' || c == ']':
			depth--
		case c == ';' && depth <= 0:
			statements = append(statements, body[start:i])
			start = i + 1
		}
	}
	return append(statements, body[start:])
}

// parseKeyStatement pulls the key name and its keysym names out of one
// "key <AE01> { ... }" statement, and reports false for anything else.
func parseKeyStatement(statement string) (name string, levels []string, ok bool) {
	rest := strings.TrimSpace(statement)
	rest, ok = strings.CutPrefix(rest, "key")
	if !ok || rest == "" || !isSpace(rest[0]) {
		return "", nil, false
	}
	rest = strings.TrimSpace(rest)
	close := strings.IndexByte(rest, '>')
	if !strings.HasPrefix(rest, "<") || close < 0 {
		return "", nil, false
	}
	list, ok := symbolList(rest[close+1:])
	if !ok {
		return "", nil, false
	}
	return rest[:close+1], splitSymbols(list), true
}

// symbolList returns the contents of the bracket group holding a key's keysym
// names.
//
// Which group that is takes some care. The short form puts it straight after
// the opening brace — "{ [ Escape ] }" — while the long form labels it, and
// the label brings a bracket group of its own: in "symbols[Group1]= [ 1,
// exclam ]" the first '[' in the statement belongs to "Group1". A statement may
// also carry an actions[Group1] list, which is not keysym names at all. So the
// labelled form is looked for by name, and only a statement without one falls
// back to the group after the brace.
func symbolList(statement string) (string, bool) {
	if label := strings.Index(statement, "symbols["); label >= 0 {
		if assign := strings.Index(statement[label:], "]="); assign >= 0 {
			return bracketGroup(statement[label+assign+len("]="):])
		}
		return "", false
	}
	open := strings.IndexByte(statement, '{')
	if open < 0 {
		return "", false
	}
	return bracketGroup(statement[open+1:])
}

// bracketGroup returns what is inside the first bracket group of s.
func bracketGroup(s string) (string, bool) {
	open := strings.IndexByte(s, '[')
	if open < 0 {
		return "", false
	}
	close := strings.IndexByte(s[open:], ']')
	if close < 0 {
		return "", false
	}
	return s[open+1 : open+close], true
}

// splitSymbols turns "1, exclam, onesuperior" into one name per shift level.
// The placeholders for a level a key does not produce become "", which name
// falls back from.
func splitSymbols(list string) []string {
	levels := strings.Split(list, ",")
	for i, level := range levels {
		levels[i] = symbolName(strings.TrimSpace(level))
	}
	return levels
}

// symbolName resolves one entry of a key's symbol list to an X11 keysym name.
//
// Which form it arrives in depends on the compositor's libxkbcommon: it used to
// write the names, and from version 1.11 it writes the keysym values as hex
// instead. Both say the same thing, and a value is turned back into the name
// the server matches on.
func symbolName(symbol string) string {
	if symbol == "NoSymbol" || symbol == "VoidSymbol" {
		return ""
	}
	if hex, ok := strings.CutPrefix(symbol, "0x"); ok {
		value, err := strconv.ParseUint(hex, 16, 32)
		if err != nil {
			return ""
		}
		return keysym.Name(uint32(value))
	}
	return symbol
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// keyDetails fills in the hints that go alongside the name: the numeric keysym
// and the text the key produces, which the server uses to decide whether caps
// lock should apply.
//
// Only printable ASCII can be worked out from a name alone, and that is all the
// server needs the hints for; for anything else the name carries the meaning on
// its own.
func keyDetails(name string) (keysym int, text string) {
	c := ui.KeysymFor(name)
	if c == 0 {
		return 0, ""
	}
	// In this range the keysym value is the character code.
	return int(c), string(c)
}
