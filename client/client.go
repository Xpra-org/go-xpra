// Package client implements the xpra client state machine: it owns the
// connection, the set of forwarded windows, and the translation in both
// directions between xpra packets and local X11 windows and events.
package client

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/rencodeplus"
	"github.com/Xpra-org/go-xpra/ui"
)

// pingInterval is how often we ping the server. The server pings us
// independently and disconnects on CLIENT_PING_TIMEOUT if we stop echoing,
// which handlePing takes care of.
const pingInterval = 5 * time.Second

// Decode-time sentinels for the draw acknowledgement (xpra/constants.py:127).
// A positive value is the real paint time in microseconds, and zero means the
// draw was skipped — so a successful paint must never report zero.
const (
	decodeError    = -1
	decodeNotFound = -2
)

// Client is the connection state machine. Everything on it is owned by the
// goroutine running Run; the network and desktop reader goroutines only feed it
// through channels.
type Client struct {
	conn    *protocol.Conn
	display ui.Display
	verbose bool

	windows map[int64]ui.Window   // xpra window id -> local window
	byLocal map[ui.WindowID]int64 // local window id -> xpra window id
	focused int64

	serverVersion string
	challengeSeen bool
	username      string
	password      string
	exitErr       error
	quit          chan struct{}
}

// New builds a client over an established connection and local display.
func New(conn *protocol.Conn, display ui.Display, verbose bool, username, password string) *Client {
	return &Client{
		conn:     conn,
		display:  display,
		verbose:  verbose,
		username: username,
		password: password,
		windows:  map[int64]ui.Window{},
		byLocal:  map[ui.WindowID]int64{},
		quit:     make(chan struct{}),
	}
}

// Run performs the handshake and then services packets and local events until
// the connection ends. It returns nil for a clean server-initiated disconnect.
func (c *Client) Run() error {
	if err := c.sendHello("", nil); err != nil {
		return fmt.Errorf("sending hello: %w", err)
	}

	ping := time.NewTicker(pingInterval)
	defer ping.Stop()

	for {
		select {
		case packet, ok := <-c.conn.Packets():
			if !ok {
				if err := c.conn.Err(); err != nil {
					return err
				}
				return c.exitErr
			}
			c.handlePacket(packet)

		case event, ok := <-c.display.Events():
			if !ok {
				return errors.New("lost the connection to the local display")
			}
			c.handleUIEvent(event)

		case <-ping.C:
			c.send("ping", time.Now().UnixMilli())

		case <-c.quit:
			return c.exitErr
		}
	}
}

// stop ends the run loop, reporting err (nil for a clean exit).
func (c *Client) stop(err error) {
	if c.exitErr == nil {
		c.exitErr = err
	}
	select {
	case <-c.quit:
	default:
		close(c.quit)
	}
}

// send queues a packet, logging rather than propagating failures: by the time a
// send fails the connection is already going away, and the read side will
// report the real cause.
func (c *Client) send(packet ...any) {
	if err := c.conn.Send(packet...); err != nil {
		log.Printf("send: %v", err)
	}
}

func (c *Client) debugf(format string, args ...any) {
	if c.verbose {
		log.Printf(format, args...)
	}
}

func (c *Client) sendHello(challengeResponse string, clientSalt []byte) error {
	caps := buildHello(c.username)
	if challengeResponse != "" {
		// The reply to a challenge is a second, complete hello carrying the
		// response (xpra/client/base/client.py:359).
		caps.Set("challenge_response", challengeResponse)
		caps.Set("challenge_client_salt", clientSalt)
	}
	return c.conn.Send("hello", caps)
}

