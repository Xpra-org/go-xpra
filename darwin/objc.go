//go:build darwin

package darwin

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

// nsPoint, nsSize and nsRect mirror NSPoint/NSSize/NSRect (identically laid
// out to CGPoint/CGSize/CGRect on 64-bit macOS, the only architectures this
// package supports): two float64s and a pair of them. They cross objc_msgSend
// by value, which purego marshals like any other Go struct argument or
// return value.
type nsPoint struct{ X, Y float64 }
type nsSize struct{ Width, Height float64 }
type nsRect struct {
	Origin nsPoint
	Size   nsSize
}

func makeRect(x, y, width, height float64) nsRect {
	return nsRect{Origin: nsPoint{X: x, Y: y}, Size: nsSize{Width: width, Height: height}}
}

// Library handles. Cocoa is the umbrella framework for AppKit and Foundation;
// CoreGraphics and libdispatch are loaded separately for the plain C
// functions this package calls directly rather than through objc_msgSend.
var (
	appKit        uintptr
	coreGraphics  uintptr
	libDispatch   uintptr
	mainQueueAddr uintptr
)

// Plain C bindings, none of them Objective-C messages: CoreGraphics'
// CGImage/CGDataProvider/CGColorSpace family for painting (see window.go),
// libdispatch for hopping onto the main thread from any goroutine (see
// run.go), NSBeep and malloc/free.
var (
	cgColorSpaceCreateDeviceRGB  func() uintptr
	cgColorSpaceRelease          func(space uintptr)
	cgDataProviderCreateWithData func(info unsafe.Pointer, data unsafe.Pointer, size uintptr, release uintptr) uintptr
	cgDataProviderRelease        func(provider uintptr)
	cgImageCreate                func(width, height, bitsPerComponent, bitsPerPixel, bytesPerRow uintptr,
		space uintptr, bitmapInfo uint32, provider uintptr, decode uintptr, shouldInterpolate bool, intent int32) uintptr
	cgImageRelease func(image uintptr)

	dispatchAsyncF func(queue uintptr, context unsafe.Pointer, work uintptr)
	dispatchSyncF  func(queue uintptr, context unsafe.Pointer, work uintptr)

	nsBeep func()

	cMalloc func(size uintptr) unsafe.Pointer
	cFree   func(ptr unsafe.Pointer)
)

func init() {
	var err error
	appKit, err = purego.Dlopen(
		"/System/Library/Frameworks/Cocoa.framework/Cocoa",
		purego.RTLD_GLOBAL|purego.RTLD_LAZY,
	)
	if err != nil {
		panic(fmt.Errorf("darwin: loading Cocoa: %w", err))
	}
	coreGraphics, err = purego.Dlopen(
		"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
		purego.RTLD_GLOBAL|purego.RTLD_LAZY,
	)
	if err != nil {
		panic(fmt.Errorf("darwin: loading CoreGraphics: %w", err))
	}
	libDispatch, err = purego.Dlopen("/usr/lib/system/libdispatch.dylib", purego.RTLD_GLOBAL|purego.RTLD_NOW)
	if err != nil {
		panic(fmt.Errorf("darwin: loading libdispatch: %w", err))
	}
	mainQueueAddr, err = purego.Dlsym(libDispatch, "_dispatch_main_q")
	if err != nil {
		panic(fmt.Errorf("darwin: resolving the main dispatch queue: %w", err))
	}

	purego.RegisterLibFunc(&cgColorSpaceCreateDeviceRGB, coreGraphics, "CGColorSpaceCreateDeviceRGB")
	purego.RegisterLibFunc(&cgColorSpaceRelease, coreGraphics, "CGColorSpaceRelease")
	purego.RegisterLibFunc(&cgDataProviderCreateWithData, coreGraphics, "CGDataProviderCreateWithData")
	purego.RegisterLibFunc(&cgDataProviderRelease, coreGraphics, "CGDataProviderRelease")
	purego.RegisterLibFunc(&cgImageCreate, coreGraphics, "CGImageCreate")
	purego.RegisterLibFunc(&cgImageRelease, coreGraphics, "CGImageRelease")

	purego.RegisterLibFunc(&dispatchAsyncF, libDispatch, "dispatch_async_f")
	purego.RegisterLibFunc(&dispatchSyncF, libDispatch, "dispatch_sync_f")

	purego.RegisterLibFunc(&nsBeep, appKit, "NSBeep")

	purego.RegisterLibFunc(&cMalloc, purego.RTLD_DEFAULT, "malloc")
	purego.RegisterLibFunc(&cFree, purego.RTLD_DEFAULT, "free")
}

// Selectors and classes shared across every file in this package. A file that
// needs one used nowhere else declares it locally instead, the same
// convention win32/api.go and its siblings use for Win32 entry points.
var (
	class_NSString = objc.GetClass("NSString")

	sel_alloc                = objc.RegisterName("alloc")
	sel_init                 = objc.RegisterName("init")
	sel_release              = objc.RegisterName("release")
	sel_retain               = objc.RegisterName("retain")
	sel_frame                = objc.RegisterName("frame")
	sel_setFrameDisplay      = objc.RegisterName("setFrame:display:")
	sel_count                = objc.RegisterName("count")
	sel_objectAtIndex        = objc.RegisterName("objectAtIndex:")
	sel_stringWithUTF8String = objc.RegisterName("stringWithUTF8String:")
	sel_UTF8String           = objc.RegisterName("UTF8String")
)

// nsString wraps a Go string as an autoreleased NSString. purego marshals a
// Go string argument as a NUL-terminated C string, which is exactly what
// stringWithUTF8String: expects.
func nsString(s string) objc.ID {
	return objc.ID(class_NSString).Send(sel_stringWithUTF8String, s)
}

// goString reads an NSString back as a Go string. purego marshals a
// NUL-terminated char* return value into a garbage-collected Go string, which
// is exactly what -UTF8String returns.
func goString(id objc.ID) string {
	if id == 0 {
		return ""
	}
	return objc.Send[string](id, sel_UTF8String)
}
