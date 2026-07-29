//go:build windows

package win32

import (
	"fmt"
	"syscall"
	"unsafe"
)

// The Win32 entry points this backend needs.
//
// user32, gdi32 and kernel32 are all on the loader's KnownDLLs list, which is
// resolved from the system directory before any search path is consulted, so
// the plain lazy load cannot be diverted by a DLL dropped next to the
// executable.
var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procGetModuleHandle = kernel32.NewProc("GetModuleHandleW")
	procBeep            = kernel32.NewProc("Beep")

	procRegisterClassEx  = user32.NewProc("RegisterClassExW")
	procCreateWindowEx   = user32.NewProc("CreateWindowExW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procDefWindowProc    = user32.NewProc("DefWindowProcW")
	procSendMessage      = user32.NewProc("SendMessageW")
	procGetMessage       = user32.NewProc("GetMessageW")
	procDispatchMessage  = user32.NewProc("DispatchMessageW")
	procPostMessage      = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procShowWindow       = user32.NewProc("ShowWindow")
	procSetWindowText    = user32.NewProc("SetWindowTextW")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procAdjustWindowRect = user32.NewProc("AdjustWindowRectEx")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procClientToScreen   = user32.NewProc("ClientToScreen")
	procInvalidateRect   = user32.NewProc("InvalidateRect")
	procBeginPaint       = user32.NewProc("BeginPaint")
	procEndPaint         = user32.NewProc("EndPaint")
	procLoadCursor       = user32.NewProc("LoadCursorW")
	procSetCursor        = user32.NewProc("SetCursor")
	procCreateIcon       = user32.NewProc("CreateIconIndirect")
	procDestroyIcon      = user32.NewProc("DestroyIcon")
	procDestroyCursor    = user32.NewProc("DestroyCursor")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procWindowFromPoint  = user32.NewProc("WindowFromPoint")
	procSetCapture       = user32.NewProc("SetCapture")
	procReleaseCapture   = user32.NewProc("ReleaseCapture")
	procGetKeyState      = user32.NewProc("GetKeyState")
	procGetKeyboardState = user32.NewProc("GetKeyboardState")
	procToUnicode        = user32.NewProc("ToUnicode")
	procMapVirtualKey    = user32.NewProc("MapVirtualKeyW")
	procSetProcessDPI    = user32.NewProc("SetProcessDPIAware")
	procSetDPIContext    = user32.NewProc("SetProcessDpiAwarenessContext")

	procSetDIBitsToDevice = gdi32.NewProc("SetDIBitsToDevice")
	procCreateDIBSection  = gdi32.NewProc("CreateDIBSection")
	procCreateBitmap      = gdi32.NewProc("CreateBitmap")
	procDeleteObject      = gdi32.NewProc("DeleteObject")
)

// Window and class styles.
const (
	wsOverlappedWindow = 0x00CF0000
	wsPopup            = 0x80000000

	wsExNoActivate = 0x08000000
	wsExToolWindow = 0x00000080
	wsExTopMost    = 0x00000008

	csHRedraw = 0x0002
	csVRedraw = 0x0001

	swShow             = 5
	swShowNoActivate   = 4
	swMinimize         = 6
	swRestore          = 9
	swpNoSize          = 0x0001
	swpNoMove          = 0x0002
	swpNoZOrder        = 0x0004
	swpNoActivate      = 0x0010
	hwndTop            = 0           // HWND_TOP
	hwndMessage        = ^uintptr(2) // (HWND)-3, the parent of message-only windows
	idcArrow           = 32512
	dpiPerMonitorAware = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, (HANDLE)-4
)

