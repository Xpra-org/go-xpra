//go:build darwin

package darwin

import (
	"errors"
	"sync"

	"github.com/ebitengine/purego/objc"

	"github.com/Xpra-org/go-xpra/ui"
)

// eventQueueSize is how much local input the client may fall behind before
// presentation-only events begin to be coalesced or discarded, matching the
// other backends.
const eventQueueSize = 256

// Display owns the AppKit connection: the registered view and window-delegate
// classes, the windows created from them, and the session-wide cursor and
// clipboard state. Every field below this comment is touched only on the main
// thread — the one running RunMain's [NSApp run] — the same rule win32's
// Display applies to its window-thread-owned fields.
type Display struct {
	windows     map[objc.ID]*Window // keyed by the owning NSWindow's id
	coordinator objc.ID             // shared NSWindowDelegate instance

	cursor          objc.ID // current session-wide NSCursor, 0 for the system default
	referenceHeight float64 // primary screen height, the coords.go flip anchor

	previousModifierFlags uint64 // last flagsChanged: flags, for edge-detecting a specific key

	clipboardOwnChangeCount int64 // change count produced by our own SetText, to suppress the echo
	clipboardStop           chan struct{}

	tray objc.ID // NSStatusItem, 0 until ShowTray succeeds

	events     chan ui.Event
	eventQueue *ui.EventQueue
	eventStop  chan struct{}
	done       chan struct{}
	once       sync.Once
}

var _ ui.Display = (*Display)(nil)
var _ ui.DesktopSizeProvider = (*Display)(nil)
var _ ui.MonitorProvider = (*Display)(nil)
var _ ui.SystemTrayProvider = (*Display)(nil)
var _ ui.ClipboardProvider = (*Display)(nil)

// Open registers this backend's AppKit classes and connects to the desktop.
//
// One Display per process: registering the view and window-delegate classes
// a second time would fail outright, which is all a client that shows one
// session needs — the same restriction win32.Open documents for the same
// reason.
//
// Open itself may run on any goroutine — cmd/go-xpra's display_darwin.go
// calls it from inside the function RunMain runs in the background — because
// every AppKit-touching step inside it is routed through Display.call onto
// the real main thread, where RunMain's [NSApp run] is already spinning by
// the time this reaches it.
func Open() (*Display, error) {
	d := &Display{
		windows:    map[objc.ID]*Window{},
		events:     make(chan ui.Event),
		eventQueue: ui.NewEventQueue(eventQueueSize),
		eventStop:  make(chan struct{}),
		done:       make(chan struct{}),
	}
	go d.dispatchEvents()

	var setupErr error
	if err := d.call(func() { setupErr = d.setup() }); err != nil {
		return nil, err
	}
	if setupErr != nil {
		return nil, setupErr
	}
	return d, nil
}

func (d *Display) setup() error {
	if err := d.registerClasses(); err != nil {
		return err
	}
	d.coordinator = objc.ID(coordinatorClass).Send(sel_alloc).Send(sel_init)
	d.updateReferenceHeight()

	center := objc.ID(objc.GetClass("NSNotificationCenter")).Send(sel_defaultCenter)
	center.Send(sel_addObserver, d.coordinator, sel_screenParamsChanged,
		nsString("NSApplicationDidChangeScreenParametersNotification"), objc.ID(0))

	d.startClipboardPoll()
	return nil
}

// Events returns the channel of incoming events. It is closed when the
// connection to the desktop ends.
func (d *Display) Events() <-chan ui.Event { return d.events }

// Bell plays the system alert sound. NSBeep needs no thread affinity, unlike
// almost everything else in this package.
func (d *Display) Bell(_percent, _pitch, _duration int64, _name string) {
	go nsBeep()
}

// Close ends the connection and every window on it. It does not itself stop
// the AppKit run loop: RunMain in run.go owns that, once the function it is
// running — which called Open and eventually called Close — returns.
func (d *Display) Close() {
	d.once.Do(func() {
		d.call(func() {
			d.destroyTray()
			d.stopClipboardPoll()
			for id, w := range d.windows {
				w.window.Send(sel_close)
				delete(d.windows, id)
			}
			if d.cursor != 0 {
				d.cursor.Send(sel_release)
				d.cursor = 0
			}
		})
		close(d.done)
		close(d.eventStop)
	})
}

