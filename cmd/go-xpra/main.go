// Command go-xpra is a minimal xpra client: it connects to a server over TCP,
// TLS or SSH and displays the forwarded windows on the local desktop, through
// X11 or Wayland on Linux and through Win32 on Windows.
//
// It supports raw RGB, JPEG, PNG and WebP pixels, and needs no cgo. See the
// README for what is and is not implemented.
package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"unicode"

	"github.com/Xpra-org/go-xpra/client"
	"github.com/Xpra-org/go-xpra/protocol"
	"github.com/Xpra-org/go-xpra/ui"
)

const (
	version        = "0.2.1"
	defaultTCPPort = "14500"
	defaultSSHPort = "22"
)

type connectionTransport string

const (
	transportTCP connectionTransport = "tcp"
	transportSSL connectionTransport = "ssl"
	transportSSH connectionTransport = "ssh"
)

type connectionURL struct {
	transport  connectionTransport
	address    string
	serverName string
	port       string
	display    string
	username   string
	password   string
}

type sslOptions struct {
	caCert   string
	insecure bool
}

func main() {
	log.SetFlags(log.Ltime)
	noArguments := len(os.Args) == 1
	verbose := flag.Bool("v", false, "log every packet and window event")
	sslCACert := flag.String("ssl-ca-cert", "", "PEM CA certificate to trust for ssl:// connections")
	sslInsecure := flag.Bool("ssl-insecure", false, "skip certificate verification for ssl:// connections")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = usage
	if err := validateOptionSpelling(os.Args[1:], flag.CommandLine); err != nil {
		fmt.Fprintf(os.Stderr, "go-xpra: %v\n\n", err)
		usage()
		os.Exit(2)
	}
	flag.Parse()

	if *showVersion {
		fmt.Printf("go-xpra %s\n", version)
		return
	}
	ssl := sslOptions{caCert: *sslCACert, insecure: *sslInsecure}
	if noArguments {
		if err := runConnectionDialog(*verbose, ssl); err != nil {
			log.Printf("error: %v", err)
			os.Exit(1)
		}
		return
	}
	if flag.NArg() != 1 {
		usage()
		os.Exit(2)
	}
	if err := run(flag.Arg(0), *verbose, ssl); err != nil {
		log.Printf("error: %v", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: go-xpra
       go-xpra [OPTIONS] (tcp|ssl)://[USERNAME[:PASSWORD]@]HOST[:PORT]/
       go-xpra [OPTIONS] ssh://[USERNAME[:PASSWORD]@]HOST[:PORT]/[DISPLAY]

Connects to an xpra server and shows its windows on the local desktop.
The default TCP/SSL port is 14500 and the default SSH port is 22. ssl://
verifies the server certificate and hostname using the system trust store
unless an SSL option says otherwise.

With no arguments, a connection dialog collects the protocol, credentials,
host, port and optional SSH display.

When credentials are omitted from the URL, the local user name is used.
For TCP/SSL, passwords come from XPRA_PASSWORD or an interactive system
prompt. SSH authentication uses the system ssh client; a password in an SSH
URL is supplied through sshpass.

Example:
  xpra start :100 --bind-tcp=127.0.0.1:14500 --start=xterm
  go-xpra tcp://127.0.0.1/

For a TLS server using a private CA:
  go-xpra --ssl-ca-cert /path/to/ca.pem ssl://server.example.com/

For an existing Xpra display reached through SSH:
  go-xpra ssh://alice@server.example.com/100

`)
	printOptionDefaults(os.Stderr, flag.CommandLine)
}

// validateOptionSpelling enforces the command's long-option convention before
// package flag strips the leading dashes. The standard parser deliberately
// accepts both -name and --name, so it cannot enforce this itself.
func validateOptionSpelling(args []string, flags *flag.FlagSet) error {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || arg == "-" || !strings.HasPrefix(arg, "-") {
			return nil
		}

		name, hasValue := optionName(arg)
		if name == "v" {
			if strings.HasPrefix(arg, "--") {
				return fmt.Errorf("option %q must be written as -v", arg)
			}
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			return fmt.Errorf("option %q must use a double dash (for example, --%s)", arg, name)
		}

		if f := flags.Lookup(name); f != nil && !hasValue && !isBoolFlag(f) {
			// The following argument is this option's value, even if it begins
			// with a dash.
			i++
		}
	}
	return nil
}

func optionName(arg string) (name string, hasValue bool) {
	name = strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-")
	if before, _, found := strings.Cut(name, "="); found {
		return before, true
	}
	return name, false
}

func isBoolFlag(f *flag.Flag) bool {
	boolean, ok := f.Value.(interface{ IsBoolFlag() bool })
	return ok && boolean.IsBoolFlag()
}

func printOptionDefaults(w io.Writer, flags *flag.FlagSet) {
	flags.VisitAll(func(f *flag.Flag) {
		prefix := "--"
		if f.Name == "v" {
			prefix = "-"
		}
		valueName, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(w, "  %s%s", prefix, f.Name)
		if valueName != "" {
			fmt.Fprintf(w, " %s", valueName)
		}
		fmt.Fprintf(w, "\n    \t%s", usage)
		if f.DefValue != "" && f.DefValue != "false" && f.DefValue != "0" {
			defaultValue := f.DefValue
			if getter, ok := f.Value.(flag.Getter); ok {
				if _, ok := getter.Get().(string); ok {
					defaultValue = strconv.Quote(defaultValue)
				}
			}
			fmt.Fprintf(w, " (default %s)", defaultValue)
		}
		fmt.Fprintln(w)
	})
}

func run(rawURL string, verbose bool, ssl sslOptions) error {
	target, err := parseConnectionURL(rawURL)
	if err != nil {
		return err
	}
	tlsConfig, err := makeTLSConfig(target, ssl)
	if err != nil {
		return err
	}

	display, err := openDisplay()
	if err != nil {
		return err
	}
	defer display.Close()

	return connect(target, verbose, ssl, tlsConfig, display)
}

// runConnectionDialog opens the local desktop first so the connection form and
// the forwarded session can share it. This matters on Windows, whose display
// backend owns one process-wide window thread and cannot be opened twice.
func runConnectionDialog(verbose bool, ssl sslOptions) error {
	display, err := openDisplay()
	if err != nil {
		return err
	}
	defer display.Close()

	target, connectRequested, err := promptConnection(display)
	if err != nil || !connectRequested {
		return err
	}
	tlsConfig, err := makeTLSConfig(target, ssl)
	if err != nil {
		return err
	}
	return connect(target, verbose, ssl, tlsConfig, display)
}

func connect(target connectionURL, verbose bool, ssl sslOptions, tlsConfig *tls.Config, display ui.Display) error {
	var conn *protocol.Conn
	var err error
	clientPassword := target.password
	switch target.transport {
	case transportSSL:
		if ssl.insecure {
			log.Printf("warning: TLS certificate verification is disabled")
		}
		conn, err = protocol.DialTLS(target.address, tlsConfig)
	case transportSSH:
		var stream io.ReadWriteCloser
		stream, err = dialSSH(target)
		if err == nil {
			conn = protocol.New(stream)
			// A password in an ssh:// URL belongs to the SSH login. If the
			// proxied Xpra connection separately challenges us, use the normal
			// XPRA_PASSWORD / interactive-prompt path.
			clientPassword = ""
		}
	case transportTCP:
		conn, err = protocol.Dial(target.address)
	default:
		err = fmt.Errorf("unsupported transport %q", target.transport)
	}
	if err != nil {
		return fmt.Errorf("connecting to %s: %w", target.address, err)
	}
	defer conn.Close()

	log.Printf("connected to %s", target.address)
	xpraClient := client.New(conn, display, verbose, target.username, clientPassword)
	promptUsername := target.username
	if promptUsername == "" {
		promptUsername = os.Getenv("USER")
		if promptUsername == "" {
			promptUsername = os.Getenv("USERNAME")
		}
	}
	xpraClient.SetPasswordPrompt(func(description string) (string, error) {
		return promptPassword(promptUsername, target.address, description)
	})
	return xpraClient.Run()
}

// parseConnectionURL accepts the standard Xpra TCP, SSL and SSH URL forms and
// returns connection details separately from credentials so passwords never
// reach logs.
func parseConnectionURL(raw string) (connectionURL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		if parseErr, ok := err.(*url.Error); ok {
			err = parseErr.Err
		}
		return connectionURL{}, fmt.Errorf("invalid Xpra URL: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	transport := connectionTransport(scheme)
	if transport != transportTCP && transport != transportSSL && transport != transportSSH {
		if u.Scheme == "" {
			return connectionURL{}, fmt.Errorf("invalid Xpra URL: a tcp://, ssl:// or ssh:// URL is required")
		}
		return connectionURL{}, fmt.Errorf(
			"unsupported Xpra URL protocol %q: only tcp, ssl and ssh are supported", u.Scheme)
	}
	if u.Opaque != "" || u.Host == "" {
		return connectionURL{}, fmt.Errorf("invalid Xpra %s URL: host is required", strings.ToUpper(scheme))
	}
	if transport != transportSSH && u.Path != "" && u.Path != "/" {
		return connectionURL{}, fmt.Errorf("invalid Xpra %s URL: path must be empty or /", strings.ToUpper(scheme))
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return connectionURL{}, fmt.Errorf("invalid Xpra %s URL: query and fragment are not supported", strings.ToUpper(scheme))
	}

	host := u.Hostname()
	if host == "" {
		return connectionURL{}, fmt.Errorf("invalid Xpra %s URL: host is required", strings.ToUpper(scheme))
	}
	if transport == transportSSH && strings.HasPrefix(host, "-") {
		return connectionURL{}, fmt.Errorf("invalid Xpra SSH URL: host cannot begin with -")
	}
	port := u.Port()
	if port == "" {
		if strings.HasSuffix(u.Host, ":") {
			return connectionURL{}, fmt.Errorf("invalid Xpra %s URL: port is empty", strings.ToUpper(scheme))
		}
		if transport == transportSSH {
			port = defaultSSHPort
		} else {
			port = defaultTCPPort
		}
	} else {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return connectionURL{}, fmt.Errorf("invalid Xpra %s URL: port must be between 1 and 65535", strings.ToUpper(scheme))
		}
		port = strconv.FormatUint(number, 10)
	}

	display := ""
	if transport == transportSSH && u.Path != "" && u.Path != "/" {
		display = strings.TrimPrefix(u.Path, "/")
		if display == "" || strings.Contains(display, "/") ||
			strings.IndexFunc(display, unicode.IsControl) >= 0 {
			return connectionURL{}, fmt.Errorf(
				"invalid Xpra SSH URL: display must be a single non-empty path segment")
		}
	}

	target := connectionURL{
		transport:  transport,
		address:    net.JoinHostPort(host, port),
		serverName: host,
		port:       port,
		display:    display,
	}
	if u.User != nil {
		target.username = u.User.Username()
		target.password, _ = u.User.Password()
	}
	return target, nil
}

func makeTLSConfig(target connectionURL, options sslOptions) (*tls.Config, error) {
	if target.transport != transportSSL {
		if options.caCert != "" || options.insecure {
			return nil, fmt.Errorf("SSL options require an ssl:// URL")
		}
		return nil, nil
	}
	if options.caCert != "" && options.insecure {
		return nil, fmt.Errorf("--ssl-ca-cert and --ssl-insecure cannot be used together")
	}

	config := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		ServerName:         target.serverName,
		InsecureSkipVerify: options.insecure,
	}
	if options.caCert == "" {
		return config, nil
	}

	roots, err := x509.SystemCertPool()
	if err != nil {
		return nil, fmt.Errorf("loading system CA certificates: %w", err)
	}
	if roots == nil {
		roots = x509.NewCertPool()
	}
	pem, err := os.ReadFile(options.caCert)
	if err != nil {
		return nil, fmt.Errorf("reading SSL CA certificate %q: %w", options.caCert, err)
	}
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("SSL CA certificate %q contains no certificates", options.caCert)
	}
	config.RootCAs = roots
	return config, nil
}