// The messages the window procedure acts on.
const (
	wmDestroy          = 0x0002
	wmSetFocus         = 0x0007
	wmPaint            = 0x000F
	wmClose            = 0x0010
	wmEraseBkgnd       = 0x0014
	wmSetCursor        = 0x0020
	wmWindowPosChanged = 0x0047
	wmPrintClient      = 0x0318
	wmSetIcon          = 0x0080
	wmKeyDown          = 0x0100
	wmKeyUp            = 0x0101
	wmSysKeyDown       = 0x0104
	wmSysKeyUp         = 0x0105
	wmMouseMove        = 0x0200
	wmLButtonDown      = 0x0201
	wmLButtonUp        = 0x0202
	wmRButtonDown      = 0x0204
	wmRButtonUp        = 0x0205
	wmMButtonDown      = 0x0207
	wmMButtonUp        = 0x0208
	wmMouseWheel       = 0x020A
	wmXButtonDown      = 0x020B
	wmXButtonUp        = 0x020C
	wmMouseHWheel      = 0x020E
	wmApp              = 0x8000

	htClient = 1

	iconSmall = 0
	iconBig   = 1
)

// Bitmap and keyboard constants.
const (
	biRGB        = 0
	biBitfields  = 3
	dibRGBColors = 0

	mapvkVSCToVKEx = 3
	// toUnicodeNoState keeps ToUnicode from updating the layout's dead-key
	// state, so that inspecting a key cannot swallow the next one. The flag
	// arrived in Windows 10 1607 and is ignored by anything older.
	toUnicodeNoState = 0x4
)

type point struct{ X, Y int32 }

type rect struct{ Left, Top, Right, Bottom int32 }

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   syscall.Handle
	Icon       syscall.Handle
	Cursor     syscall.Handle
	Background syscall.Handle
	MenuName   *uint16
	ClassName  *uint16
	IconSm     syscall.Handle
}

type msg struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
	Private uint32
}

type paintStruct struct {
	Hdc         syscall.Handle
	Erase       int32
	RcPaint     rect
	Restore     int32
	IncUpdate   int32
	RgbReserved [32]byte
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// bitmapV5Header is BITMAPV5HEADER. Its explicit channel masks make the alpha
// channel unambiguous to CreateIconIndirect.
type bitmapV5Header struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
	RedMask       uint32
	GreenMask     uint32
	BlueMask      uint32
	AlphaMask     uint32
	CSType        uint32
	Endpoints     [36]byte
	GammaRed      uint32
	GammaGreen    uint32
	GammaBlue     uint32
	Intent        uint32
	ProfileData   uint32
	ProfileSize   uint32
	Reserved      uint32
}

type iconInfo struct {
	Icon               int32
	HotspotX, HotspotY uint32
	Mask, Color        syscall.Handle
}

// The wrappers below all follow the same shape: the Win32 call reports failure
// through its return value, and only then is the thread's last error worth
// looking at, which is why err is ignored on the success paths.

func getModuleHandle() (syscall.Handle, error) {
	r, _, err := procGetModuleHandle.Call(0)
	if r == 0 {
		return 0, fmt.Errorf("GetModuleHandle: %w", err)
	}
	return syscall.Handle(r), nil
}

func beep(frequency, duration uint32) {
	procBeep.Call(uintptr(frequency), uintptr(duration))
}

func registerClassEx(class *wndClassEx) error {
	r, _, err := procRegisterClassEx.Call(uintptr(unsafe.Pointer(class)))
	if r == 0 {
		return fmt.Errorf("RegisterClassEx: %w", err)
	}
	return nil
}

func createWindowEx(exStyle uint32, class, title *uint16, style uint32,
	x, y, width, height int32, parent syscall.Handle, instance syscall.Handle) (syscall.Handle, error) {
	r, _, err := procCreateWindowEx.Call(
		uintptr(exStyle),
		uintptr(unsafe.Pointer(class)),
		uintptr(unsafe.Pointer(title)),
		uintptr(style),
		uintptr(x), uintptr(y), uintptr(width), uintptr(height),
		uintptr(parent), 0, uintptr(instance), 0)
	if r == 0 {
		return 0, fmt.Errorf("CreateWindowEx: %w", err)
	}
	return syscall.Handle(r), nil
}