// post queues f to run on the main thread and returns immediately.
func (d *Display) post(f func()) {
	select {
	case <-d.done:
		return
	default:
	}
	dispatchOnMain(f)
}

// call runs f on the main thread and waits for it to finish.
func (d *Display) call(f func()) error {
	finished := make(chan struct{})
	d.post(func() {
		defer close(finished)
		f()
	})
	select {
	case <-finished:
		return nil
	case <-d.done:
		return errors.New("the main run loop has stopped")
	}
}

// emit hands an event to the relay without blocking the main thread. The
// queue may discard only motion and configure events; input transitions
// remain ordered and are always preserved.
func (d *Display) emit(event ui.Event) {
	d.eventQueue.Push(event)
}

// dispatchEvents is the only goroutine that writes to events. Keeping this
// relay separate lets a slow client apply backpressure without ever stalling
// the AppKit run loop — identical in design to win32's dispatchEvents.
func (d *Display) dispatchEvents() {
	defer close(d.events)
	for {
		event, ok := d.eventQueue.Pop()
		if !ok {
			select {
			case <-d.eventQueue.Wake:
				continue
			case <-d.eventStop:
				return
			}
		}
		select {
		case d.events <- event:
		case <-d.eventStop:
			return
		}
	}
}

// updateReferenceHeight caches the primary screen's height, the anchor
// coords.go flips every rectangle against. It is recomputed whenever the
// screen arrangement changes.
func (d *Display) updateReferenceHeight() {
	screens := objc.ID(objc.GetClass("NSScreen")).Send(sel_screens)
	if objc.Send[int](screens, sel_count) == 0 {
		d.referenceHeight = 0
		return
	}
	primary := screens.Send(sel_objectAtIndex, 0)
	frame := objc.Send[nsRect](primary, sel_frame)
	d.referenceHeight = frame.Size.Height
}

// windowForView finds the Window a view (self in a mouse or keyboard IMP
// callback) belongs to, by asking AppKit which NSWindow currently owns it —
// simpler and more robust than stashing a back-pointer in an Objective-C
// ivar.
func (d *Display) windowForView(view objc.ID) *Window {
	return d.windows[view.Send(sel_window)]
}

// eventLocation converts a mouse event's window-relative position into the
// absolute screen coordinates ui.Motion and ui.Button require.
//
// convertPoint:fromView: (with a nil fromView) is AppKit's own documented way
// to turn an event's window-base location into a specific view's local
// space; because the view overrides isFlipped to report YES, the point that
// comes back is already top-left/Y-down within the view, so only the
// window's own (already-flipped, by coords.go) screen origin needs adding —
// no further flip.
func (d *Display) eventLocation(w *Window, event objc.ID) (x, y int) {
	windowPoint := objc.Send[nsPoint](event, sel_locationInWindow)
	viewPoint := objc.Send[nsPoint](w.view, sel_convertPointFromView, windowPoint, objc.ID(0))
	return w.x + int(viewPoint.X), w.y + int(viewPoint.Y)
}

// pointerButton reports a button transition at a mouse event's location.
func (d *Display) pointerButton(self, event objc.ID, button int, pressed bool) {
	w := d.windowForView(self)
	if w == nil {
		return
	}
	x, y := d.eventLocation(w, event)
	d.emit(ui.Button{Window: w.ID(), Button: button, Pressed: pressed, X: x, Y: y})
}

func (d *Display) mouseDown(self objc.ID, _ objc.SEL, event objc.ID) {
	d.pointerButton(self, event, 1, true)
}
func (d *Display) mouseUp(self objc.ID, _ objc.SEL, event objc.ID) {
	d.pointerButton(self, event, 1, false)
}
func (d *Display) rightMouseDown(self objc.ID, _ objc.SEL, event objc.ID) {
	d.pointerButton(self, event, 3, true)
}
func (d *Display) rightMouseUp(self objc.ID, _ objc.SEL, event objc.ID) {
	d.pointerButton(self, event, 3, false)
}