func (c *Client) handlePacket(packet protocol.Packet) {
	// A backwards-compatible server sends the legacy packet names and a modern
	// one sends the current names; normalising here means one case each.
	switch name := protocol.Canonical(packet.Type()); name {
	case "hello":
		c.handleHello(packet)
	case "challenge":
		c.handleChallenge(packet)
	case "startup-complete":
		log.Printf("session ready")
	case "server-event":
		c.handleServerEvent(packet)
	case "notification-show":
		c.handleNotificationShow(packet)
	case "notification-close":
		c.handleNotificationClose(packet)

	case "window-create":
		c.handleNewWindow(packet, false)
	case "new-override-redirect":
		c.handleNewWindow(packet, true)
	case "window-destroy":
		c.handleLostWindow(packet)
	case "window-move-resize":
		c.handleMoveResize(packet)
	case "window-metadata":
		c.handleMetadata(packet)
	case "window-raise":
		c.handleRaiseWindow(packet)
	case "window-bell":
		c.handleBell(packet)
	case "window-draw":
		c.handleDraw(packet)

	case "ping":
		c.handlePing(packet)
	case "ping_echo":
		// Round-trip timing, which this client does not track.

	case "disconnect", "connection-close":
		reason := packet.Str(1)
		log.Printf("server disconnected us: %s", reason)
		c.stop(nil)

	default:
		// Unhandled packets are harmless: the server only sends a packet
		// family if the hello asked for it, so anything here is informational.
		c.debugf("ignoring %s packet", name)
	}
}

// handleServerEvent logs an informational server lifecycle event. Dedicated
// packets such as startup-complete and disconnect remain authoritative, so
// these events never change client state.
func (c *Client) handleServerEvent(packet protocol.Packet) {
	if len(packet) < 2 {
		log.Printf("ignoring malformed server-event packet with no event type")
		return
	}
	eventType := packet.Str(1)
	if eventType == "" {
		log.Printf("ignoring malformed server-event packet with an invalid event type")
		return
	}
	log.Printf("server event: %s", eventType)
	if len(packet) > 2 {
		c.debugf("server event %q arguments: %v", eventType, packet[2:])
	}
}

// handleNotificationShow logs the textual part of a forwarded desktop
// notification. Icons, actions and hints may contain large or binary values
// and have no useful representation in a log, so they are deliberately
// ignored.
func (c *Client) handleNotificationShow(packet protocol.Packet) {
	// [dbus_id, nid, app_name, replaces_nid, app_icon, summary, body, ...]
	if len(packet) < 8 {
		log.Printf("ignoring malformed notification-show packet")
		return
	}
	appName, summary, body := packet.Str(3), packet.Str(6), packet.Str(7)
	if appName == "" {
		log.Printf("notification: %s", summary)
	} else {
		log.Printf("notification from %s: %s", appName, summary)
	}
	for line := range strings.Lines(body) {
		log.Printf("  %s", strings.TrimSuffix(line, "\n"))
	}
}

func (c *Client) handleNotificationClose(packet protocol.Packet) {
	if len(packet) < 2 {
		log.Printf("ignoring malformed notification-close packet")
		return
	}
	c.debugf("notification %d closed", packet.Int(1))
}

func (c *Client) handleHello(packet protocol.Packet) {
	caps := packet.Dict(1)
	c.serverVersion = caps.Str("version")
	log.Printf("connected to xpra server version %s", c.serverVersion)
}

func (c *Client) handleChallenge(packet protocol.Packet) {
	if c.challengeSeen {
		c.stop(errors.New("authentication failed: server rejected the password"))
		return
	}
	c.challengeSeen = true

	pw := c.password
	if pw == "" {
		pw = os.Getenv("XPRA_PASSWORD")
	}
	if pw == "" {
		c.stop(errors.New("server requires a password: include it in the URL or set XPRA_PASSWORD"))
		return
	}
	response, salt, err := challengeReply(packet, pw)
	if err != nil {
		c.stop(fmt.Errorf("authentication: %w", err))
		return
	}
	if err := c.sendHello(response, salt); err != nil {
		c.stop(fmt.Errorf("sending the challenge response: %w", err))
	}
}

func (c *Client) handlePing(packet protocol.Packet) {
	// ["ping_echo", echotime, load1, load5, load15, latency_ms, session_id].
	// The load averages and latency are informational; -1 means "unknown".
	sessionID := ""
	if len(packet) >= 4 {
		sessionID = packet.Str(3)
	}
	c.send("ping_echo", packet.Int(1), 0, 0, 0, -1, sessionID)
}

// packetWindowSize reads the width and height shared by the window-create and
// window-move-resize packets. Window dimensions are USHORT values in the
// window-system protocols, so cap the decoded int64 values before converting
// them to int. This is important on 32-bit clients, where a malicious packet
// could otherwise overflow during that conversion.
func packetWindowSize(packet protocol.Packet) (width, height int) {
	const maxDimension = int64(^uint16(0))
	dimension := func(index int) int {
		return int(min(max(packet.Int(index), 1), maxDimension))
	}
	return dimension(4), dimension(5)
}

