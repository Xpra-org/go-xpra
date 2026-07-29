package main

import (
	"strings"
	"testing"

	"github.com/Xpra-org/go-xpra/ui"
)

func TestConnectionFormDefaults(t *testing.T) {
	form := newConnectionForm()
	if got := dialogProtocols[form.protocol].name; got != "tcp" {
		t.Errorf("protocol = %q, want tcp", got)
	}
	if got := form.port.string(); got != defaultTCPPort {
		t.Errorf("port = %q, want %s", got, defaultTCPPort)
	}
	if form.focus != focusHost {
		t.Errorf("focus = %d, want host (%d)", form.focus, focusHost)
	}
}

func TestConnectionFormProtocolChangePopulatesDefaultPort(t *testing.T) {
	form := newConnectionForm()
	form.port.set("1234")
	form.selectProtocol(1)

	if got := dialogProtocols[form.protocol].name; got != "ssl" {
		t.Errorf("protocol = %q, want ssl", got)
	}
	if got := form.port.string(); got != dialogProtocols[1].defaultPort {
		t.Errorf("port = %q, want protocol default %q", got, dialogProtocols[1].defaultPort)
	}
}

func TestConnectionFormTarget(t *testing.T) {
	form := newConnectionForm()
	form.selectProtocol(1)
	form.username.set("alice")
	form.password.set("secret")
	form.host.set("[2001:db8::1]")
	form.port.set("15000")

	target, err := form.target()
	if err != nil {
		t.Fatalf("target: %v", err)
	}
	if target.address != "[2001:db8::1]:15000" {
		t.Errorf("address = %q, want [2001:db8::1]:15000", target.address)
	}
	if target.serverName != "2001:db8::1" {
		t.Errorf("serverName = %q, want 2001:db8::1", target.serverName)
	}
	if target.username != "alice" || target.password != "secret" {
		t.Errorf("credentials = %q/%q, want alice/secret", target.username, target.password)
	}
	if !target.secure {
		t.Error("secure = false, want true")
	}
}

func TestConnectionFormTargetValidation(t *testing.T) {
	tests := []struct {
		name string
		host string
		port string
		want string
	}{
		{name: "missing host", port: defaultTCPPort, want: "Host is required"},
		{name: "empty port", host: "example.com", want: "Port must"},
		{name: "zero port", host: "example.com", port: "0", want: "Port must"},
		{name: "large port", host: "example.com", port: "65536", want: "Port must"},
		{name: "named port", host: "example.com", port: "xpra", want: "Port must"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := newConnectionForm()
			form.host.set(tt.host)
			form.port.set(tt.port)
			_, err := form.target()
			if err == nil {
				t.Fatal("target succeeded")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %q, want it to contain %q", err, tt.want)
			}
		})
	}
}

func TestConnectionFormTextEditing(t *testing.T) {
	form := newConnectionForm()
	form.host.set("examle.comm")
	form.host.cursor = 4
	form.key(ui.Key{Name: "p", Text: "p", Pressed: true})
	form.key(ui.Key{Name: "End", Pressed: true})
	form.key(ui.Key{Name: "BackSpace", Pressed: true})

	if got := form.host.string(); got != "example.com" {
		t.Errorf("host = %q, want example.com", got)
	}
}

func TestConnectionFormPortInputIsFiveDigits(t *testing.T) {
	form := newConnectionForm()
	form.focus = focusPort
	form.port.set("")
	form.key(ui.Key{Name: "1", Text: "12a3456", Pressed: true})

	if got := form.port.string(); got != "12345" {
		t.Errorf("port = %q, want 12345", got)
	}
}

func TestPromptConnectionConnectsFromKeyboard(t *testing.T) {
	display := newDialogTestDisplay()
	display.events <- ui.Key{
		Window: display.window.ID(), Name: "e", Text: "example.com", Pressed: true,
	}
	display.events <- ui.Key{
		Window: display.window.ID(), Name: "Return", Pressed: true,
	}

	target, connectRequested, err := promptConnection(display)
	if err != nil {
		t.Fatalf("promptConnection: %v", err)
	}
	if !connectRequested {
		t.Fatal("connectRequested = false")
	}
	if target.address != "example.com:"+defaultTCPPort {
		t.Errorf("address = %q, want example.com:%s", target.address, defaultTCPPort)
	}
	if !display.window.mapped || !display.window.raised || !display.window.destroyed {
		t.Errorf("window lifecycle = mapped:%v raised:%v destroyed:%v",
			display.window.mapped, display.window.raised, display.window.destroyed)
	}
	if display.window.paints == 0 {
		t.Error("dialog was never painted")
	}
}

func TestPromptConnectionCancelButton(t *testing.T) {
	display := newDialogTestDisplay()
	form := newConnectionForm()
	cancel := form.layout().cancel
	display.events <- ui.Button{
		Window: display.window.ID(), Button: 1, Pressed: true,
		X: 40 + cancel.x + cancel.width/2, Y: 40 + cancel.y + cancel.height/2,
	}

	_, connectRequested, err := promptConnection(display)
	if err != nil {
		t.Fatalf("promptConnection: %v", err)
	}
	if connectRequested {
		t.Fatal("connectRequested = true")
	}
	if !display.window.destroyed {
		t.Error("dialog window was not destroyed")
	}
}

type dialogTestDisplay struct {
	window *dialogTestWindow
	events chan ui.Event
}

func newDialogTestDisplay() *dialogTestDisplay {
	return &dialogTestDisplay{
		window: &dialogTestWindow{id: 1, x: 0, y: 0, width: dialogWidth, height: dialogHeight},
		events: make(chan ui.Event, 8),
	}
}

func (d *dialogTestDisplay) NewWindow(x, y, width, height int, _ bool) (ui.Window, error) {
	d.window.x, d.window.y = x, y
	d.window.width, d.window.height = width, height
	return d.window, nil
}

func (d *dialogTestDisplay) Events() <-chan ui.Event { return d.events }
func (d *dialogTestDisplay) Bell(int64, int64, int64, string) {
}
func (d *dialogTestDisplay) SetCursor(*ui.Cursor) error { return nil }
func (d *dialogTestDisplay) Close()                     {}

type dialogTestWindow struct {
	id                  ui.WindowID
	x, y, width, height int
	title               string
	mapped              bool
	raised              bool
	destroyed           bool
	paints              int
}

func (w *dialogTestWindow) ID() ui.WindowID { return w.id }
func (w *dialogTestWindow) Geometry() (int, int, int, int) {
	return w.x, w.y, w.width, w.height
}
func (w *dialogTestWindow) SetTitle(title string) { w.title = title }
func (w *dialogTestWindow) Map()                  { w.mapped = true }
func (w *dialogTestWindow) Raise()                { w.raised = true }
func (w *dialogTestWindow) Minimize(bool)         {}
func (w *dialogTestWindow) Destroy()              { w.destroyed = true }
func (w *dialogTestWindow) MoveResize(x, y, width, height int) error {
	w.x, w.y, w.width, w.height = x, y, width, height
	return nil
}
func (w *dialogTestWindow) Resized(x, y, width, height int) error {
	w.x, w.y, w.width, w.height = x, y, width, height
	return nil
}
func (w *dialogTestWindow) Paint(_, _, width, height int, pixels []byte, rowstride int, format string) error {
	if format != "RGBA" {
		return nil
	}
	if rowstride < width*4 || len(pixels) < rowstride*height {
		return nil
	}
	w.paints++
	return nil
}
