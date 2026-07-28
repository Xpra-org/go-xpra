# Development tools

Nothing here is part of the client. These are the tools for working on it, under `internal/` so
that nothing outside this module can import them and the release workflow never builds them.

## `mockserver`

A fake xpra server, so that a smoke test of the whole client needs nothing installed but Go:

```shell
go run ./internal/mockserver &
go run ./cmd/go-xpra tcp://127.0.0.1:14500/
```

It listens on `127.0.0.1:14500` unless `-listen` says otherwise, serves one client at a time, and
keeps running afterwards, so the client can be restarted against it as often as you like.

A window titled `go-xpra mock window` appears at 200,150. What to check, and what it means when it
is wrong:

| Expected | If not |
| --- | --- |
| four quadrants — red top left, green top right, blue bottom left, white bottom right | red and blue swapped means the `BGRX` conversion is the wrong way round |
| a grey rectangle 30 pixels in from the top left, 120x60 | it is sent with a rowstride wider than its pixels, so a client that assumes `width*4`, or that ignores the x and y offset, gets this wrong |
| a content area of exactly 400x300 | the window frame is being counted as part of the geometry |

The server logs every packet the client sends back, which is the other half of the test: moving the
pointer, clicking, scrolling and typing should produce `pointer`, `pointer-button` and `key-action`
packets with screen coordinates, X11 button numbers and X11 keysym names. Closing the window should
produce `close-window`, after which the server destroys the window and disconnects, so the client's
whole lifecycle runs end to end.

One line is worth reading closely:

```
<- damage-sequence 1 wid=1 400x300 decode=1us ""
```

The decode time must never be zero. xpra reads a zero as a failed paint, so a client that reports
one looks broken to a real server even though it painted fine.

It is a development tool and not a conformance test. It accepts any hello, never authenticates,
forwards one window, and speaks raw RGB only — so it exercises the pixel path, the input path and
the window lifecycle, but not the JPEG, PNG or WebP decoders, and not the authentication handshake.
