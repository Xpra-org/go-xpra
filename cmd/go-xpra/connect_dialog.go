package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"net"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/Xpra-org/go-xpra/ui"
)

const (
	dialogWidth  = 520
	dialogHeight = 440

	focusProtocol = iota
	focusUsername
	focusPassword
	focusHost
	focusPort
	focusDisplay
	focusCancel
	focusConnect
	focusCount
)

type dialogProtocol struct {
	name        string
	defaultPort string
	transport   connectionTransport
}

var dialogProtocols = []dialogProtocol{
	{name: "tcp", defaultPort: defaultTCPPort, transport: transportTCP},
	{name: "ssl", defaultPort: defaultTCPPort, transport: transportSSL},
	{name: "ssh", defaultPort: defaultSSHPort, transport: transportSSH},
}

type textInput struct {
	text   []rune
	cursor int
}

func (i *textInput) set(text string) {
	i.text = []rune(text)
	i.cursor = len(i.text)
}

func (i *textInput) insert(text string) {
	runes := []rune(text)
	if len(runes) == 0 {
		return
	}
	next := make([]rune, 0, len(i.text)+len(runes))
	next = append(next, i.text[:i.cursor]...)
	next = append(next, runes...)
	next = append(next, i.text[i.cursor:]...)
	i.text = next
	i.cursor += len(runes)
}

func (i *textInput) backspace() {
	if i.cursor == 0 {
		return
	}
	i.text = append(i.text[:i.cursor-1], i.text[i.cursor:]...)
	i.cursor--
}

func (i *textInput) delete() {
	if i.cursor == len(i.text) {
		return
	}
	i.text = append(i.text[:i.cursor], i.text[i.cursor+1:]...)
}

func (i *textInput) string() string { return string(i.text) }

type connectionForm struct {
	protocol int
	username textInput
	password textInput
	host     textInput
	port     textInput
	display  textInput

	focus        int
	dropdownOpen bool
	message      string
	width        int
	height       int
}

func newConnectionForm() *connectionForm {
	f := &connectionForm{
		focus:  focusHost,
		width:  dialogWidth,
		height: dialogHeight,
	}
	f.port.set(dialogProtocols[0].defaultPort)
	return f
}

func (f *connectionForm) selectProtocol(index int) {
	if index < 0 || index >= len(dialogProtocols) {
		return
	}
	if f.protocol != index {
		f.protocol = index
		f.port.set(dialogProtocols[index].defaultPort)
	}
	f.dropdownOpen = false
	f.message = ""
}

func (f *connectionForm) inputForFocus() *textInput {
	switch f.focus {
	case focusUsername:
		return &f.username
	case focusPassword:
		return &f.password
	case focusHost:
		return &f.host
	case focusPort:
		return &f.port
	case focusDisplay:
		if f.sshSelected() {
			return &f.display
		}
		return nil
	default:
		return nil
	}
}

func (f *connectionForm) sshSelected() bool {
	return dialogProtocols[f.protocol].transport == transportSSH
}

func (f *connectionForm) target() (connectionURL, error) {
	host := strings.TrimSpace(f.host.string())
	if host == "" {
		return connectionURL{}, fmt.Errorf("Host is required")
	}
	// Accept the familiar bracketed spelling as well as a bare IPv6 literal.
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	}
	port := strings.TrimSpace(f.port.string())
	number, err := strconv.ParseUint(port, 10, 16)
	if err != nil || number == 0 {
		return connectionURL{}, fmt.Errorf("Port must be between 1 and 65535")
	}
	port = strconv.FormatUint(number, 10)
	protocol := dialogProtocols[f.protocol]
	if protocol.transport == transportSSH && strings.HasPrefix(host, "-") {
		return connectionURL{}, fmt.Errorf("Host cannot begin with -")
	}
	display := ""
	if protocol.transport == transportSSH {
		display = strings.TrimSpace(f.display.string())
		if strings.Contains(display, "/") || strings.IndexFunc(display, unicode.IsControl) >= 0 {
			return connectionURL{}, fmt.Errorf("Display must be a single path segment")
		}
	}
	return connectionURL{
		transport:  protocol.transport,
		address:    net.JoinHostPort(host, port),
		serverName: host,
		port:       port,
		display:    display,
		username:   f.username.string(),
		password:   f.password.string(),
	}, nil
}

type dialogRect struct {
	x, y, width, height int
}

func (r dialogRect) contains(x, y int) bool {
	return x >= r.x && x < r.x+r.width && y >= r.y && y < r.y+r.height
}

type dialogLayout struct {
	protocol dialogRect
	inputs   [5]dialogRect
	cancel   dialogRect
	connect  dialogRect
}