func destroyWindow(hwnd syscall.Handle) {
	procDestroyWindow.Call(uintptr(hwnd))
}

func defWindowProc(hwnd syscall.Handle, message uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(message), wparam, lparam)
	return r
}

func sendMessage(hwnd syscall.Handle, message uint32, wparam, lparam uintptr) uintptr {
	r, _, _ := procSendMessage.Call(uintptr(hwnd), uintptr(message), wparam, lparam)
	return r
}

// getMessage returns false when the loop should end, which is either WM_QUIT or
// an error we can do nothing about anyway.
func getMessage(m *msg) bool {
	r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(m)), 0, 0, 0)
	return int32(r) > 0
}

func dispatchMessage(m *msg) {
	procDispatchMessage.Call(uintptr(unsafe.Pointer(m)))
}

func postMessage(hwnd syscall.Handle, message uint32, wparam, lparam uintptr) {
	procPostMessage.Call(uintptr(hwnd), uintptr(message), wparam, lparam)
}

func postQuitMessage() {
	procPostQuitMessage.Call(0)
}

func showWindow(hwnd syscall.Handle, command int32) {
	procShowWindow.Call(uintptr(hwnd), uintptr(command))
}

func setWindowText(hwnd syscall.Handle, title *uint16) {
	procSetWindowText.Call(uintptr(hwnd), uintptr(unsafe.Pointer(title)))
}

func setWindowPos(hwnd, insertAfter syscall.Handle, x, y, width, height int32, flags uint32) {
	procSetWindowPos.Call(uintptr(hwnd), uintptr(insertAfter),
		uintptr(x), uintptr(y), uintptr(width), uintptr(height), uintptr(flags))
}

// adjustWindowRect grows a content rectangle into the outer rectangle a window
// with these styles needs, so that the content lands exactly where the server
// asked for it.
func adjustWindowRect(r *rect, style, exStyle uint32) error {
	res, _, err := procAdjustWindowRect.Call(uintptr(unsafe.Pointer(r)), uintptr(style), 0, uintptr(exStyle))
	if res == 0 {
		return fmt.Errorf("AdjustWindowRectEx: %w", err)
	}
	return nil
}

func getClientRect(hwnd syscall.Handle, r *rect) bool {
	res, _, _ := procGetClientRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(r)))
	return res != 0
}

func clientToScreen(hwnd syscall.Handle, p *point) bool {
	res, _, _ := procClientToScreen.Call(uintptr(hwnd), uintptr(unsafe.Pointer(p)))
	return res != 0
}

func invalidateRect(hwnd syscall.Handle, r *rect) {
	procInvalidateRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(r)), 0)
}

func beginPaint(hwnd syscall.Handle, ps *paintStruct) syscall.Handle {
	r, _, _ := procBeginPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(ps)))
	return syscall.Handle(r)
}

func endPaint(hwnd syscall.Handle, ps *paintStruct) {
	procEndPaint.Call(uintptr(hwnd), uintptr(unsafe.Pointer(ps)))
}

func loadCursor(id uintptr) syscall.Handle {
	r, _, _ := procLoadCursor.Call(0, id)
	return syscall.Handle(r)
}

func setCursor(cursor syscall.Handle) {
	procSetCursor.Call(uintptr(cursor))
}

func createDIBSection(info unsafe.Pointer, bits *unsafe.Pointer) (syscall.Handle, error) {
	r, _, err := procCreateDIBSection.Call(0, uintptr(info), dibRGBColors,
		uintptr(unsafe.Pointer(bits)), 0, 0)
	if r == 0 {
		return 0, fmt.Errorf("CreateDIBSection: %w", err)
	}
	return syscall.Handle(r), nil
}

