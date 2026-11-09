# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

go-xpra is a minimal [Xpra](https://xpra.org/) client written in pure Go with no cgo. It has
native backends for X11 and Wayland on Linux, Win32 on Windows, and AppKit on macOS. `README.md`
explains the design in depth; read it before making structural changes.

## Commands

```shell
go build ./cmd/go-xpra                       # build the client
go test ./...                                # all tests (what CI runs, on Linux and Windows)
go test ./client -run TestWindowAckShape -v  # a single test
go vet ./...                                 # also run with GOOS=windows and GOOS=darwin
gofmt -l .                                   # should print nothing
```

Each backend compiles only for its own platform. After changing shared code, run
`GOOS=windows go vet ./...` and `GOOS=darwin go vet ./...` as well as the native vet. Release
builds use `CGO_ENABLED=0` (see `.github/workflows/release.yml`).

To run end to end without xpra installed, use the fake server in `internal/mockserver`. It
forwards one window of known pixels. `internal/README.md` describes what should appear on screen.

```shell
go run ./internal/mockserver &
go run ./cmd/go-xpra tcp://127.0.0.1:14500/
```

After changing a dependency, regenerate the dependency map with `python3 docs/dependency-graph.py`,
which writes the committed `docs/index.html`.

## Architecture

- `rencodeplus` is the wire serialization.
- `protocol` handles the 8-byte header framing, lz4, out-of-band chunk reassembly, the
  WebSocket/TLS/Unix transports, and typed `Packet` accessors.
- `client` is the state machine.
- `ui` defines the only interface the client sees of a desktop: `ui.Display`, `ui.Window` and
  six event types.
- `x11`, `wayland`, `win32` and `darwin` implement `ui`.
- `cmd/go-xpra` parses URLs and flags, picks the backend through the build-tagged
  `display_*.go` files, runs the SSH subprocess, handles password prompts and runs the connect
  dialog.

### Concurrency

There is one reader goroutine and one writer goroutine for the connection, plus one goroutine
for the desktop. The main goroutine owns all client state and is the only goroutine that writes
it. Some backends add a coalescing queue between the desktop goroutine and the client.

Each backend has its own threading rule:

- **Windows:** one OS-locked thread owns every window. Other goroutines hand it work through a
  queue, drained when a message is posted to a hidden helper window.
- **Wayland:** one mutex guards the connection's object table. The blocking socket read happens
  outside the mutex, so `ui.Window` calls can issue requests inline.
- **macOS:** AppKit must own the OS main thread, so `main` runs inside `darwin.RunMain`.

### Protocol rules

- Only servers running Xpra 7.0 or later are supported. The hello advertises
  `MinProtocolVersion` {7,0} (`protocol/options.go`), so the server uses only the modern packet
  names.
  - Never send or accept legacy names such as `ping_echo`, `clipboard-token`, `window-draw-ack`,
    `draw`, `new-window` or `disconnect`. The v8 server rejects them.
  - Use `ping-echo`, `clipboard-data`, `window-ack`, `window-draw`, `window-create` and
    `connection-close`.
  - `compatibility_test.go` checks that legacy names are ignored.
- Incoming packets are dispatched by a switch on `packet.Type()` in `Client.handlePacket`
  (`client/client.go`). The hello capabilities are built in `client/hello.go`. The server sends
  only what the hello advertises, so a feature that is left out costs nothing.
- The hello asks for `BGRX` and `BGRA` pixels because all three window systems use that byte
  order natively. Painting is then a per-row copy with no channel shuffling. Keep it that way.
- `"chunks": true` means large binary values arrive as out-of-band chunks before their packet.
  `protocol` substitutes them back into the packet, so the client never handles them.
- Keys are named the X11 way on every platform. The shared keysym names are in `keysym`.

## Constraints

- Keep the dependency graph small: no cgo, no GUI toolkit. Each backend talks to its window
  system directly through pure-Go bindings (`xgb`, generated Wayland bindings, `syscall` for
  user32 and gdi32, and purego for Objective-C). SSH runs the system `ssh` binary instead of
  linking an SSH client library.
- Commit directly to `main`; there are no feature branches. `v3.x` is a separate stable branch,
  and changes are backported to it only when asked.

## Releasing

To bump the version, update all of these together:

- `internal/version/version.go`
- `CHANGELOG.md`: a new top entry whose bullets link to commits
- `packaging/rpm/go-xpra.spec`: the `Version:` line and `%changelog`. Also update the source
  tarball sha256 in `%prep` once the tag exists.
- `packaging/debian/go-xpra/changelog`
- the `.TH` header (version and date) in `packaging/go-xpra.1`