func (f *connectionForm) layout() dialogLayout {
	width := max(f.width, 360)
	controlX := min(145, width/3)
	controlWidth := max(width-controlX-24, 170)
	buttonY := max(f.height-56, 365)
	return dialogLayout{
		protocol: dialogRect{x: controlX, y: 38, width: min(controlWidth, 180), height: 34},
		inputs: [5]dialogRect{
			{x: controlX, y: 88, width: controlWidth, height: 34},
			{x: controlX, y: 138, width: controlWidth, height: 34},
			{x: controlX, y: 188, width: controlWidth, height: 34},
			{x: controlX, y: 238, width: min(controlWidth, 180), height: 34},
			{x: controlX, y: 288, width: controlWidth, height: 34},
		},
		cancel:  dialogRect{x: max(width-238, 122), y: buttonY, width: 100, height: 34},
		connect: dialogRect{x: max(width-124, 236), y: buttonY, width: 100, height: 34},
	}
}

// promptConnection presents the startup form on an already-open desktop. The
// bool is false when the user cancels or closes it.
func promptConnection(display ui.Display) (connectionURL, bool, error) {
	form := newConnectionForm()
	window, err := display.NewWindow(40, 40, form.width, form.height, false)
	if err != nil {
		return connectionURL{}, false, fmt.Errorf("creating connection dialog: %w", err)
	}
	defer window.Destroy()
	window.SetTitle("Connect to Xpra")
	icon, err := loadDialogIcon()
	if err != nil {
		return connectionURL{}, false, err
	}
	if err := window.SetIcon(icon); err != nil {
		return connectionURL{}, false, fmt.Errorf("setting connection dialog icon: %w", err)
	}
	if err := paintConnectionForm(window, form); err != nil {
		return connectionURL{}, false, err
	}
	window.Map()
	window.Raise()

	for event := range display.Events() {
		switch e := event.(type) {
		case ui.CloseRequest:
			if e.Window == window.ID() {
				return connectionURL{}, false, nil
			}
		case ui.Configure:
			if e.Window != window.ID() {
				continue
			}
			if err := window.Resized(e.X, e.Y, e.Width, e.Height); err != nil {
				return connectionURL{}, false, fmt.Errorf("resizing connection dialog: %w", err)
			}
			form.width, form.height = e.Width, e.Height
			if err := paintConnectionForm(window, form); err != nil {
				return connectionURL{}, false, err
			}
		case ui.Button:
			if e.Window != window.ID() || e.Button != 1 || !e.Pressed {
				continue
			}
			x, y, _, _ := window.Geometry()
			action := form.click(e.X-x, e.Y-y)
			if action == focusCancel {
				return connectionURL{}, false, nil
			}
			if action == focusConnect {
				target, err := form.target()
				if err == nil {
					return target, true, nil
				}
				form.message = err.Error()
			}
			if err := paintConnectionForm(window, form); err != nil {
				return connectionURL{}, false, err
			}
		case ui.Key:
			if e.Window != window.ID() || !e.Pressed {
				continue
			}
			action := form.key(e)
			if action == focusCancel {
				return connectionURL{}, false, nil
			}
			if action == focusConnect {
				target, err := form.target()
				if err == nil {
					return target, true, nil
				}
				form.message = err.Error()
			}
			if err := paintConnectionForm(window, form); err != nil {
				return connectionURL{}, false, err
			}
		}
	}
	return connectionURL{}, false, fmt.Errorf("local desktop closed while showing connection dialog")
}

// click updates the form and returns focusCancel or focusConnect when the
// corresponding action was requested, and -1 otherwise.
func (f *connectionForm) click(x, y int) int {
	layout := f.layout()
	if f.dropdownOpen {
		for index := range dialogProtocols {
			option := dialogRect{
				x:     layout.protocol.x,
				y:     layout.protocol.y + layout.protocol.height*(index+1),
				width: layout.protocol.width, height: layout.protocol.height,
			}
			if option.contains(x, y) {
				f.selectProtocol(index)
				f.focus = focusProtocol
				return -1
			}
		}
		f.dropdownOpen = false
	}
	if layout.protocol.contains(x, y) {
		f.focus = focusProtocol
		f.dropdownOpen = !f.dropdownOpen
		return -1
	}
	inputCount := 4
	if f.sshSelected() {
		inputCount = len(layout.inputs)
	}
	for index, field := range layout.inputs[:inputCount] {
		if field.contains(x, y) {
			f.focus = focusUsername + index
			f.dropdownOpen = false
			return -1
		}
	}
	if layout.cancel.contains(x, y) {
		f.focus = focusCancel
		return focusCancel
	}
	if layout.connect.contains(x, y) {
		f.focus = focusConnect
		return focusConnect
	}
	return -1
}

