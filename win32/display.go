//go:build windows

package win32

import (
	"errors"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"github.com/Xpra-org/go-xpra/ui"
)

// className is the window class everything here is created from: the forwarded
// windows and the invisible helper that wakes the message loop.
const className = "go-xpra-window"

// The messages this backend posts to itself. WM_APP and above are reserved for
// exactly that.
const (
	// wmRunCalls asks the window thread to run whatever is queued in calls.
	wmRunCalls = wmApp + iota
	// wmStop ends the message loop.
	wmStop
)

// wheelDelta is one notch of a mouse wheel.
const wheelDelta = 120

// eventQueueSize is how much local input the client may fall behind before
// presentation-only events begin to be coalesced or discarded.
const eventQueueSize = 256

// Display owns the thread the forwarded windows live on.
//
// A window belongs to the thread that created it: only that thread may pump its
// messages, and most calls that touch it have to come from there too. So one
// goroutine is pinned to a thread, does nothing but run the message loop and
// whatever the client hands it through calls, and the two exchange work in one
// direction and events in the other.
type Display struct {
	class    *uint16
	instance syscall.Handle

	// Only the window thread touches these.
	helper  syscall.Handle
	windows map[syscall.Handle]*Window
	held    int // pointer buttons currently down, for mouse capture
	cursor  syscall.Handle
	// cursorOwned distinguishes CreateIconIndirect cursors from the shared
	// system arrow, which must never be destroyed by this process.
	cursorOwned bool

	calls      chan func()
	events     chan ui.Event
	eventQueue *ui.EventQueue
	eventStop  chan struct{}
	done       chan struct{}
	once       sync.Once
}

var _ ui.Display = (*Display)(nil)

// Open starts the window thread.
//
// One Display per process: it registers a window class and a callback, neither
// of which is ever taken back, which is all a client that shows one session
// needs.
func Open() (*Display, error) {
	class, err := syscall.UTF16PtrFromString(className)
	if err != nil {
		return nil, err
	}
	d := &Display{
		class:      class,
		windows:    map[syscall.Handle]*Window{},
		calls:      make(chan func(), 64),
		events:     make(chan ui.Event),
		eventQueue: ui.NewEventQueue(eventQueueSize),
		eventStop:  make(chan struct{}),
		done:       make(chan struct{}),
	}

	started := make(chan error, 1)
	go d.dispatchEvents()
	go d.windowThread(started)
	if err := <-started; err != nil {
		return nil, err
	}
	return d, nil
}

// Events returns the channel of incoming events. It is closed when the message
// loop ends.
func (d *Display) Events() <-chan ui.Event { return d.events }

// Bell plays a real tone without blocking the client or window thread. Win32
// supports the requested pitch and duration but has no equivalent for X11's
// relative volume or named bell.
func (d *Display) Bell(_percent, pitch, duration int64, _name string) {
	if pitch < 37 || pitch > 32767 {
		pitch = 800
	}
	if duration <= 0 {
		duration = 100
	} else if duration > 5000 {
		duration = 5000
	}
	go beep(uint32(pitch), uint32(duration))
}

// SetCursor swaps the session-wide native cursor on the window thread. Every
// forwarded window reads this field from WM_SETCURSOR, so existing and future
// windows change together while title bars retain their normal system cursors.
func (d *Display) SetCursor(image *ui.Cursor) error {
	var cursorErr error
	if err := d.call(func() {
		next := loadCursor(idcArrow)
		owned := false
		if image != nil {
			next, cursorErr = createNativeCursor(image)
			if cursorErr != nil {
				return
			}
			owned = true
		}
		old, oldOwned := d.cursor, d.cursorOwned
		d.cursor, d.cursorOwned = next, owned

		// WM_SETCURSOR will handle the next pointer movement. If the pointer is
		// captured or already over one of our client areas, update it now too.
		if d.held > 0 {
			setCursor(next)
		} else {
			var p point
			if getCursorPos(&p) {
				if w, ok := d.windows[windowFromPoint(p)]; ok {
					x, y, width, height, valid := clientGeometry(w.hwnd)
					if valid && int(p.X) >= x && int(p.X) < x+width &&
						int(p.Y) >= y && int(p.Y) < y+height {
						setCursor(next)
					}
				}
			}
		}
		if oldOwned {
			destroyCursor(old)
		}
	}); err != nil {
		return err
	}
	return cursorErr
}

// Close ends the message loop, which destroys every window with it.
func (d *Display) Close() {
	d.once.Do(func() {
		close(d.done)
		postMessage(d.helper, wmStop, 0, 0)
	})
}

