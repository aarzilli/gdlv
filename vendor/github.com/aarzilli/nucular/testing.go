package nucular

import (
	"bytes"
	"image"

	"github.com/aarzilli/nucular/rect"
	nstyle "github.com/aarzilli/nucular/style"

	"golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/mouse"
)

type TestWindow struct {
	Img *image.RGBA

	ctx    *context
	layout panel
}

func NewTestWindow(flags WindowFlags, size image.Point, updatefn UpdateFn) *TestWindow {
	ctx := &context{}
	ctx.Input.Mouse.valid = true
	wnd := &TestWindow{ctx: ctx}
	wnd.Img = image.NewRGBA(image.Rect(0, 0, size.X, size.Y))
	wnd.layout.Flags = flags
	ctx.setupMasterWindow(&wnd.layout, updatefn)
	ctx.Windows[0].Bounds = rect.Rect{0, 0, size.X, size.Y}
	ctx.mw = wnd
	wnd.SetStyle(nstyle.FromTheme(nstyle.DefaultTheme, 1.0))
	return wnd
}

func (w *TestWindow) context() *context {
	return w.ctx
}

func (w *TestWindow) ActivateEditor(ed *TextEditor) {
	w.ctx.activateEditor = ed
}

func (w *TestWindow) Close() {
}

func (w *TestWindow) Closed() bool {
	return false
}

func (w *TestWindow) Changed() {
}

func (w *TestWindow) Main() {
}

func (w *TestWindow) SetStyle(style *nstyle.Style) {
	w.ctx.Style = *style
	w.ctx.Style.Defaults()
}

func (w *TestWindow) Style() *nstyle.Style {
	return &w.ctx.Style
}

// Update runs the update function.
func (w *TestWindow) Update() {
	in := &w.ctx.Input
	in.Mouse.clip = nk_null_rect
	w.ctx.Update()
	contextAllCommands(w.ctx)
	w.ctx.Draw(w.Img)
	w.ctx.Reset()
}

func (w *TestWindow) GetPerf() bool {
	return false
}

func (w *TestWindow) SetPerf(p bool) {
}

func (w *TestWindow) PopupOpen(title string, flags WindowFlags, rect rect.Rect, scale bool, updateFn UpdateFn) {
	w.ctx.popupOpen(title, flags, rect, scale, updateFn, nil)
}

func (w *TestWindow) PopupOpenPersistent(title string, flags WindowFlags, rect rect.Rect, scale bool, updateFn UpdateFn, saveFn SaveFn) {
	w.ctx.popupOpen(title, flags, rect, scale, updateFn, saveFn)
}

// Click simulates a click at point p.
// The update function will be run as many times as needed, the window will
// be drawn every time.
func (w *TestWindow) Click(button mouse.Button, p image.Point) {
	if button < 0 || int(button) >= len(w.ctx.Input.Mouse.Buttons) {
		return
	}

	in := &w.ctx.Input
	// Mouse move
	in.Mouse.Pos = p
	in.Mouse.Delta = in.Mouse.Pos.Sub(in.Mouse.Prev)
	w.Update()

	// Button press
	btn := &w.ctx.Input.Mouse.Buttons[button]
	btn.ClickedPos = p
	btn.Clicked = true
	btn.Down = true
	w.Update()

	// Button release
	btn.Clicked = true
	btn.Down = false
	w.Update()
}

// Type simulates typing.
// The update function will be run as many times as needed, the window will
// be drawn every time.
func (w *TestWindow) Type(text string) {
	w.ctx.Input.Keyboard.Text = text
	w.Update()
}

func (w *TestWindow) TypeKey(e key.Event) {
	var b bytes.Buffer
	w.ctx.processKeyEvent(e, &b)
	w.ctx.Input.Keyboard.Text = w.ctx.Input.Keyboard.Text + b.String()
	w.Update()
}

func (w *TestWindow) Save() ([]byte, error) {
	return nil, nil
}

func (w *TestWindow) Restore([]byte, RestoreFn) {
	return
}

func (w *TestWindow) ListWindowsData() []interface{} {
	return w.ctx.ListWindowsData()
}