func (f *connectionForm) key(event ui.Key) int {
	switch event.Name {
	case "Escape":
		if f.dropdownOpen {
			f.dropdownOpen = false
			return -1
		}
		return focusCancel
	case "Tab", "ISO_Left_Tab":
		direction := 1
		if event.Name == "ISO_Left_Tab" || hasModifier(event.Modifiers, "shift") {
			direction = -1
		}
		for {
			f.focus = (f.focus + direction + focusCount) % focusCount
			if f.focus != focusDisplay || f.sshSelected() {
				break
			}
		}
		f.dropdownOpen = false
		return -1
	case "Return", "KP_Enter":
		if f.focus == focusCancel {
			return focusCancel
		}
		if f.focus == focusProtocol {
			f.dropdownOpen = !f.dropdownOpen
			return -1
		}
		return focusConnect
	}

	if f.focus == focusProtocol {
		switch event.Name {
		case "Up":
			f.selectProtocol((f.protocol - 1 + len(dialogProtocols)) % len(dialogProtocols))
		case "Down":
			f.selectProtocol((f.protocol + 1) % len(dialogProtocols))
		case "space":
			f.dropdownOpen = !f.dropdownOpen
		}
		return -1
	}
	input := f.inputForFocus()
	if input == nil {
		if event.Name == "space" && f.focus == focusCancel {
			return focusCancel
		}
		if event.Name == "space" && f.focus == focusConnect {
			return focusConnect
		}
		return -1
	}
	switch event.Name {
	case "BackSpace":
		input.backspace()
	case "Delete":
		input.delete()
	case "Left":
		input.cursor = max(input.cursor-1, 0)
	case "Right":
		input.cursor = min(input.cursor+1, len(input.text))
	case "Home":
		input.cursor = 0
	case "End":
		input.cursor = len(input.text)
	default:
		if event.Text != "" && !hasModifier(event.Modifiers, "control") && !hasModifier(event.Modifiers, "mod1") {
			if f.focus == focusPort {
				f.insertPort(event.Text)
			} else {
				input.insert(event.Text)
			}
		}
	}
	f.message = ""
	return -1
}

func (f *connectionForm) insertPort(text string) {
	for _, character := range text {
		if character < '0' || character > '9' || len(f.port.text) >= 5 {
			continue
		}
		f.port.insert(string(character))
	}
}

func hasModifier(modifiers []string, name string) bool {
	for _, modifier := range modifiers {
		if modifier == name {
			return true
		}
	}
	return false
}

var dialogColors = struct {
	background color.RGBA
	text       color.RGBA
	muted      color.RGBA
	border     color.RGBA
	focus      color.RGBA
	field      color.RGBA
	button     color.RGBA
	primary    color.RGBA
	error      color.RGBA
}{
	background: color.RGBA{R: 244, G: 246, B: 249, A: 255},
	text:       color.RGBA{R: 28, G: 35, B: 45, A: 255},
	muted:      color.RGBA{R: 92, G: 101, B: 113, A: 255},
	border:     color.RGBA{R: 175, G: 183, B: 194, A: 255},
	focus:      color.RGBA{R: 42, G: 111, B: 219, A: 255},
	field:      color.RGBA{R: 255, G: 255, B: 255, A: 255},
	button:     color.RGBA{R: 226, G: 230, B: 236, A: 255},
	primary:    color.RGBA{R: 42, G: 111, B: 219, A: 255},
	error:      color.RGBA{R: 177, G: 35, B: 35, A: 255},
}

