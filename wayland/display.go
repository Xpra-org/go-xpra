//go:build linux

package wayland

import (
	"fmt"
	"log"
	"sync"

	"github.com/rajveermalviya/go-wayland/wayland/client"
	"github.com/rajveermalviya/go-wayland/wayland/cursor"
	xdg_shell "github.com/rajveermalviya/go-wayland/wayland/stable/xdg-shell"
	xdg_decoration "github.com/rajveermalviya/go-wayland/wayland/unstable/xdg-decoration-v1"

	"github.com/Xpra-org/go-xpra/ui"
)

// appID names the client to the compositor, which uses it to find the desktop
// entry a window belongs to.
const appID = "org.xpra.go-xpra"

// eventQueueSize is how much local input the client may fall behind before
// presentation-only events begin to be coalesced or discarded.
const eventQueueSize = 256

// The protocol versions we ask for. Binding above what we understand would let
// the compositor send events we cannot decode, so each is capped at the last
// version this backend was written against, and dropped to whatever the
// compositor offers if that is lower.
const (
	// wl_surface.damage_buffer arrived in wl_compositor 4.
	compositorVersion = 4
	// xrgb8888 is guaranteed in every version of wl_shm.
	shmVersion = 1
	// wl_pointer.axis_discrete, which counts wheel notches for us, arrived in
	// wl_seat 5.
	seatVersion = 5
	// Nothing after xdg_wm_base 1 is needed: this client never repositions a
	// popup or suspends a toplevel.
	wmBaseVersion     = 1
	decorationVersion = 1
)

// Display is a connection to the Wayland compositor and the globals the windows
// need from it.
//
// The wire protocol multiplexes every object over one socket, and the bindings
// keep the object table in a Context that is not safe for concurrent use. So
// everything that touches the connection — the client goroutine calling into
// ui.Window, and the dispatch goroutine running an incoming event's handler —
// is serialized on mu. Only the blocking read itself is left outside it, which
// is what lets a request go out while we are waiting to be told something.
type Display struct {
	ctx        *client.Context
	display    *client.Display
	registry   *client.Registry
	compositor *client.Compositor
	shm        *client.Shm
	wmBase     *xdg_shell.WmBase
	seat       *client.Seat
	pointer    *client.Pointer
	keyboard   *client.Keyboard

	// decoration is nil on a compositor that draws no window frames — GNOME and
	// weston both — in which case the windows have no title bar.
	decoration *xdg_decoration.DecorationManager

	// seatBound is the version wl_seat was actually bound at, which decides
	// whether the wheel arrives as notches or as a scroll distance.
	seatBound uint32

	mu      sync.Mutex
	windows map[uint32]*Window // wl_surface id -> window

	// lastToplevel is the most recent ordinary window, which is what an
	// override-redirect popup is anchored to for want of anywhere else: a
	// Wayland popup must name a parent surface, whereas the server sends its
	// menus as free-standing windows at absolute positions.
	lastToplevel *Window

	pointerOver   *Window
	pointerX      int
	pointerY      int
	keyboardFocus *Window

	// The pointer image, which belongs to the seat rather than to any window;
	// see cursor.go. enterSerial is what the compositor makes us quote to prove
	// the pointer is ours to set.
	enterSerial   uint32
	cursorSurface *client.Surface
	shown         *shownCursor
	ownedCursor   *ownedCursor
	theme         *cursor.Theme
	themeMissing  bool

	keys *keymap
	mods uint32

	eventQueue *ui.EventQueue
	events     chan ui.Event
	eventStop  chan struct{}
	done       chan struct{}
	once       sync.Once
}

var _ ui.Display = (*Display)(nil)

