// Command go-xpra is a minimal xpra client: it connects to a server over TCP
// and displays the forwarded windows on the local X11 display.
//
// It supports raw RGB pixel data only — no image or video codecs — and needs no
// cgo. See the README for what is and is not implemented.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/Xpra-org/go-xpra/client"
	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/x11"
)

func main() {
	log.SetFlags(log.Ltime)
	verbose := flag.Bool("v", false, "log every packet and window event")
	flag.Usage = usage
	flag.Parse()

	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *verbose); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: go-xpra [-v] HOST:PORT

Connects to an xpra server over TCP and shows its windows on $DISPLAY.

Set XPRA_PASSWORD to authenticate against a password-protected server.

Example:
  xpra start :100 --bind-tcp=127.0.0.1:14500 --start=xterm
  go-xpra 127.0.0.1:14500

`)
	flag.PrintDefaults()
}

func run(address string, verbose bool) error {
	address = normalizeAddress(address)

	display, err := x11.Open()
	if err != nil {
		return err
	}
	defer display.Close()

	conn, err := protocol.Dial(address)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", address, err)
	}
	defer conn.Close()

	log.Printf("connected to %s", address)
	return client.New(conn, display, verbose).Run()
}

// normalizeAddress accepts a bare host:port, and also tolerates a tcp:// URI
// so that an address copied from xpra's own output works.
func normalizeAddress(address string) string {
	address = strings.TrimPrefix(address, "tcp://")
	return strings.TrimSuffix(address, "/")
}
