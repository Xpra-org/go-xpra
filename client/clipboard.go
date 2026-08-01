package client

import (
	"log"
	"unicode/utf8"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/rencodeplus"
	"github.com/Xpra-org/go-xpra/ui"
)

const (
	maxClipboardBytes      = 16 * 1024 * 1024
	maxClipboardTokenBytes = 4 * 1024 * 1024
	clipboardSelection     = "CLIPBOARD"
)

var (
	clipboardSelections = []string{clipboardSelection}
	clipboardTargets    = []string{
		"UTF8_STRING",
		"text/plain;charset=utf-8",
		"text/plain",
		"TEXT",
		"STRING",
	}
)

type clipboardRequest struct {
	generation uint64
	target     string
}

func (c *Client) configureClipboard(caps protocol.Dict) {
	c.clipboardReady = false
	c.clipboardCanSend = false
	c.clipboardCanReceive = false
	clear(c.clipboardRequests)
	if c.clipboard == nil || caps == nil || (caps.Has("enabled") && !caps.Bool("enabled")) {
		return
	}
	selections := stringsFrom(caps["selections"])
	if len(selections) > 0 && !containsString(selections, clipboardSelection) {
		c.debugf("server clipboard does not offer %s", clipboardSelection)
		return
	}
	direction := caps.Str("direction")
	if direction == "" {
		direction = "both"
	}
	switch direction {
	case "both":
		c.clipboardCanSend, c.clipboardCanReceive = true, true
	case "to-server":
		c.clipboardCanSend = true
	case "to-client":
		c.clipboardCanReceive = true
	case "disabled":
		return
	default:
		log.Printf("ignoring server clipboard with unknown direction %q", direction)
		return
	}
	c.clipboardReady = true
	c.send("clipboard-enable-selections", clipboardSelections)
	if c.clipboardCanSend && c.clipboardKnown {
		c.sendClipboardToken()
	}
}

func (c *Client) handleClipboardChange(e ui.ClipboardChange) {
	if len(e.Text) > maxClipboardBytes {
		log.Printf("ignoring local clipboard text larger than %d bytes", maxClipboardBytes)
		return
	}
	if !utf8.ValidString(e.Text) {
		log.Printf("ignoring local clipboard text which is not valid UTF-8")
		return
	}
	if c.clipboardKnown && c.clipboardText == e.Text {
		return
	}
	c.clipboardText, c.clipboardKnown = e.Text, true
	if c.clipboardReady && c.clipboardCanSend {
		c.sendClipboardToken()
	}
}

func (c *Client) sendClipboardToken() {
	if !c.clipboardKnown {
		return
	}
	data := []byte(c.clipboardText)
	inline := len(data) <= maxClipboardTokenBytes
	if protocol.BackwardsCompatible {
		packet := []any{"clipboard-token", clipboardSelection, clipboardTargets}
		if inline {
			packet = append(packet, "UTF8_STRING", "UTF8_STRING", 8, "bytes", data, true, true)
		}
		c.send(packet...)
		return
	}
	options := rencodeplus.Dict{
		{Key: "claim", Value: true},
		{Key: "greedy", Value: true},
		{Key: "targets", Value: clipboardTargets},
	}
	if inline {
		options.Set("data", map[string]any{
			"UTF8_STRING": []any{"UTF8_STRING", int64(8), "bytes", data},
		})
	}
	c.send("clipboard-data", clipboardSelection, options)
}

func (c *Client) handleClipboardToken(packet protocol.Packet) {
	if len(packet) < 2 || packet.Str(1) != clipboardSelection || !c.mayReceiveClipboard() {
		return
	}
	claim := true
	if len(packet) >= 10 {
		claim = packet.Bool(8)
	}
	if !claim {
		return
	}
	c.beginRemoteClipboard()
	targets := valueStrings(packetValue(packet, 2))
	if len(packet) >= 8 {
		if text, ok := clipboardWireText(packet.Str(3), packet.Int(5), packet.Str(6), packetValue(packet, 7)); ok {
			c.applyRemoteClipboard(text)
			return
		}
	}
	c.requestRemoteClipboard(targets)
}

func (c *Client) handleClipboardData(packet protocol.Packet) {
	if len(packet) < 3 || packet.Str(1) != clipboardSelection || !c.mayReceiveClipboard() {
		return
	}
	options := packet.Dict(2)
	if options == nil || (options.Has("token") && !options.Bool("token")) ||
		(options.Has("claim") && !options.Bool("claim")) {
		return
	}
	c.beginRemoteClipboard()
	if items, ok := options["data"].(map[string]any); ok {
		for _, target := range clipboardTargets {
			item, present := items[target]
			if !present {
				continue
			}
			parts, ok := item.([]any)
			if !ok || len(parts) < 4 {
				continue
			}
			format, _ := parts[1].(int64)
			encoding, _ := stringValue(parts[2])
			if text, ok := clipboardWireText(target, format, encoding, parts[3]); ok {
				c.applyRemoteClipboard(text)
				return
			}
		}
	}
	c.requestRemoteClipboard(stringsFrom(options["targets"]))
}