// handleNewWindow creates a local window for a new remote one.
//
// The packet is [wid, x, y, w, h, metadata, client_properties]. Override-
// redirect windows arrive either under their own packet type or, from a server
// with backwards compatibility disabled, flagged in the metadata.
func (c *Client) handleNewWindow(packet protocol.Packet, overrideRedirect bool) {
	wid := packet.Int(1)
	x, y := int(packet.Int(2)), int(packet.Int(3))
	width, height := packetWindowSize(packet)
	metadata := packet.Dict(6)
	overrideRedirect = overrideRedirect || metadata.Bool("override-redirect")

	if old, ok := c.windows[wid]; ok {
		// The server reused a window id we still hold; drop the stale one.
		c.forgetWindow(wid, old)
	}
	window, err := c.display.NewWindow(x, y, width, height, overrideRedirect)
	if err != nil {
		log.Printf("cannot create a window for wid %d: %v", wid, err)
		return
	}
	window.SetTitle(metadata.Str("title"))
	window.Map()

	c.windows[wid] = window
	c.byLocal[window.ID()] = wid
	c.debugf("window %d created: %dx%d at %d,%d override-redirect=%v title=%q",
		wid, width, height, x, y, overrideRedirect, metadata.Str("title"))

	// Telling the server the window is mapped is what makes it send the first
	// draw packets (xpra/x11/subsystem/window.py:634). Override-redirect
	// windows are never mapped by the client — the server damages them itself,
	// and it warns if we send a map for one.
	if !overrideRedirect {
		c.send("map-window", wid, x, y, width, height, rencodeplus.Dict{}, rencodeplus.Dict{})
	}
}

func (c *Client) handleLostWindow(packet protocol.Packet) {
	wid := packet.Int(1)
	window, ok := c.windows[wid]
	if !ok {
		return
	}
	c.forgetWindow(wid, window)
	c.debugf("window %d destroyed", wid)
}

func (c *Client) forgetWindow(wid int64, window ui.Window) {
	delete(c.byLocal, window.ID())
	delete(c.windows, wid)
	if c.focused == wid {
		c.focused = 0
	}
	window.Destroy()
}

func (c *Client) handleMoveResize(packet protocol.Packet) {
	wid := packet.Int(1)
	window, ok := c.windows[wid]
	if !ok {
		return
	}
	x, y := int(packet.Int(2)), int(packet.Int(3))
	width, height := packetWindowSize(packet)
	if err := window.MoveResize(x, y, width, height); err != nil {
		log.Printf("resizing window %d: %v", wid, err)
	}
}

func (c *Client) handleMetadata(packet protocol.Packet) {
	wid := packet.Int(1)
	window, ok := c.windows[wid]
	if !ok {
		return
	}
	// Metadata updates are partial: a packet carries only the keys that
	// changed, so an absent key means "unchanged", not "reset".
	metadata := packet.Dict(2)
	if metadata.Has("title") {
		window.SetTitle(metadata.Str("title"))
	}
}

func (c *Client) handleRaiseWindow(packet protocol.Packet) {
	wid := packet.Int(1)
	window, ok := c.windows[wid]
	if !ok {
		c.debugf("cannot raise window %d: not found", wid)
		return
	}
	window.Raise()
}

// handleBell forwards the useful parts of
// [wid, device, percent, pitch, duration, class, id, name] to the local
// desktop. The bell is still meaningful when wid is zero or the window has
// already disappeared, so it deliberately does not require a window lookup.
func (c *Client) handleBell(packet protocol.Packet) {
	c.display.Bell(packet.Int(3), packet.Int(4), packet.Int(5), packet.Str(8))
}