func paintConnectionForm(window ui.Window, form *connectionForm) error {
	width, height := max(form.width, 1), max(form.height, 1)
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	fill(canvas, canvas.Bounds(), dialogColors.background)
	layout := form.layout()

	labels := []struct {
		text string
		y    int
	}{
		{"Protocol", layout.protocol.y},
		{"Username", layout.inputs[0].y},
		{"Password", layout.inputs[1].y},
		{"Host", layout.inputs[2].y},
		{"Port", layout.inputs[3].y},
	}
	if form.sshSelected() {
		labels[2].text = "SSH Password"
		labels = append(labels, struct {
			text string
			y    int
		}{"Display", layout.inputs[4].y})
	}
	for _, label := range labels {
		drawText(canvas, 24, label.y+22, label.text, dialogColors.text)
	}

	drawField(canvas, layout.protocol, form.focus == focusProtocol)
	drawText(canvas, layout.protocol.x+10, layout.protocol.y+22,
		dialogProtocols[form.protocol].name, dialogColors.text)
	arrowX := layout.protocol.x + layout.protocol.width - 18
	drawText(canvas, arrowX, layout.protocol.y+21, "v", dialogColors.muted)

	inputs := []*textInput{&form.username, &form.password, &form.host, &form.port}
	if form.sshSelected() {
		inputs = append(inputs, &form.display)
	}
	for index, input := range inputs {
		field := layout.inputs[index]
		focused := form.focus == focusUsername+index
		drawField(canvas, field, focused)
		drawInput(canvas, field, input, index == 1, focused)
	}

	drawButton(canvas, layout.cancel, "Cancel", form.focus == focusCancel, false)
	drawButton(canvas, layout.connect, "Connect", form.focus == focusConnect, true)
	if form.message != "" {
		drawText(canvas, 24, max(layout.cancel.y-18, 342), form.message, dialogColors.error)
	}

	// Draw the open menu last so it sits over the fields below it.
	if form.dropdownOpen {
		for index, protocol := range dialogProtocols {
			option := dialogRect{
				x:     layout.protocol.x,
				y:     layout.protocol.y + layout.protocol.height*(index+1),
				width: layout.protocol.width, height: layout.protocol.height,
			}
			fill(canvas, rectImage(option), dialogColors.field)
			stroke(canvas, option, dialogColors.border)
			if index == form.protocol {
				fill(canvas, image.Rect(option.x+1, option.y+1, option.x+5, option.y+option.height-1),
					dialogColors.focus)
			}
			drawText(canvas, option.x+10, option.y+22, protocol.name, dialogColors.text)
		}
	}

	if err := window.Paint(0, 0, width, height, canvas.Pix, canvas.Stride, "RGBA"); err != nil {
		return fmt.Errorf("painting connection dialog: %w", err)
	}
	return nil
}

func drawField(canvas *image.RGBA, r dialogRect, focused bool) {
	fill(canvas, rectImage(r), dialogColors.field)
	border := dialogColors.border
	if focused {
		border = dialogColors.focus
	}
	stroke(canvas, r, border)
}

func drawInput(canvas *image.RGBA, field dialogRect, input *textInput, password, focused bool) {
	text := append([]rune(nil), input.text...)
	if password {
		for index := range text {
			text[index] = '*'
		}
	}
	maxRunes := max((field.width-18)/basicfont.Face7x13.Advance, 1)
	start := 0
	if len(text) > maxRunes {
		start = max(input.cursor-maxRunes, 0)
		if start+maxRunes > len(text) {
			start = len(text) - maxRunes
		}
		text = text[start : start+maxRunes]
	}
	drawText(canvas, field.x+8, field.y+22, string(text), dialogColors.text)
	if focused {
		cursor := min(max(input.cursor-start, 0), len(text))
		x := field.x + 8 + cursor*basicfont.Face7x13.Advance
		fill(canvas, image.Rect(x, field.y+7, x+1, field.y+27), dialogColors.text)
	}
}

func drawButton(canvas *image.RGBA, r dialogRect, label string, focused, primary bool) {
	background, foreground := dialogColors.button, dialogColors.text
	if primary {
		background, foreground = dialogColors.primary, color.RGBA{R: 255, G: 255, B: 255, A: 255}
	}
	fill(canvas, rectImage(r), background)
	border := dialogColors.border
	if focused {
		border = dialogColors.focus
	}
	stroke(canvas, r, border)
	textWidth := utf8.RuneCountInString(label) * basicfont.Face7x13.Advance
	drawText(canvas, r.x+(r.width-textWidth)/2, r.y+22, label, foreground)
}

func drawText(canvas *image.RGBA, x, baseline int, text string, foreground color.Color) {
	drawer := font.Drawer{
		Dst:  canvas,
		Src:  image.NewUniform(foreground),
		Face: basicfont.Face7x13,
		Dot:  fixed.P(x, baseline),
	}
	drawer.DrawString(text)
}

func rectImage(r dialogRect) image.Rectangle {
	return image.Rect(r.x, r.y, r.x+r.width, r.y+r.height)
}

func fill(canvas *image.RGBA, r image.Rectangle, c color.Color) {
	draw.Draw(canvas, r.Intersect(canvas.Bounds()), image.NewUniform(c), image.Point{}, draw.Src)
}

func stroke(canvas *image.RGBA, r dialogRect, c color.Color) {
	fill(canvas, image.Rect(r.x, r.y, r.x+r.width, r.y+1), c)
	fill(canvas, image.Rect(r.x, r.y+r.height-1, r.x+r.width, r.y+r.height), c)
	fill(canvas, image.Rect(r.x, r.y, r.x+1, r.y+r.height), c)
	fill(canvas, image.Rect(r.x+r.width-1, r.y, r.x+r.width, r.y+r.height), c)
}