func (d *Display) windowThread(started chan<- error) {
	// The thread must not change under us, and it must end when the loop does:
	// a locked thread is terminated when its goroutine exits, and Windows
	// destroys the windows the thread owned along with it.
	runtime.LockOSThread()
	defer close(d.eventStop)
	// Whatever ends the loop, nothing may be left waiting for a thread that is
	// no longer there to answer.
	defer d.Close()

	if err := d.setup(); err != nil {
		started <- err
		return
	}
	defer func() {
		if d.cursorOwned {
			destroyCursor(d.cursor)
		}
	}()
	started <- nil

	var m msg
	for getMessage(&m) {
		dispatchMessage(&m)
	}
}

func (d *Display) setup() error {
	setDPIAware()

	instance, err := getModuleHandle()
	if err != nil {
		return err
	}
	d.instance = instance

	d.cursor = loadCursor(idcArrow)
	class := wndClassEx{
		// Redrawing the whole window when it changes size is what fills in the
		// new edge, which no damage rectangle has covered yet.
		Style:    csHRedraw | csVRedraw,
		WndProc:  syscall.NewCallback(d.windowProc),
		Instance: instance,
		// WM_SETCURSOR chooses the session cursor for client areas. Leaving the
		// class cursor empty prevents Windows from replacing it on mouse moves.
		Cursor:    0,
		ClassName: d.class,
	}
	class.Size = uint32(unsafe.Sizeof(class))
	if err := registerClassEx(&class); err != nil {
		return err
	}

	// A message-only window: invisible, never painted, and there so that the
	// client goroutine has somewhere to post when it needs the window thread to
	// run something. Posting to a window rather than to the thread means the
	// work still gets done inside the modal loops Windows runs while the user
	// drags or resizes a window.
	d.helper, err = createWindowEx(0, d.class, nil, 0, 0, 0, 0, 0,
		syscall.Handle(hwndMessage), instance)
	if err != nil {
		return err
	}
	return nil
}

// post queues f to run on the window thread and returns immediately.
func (d *Display) post(f func()) {
	select {
	case <-d.done:
		return
	default:
	}
	select {
	case d.calls <- f:
		postMessage(d.helper, wmRunCalls, 0, 0)
	case <-d.done:
	}
}

// call runs f on the window thread and waits for it to finish.
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
		return errors.New("the window thread has stopped")
	}
}

func (d *Display) runCalls() {
	for {
		select {
		case f := <-d.calls:
			f()
		default:
			return
		}
	}
}

// emit hands an event to the relay without blocking the window thread. The
// queue may discard only motion and configure events; input transitions remain
// ordered and are always preserved.
func (d *Display) emit(event ui.Event) {
	d.eventQueue.Push(event)
}

// dispatchEvents is the only goroutine that writes to events. Keeping this
// relay separate lets a slow client apply backpressure without ever stalling
// the Windows message loop.
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

// windowProc handles the messages for every window of our class.
//
// It runs on the window thread, so it may touch the thread-owned fields of
// Display freely, but nothing it does may block: the desktop is waiting for it.
func (d *Display) windowProc(hwnd, message, wparam, lparam uintptr) uintptr {
	switch uint32(message) {
	case wmRunCalls:
		d.runCalls()
		return 0
	case wmStop:
		postQuitMessage()
		return 0
	}

	w := d.windows[syscall.Handle(hwnd)]
	if w == nil {
		// Messages arrive during CreateWindowEx, before the window is in the
		// map, and after a destroy has taken it out again.
		return defWindowProc(syscall.Handle(hwnd), uint32(message), wparam, lparam)
	}

	switch uint32(message) {
	case wmPaint:
		var ps paintStruct
		hdc := beginPaint(w.hwnd, &ps)
		w.blit(hdc, ps.RcPaint)
		endPaint(w.hwnd, &ps)
		return 0

	case wmPrintClient:
		// Windows asking the window to draw itself somewhere other than the
		// screen, which is how a capture or a thumbnail reaches a window that
		// is covered by another.
		w.print(syscall.Handle(wparam))
		return 0

	case wmEraseBkgnd:
		// Every pixel comes from the window's own buffer, which starts out
		// black, so there is nothing to erase and erasing would only flicker.
		return 1

	case wmSetCursor:
		if uint16(lparam) == htClient {
			setCursor(d.cursor)
			return 1
		}

	case wmWindowPosChanged:
		if x, y, width, height, ok := clientGeometry(w.hwnd); ok {
			d.emit(ui.Configure{Window: w.ID(), X: x, Y: y, Width: width, Height: height})
		}

	case wmClose:
		// The server owns the decision: it closes the window by sending a
		// window-destroy packet back, and until then the window stays put.
		d.emit(ui.CloseRequest{Window: w.ID()})
		return 0

	case wmDestroy:
		if w.icon != 0 {
			destroyIcon(w.icon)
			w.icon = 0
		}
		delete(d.windows, w.hwnd)
		return 0

	case wmSetFocus:
		d.emit(ui.Focus{Window: w.ID()})
		return 0

	case wmMouseMove:
		x, y := clientPos(w.hwnd, lparam)
		d.emit(ui.Motion{Window: w.ID(), X: x, Y: y})
		return 0

	case wmLButtonDown:
		d.pointerButton(w, 1, true, lparam)
		return 0
	case wmLButtonUp:
		d.pointerButton(w, 1, false, lparam)
		return 0
	case wmMButtonDown:
		d.pointerButton(w, 2, true, lparam)
		return 0
	case wmMButtonUp:
		d.pointerButton(w, 2, false, lparam)
		return 0
	case wmRButtonDown:
		d.pointerButton(w, 3, true, lparam)
		return 0
	case wmRButtonUp:
		d.pointerButton(w, 3, false, lparam)
		return 0

	case wmXButtonDown, wmXButtonUp:
		button := 8 // back
		if hiword(wparam) == 2 {
			button = 9 // forward
		}
		d.pointerButton(w, button, uint32(message) == wmXButtonDown, lparam)
		// These are the only pointer messages with a meaningful return value.
		return 1

	case wmMouseWheel:
		d.wheel(w, wparam, lparam, 4, 5)
		return 0
	case wmMouseHWheel:
		d.wheel(w, wparam, lparam, 7, 6)
		return 0

	case wmKeyDown, wmSysKeyDown:
		d.emit(d.key(w, wparam, lparam, true))
		// Swallowed rather than passed on: DefWindowProc would open the system
		// menu on Alt or F10, and the key belongs to the remote application.
		return 0
	case wmKeyUp, wmSysKeyUp:
		d.emit(d.key(w, wparam, lparam, false))
		return 0
	}
	return defWindowProc(w.hwnd, uint32(message), wparam, lparam)
}

