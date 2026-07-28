// Command go-xpra is a minimal xpra client: it connects to a server over TCP
// and displays the forwarded windows on the local desktop, through X11 on Linux
// and through Win32 on Windows.
//
// It supports raw RGB, JPEG, PNG and WebP pixels, and needs no cgo. See the
// README for what is and is not implemented.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/Xpra-org/go-xpra/client"
	"github.com/Xpra-org/go-xpra/protocol"
)

const (
	version        = "0.2.1"
	defaultTCPPort = "14500"
)

type connectionURL struct {
	address  string
	username string
	password string
}

func main() {
	log.SetFlags(log.Ltime)
	verbose := flag.Bool("v", false, "log every packet and window event")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = usage
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-xpra %s\n", version)
		return
	}
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
	fmt.Fprintf(os.Stderr, `usage: go-xpra [-v] tcp://[USERNAME[:PASSWORD]@]HOST[:PORT]/

Connects to an xpra server over TCP and shows its windows on the local desktop.
The default TCP port is 14500.

When credentials are omitted from the URL, the local user name and
XPRA_PASSWORD are used.

Example:
  xpra start :100 --bind-tcp=127.0.0.1:14500 --start=xterm
  go-xpra tcp://127.0.0.1/

`)
	flag.PrintDefaults()
}

func run(rawURL string, verbose bool) error {
	target, err := parseConnectionURL(rawURL)
	if err != nil {
		return err
	}

	display, err := openDisplay()
	if err != nil {
		return err
	}
	defer display.Close()

	conn, err := protocol.Dial(target.address)
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", target.address, err)
	}
	defer conn.Close()

	log.Printf("connected to %s", target.address)
	return client.New(conn, display, verbose, target.username, target.password).Run()
}

// parseConnectionURL accepts the standard Xpra TCP URL form and returns the
// socket address separately from credentials so passwords never reach logs.
func parseConnectionURL(raw string) (connectionURL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		if parseErr, ok := err.(*url.Error); ok {
			err = parseErr.Err
		}
		return connectionURL{}, fmt.Errorf("invalid Xpra URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "tcp") {
		if u.Scheme == "" {
			return connectionURL{}, fmt.Errorf("invalid Xpra URL: a tcp:// URL is required")
		}
		return connectionURL{}, fmt.Errorf("unsupported Xpra URL protocol %q: only tcp is supported", u.Scheme)
	}
	if u.Opaque != "" || u.Host == "" {
		return connectionURL{}, fmt.Errorf("invalid Xpra TCP URL: host is required")
	}
	if u.Path != "" && u.Path != "/" {
		return connectionURL{}, fmt.Errorf("invalid Xpra TCP URL: path must be empty or /")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return connectionURL{}, fmt.Errorf("invalid Xpra TCP URL: query and fragment are not supported")
	}

	host := u.Hostname()
	if host == "" {
		return connectionURL{}, fmt.Errorf("invalid Xpra TCP URL: host is required")
	}
	port := u.Port()
	if port == "" {
		if strings.HasSuffix(u.Host, ":") {
			return connectionURL{}, fmt.Errorf("invalid Xpra TCP URL: port is empty")
		}
		port = defaultTCPPort
	} else {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return connectionURL{}, fmt.Errorf("invalid Xpra TCP URL: port must be between 1 and 65535")
		}
		port = strconv.FormatUint(number, 10)
	}

	target := connectionURL{address: net.JoinHostPort(host, port)}
	if u.User != nil {
		target.username = u.User.Username()
		target.password, _ = u.User.Password()
	}
	return target, nil
}
