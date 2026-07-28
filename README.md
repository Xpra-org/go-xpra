# go-xpra

A minimal [Xpra](https://xpra.org/) client in Go: it connects to a server over TCP and shows the
forwarded windows on the local X11 display.

The scope is deliberately small — connect, show windows, forward input — and the implementation
is pure Go with no cgo.

```shell
go build ./cmd/go-xpra

xpra start :100 --bind-tcp=127.0.0.1:14500 --start=xterm
./go-xpra tcp://127.0.0.1/
```

Connection URLs use Xpra's standard `tcp://[username[:password]@]host[:port]/` form. The port
defaults to 14500. Other protocols are rejected because this client currently supports TCP only.
Credentials can be included in the URL; omitted values fall back to `USER` and `XPRA_PASSWORD`.
Pass `-v` to log every window event and unhandled packet type.

## What it does

- plain TCP transport, `rencodeplus` packet encoding, inbound `lz4` decompression
- password authentication (the `hmac+sha256` challenge)
- window create, destroy, move/resize, title changes, and override-redirect popups
- **raw RGB pixels only** — no image or video codecs at all
- pointer, keyboard and focus forwarding
- local window resize is reported back to the server, and the window manager's close button
  closes the remote application

## What it does not do

Everything else: jpeg/png/webp/h264 and the other encodings, mmap, chunked packets, clipboard,
audio, cursors, window icons, notifications, bell, system tray, keymap upload, and the
`ssl`/`ws`/`wss`/`ssh` transports. Linux and X11 only — no Wayland, Windows or macOS.

Anything not advertised in the hello is never sent by the server, so most of that list costs
nothing to leave out.

## Layout

| Package | What it holds |
| --- | --- |
| `rencodeplus` | the packet serialization format, encoder and decoder |
| `protocol` | the 8-byte packet header, the framed connection, typed packet accessors |
| `client` | the state machine, the hello capabilities, and X-event-to-packet translation |
| `x11` | the X connection, forwarded windows, and RGB painting |
| `cmd/go-xpra` | the command line entry point |

Four goroutines: one reads packets from the socket, one writes them, one reads X events, and the
main one owns all client state and is the only writer to either.

## Notes on the design

Two choices do most of the work of keeping this small.

**`"chunks": false` in the hello.** The server otherwise sends large binary payloads as separate
out-of-band packets to be spliced back in by index. Turning that off makes pixel data arrive
inline and removes the entire reassembly path.

**Building directly on X11, via `xgb`.** An xpra client wants real top-level windows at absolute
positions, unmanaged override-redirect popups, and X11 keycodes — all of which map onto Xlib
concepts directly and onto a GUI toolkit awkwardly. It also means each window's backing pixmap can
be the X server's own, installed as the window background, so exposures repaint without the client
being involved and painting a damage rectangle is one `PutImage` plus one `ClearArea`.

The hello asks for the `BGRX` and `BGRA` pixel layouts specifically, because they are what the X
server already wants: painting is then a per-row copy with no channel shuffling.

## Testing

`go test ./...` covers the parts that do not need a server or a display: the `rencodeplus` codec
against byte-exact vectors captured from xpra's own implementation, packet framing including lz4
and malformed input, the authentication digest against a vector from `xpra.net.digest`, the pixel
converters, and keysym naming.

The rest needs a real server. A useful check beyond "it looks right" is to compare the client's
rendering against the server's own display pixel for pixel:

```shell
xpra start :100 --bind-tcp=127.0.0.1:14500 --start=xterm
Xvfb :99 -screen 0 1280x800x24 &
DISPLAY=:99 ./go-xpra tcp://127.0.0.1/ &

DISPLAY=:100 import -window "$(DISPLAY=:100 xdotool search --class xterm | head -1)" ref.png
DISPLAY=:99  import -window "$(DISPLAY=:99 xdotool search --name antoine | head -1)" ours.png
compare -metric AE ref.png ours.png null:      # expect 0
```