// otherMouseButton handles the middle button and any extra buttons a mouse
// may have. NSEvent numbers the middle button 2, same as X11's; the two
// beyond it map onto the X11 back/forward buttons, the closest equivalents
// xpra has a name for.
func (d *Display) otherMouseButton(self, event objc.ID, pressed bool) {
	switch objc.Send[int](event, sel_buttonNumber) {
	case 2:
		d.pointerButton(self, event, 2, pressed)
	case 3:
		d.pointerButton(self, event, 8, pressed)
	case 4:
		d.pointerButton(self, event, 9, pressed)
	}
}

func (d *Display) otherMouseDown(self objc.ID, _ objc.SEL, event objc.ID) {
	d.otherMouseButton(self, event, true)
}
func (d *Display) otherMouseUp(self objc.ID, _ objc.SEL, event objc.ID) {
	d.otherMouseButton(self, event, false)
}

// mouseMoved reports pointer motion. It is also registered for
// mouseDragged:/rightMouseDragged:/otherMouseDragged:, which is what AppKit
// sends instead of mouseMoved: while a button is held.
//
// This only reaches windows AppKit is willing to deliver mouseMoved: to
// without a tracking area — in practice the key window — which matches the
// single focused forwarded window xpra sessions normally drive.
func (d *Display) mouseMoved(self objc.ID, _ objc.SEL, event objc.ID) {
	w := d.windowForView(self)
	if w == nil {
		return
	}
	x, y := d.eventLocation(w, event)
	d.emit(ui.Motion{Window: w.ID(), X: x, Y: y})
}

// wheelClicks turns a scroll delta into X11-style button presses: xpra has no
// scroll axis, and a notch is a press and release of one of four buttons,
// exactly as win32's wheel() translates WM_MOUSEWHEEL.
func (d *Display) wheelClicks(w *Window, delta float64, positiveButton, negativeButton, x, y int) {
	if delta == 0 {
		return
	}
	button := positiveButton
	if delta < 0 {
		button, delta = negativeButton, -delta
	}
	clicks := min(max(int(delta), 1), 10)
	for range clicks {
		d.emit(ui.Button{Window: w.ID(), Button: button, Pressed: true, X: x, Y: y})
		d.emit(ui.Button{Window: w.ID(), Button: button, Pressed: false, X: x, Y: y})
	}
}

func (d *Display) scrollWheel(self objc.ID, _ objc.SEL, event objc.ID) {
	w := d.windowForView(self)
	if w == nil {
		return
	}
	x, y := d.eventLocation(w, event)
	deltaY := objc.Send[float64](event, sel_deltaY)
	deltaX := objc.Send[float64](event, sel_deltaX)
	d.wheelClicks(w, deltaY, 4, 5, x, y)
	d.wheelClicks(w, deltaX, 7, 6, x, y)
}

func (d *Display) keyDown(self objc.ID, _ objc.SEL, event objc.ID) { d.key(self, event, true) }
func (d *Display) keyUp(self objc.ID, _ objc.SEL, event objc.ID)   { d.key(self, event, false) }

func (d *Display) key(self, event objc.ID, pressed bool) {
	w := d.windowForView(self)
	if w == nil {
		return
	}
	keyCode := objc.Send[uint16](event, sel_keyCode)
	chars := goString(event.Send(sel_charactersIgnoringModifiers))
	flags := objc.Send[uint64](event, sel_modifierFlags)
	d.emit(keyEvent(w.ID(), keyCode, chars, flags, pressed))
}

// flagsChanged reports a modifier key going down or up. Modifier keys never
// produce keyDown:/keyUp:, only this.
func (d *Display) flagsChanged(self objc.ID, _ objc.SEL, event objc.ID) {
	w := d.windowForView(self)
	if w == nil {
		return
	}
	keyCode := objc.Send[uint16](event, sel_keyCode)
	flags := objc.Send[uint64](event, sel_modifierFlags)
	pressed := modifierKeyPressed(keyCode, flags, d.previousModifierFlags)
	d.previousModifierFlags = flags
	d.emit(keyEvent(w.ID(), keyCode, "", flags, pressed))
}

// resetCursorRects applies the session-wide cursor to a view's whole bounds.
// AppKit calls this itself whenever it needs to know which cursor a view
// wants, and Display.SetCursor forces a fresh call through
// invalidateCursorRectsForView: whenever the session cursor changes.
func (d *Display) resetCursorRects(self objc.ID, _ objc.SEL) {
	if d.cursor == 0 {
		return
	}
	bounds := objc.Send[nsRect](self, sel_bounds)
	self.Send(sel_addCursorRect, bounds, d.cursor)
}