func createBitmap(width, height int32, planes, bitsPerPixel uint32, bits unsafe.Pointer) (syscall.Handle, error) {
	r, _, err := procCreateBitmap.Call(uintptr(width), uintptr(height), uintptr(planes),
		uintptr(bitsPerPixel), uintptr(bits))
	if r == 0 {
		return 0, fmt.Errorf("CreateBitmap: %w", err)
	}
	return syscall.Handle(r), nil
}

func createIconIndirect(info *iconInfo) (syscall.Handle, error) {
	r, _, err := procCreateIcon.Call(uintptr(unsafe.Pointer(info)))
	if r == 0 {
		return 0, fmt.Errorf("CreateIconIndirect: %w", err)
	}
	return syscall.Handle(r), nil
}

func destroyCursor(cursor syscall.Handle) {
	procDestroyCursor.Call(uintptr(cursor))
}

func destroyIcon(icon syscall.Handle) {
	procDestroyIcon.Call(uintptr(icon))
}

func deleteObject(object syscall.Handle) {
	procDeleteObject.Call(uintptr(object))
}

func getCursorPos(p *point) bool {
	r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(p)))
	return r != 0
}

func windowFromPoint(p point) syscall.Handle {
	var r uintptr
	if unsafe.Sizeof(uintptr(0)) == 8 {
		packed := uintptr(uint64(uint32(p.X)) | uint64(uint32(p.Y))<<32)
		r, _, _ = procWindowFromPoint.Call(packed)
	} else {
		r, _, _ = procWindowFromPoint.Call(uintptr(uint32(p.X)), uintptr(uint32(p.Y)))
	}
	return syscall.Handle(r)
}

func setCapture(hwnd syscall.Handle) {
	procSetCapture.Call(uintptr(hwnd))
}

func releaseCapture() {
	procReleaseCapture.Call()
}

// getKeyState reports the state of a virtual key: the high bit is set while the
// key is held, and the low bit while a toggle such as caps lock is on.
func getKeyState(vk byte) int16 {
	r, _, _ := procGetKeyState.Call(uintptr(vk))
	return int16(r)
}

func getKeyboardState(state *[256]byte) bool {
	r, _, _ := procGetKeyboardState.Call(uintptr(unsafe.Pointer(state)))
	return r != 0
}

// toUnicode asks the active keyboard layout what a key produces. It returns the
// number of UTF-16 units written, zero for a key that produces no text, and a
// negative number for a dead key.
func toUnicode(vk, scan uint32, state *[256]byte, buf []uint16, flags uint32) int {
	r, _, _ := procToUnicode.Call(uintptr(vk), uintptr(scan),
		uintptr(unsafe.Pointer(state)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), uintptr(flags))
	return int(int32(r))
}

func mapVirtualKey(code, mapType uint32) uint32 {
	r, _, _ := procMapVirtualKey.Call(uintptr(code), uintptr(mapType))
	return uint32(r)
}

// setDPIAware asks for physical pixels. Without it Windows reports scaled
// coordinates and stretches whatever we paint, which on a display at anything
// but 100% would put every window and every pointer position in the wrong place.
func setDPIAware() {
	// Per-monitor v2 is the modern spelling; the older call is the fallback for
	// Windows versions that predate it.
	if err := procSetDPIContext.Find(); err == nil {
		if r, _, _ := procSetDPIContext.Call(dpiPerMonitorAware); r != 0 {
			return
		}
	}
	procSetProcessDPI.Call()
}

// setDIBitsToDevice copies pixels from a device-independent bitmap onto a
// device context.
func setDIBitsToDevice(hdc syscall.Handle, xDest, yDest, width, height, xSrc, ySrc int32,
	startScan, scanLines uint32, bits *byte, info *bitmapInfoHeader) {
	procSetDIBitsToDevice.Call(uintptr(hdc),
		uintptr(xDest), uintptr(yDest), uintptr(width), uintptr(height),
		uintptr(xSrc), uintptr(ySrc), uintptr(startScan), uintptr(scanLines),
		uintptr(unsafe.Pointer(bits)), uintptr(unsafe.Pointer(info)), dibRGBColors)
}