func (c *Client) mayReceiveClipboard() bool {
	return c.clipboardReady && c.clipboardCanReceive && c.clipboard != nil
}

func (c *Client) requestRemoteClipboard(targets []string) {
	target := preferredClipboardTarget(targets)
	if target == "" {
		return
	}
	c.nextClipboardRequest++
	id := c.nextClipboardRequest
	c.clipboardRequests[id] = clipboardRequest{generation: c.clipboardGeneration, target: target}
	c.send("clipboard-request", id, clipboardSelection, target)
}

func (c *Client) beginRemoteClipboard() {
	c.clipboardGeneration++
	clear(c.clipboardRequests)
}

func (c *Client) handleClipboardContents(packet protocol.Packet) {
	if len(packet) < 7 {
		return
	}
	id := packet.Int(1)
	request, ok := c.clipboardRequests[id]
	delete(c.clipboardRequests, id)
	if !ok || request.generation != c.clipboardGeneration || packet.Str(2) != clipboardSelection {
		return
	}
	if text, ok := clipboardWireText(request.target, packet.Int(4), packet.Str(5), packetValue(packet, 6)); ok {
		c.applyRemoteClipboard(text)
	}
}

func (c *Client) handleClipboardContentsNone(packet protocol.Packet) {
	if len(packet) >= 2 {
		delete(c.clipboardRequests, packet.Int(1))
	}
}

func (c *Client) handleClipboardRequest(packet protocol.Packet) {
	if len(packet) < 4 {
		return
	}
	id, selection, target := packet.Int(1), packet.Str(2), packet.Str(3)
	if !c.clipboardReady || !c.clipboardCanSend || selection != clipboardSelection {
		c.send("clipboard-contents-none", id, selection)
		return
	}
	if target == "TARGETS" {
		c.send("clipboard-contents", id, selection, "ATOM", 32, "atoms", clipboardTargets, 0)
		return
	}
	if !isClipboardTextTarget(target) || !c.clipboardKnown {
		c.send("clipboard-contents-none", id, selection)
		return
	}
	c.send("clipboard-contents", id, selection, target, 8, "bytes", []byte(c.clipboardText), 0)
}

func (c *Client) handleClipboardEnableSelections(packet protocol.Packet) {
	if len(packet) < 2 || !containsString(valueStrings(packetValue(packet, 1)), clipboardSelection) {
		c.clipboardReady = false
		return
	}
	if c.clipboard != nil && (c.clipboardCanSend || c.clipboardCanReceive) {
		c.clipboardReady = true
		if c.clipboardCanSend && c.clipboardKnown {
			c.sendClipboardToken()
		}
	}
}

func (c *Client) handleClipboardStatus(packet protocol.Packet) {
	if len(packet) < 2 {
		return
	}
	enabled := packet.Bool(1)
	c.clipboardReady = enabled && c.clipboard != nil && (c.clipboardCanSend || c.clipboardCanReceive)
	if !c.clipboardReady {
		clear(c.clipboardRequests)
		return
	}
	if c.clipboardCanSend && c.clipboardKnown {
		c.sendClipboardToken()
	}
}

func (c *Client) applyRemoteClipboard(text string) {
	if len(text) > maxClipboardBytes {
		log.Printf("ignoring remote clipboard text larger than %d bytes", maxClipboardBytes)
		return
	}
	if c.clipboardKnown && c.clipboardText == text {
		return
	}
	if err := c.clipboard.SetText(text); err != nil {
		log.Printf("setting clipboard: %v", err)
		return
	}
	c.clipboardText, c.clipboardKnown = text, true
}

func clipboardWireText(target string, format int64, encoding string, value any) (string, bool) {
	if !isClipboardTextTarget(target) || format != 8 || encoding != "bytes" {
		return "", false
	}
	var data []byte
	switch value := value.(type) {
	case []byte:
		data = value
	case string:
		data = []byte(value)
	default:
		return "", false
	}
	if len(data) > maxClipboardBytes {
		return "", false
	}
	if utf8.Valid(data) {
		return string(data), true
	}
	if target != "STRING" {
		return "", false
	}
	runes := make([]rune, len(data))
	for i, b := range data {
		runes[i] = rune(b)
	}
	return string(runes), true
}

func preferredClipboardTarget(targets []string) string {
	if len(targets) == 0 {
		return "UTF8_STRING"
	}
	for _, preferred := range clipboardTargets {
		if containsString(targets, preferred) {
			return preferred
		}
	}
	return ""
}

func isClipboardTextTarget(target string) bool {
	return containsString(clipboardTargets, target)
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func stringsFrom(value any) []string {
	return valueStrings(value)
}

func valueStrings(value any) []string {
	switch values := value.(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := stringValue(value); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

func stringValue(value any) (string, bool) {
	switch value := value.(type) {
	case string:
		return value, true
	case []byte:
		return string(value), true
	default:
		return "", false
	}
}

func packetValue(packet protocol.Packet, index int) any {
	if index < 0 || index >= len(packet) {
		return nil
	}
	return packet[index]
}