// handleDraw paints one damage rectangle.
//
// The packet is [wid, x, y, w, h, coding, pixels, sequence, rowstride, options].
// Every draw must be acknowledged, including the ones we fail to paint, or the
// server's unacknowledged-packet accounting stalls and it stops sending
// updates for that window (xpra/server/window/compress.py:1882).
func (c *Client) handleDraw(packet protocol.Packet) {
	wid := packet.Int(1)
	x, y := int(packet.Int(2)), int(packet.Int(3))
	width, height := int(packet.Int(4)), int(packet.Int(5))
	coding := packet.Str(6)
	sequence := packet.Int(8)
	rowstride := int(packet.Int(9))
	options := packet.Dict(10)

	ack := func(decodeTime int64, message string) {
		c.send("damage-sequence", sequence, wid, width, height, decodeTime, message)
	}

	window, ok := c.windows[wid]
	if !ok {
		ack(decodeNotFound, "window not found")
		return
	}
	pixels, ok := packet.Bytes(7)
	if !ok {
		ack(decodeError, "no pixel data")
		return
	}

	start := time.Now()
	format := ""
	switch coding {
	case "rgb", "rgb24", "rgb32":
		// Pixel-level compression is opt-in and we never advertise the "lz4"
		// or "zstd" capability that enables it
		// (xpra/server/source/encoding.py), so its presence means the pixels
		// are not what we expect.
		for _, algo := range []string{"lz4", "zstd"} {
			if options.Has(algo) {
				log.Printf("window %d: pixels are %s-compressed, which we did not advertise", wid, algo)
				ack(decodeError, "unexpected "+algo+" compressed pixels")
				return
			}
		}
		// The server always names the layout it used; the fallbacks match what
		// xpra's own client assumes (xpra/client/gui/window/backing.py).
		format = options.Str("rgb_format")
		if format == "" {
			format = map[string]string{"rgb24": "RGB", "rgb32": "RGBX"}[coding]
		}

	case "jpeg", "png", "png/P", "png/L", "webp":
		var err error
		pixels, rowstride, err = decodeImage(coding, pixels, width, height)
		if err != nil {
			log.Printf("window %d: %v", wid, err)
			ack(decodeError, err.Error())
			return
		}
		format = "BGRX"

	default:
		log.Printf("window %d: server sent unsupported encoding %q", wid, coding)
		ack(decodeError, "unsupported encoding "+coding)
		return
	}

	if err := window.Paint(x, y, width, height, pixels, rowstride, format); err != nil {
		log.Printf("window %d: %v", wid, err)
		ack(decodeError, err.Error())
		return
	}
	elapsed := time.Since(start).Microseconds()
	if elapsed <= 0 {
		// The ack encodes failures as zero and negative values, so a paint too
		// fast to measure has to round up rather than down.
		elapsed = 1
	}
	ack(elapsed, "")
}

// handleUIEvent translates a local window event into the packets the server
// expects.
func (c *Client) handleUIEvent(event ui.Event) {
	switch e := event.(type) {
	case ui.Configure:
		c.handleConfigure(e)
	case ui.CloseRequest:
		c.handleCloseRequest(e)
	case ui.Motion:
		c.handleMotion(e)
	case ui.Button:
		c.handleButton(e)
	case ui.Key:
		c.handleKey(e)
	case ui.Focus:
		c.handleFocus(e)
	}
}

// handleConfigure reports a geometry change the local desktop made, so the
// remote application can reflow to match.
func (c *Client) handleConfigure(e ui.Configure) {
	wid, window, ok := c.lookup(e.Window)
	if !ok {
		return
	}
	oldX, oldY, oldW, oldH := window.Geometry()
	if e.X == oldX && e.Y == oldY && e.Width == oldW && e.Height == oldH {
		return
	}
	if err := window.Resized(e.X, e.Y, e.Width, e.Height); err != nil {
		log.Printf("window %d: %v", wid, err)
		return
	}
	c.send("configure-window", wid, e.X, e.Y, e.Width, e.Height, rencodeplus.Dict{})
}

// handleCloseRequest passes the user's close request to the server, which owns
// the decision: the window goes away when a window-destroy packet says so.
func (c *Client) handleCloseRequest(e ui.CloseRequest) {
	wid, _, ok := c.lookup(e.Window)
	if !ok {
		return
	}
	c.debugf("window %d: close requested", wid)
	c.send("close-window", wid)
}

// lookup resolves a local window id to the xpra window it belongs to. Events
// for windows we have already destroyed are routine and simply reported as
// absent.
func (c *Client) lookup(id ui.WindowID) (int64, ui.Window, bool) {
	wid, ok := c.byLocal[id]
	if !ok {
		return 0, nil, false
	}
	window, ok := c.windows[wid]
	if !ok {
		return 0, nil, false
	}
	return wid, window, true
}