// Open connects to the compositor named by $WAYLAND_DISPLAY.
func Open() (*Display, error) {
	display, err := client.Connect("")
	if err != nil {
		return nil, fmt.Errorf("connecting to the Wayland compositor: %w", err)
	}
	d := &Display{
		display:    display,
		ctx:        display.Context(),
		windows:    map[uint32]*Window{},
		keys:       &keymap{levels: map[uint32][]string{}},
		eventQueue: ui.NewEventQueue(eventQueueSize),
		events:     make(chan ui.Event),
		eventStop:  make(chan struct{}),
		done:       make(chan struct{}),
	}
	display.SetErrorHandler(d.protocolError)
	display.SetDeleteIdHandler(d.deleteID)

	if d.registry, err = display.GetRegistry(); err != nil {
		d.ctx.Close()
		return nil, fmt.Errorf("getting the Wayland registry: %w", err)
	}
	d.registry.SetGlobalHandler(d.global)

	// The globals are announced in reply to get_registry, so one roundtrip is
	// enough to have heard about all of them.
	if err := d.roundtrip(); err != nil {
		d.ctx.Close()
		return nil, fmt.Errorf("listing the Wayland globals: %w", err)
	}
	for _, required := range []struct {
		name    string
		present bool
	}{
		{"wl_compositor", d.compositor != nil},
		{"wl_shm", d.shm != nil},
		{"xdg_wm_base", d.wmBase != nil},
	} {
		if !required.present {
			d.ctx.Close()
			return nil, fmt.Errorf("the compositor does not offer %s", required.name)
		}
	}
	if d.decoration == nil {
		// Saying this once is what makes a bare window look like the compositor
		// rather than like a bug: without the extension every client is expected
		// to draw its own title bar, and this one draws none.
		log.Printf("wayland: the compositor does not offer xdg-decoration, " +
			"so windows will have no title bar")
	}
	// A client that does not answer a ping is killed as unresponsive.
	d.wmBase.SetPingHandler(func(e xdg_shell.WmBasePingEvent) {
		if err := d.wmBase.Pong(e.Serial); err != nil {
			log.Printf("wayland: pong: %v", err)
		}
	})

	go d.dispatchLoop()
	go d.relayEvents()
	return d, nil
}

// Events returns the channel of incoming events. It is closed when the
// connection to the compositor ends.
func (d *Display) Events() <-chan ui.Event { return d.events }

// Bell does nothing. Wayland has no bell protocol and no way to reach the
// desktop's sound theme that does not go through C libraries, so a remote
// application's bell is silent here.
func (d *Display) Bell(_percent, _pitch, _duration int64, _name string) {}

// Close tears down the connection, which ends the dispatch loop and with it
// every window on it.
func (d *Display) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.shutdown()
}

// shutdown is Close with the lock already held, which is how the dispatch loop
// has to reach it: a handler runs under mu, so one that wants to give up the
// connection cannot go through Close and take it again.
func (d *Display) shutdown() {
	d.once.Do(func() {
		close(d.done)
		d.releaseCursor()
		d.ctx.Close()
	})
}

// global binds the interfaces this backend uses as the compositor announces
// them, at the lowest of the version it offers and the version we understand.
//
// It runs under mu, from inside dispatch.
func (d *Display) global(e client.RegistryGlobalEvent) {
	switch e.Interface {
	case "wl_compositor":
		d.compositor = client.NewCompositor(d.ctx)
		d.bind(e, d.compositor, compositorVersion)
	case "wl_shm":
		d.shm = client.NewShm(d.ctx)
		d.bind(e, d.shm, shmVersion)
	case "xdg_wm_base":
		d.wmBase = xdg_shell.NewWmBase(d.ctx)
		d.bind(e, d.wmBase, wmBaseVersion)
	case "zxdg_decoration_manager_v1":
		d.decoration = xdg_decoration.NewDecorationManager(d.ctx)
		d.bind(e, d.decoration, decorationVersion)
	case "wl_seat":
		if d.seat != nil {
			// One seat is all a client showing one session can use; a second
			// would only fight the first for the keyboard focus.
			return
		}
		d.seat = client.NewSeat(d.ctx)
		d.seatBound = d.bind(e, d.seat, seatVersion)
		d.seat.SetCapabilitiesHandler(d.seatCapabilities)
	}
}

