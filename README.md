<p align="center">
  <img src="xpra.png" alt="go-xpra logo" width="320">
</p>

# go-xpra

A minimal [Xpra](https://xpra.org/) client in Go: it connects to a server over TCP, TLS,
WebSocket or SSH and shows the forwarded windows on the local desktop — X11 or Wayland on Linux,
Win32 on Windows.

The scope is deliberately small — connect, show windows, forward input — and the implementation
is pure Go with no cgo.

## Download

Prebuilt binaries are available directly from GitHub on each
[release page](https://github.com/Xpra-org/go-xpra/releases). Linux packages are also available
from the Xpra repositories under the package name `go-xpra`; see the
[Xpra download setup instructions](https://github.com/Xpra-org/xpra/wiki/Download).

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

TCP and TLS connection URLs use Xpra's standard
`(tcp|ssl)://[username[:password]@]host[:port]/` form. The port defaults to 14500. `ssl://`
verifies the server certificate and hostname against the system trust store. Add a private CA
without replacing the system roots:

```shell
./go-xpra --ssl-ca-cert /path/to/ca.pem ssl://server.example.com/
```

WebSocket connections use
`(ws|wss)://[username[:password]@]host[:port]/[path][?query]`. WS defaults to port 80 and WSS
to 443. The path and query are sent in the HTTP upgrade request, which supports deployments behind
a reverse proxy:

```shell
./go-xpra wss://server.example.com/xpra/
```

SSH connections use `ssh://[username[:password]@]host[:port]/[display]`, default to port 22, and
run `xpra _proxy [display]` on the remote host:

```shell
./go-xpra ssh://alice@server.example.com/100
```

The display may be omitted when that account has only one active Xpra session. SSH is provided by
the system `ssh` executable, so host keys, identities, agents, jump hosts and other settings come
from the normal OpenSSH configuration. The remote host must have `xpra` in its `PATH`. OpenSSH
reads interactive host-key and password prompts from its controlling terminal because stdin
carries the Xpra protocol; agent or key authentication is the reliable choice for GUI launches.
An SSH password included in the URL is passed to `sshpass` through its `SSHPASS` environment
variable rather than copying it into the spawned `sshpass` or `ssh` arguments. Supplying one
therefore requires `sshpass` in `PATH`.

Running `./go-xpra` without any arguments opens a connection dialog instead. It offers the
supported `tcp`, `ssl`, `ws`, `wss` and `ssh` protocols and fills in the protocol's default port
automatically. WebSockets expose an endpoint path field, and SSH exposes an optional display
field. The SSH password field is the SSH login password; for TCP, TLS and WebSockets it is the
Xpra protocol password.

`--ssl-insecure` disables certificate and hostname verification for local testing and logs a
warning; it cannot be combined with `--ssl-ca-cert`. SSL options are rejected with `tcp://` URLs.
They are also rejected with `ws://` and `ssh://` URLs, and apply to both `ssl://` and `wss://`.
Credentials can be included in any supported URL. An omitted user name falls back to `USER` (or
`USERNAME` on Windows). When an authenticated Xpra server challenges a connection with no Xpra
password, the client first checks `XPRA_PASSWORD`, then uses `pinentry` on Linux with a hidden
terminal prompt as its fallback, or the native Windows credentials dialog. An SSH URL password is
consumed by SSH and is not reused for this protocol challenge. Pass `-v` to log every window event
and unhandled packet type.

Legacy packet reception is enabled by default, matching Xpra itself. Set
`XPRA_BACKWARDS_COMPATIBLE=0` to accept only the Xpra 6.5+ packet types; outgoing
packets always use the modern names.

On Linux, `--backend` picks between the two backends and defaults to `auto`, which uses X11 when
`$DISPLAY` is set and Wayland otherwise. X11 wins on purpose: a session running XWayland is still
an X session as far as this client is concerned, and it is the better path through one, because
xpra deals in absolute window positions that an X server honours and a Wayland compositor cannot.
Pass `--backend wayland` to use the compositor directly anyway.

## What it does

- plain TCP, TLS, WebSocket, secure WebSocket and SSH-subprocess transports, `rencodeplus` packet
  encoding, inbound `lz4` decompression, and out-of-band binary chunk reassembly
- password authentication (the `hmac+sha256` challenge)
- window create, destroy, move/resize, raise, minimize/restore, title changes, and
  override-redirect popups
- raw RGB, JPEG, PNG (including palette and grayscale PNG), and WebP pixels
- pointer, keyboard and focus forwarding, with keys named the X11 way on every platform
- server-provided PNG pointer cursors, including hotspot changes and default-cursor resets
- server-provided per-window PNG icons on X11 and Windows, and on Wayland compositors supporting
  `xdg-toplevel-icon-v1`
- native Wayland windows through `xdg-shell` and `wl_shm`, with menus as real `xdg_popup`s and
  window frames from `xdg-decoration`
- forwarded application bells using the native X11 or Win32 sound
- desktop notification logging
- server lifecycle event logging
- local window resize is reported back to the server, and the title bar's close button closes the
  remote application

## What it does not do

Everything else: h264 and the other encodings, mmap, clipboard, audio, native notification
popups, system tray, and keymap upload. Linux and Windows only — no macOS.

Anything not advertised in the hello is never sent by the server, so most of that list costs
nothing to leave out.

The Windows backend is newer and thinner than the X11 one. It names keys from the active keyboard
layout, but only as far as printable ASCII: a key that produces anything else has no X11 keysym
name here and is dropped rather than guessed at, so non-Latin layouts and dead keys do not type.

The Wayland backend gives up what the protocol does not offer a client. The compositor decides
where each window goes, so the positions the server sends are kept as bookkeeping and answered
with rather than applied; a raise from the server does nothing, because stacking is the
compositor's alone; and a window can be minimized but not restored, there being no request for it.
There is no bell. Windows have a title bar only under a compositor that implements
`xdg-decoration` — KDE and the wlroots ones do, GNOME and weston do not — and are otherwise bare,
with the close gesture wherever the compositor keeps it; the client says which it found on
startup. Its keyboard is the best of the three: the
compositor hands over its whole keymap, so every layout names its keys correctly.

## Layout

| Package | What it holds |
| --- | --- |
| `rencodeplus` | the packet serialization format, encoder and decoder |
| `protocol` | the 8-byte packet header, the framed connection, typed packet accessors |
| `client` | the state machine, the hello capabilities, and event-to-packet translation |
| `ui` | all the client sees of the desktop: windows, events, pixel conversion |
| `keysym` | X11 keysym names, shared by the two Linux backends |
| `x11` | the X connection, forwarded windows, and RGB painting — Linux |
| `wayland` | the compositor connection, `xdg-shell` windows, and shared-memory painting — Linux |
| `win32` | the window thread, forwarded windows, and RGB painting — Windows |
| `cmd/go-xpra` | the command line entry point |

Four goroutines: one reads packets from the connection stream, one writes them, one belongs to the local
desktop — reading X events, dispatching Wayland ones, or owning the Windows windows and their
message loop — and the main one owns all client state and is the only writer to either. The two
backends that cannot let their desktop goroutine block on a slow client put a coalescing queue in
between, which adds a fifth.

## Notes on the design

Two choices do most of the work of keeping this small.

**`"chunks": true` in the hello.** Large binary values can arrive before their encoded main
packet as separately compressed out-of-band chunks. The protocol reader bounds and stores up to
three of them, then replaces the main packet's empty top-level placeholders by index. That keeps
large pixel and icon payloads out of the serialization layer without changing what the client
state machine sees.

**Building on the window system directly, with no toolkit.** An xpra client wants real top-level
windows at absolute positions, unmanaged override-redirect popups, and keys named the way an X
server names them — all of which map onto a window system's own API directly and onto a GUI
toolkit awkwardly. So each backend talks to its system as it is: `xgb` on X11, generated wire
bindings on Wayland, `user32` and `gdi32` through `syscall` on Windows, all of them pure Go. What
the client sees of any of them is the handful of methods and six events in `ui`.

The hello asks for the `BGRX` and `BGRA` pixel layouts specifically, because they are what all
three systems already want — an X11 `ZPixmap` at depth 24, a 32-bit `BI_RGB` DIB and a `wl_shm`
`xrgb8888` buffer have the same byte order — so painting is a per-row copy with no channel
shuffling.

Where those pixels live is the one place the backends genuinely differ. On X11 each window's
backing store is a pixmap on the server, installed as the window background, so exposures repaint
without the client being involved. Windows has no equivalent, so the buffer is ordinary Go memory
that `WM_PAINT` blits from: no GDI handles to own, no thread to own them on, and a damage
rectangle is still just a copy. Wayland lands between the two — the buffer is an anonymous file
mapped into both processes, so it is ordinary memory to write into and the compositor's to read,
and a repaint is a copy plus the rectangle it touched.

**The Wayland coordinates are a fiction, kept deliberately.** A client cannot place its own
windows and cannot ask where one ended up. But xpra sends absolute positions, and wants pointer
events and configure replies back in the same frame of reference, so what the server says is
remembered and used to answer it while the compositor puts the windows where it likes. Pointer
positions are reported relative to where the server thinks the window is, which keeps the remote
application's idea of the pointer consistent with its own windows, and a menu is anchored to its
parent at the offset the server asked for — which is right whenever the parent is where the
server thinks it is, and close enough when it is not.

**One mutex owns the Wayland connection.** The wire protocol multiplexes every object over one
socket, and the object table behind it is not safe for concurrent use — but the socket read has to
be able to block while the client keeps sending. So the blocking read is left outside the lock and
only the handler that follows it runs inside, which is enough to let every `ui.Window` call issue
its requests inline, with no work queue and no thread of its own.

**One thread owns the windows on Windows.** A window belongs to the thread that created it, and
only that thread may pump its messages, so one goroutine is pinned to a thread with
`runtime.LockOSThread` and does nothing else. The client hands it work through a queue it drains
on a message posted to an invisible helper window — posted to a window rather than to the thread
so that the work still gets done inside the modal loops Windows runs while the user drags or
resizes a window.

## Dependencies

Keeping the dependency graph small is a deliberate constraint here — it is why each window system
is addressed through its own bindings rather than a GUI toolkit, why there is no cgo, and why `ssh`
shells out to the system binary instead of linking a client. What is left is six direct
requirements in `go.mod`.

**[xpra-org.github.io/go-xpra](https://xpra-org.github.io/go-xpra/)** maps what they come to: for
each module, every package it reaches, which of those it is the *only* route to — what dropping it
would really remove — and the same again for the standard library behind them. The page is
committed as `docs/index.html` and is entirely self-contained, so it works offline from a checkout
too.

Linux compiles 245 packages: 39 third-party from 7 modules, 8 of our own, and 198 from the standard
library. Windows compiles 221, of which 16 are third-party from 3 modules — the X11 and Wayland
stacks are Linux-only, so `xgb`, `xgbutil` and `go-wayland` are absent from it entirely. Which
dependency looks expensive depends on whether the standard library is counted: `xgbutil` is the
largest without it, reaching 14 packages of which 9 have no other route, while `coder/websocket` is
4 packages of its own — and, once the standard library is counted, the only route to 110 of the 188
it reaches, nearly all of them `crypto/tls`. The page also flags the three modules `go.sum` pins that no build ever
compiles a line of — `freetype-go`, `graphics-go` and `x/text` — and that `xgb` resolves to v1.3.1
against the v1.3.0 `xgbutil` asks for.

Regenerate it after changing a dependency (needs python 3 and `go`, nothing else):

```shell
python3 docs/dependency-graph.py
```

[`docs/README.md`](docs/README.md) describes what the page shows and how the data is derived.

## Testing

`go test ./...` covers the parts that do not need a server or a display: the `rencodeplus` codec
against byte-exact vectors captured from xpra's own implementation, packet framing including lz4,
out-of-band chunk reassembly and malformed input, WebSocket framing and subprotocol negotiation,
TLS/WSS trust and hostname verification against a local certificate, the authentication digest
against a vector from `xpra.net.digest`, the pixel converters, the event queue, keysym naming, and
the parser for the XKB keymap a Wayland compositor hands over. Each backend's share of that is
compiled only for its own platform, so the keyboard is covered on the machine the tests run on and
CI runs them on both.

`internal/mockserver` covers the next layer without needing xpra installed at all: it is a fake
server that forwards one window of known pixels, so the window lifecycle, the paint path and the
input path can be run end to end with nothing but Go.

```shell
go run ./internal/mockserver &
go run ./cmd/go-xpra tcp://127.0.0.1:14500/
```

SSH can be checked against the same server-side session without opening a TCP listener:

```shell
xpra start :100 --start=xterm
go run ./cmd/go-xpra ssh://localhost/100
```

[internal/README.md](internal/README.md) says what should appear on screen, what it means when it
does not, and what to read in the packets it logs back.

The encodings it does not speak, and anything involving a real application, still need a real
server. A useful check beyond "it looks right" is to compare the client's rendering against the
server's own display pixel for pixel:

```shell
xpra start :100 --bind-tcp=127.0.0.1:14500 --start=xterm
Xvfb :99 -screen 0 1280x800x24 &
DISPLAY=:99 ./go-xpra tcp://127.0.0.1/ &

DISPLAY=:100 import -window "$(DISPLAY=:100 xdotool search --class xterm | head -1)" ref.png
DISPLAY=:99  import -window "$(DISPLAY=:99 xdotool search --name antoine | head -1)" ours.png
compare -metric AE ref.png ours.png null:      # expect 0
```

The Wayland backend needs a compositor rather than an X server, and one nested inside `Xvfb` keeps
the whole thing off the real desktop while still delivering real input — which is what makes
`xdotool` able to drive it:

```shell
Xvfb :99 -screen 0 1280x800x24 &
DISPLAY=:99 WLR_BACKENDS=x11 WLR_RENDERER=pixman labwc &   # any wlroots compositor will do
env -u DISPLAY WAYLAND_DISPLAY=wayland-0 ./go-xpra tcp://127.0.0.1/ &

DISPLAY=:99 xdotool mousemove 500 400 click 1
DISPLAY=:99 xdotool type 'echo {braces} 100%'
WAYLAND_DISPLAY=wayland-0 grim shot.png
```

Unsetting `DISPLAY` is what makes `--backend auto` choose Wayland. Worth exercising by hand there:
typing punctuation, a right-click menu landing next to the pointer rather than at a corner,
resizing the window and watching the remote application reflow, and the compositor's close button
reaching the remote application rather than only the local window.