func acceptsFirstResponderYes(_ objc.ID, _ objc.SEL) bool { return true }

// isFlippedYes makes the view's own coordinate space top-left/Y-down, which
// is what removes the need to hand-flip mouse and drawing coordinates within
// a single view — only window- and screen-level geometry goes through
// coords.go.
func isFlippedYes(_ objc.ID, _ objc.SEL) bool { return true }

// windowShouldClose reports the close button being pressed. Nothing closes
// locally: the request goes to the server, and the window disappears only
// when the server sends a destroy packet back for it — the same contract
// win32's wmClose handling keeps.
func (d *Display) windowShouldClose(_ objc.ID, _ objc.SEL, sender objc.ID) bool {
	if w := d.windows[sender]; w != nil {
		d.emit(ui.CloseRequest{Window: w.ID()})
	}
	return false
}

func (d *Display) windowDidResize(_ objc.ID, _ objc.SEL, notification objc.ID) {
	d.windowGeometryChanged(notification)
}

func (d *Display) windowDidMove(_ objc.ID, _ objc.SEL, notification objc.ID) {
	d.windowGeometryChanged(notification)
}

// windowGeometryChanged reports a geometry change the desktop made on its
// own — dragging or resizing a decorated window — to the server. It never
// touches the Window's own bookkeeping directly: Window.Resized, called by
// the client in response to the ui.Configure this emits, is what does that,
// the same division win32 keeps between windowProc and Window.Resized.
func (d *Display) windowGeometryChanged(notification objc.ID) {
	win := notification.Send(sel_object)
	w := d.windows[win]
	if w == nil {
		return
	}
	outer := objc.Send[nsRect](win, sel_frame)
	content := objc.Send[nsRect](win, sel_contentRectForFrameRect, outer)
	x, y, width, height := cocoaToTopLeft(
		content.Origin.X, content.Origin.Y, content.Size.Width, content.Size.Height, d.referenceHeight)
	if width <= 0 || height <= 0 {
		return
	}
	d.emit(ui.Configure{Window: w.ID(), X: x, Y: y, Width: width, Height: height})
}

func (d *Display) windowDidBecomeKey(_ objc.ID, _ objc.SEL, notification objc.ID) {
	win := notification.Send(sel_object)
	if w := d.windows[win]; w != nil {
		d.emit(ui.Focus{Window: w.ID()})
	}
}

func (d *Display) screenParametersChanged(_ objc.ID, _ objc.SEL, _ objc.ID) {
	d.updateReferenceHeight()
}

// Selectors used only in this file.
var (
	sel_mouseDown                    = objc.RegisterName("mouseDown:")
	sel_mouseUp                      = objc.RegisterName("mouseUp:")
	sel_rightMouseDown               = objc.RegisterName("rightMouseDown:")
	sel_rightMouseUp                 = objc.RegisterName("rightMouseUp:")
	sel_otherMouseDown               = objc.RegisterName("otherMouseDown:")
	sel_otherMouseUp                 = objc.RegisterName("otherMouseUp:")
	sel_mouseMoved                   = objc.RegisterName("mouseMoved:")
	sel_mouseDragged                 = objc.RegisterName("mouseDragged:")
	sel_rightMouseDragged            = objc.RegisterName("rightMouseDragged:")
	sel_otherMouseDragged            = objc.RegisterName("otherMouseDragged:")
	sel_scrollWheel                  = objc.RegisterName("scrollWheel:")
	sel_keyDown                      = objc.RegisterName("keyDown:")
	sel_keyUp                        = objc.RegisterName("keyUp:")
	sel_flagsChanged                 = objc.RegisterName("flagsChanged:")
	sel_acceptsFirstResponder        = objc.RegisterName("acceptsFirstResponder")
	sel_isFlipped                    = objc.RegisterName("isFlipped")
	sel_resetCursorRects             = objc.RegisterName("resetCursorRects")
	sel_addCursorRect                = objc.RegisterName("addCursorRect:cursor:")
	sel_bounds                       = objc.RegisterName("bounds")
	sel_window                       = objc.RegisterName("window")
	sel_close                        = objc.RegisterName("close")
	sel_locationInWindow             = objc.RegisterName("locationInWindow")
	sel_convertPointFromView         = objc.RegisterName("convertPoint:fromView:")
	sel_buttonNumber                 = objc.RegisterName("buttonNumber")
	sel_deltaX                       = objc.RegisterName("deltaX")
	sel_deltaY                       = objc.RegisterName("deltaY")
	sel_keyCode                      = objc.RegisterName("keyCode")
	sel_charactersIgnoringModifiers  = objc.RegisterName("charactersIgnoringModifiers")
	sel_modifierFlags                = objc.RegisterName("modifierFlags")
	sel_windowShouldClose            = objc.RegisterName("windowShouldClose:")
	sel_windowDidResize              = objc.RegisterName("windowDidResize:")
	sel_windowDidMove                = objc.RegisterName("windowDidMove:")
	sel_windowDidBecomeKey           = objc.RegisterName("windowDidBecomeKey:")
	sel_screenParamsChanged          = objc.RegisterName("screenParametersChanged:")
	sel_object                       = objc.RegisterName("object")
	sel_contentRectForFrameRect      = objc.RegisterName("contentRectForFrameRect:")
	sel_screens                      = objc.RegisterName("screens")
	sel_defaultCenter                = objc.RegisterName("defaultCenter")
	sel_addObserver                  = objc.RegisterName("addObserver:selector:name:object:")
	sel_invalidateCursorRectsForView = objc.RegisterName("invalidateCursorRectsForView:")
)

