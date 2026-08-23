//go:build darwin

package darwin

import (
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const nsApplicationActivationPolicyRegular = 0

// nsApp is the shared application object. RunMain sets it once, before any
// other goroutine can be running, so every other file in this package may
// read it freely afterwards.
var nsApp objc.ID

var (
	sel_sharedApplication         = objc.RegisterName("sharedApplication")
	sel_setActivationPolicy       = objc.RegisterName("setActivationPolicy:")
	sel_activateIgnoringOtherApps = objc.RegisterName("activateIgnoringOtherApps:")
	sel_run                       = objc.RegisterName("run")
	sel_stopRunLoop               = objc.RegisterName("stop:")
	sel_postEventAtStart          = objc.RegisterName("postEvent:atStart:")
	sel_otherEventWithType        = objc.RegisterName(
		"otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:")
)

func init() {
	// init() functions all run on the main goroutine before any other
	// goroutine can exist to have the scheduler migrate it off the process's
	// real OS main thread, so this is the one place locking it is guaranteed
	// to lock the thread RunMain actually needs.
	runtime.LockOSThread()
}

// RunMain runs fn on a background goroutine while the calling goroutine —
// which must be the real OS main thread, which this package's init()
// guarantees by locking it before main.main can run — spins the AppKit run
// loop. It returns fn's result once fn has finished and the run loop has been
// stopped.
//
// Cocoa requires exactly this: [NSApplication run] must execute on the true
// main thread, unlike Win32's message loop, which any thread may own — so
// this backend cannot give itself its own dedicated goroutine+thread the way
// the X11, Wayland and Win32 backends do, and cmd/go-xpra's main() has to
// route through this function on darwin instead of calling straight through.
// Every other AppKit call in this package reaches the main thread through
// Display.post/Display.call in display.go, which is this package's
// equivalent of those backends' message pump.
func RunMain(fn func() error) error {
	nsApp = objc.ID(objc.GetClass("NSApplication")).Send(sel_sharedApplication)
	nsApp.Send(sel_setActivationPolicy, nsApplicationActivationPolicyRegular)
	nsApp.Send(sel_activateIgnoringOtherApps, true)

	errCh := make(chan error, 1)
	go func() { errCh <- fn() }()

	var result error
	go func() {
		result = <-errCh
		dispatchOnMain(func() {
			nsApp.Send(sel_stopRunLoop, objc.ID(0))
			// -[NSApplication stop:] only takes effect once the run loop
			// processes one more event, per Apple's own documentation, so a
			// trivial event is posted immediately to force that wakeup
			// rather than waiting on a real one that may never arrive.
			postWakeupEvent()
		})
	}()

	nsApp.Send(sel_run)
	return result
}

// postWakeupEvent posts a do-nothing event so the run loop notices a pending
// stop: request without needing a real user event to wake it.
func postWakeupEvent() {
	const nsEventTypeApplicationDefined = 15
	event := objc.ID(objc.GetClass("NSEvent")).Send(sel_otherEventWithType,
		nsEventTypeApplicationDefined,
		nsPoint{},
		uintptr(0),
		float64(0),
		int(0),
		objc.ID(0),
		int16(0),
		int(0),
		int(0),
	)
	nsApp.Send(sel_postEventAtStart, event, true)
}

// mainCalls is the queue Display.post/Display.call in display.go feed into.
// It is process-wide rather than per-Display because there is exactly one
// AppKit run loop per process regardless of how many Displays exist — in
// practice always one — matching how there is exactly one nsApp.
var mainCalls = make(chan func(), 64)

// runMainTrampoline is the single C function pointer dispatch_async_f/
// dispatch_sync_f are ever given; it is created once at package
// initialization rather than per call, since purego.NewCallback's function
// pointers are never deallocated and a fresh one per Paint call would leak
// without bound.
var runMainTrampoline = purego.NewCallback(runQueuedMainCalls)

// runQueuedMainCalls drains every closure queued so far. It always runs on
// the main thread, called there by libdispatch.
func runQueuedMainCalls(_ unsafe.Pointer) {
	for {
		select {
		case f := <-mainCalls:
			f()
		default:
			return
		}
	}
}

// dispatchOnMain queues f to run on the main thread's run loop and returns
// immediately. It is the GCD-based equivalent of posting to Win32's helper
// window: a plain C function pointer carries the wakeup, sidestepping
// purego's newer and less-proven Objective-C block support entirely.
func dispatchOnMain(f func()) {
	mainCalls <- f
	dispatchAsyncF(mainQueueAddr, nil, runMainTrampoline)
}
