# go-xpra

A minimal [Xpra](https://xpra.org/) client in Go: it connects to a server over TCP and shows the
forwarded windows on the local desktop — X11 on Linux, Win32 on Windows.

The scope is deliberately small — connect, show windows, forward input — and the implementation
is pure Go with no cgo.

```shell
go build ./cmd/go-xpra

xpra start :100 --bind-tcp=127.0.0.1:14500 --start=xterm
./go-xpra tcp://127.0.0.1/
```

The same source builds a `go-xpra.exe` that takes the same arguments and needs no X server on the
Windows side: each forwarded window becomes a real Windows window.

Building it under MSYS2 needs the toolchain's own `go.exe` first on `PATH`. The copy in
`$MINGW_PREFIX/bin` is trimmed, so it looks for its `GOROOT` beside itself and does not find one:

```shell
export PATH=$MINGW_PREFIX/lib/go/bin:$PATH
```

Connection URLs use Xpra's standard `tcp://[username[:password]@]host[:port]/` form. The port
defaults to 14500. Other protocols are rejected because this client currently supports TCP only.
Credentials can be included in the URL; omitted values fall back to `USER` and `XPRA_PASSWORD`.
Pass `-v` to log every window event and unhandled packet type.

## What it does

- plain TCP transport, `rencodeplus` packet encoding, inbound `lz4` decompression
- password authentication (the `hmac+sha256` challenge)
- window create, destroy, move/resize, raise, title changes, and override-redirect popups
- raw RGB, JPEG, PNG (including palette and grayscale PNG), and WebP pixels
- pointer, keyboard and focus forwarding, with keys named the X11 way on both platforms
- server lifecycle event logging
- local window resize is reported back to the server, and the title bar's close button closes the
  remote application

## What it does not do

Everything else: h264 and the other encodings, mmap, chunked packets, clipboard,
audio, cursors, window icons, notifications, bell, system tray, keymap upload, and the
`ssl`/`ws`/`wss`/`ssh` transports. Linux and Windows only — no Wayland and no macOS.

Anything not advertised in the hello is never sent by the server, so most of that list costs
nothing to leave out.

The Windows backend is newer and thinner than the X11 one. It names keys from the active keyboard
layout, but only as far as printable ASCII: a key that produces anything else has no X11 keysym
name here and is dropped rather than guessed at, so non-Latin layouts and dead keys do not type.

## Layout

| Package | What it holds |
| --- | --- |
| `rencodeplus` | the packet serialization format, encoder and decoder |
| `protocol` | the 8-byte packet header, the framed connection, typed packet accessors |
| `client` | the state machine, the hello capabilities, and event-to-packet translation |
| `ui` | all the client sees of the desktop: windows, events, pixel conversion |
| `x11` | the X connection, forwarded windows, and RGB painting — Linux |
| `win32` | the window thread, forwarded windows, and RGB painting — Windows |
| `cmd/go-xpra` | the command line entry point |

Four goroutines: one reads packets from the socket, one writes them, one belongs to the local
desktop — reading X events, or owning the Windows windows and their message loop — and the main
one owns all client state and is the only writer to either.

## Notes on the design

Two choices do most of the work of keeping this small.

**`"chunks": false` in the hello.** The server otherwise sends large binary payloads as separate
out-of-band packets to be spliced back in by index. Turning that off makes pixel data arrive
inline and removes the entire reassembly path.

**Building on the window system directly, with no toolkit.** An xpra client wants real top-level
windows at absolute positions, unmanaged override-redirect popups, and keys named the way an X
server names them — all of which map onto a window system's own API directly and onto a GUI
toolkit awkwardly. So each backend talks to its system as it is: `xgb` on X11, `user32` and
`gdi32` through `syscall` on Windows, both of them pure Go. What the client sees of either is the
handful of methods and six events in `ui`.

The hello asks for the `BGRX` and `BGRA` pixel layouts specifically, because they are what both
systems already want — an X11 `ZPixmap` at depth 24 and a 32-bit `BI_RGB` DIB have the same byte
order — so painting is a per-row copy with no channel shuffling.

Where those pixels live is the one place the two backends genuinely differ. On X11 each window's
backing store is a pixmap on the server, installed as the window background, so exposures repaint
without the client being involved. Windows has no equivalent, so the buffer is ordinary Go memory
that `WM_PAINT` blits from: no GDI handles to own, no thread to own them on, and a damage
rectangle is still just a copy.

**One thread owns the windows on Windows.** A window belongs to the thread that created it, and
only that thread may pump its messages, so one goroutine is pinned to a thread with
`runtime.LockOSThread` and does nothing else. The client hands it work through a queue it drains
on a message posted to an invisible helper window — posted to a window rather than to the thread
so that the work still gets done inside the modal loops Windows runs while the user drags or
resizes a window.

## Testing

`go test ./...` covers the parts that do not need a server or a display: the `rencodeplus` codec
against byte-exact vectors captured from xpra's own implementation, packet framing including lz4
and malformed input, the authentication digest against a vector from `xpra.net.digest`, the pixel
converters, and keysym naming. Each backend's half of that last one is compiled only for its own
platform, so the keyboard is covered on the machine the tests run on and CI runs them on both.

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