// viewClass and coordinatorClass are the two AppKit classes this backend
// registers at runtime: viewClass backs every forwarded window's content
// view and handles input, coordinatorClass is the single shared
// NSWindowDelegate every window uses for close/resize/move/focus
// notifications and the screen-change observer.
var (
	viewClass        objc.Class
	coordinatorClass objc.Class
)

func (d *Display) registerClasses() error {
	var err error
	viewClass, err = objc.RegisterClass(
		"GoXpraView",
		objc.GetClass("NSView"),
		nil,
		nil,
		[]objc.MethodDef{
			{Cmd: sel_mouseDown, Fn: d.mouseDown},
			{Cmd: sel_mouseUp, Fn: d.mouseUp},
			{Cmd: sel_rightMouseDown, Fn: d.rightMouseDown},
			{Cmd: sel_rightMouseUp, Fn: d.rightMouseUp},
			{Cmd: sel_otherMouseDown, Fn: d.otherMouseDown},
			{Cmd: sel_otherMouseUp, Fn: d.otherMouseUp},
			{Cmd: sel_mouseMoved, Fn: d.mouseMoved},
			{Cmd: sel_mouseDragged, Fn: d.mouseMoved},
			{Cmd: sel_rightMouseDragged, Fn: d.mouseMoved},
			{Cmd: sel_otherMouseDragged, Fn: d.mouseMoved},
			{Cmd: sel_scrollWheel, Fn: d.scrollWheel},
			{Cmd: sel_keyDown, Fn: d.keyDown},
			{Cmd: sel_keyUp, Fn: d.keyUp},
			{Cmd: sel_flagsChanged, Fn: d.flagsChanged},
			{Cmd: sel_acceptsFirstResponder, Fn: acceptsFirstResponderYes},
			{Cmd: sel_isFlipped, Fn: isFlippedYes},
			{Cmd: sel_resetCursorRects, Fn: d.resetCursorRects},
		},
	)
	if err != nil {
		return err
	}

	coordinatorClass, err = objc.RegisterClass(
		"GoXpraWindowDelegate",
		objc.GetClass("NSObject"),
		[]*objc.Protocol{objc.GetProtocol("NSWindowDelegate")},
		nil,
		[]objc.MethodDef{
			{Cmd: sel_windowShouldClose, Fn: d.windowShouldClose},
			{Cmd: sel_windowDidResize, Fn: d.windowDidResize},
			{Cmd: sel_windowDidMove, Fn: d.windowDidMove},
			{Cmd: sel_windowDidBecomeKey, Fn: d.windowDidBecomeKey},
			{Cmd: sel_screenParamsChanged, Fn: d.screenParametersChanged},
			{Cmd: sel_trayExit, Fn: d.trayExit},
		},
	)
	return err
}
