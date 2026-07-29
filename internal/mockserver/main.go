// Command mockserver is a fake xpra server for testing the client without a
// real xpra installation. It shows one window painted in known colours and logs
// everything the client sends back, so a smoke test needs nothing but Go.
//
//	go run ./internal/mockserver &
//	go run ./cmd/go-xpra tcp://127.0.0.1:14500/
//
// The window is deliberately easy to check by eye: four coloured quadrants,
// which make a red/blue channel swap obvious, and a grey rectangle sent with a
// padded rowstride at an offset, which is what a real damage update looks like.
// Closing the window makes the server destroy it and disconnect, so the client's
// whole lifecycle runs end to end.
//
// It is a development tool and not a conformance test: it accepts any hello,
// never authenticates, and speaks raw RGB only.
package main

import (
	"flag"
	"log"
	"net"

	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/rencodeplus"
)

// The window this server forwards, in the geometry the client should end up
// with: the content area, not counting whatever frame the desktop puts round it.
const (
	windowID  = 1
	winX      = 200
	winY      = 150
	winWidth  = 400
	winHeight = 300
)

func main() {
	log.SetFlags(log.Ltime)
	listen := flag.String("listen", "127.0.0.1:14500", "address to listen on")
	flag.Parse()

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	defer ln.Close()
	log.Printf("listening on %s", ln.Addr())

	// One client at a time, serially: this exists to be watched, and two
	// sessions interleaving their logs would only make that harder.
	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Fatalf("accept: %v", err)
		}
		log.Printf("client connected from %s", conn.RemoteAddr())
		serve(protocol.New(conn))
		log.Printf("client gone, waiting for the next one")
	}
}

// serve runs one session, returning when the client disconnects.
func serve(conn *protocol.Conn) {
	defer conn.Close()

	for packet := range conn.Packets() {
		switch packet.Type() {
		case "hello":
			log.Printf("<- hello")
			send(conn, "hello", rencodeplus.Dict{
				{Key: "version", Value: "6.6-mock"},
				{Key: "encoding", Value: rencodeplus.Dict{
					{Key: "core", Value: []string{"rgb24", "rgb32"}},
				}},
			})
			send(conn, "startup-complete")
			send(conn, "window-create", windowID, winX, winY, winWidth, winHeight,
				rencodeplus.Dict{{Key: "title", Value: "go-xpra mock window"}},
				rencodeplus.Dict{})

		case "window-map":
			log.Printf("<- window-map %v", []any(packet)[1:])
			paint(conn)

		case "window-draw-ack":
			// The client must never report a decode time of zero: xpra reads
			// that as a failed paint, so it is worth seeing in the log.
			log.Printf("<- window-draw-ack %d wid=%d %dx%d decode=%dus %q",
				packet.Int(1), packet.Int(2), packet.Int(3), packet.Int(4),
				packet.Int(5), packet.Str(6))

		case "window-close":
			log.Printf("<- window-close %d, destroying it and disconnecting", packet.Int(1))
			send(conn, "window-destroy", windowID)
			send(conn, "connection-close", "test over")

		case "ping", "ping_echo":
			// Keepalive noise, and not what anyone is watching for.

		default:
			// Everything else — pointer, keyboard, focus and configure — is the
			// point of the exercise, so print it as it arrived.
			log.Printf("<- %v", []any(packet))
		}
	}
	if err := conn.Err(); err != nil {
		log.Printf("connection: %v", err)
	}
}

// paint sends the window's pixels: first the whole window, then a partial
// update over the top of it.
func paint(conn *protocol.Conn) {
	pixels := make([]byte, winWidth*winHeight*4)
	for y := range winHeight {
		for x := range winWidth {
			i := (y*winWidth + x) * 4
			var b, g, r byte
			switch {
			case x < winWidth/2 && y < winHeight/2:
				r = 0xff // top left: red
			case y < winHeight/2:
				g = 0xff // top right: green
			case x < winWidth/2:
				b = 0xff // bottom left: blue
			default:
				b, g, r = 0xff, 0xff, 0xff // bottom right: white
			}
			pixels[i], pixels[i+1], pixels[i+2], pixels[i+3] = b, g, r, 0
		}
	}
	draw(conn, 0, 0, winWidth, winHeight, pixels, winWidth*4, 1)

	// A rowstride wider than the pixels it carries, which is what a real server
	// sends whenever its rows are padded, and a rectangle that covers only part
	// of the window, which is what damage actually looks like. Between them
	// they catch a client that assumes rowstride == width*4 or ignores x and y.
	const rw, rh = 120, 60
	const stride = rw*4 + 16
	rect := make([]byte, stride*rh)
	for y := range rh {
		for x := range rw {
			i := y*stride + x*4
			rect[i], rect[i+1], rect[i+2] = 0x40, 0x40, 0x40
		}
	}
	draw(conn, 30, 30, rw, rh, rect, stride, 2)
}

func draw(conn *protocol.Conn, x, y, w, h int, pixels []byte, rowstride, sequence int) {
	send(conn, "window-draw", windowID, x, y, w, h, "rgb32", pixels, sequence, rowstride,
		rencodeplus.Dict{{Key: "rgb_format", Value: "BGRX"}})
	log.Printf("-> window-draw %dx%d at %d,%d rowstride=%d", w, h, x, y, rowstride)
}

func send(conn *protocol.Conn, packet ...any) {
	if err := conn.Send(packet...); err != nil {
		log.Printf("sending %v: %v", packet[0], err)
	}
}
