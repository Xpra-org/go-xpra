// Package darwin renders xpra windows onto the macOS desktop through AppKit.
//
// It talks to Objective-C the same way the rest of this client talks to its
// platform: without cgo. github.com/ebitengine/purego and its objc
// sub-package resolve AppKit, Foundation and CoreGraphics symbols with
// dlopen/dlsym and drive them through objc_msgSend directly, so building this
// package needs no C toolchain and cross-compiling to darwin from another
// platform stays possible — the same property the X11, Wayland and Win32
// backends already have.
//
// AppKit's one hard constraint that survives the translation is thread
// affinity: [NSApplication run] must execute on the process's real OS main
// thread, not merely a thread this package has pinned itself. See the doc
// comment on RunMain in run.go for how that is arranged, and coords.go for
// the other structural difference from every other backend here — AppKit's
// bottom-left-origin coordinate space.
//
// Painting takes a real trade-off other backends don't have to: Window.Paint
// uploads its whole pixel buffer as a CGImage on every call rather than
// blitting just the damage rectangle, because that is the best-supported
// purego call shape (a single object-argument message send) among the ways
// AppKit can be asked to show pixels — see window.go's blit. True partial
// redraw, through a CALayer delegate, is a documented follow-up rather than
// part of this first cut.
//
// The package has nothing in it on any other platform — cmd/go-xpra selects
// the backend for the system it is built for — so it must keep at least this
// one unconstrained file to remain a buildable package everywhere.
package darwin