// pointerButton reports a button and keeps the pointer captured for as long as
// one is held, so that a drag leaving the window still reports to it — which is
// what makes selections and scrollbars work.
func (d *Display) pointerButton(w *Window, button int, pressed bool, lparam uintptr) {
	if pressed {
		if d.held == 0 {
			setCapture(w.hwnd)
		}
		d.held++
	} else if d.held > 0 {
		d.held--
		if d.held == 0 {
			releaseCapture()
		}
	}
	x, y := clientPos(w.hwnd, lparam)
	d.emit(ui.Button{Window: w.ID(), Button: button, Pressed: pressed, X: x, Y: y})
}

// wheel turns a wheel movement into button presses: X11, and so xpra, has no
// scroll axis, and a notch is a press and release of buttons 4 to 7.
//
// Unlike the other pointer messages, the wheel ones carry screen coordinates
// already, because the wheel goes to the focused window wherever the pointer is.
func (d *Display) wheel(w *Window, wparam, lparam uintptr, positive, negative int) {
	delta := int(int16(hiword(wparam)))
	if delta == 0 {
		return
	}
	button := positive
	if delta < 0 {
		button, delta = negative, -delta
	}
	// A high-resolution wheel sends fractions of a notch, which round up so
	// that a small turn still scrolls; a fast flick is capped so that one
	// message cannot flood the server.
	clicks := min(max(delta/wheelDelta, 1), 10)

	x, y := int(int16(lparam)), int(int16(lparam>>16))
	for range clicks {
		d.emit(ui.Button{Window: w.ID(), Button: button, Pressed: true, X: x, Y: y})
		d.emit(ui.Button{Window: w.ID(), Button: button, Pressed: false, X: x, Y: y})
	}
}

// clientPos converts the client-relative position in a mouse message into the
// screen coordinates the server works in.
func clientPos(hwnd syscall.Handle, lparam uintptr) (x, y int) {
	p := point{X: int32(int16(lparam)), Y: int32(int16(lparam >> 16))}
	clientToScreen(hwnd, &p)
	return int(p.X), int(p.Y)
}

// clientGeometry returns the window's content area in screen coordinates, and
// false when there is none to report.
func clientGeometry(hwnd syscall.Handle) (x, y, width, height int, ok bool) {
	var r rect
	origin := point{}
	if !getClientRect(hwnd, &r) || !clientToScreen(hwnd, &origin) {
		return 0, 0, 0, 0, false
	}
	width, height = int(r.Right-r.Left), int(r.Bottom-r.Top)
	if width <= 0 || height <= 0 {
		// A minimized window has no content area and is parked at coordinates
		// far off screen; the server is better off hearing nothing.
		return 0, 0, 0, 0, false
	}
	return int(origin.X), int(origin.Y), width, height, true
}

func hiword(v uintptr) uint16 { return uint16(v >> 16) }