// bind binds one global and reports the version it settled on.
func (d *Display) bind(e client.RegistryGlobalEvent, proxy client.Proxy, ours uint32) uint32 {
	version := min(e.Version, ours)
	if err := bindGlobal(d.registry, e.Name, e.Interface, version, proxy); err != nil {
		log.Printf("wayland: binding %s: %v", e.Interface, err)
	}
	return version
}

// seatCapabilities takes the pointer and the keyboard as the seat grows them. A
// capability going away is not acted on: the device stops sending events, which
// is all this client would do about it anyway.
func (d *Display) seatCapabilities(e client.SeatCapabilitiesEvent) {
	if e.Capabilities&uint32(client.SeatCapabilityPointer) != 0 && d.pointer == nil {
		pointer, err := d.seat.GetPointer()
		if err != nil {
			log.Printf("wayland: getting the pointer: %v", err)
		} else {
			d.pointer = pointer
			d.watchPointer(pointer)
		}
	}
	if e.Capabilities&uint32(client.SeatCapabilityKeyboard) != 0 && d.keyboard == nil {
		keyboard, err := d.seat.GetKeyboard()
		if err != nil {
			log.Printf("wayland: getting the keyboard: %v", err)
		} else {
			d.keyboard = keyboard
			d.watchKeyboard(keyboard)
		}
	}
}

// deleteID drops an object the compositor has confirmed is gone.
//
// It is the other half of destroySurface in wire.go: a surface has to outlive
// its own destroy request, because an input event naming it may still be in
// flight, and this is the point at which no more can be. Objects the bindings
// dropped themselves are simply not there any more.
func (d *Display) deleteID(e client.DisplayDeleteIdEvent) {
	if proxy := d.ctx.GetProxy(e.Id); proxy != nil {
		d.ctx.Unregister(proxy)
	}
}

// protocolError reports a request the compositor refused. There is no
// recovering from one: the compositor closes the connection on us as soon as it
// has said so, and anything still queued to send would go nowhere.
func (d *Display) protocolError(e client.DisplayErrorEvent) {
	log.Printf("wayland: protocol error %d: %s", e.Code, e.Message)
	d.shutdown()
}

// roundtrip dispatches events until the compositor has answered everything
// asked of it so far.
func (d *Display) roundtrip() error {
	callback, err := d.display.Sync()
	if err != nil {
		return err
	}
	defer callback.Destroy()

	done := false
	callback.SetDoneHandler(func(client.CallbackDoneEvent) { done = true })
	for !done {
		if err := d.dispatch(); err != nil {
			return err
		}
	}
	return nil
}

// dispatch reads one message and runs its handler.
//
// The read is deliberately outside the lock and the handler inside it: the read
// blocks for as long as the compositor has nothing to say, and holding the lock
// across it would stop the client from ever sending anything.
func (d *Display) dispatch() error {
	senderID, opcode, fd, data, err := d.ctx.ReadMsg()
	if err != nil {
		return err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	// A message for an object we have already destroyed is routine — it was in
	// flight when we let it go — and there is nothing to do about it.
	if sender, ok := d.ctx.GetProxy(senderID).(client.Dispatcher); ok {
		sender.Dispatch(opcode, fd, data)
	}
	return nil
}

func (d *Display) dispatchLoop() {
	defer close(d.eventStop)
	for {
		if err := d.dispatch(); err != nil {
			select {
			case <-d.done:
				// We closed the connection ourselves.
			default:
				log.Printf("wayland: %v", err)
			}
			return
		}
	}
}

// emit hands an event to the relay without blocking the dispatch loop, which
// has to keep draining the socket even while the client is busy.
func (d *Display) emit(event ui.Event) {
	d.eventQueue.Push(event)
}

// relayEvents is the only goroutine that writes to events, so that a slow
// client applies backpressure to the queue rather than to the compositor.
func (d *Display) relayEvents() {
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
