package nucular

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/command"
	"github.com/aarzilli/nucular/rect"
	nstyle "github.com/aarzilli/nucular/style"

	"golang.org/x/exp/shiny/driver"
	"golang.org/x/exp/shiny/screen"
	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/mouse"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
)

//go:generate go-bindata -o internal/assets/assets.go -pkg assets DroidSansMono.ttf

const perfUpdate = false

var UnknownCommandErr = errors.New("unknown command")

var clipboardStarted bool = false
var clipboardMu sync.Mutex

type MasterWindow interface {
	context() *context

	Main()
	Changed()
	Close()
	Closed() bool
	ActivateEditor(ed *TextEditor)

	Style() *nstyle.Style
	SetStyle(*nstyle.Style)

	GetPerf() bool
	SetPerf(bool)

	PopupOpen(title string, flags WindowFlags, rect rect.Rect, scale bool, updateFn UpdateFn)
}

type masterWindow struct {
	screen screen.Screen
	wnd    screen.Window
	wndb   screen.Buffer
	bounds image.Rectangle

	// show performance counters
	Perf bool
	// window is focused
	Focus bool

	ctx        *context
	layout     panel
	prevCmds   []command.Command
	textbuffer bytes.Buffer

	uilock  sync.Mutex
	closing bool
}

// Creates new master window
func NewMasterWindow(updatefn UpdateFn, flags WindowFlags) MasterWindow {
	ctx := &context{}
	ctx.Input.Mouse.valid = true
	wnd := &masterWindow{ctx: ctx}
	wnd.layout.Flags = flags

	clipboardMu.Lock()
	if !clipboardStarted {
		clipboardStarted = true
		clipboard.Start()
	}
	clipboardMu.Unlock()

	ctx.setupMasterWindow(&wnd.layout, updatefn)
	ctx.mw = wnd

	wnd.SetStyle(nstyle.FromTheme(nstyle.DefaultTheme, 1.0))

	return wnd
}

// Shows window, runs event loop
func (mw *masterWindow) Main() {
	driver.Main(mw.main)
}

func (mw *masterWindow) context() *context {
	return mw.ctx
}

func (mw *masterWindow) main(s screen.Screen) {
	var err error
	mw.screen = s
	width, height := int(640*mw.ctx.Style.Scaling), int(480*mw.ctx.Style.Scaling)
	mw.wnd, err = s.NewWindow(&screen.NewWindowOptions{width, height})
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not create window: %v", err)
		return
	}
	mw.setupBuffer(image.Point{width, height})
	mw.Changed()

	go mw.updater()

	for {
		ei := mw.wnd.NextEvent()
		mw.uilock.Lock()
		r := mw.handleEventLocked(ei)
		mw.uilock.Unlock()
		if !r {
			break
		}
	}
}

func (w *masterWindow) handleEventLocked(ei interface{}) bool {
	switch e := ei.(type) {
	case paint.Event:
		w.updateLocked()

	case lifecycle.Event:
		if e.Crosses(lifecycle.StageDead) == lifecycle.CrossOn || e.To == lifecycle.StageDead || w.closing {
			w.closing = true
			w.closeLocked()
			return false
		}
		c := false
		switch cross := e.Crosses(lifecycle.StageFocused); cross {
		case lifecycle.CrossOn:
			w.Focus = true
			c = true
		case lifecycle.CrossOff:
			w.Focus = false
			c = true
		}
		if c {
			changed := atomic.LoadInt32(&w.ctx.changed)
			if changed < 2 {
				atomic.StoreInt32(&w.ctx.changed, 2)
			}
		}
	case size.Event:
		sz := e.Size()
		bb := w.wndb.Bounds()
		if sz.X <= bb.Dx() && sz.Y <= bb.Dy() {
			w.bounds = w.wndb.Bounds()
			w.bounds.Max.Y = w.bounds.Min.Y + sz.Y
			w.bounds.Max.X = w.bounds.Min.X + sz.X
		} else {
			if w.wndb != nil {
				w.wndb.Release()
			}
			w.setupBuffer(sz)
		}
		w.prevCmds = w.prevCmds[:0]
		w.Changed()

	case mouse.Event:
		changed := atomic.LoadInt32(&w.ctx.changed)
		if changed < 2 {
			atomic.StoreInt32(&w.ctx.changed, 2)
		}
		switch e.Direction {
		case mouse.DirStep:
			switch e.Button {
			case mouse.ButtonWheelUp:
				w.ctx.Input.Mouse.ScrollDelta++
			case mouse.ButtonWheelDown:
				w.ctx.Input.Mouse.ScrollDelta--
			}
		case mouse.DirPress, mouse.DirRelease:
			down := e.Direction == mouse.DirPress

			if e.Button >= 0 && int(e.Button) < len(w.ctx.Input.Mouse.Buttons) {
				btn := &w.ctx.Input.Mouse.Buttons[e.Button]
				if btn.Down == down {
					break
				}

				if down {
					btn.ClickedPos.X = int(e.X)
					btn.ClickedPos.Y = int(e.Y)
				}
				btn.Clicked = true
				btn.Down = down
			}
		case mouse.DirNone:
			w.ctx.Input.Mouse.Pos.X = int(e.X)
			w.ctx.Input.Mouse.Pos.Y = int(e.Y)
			w.ctx.Input.Mouse.Delta = w.ctx.Input.Mouse.Pos.Sub(w.ctx.Input.Mouse.Prev)
		}

	case key.Event:
		changed := atomic.LoadInt32(&w.ctx.changed)
		if changed < 2 {
			atomic.StoreInt32(&w.ctx.changed, 2)
		}
		w.ctx.processKeyEvent(e, &w.textbuffer)
	}

	return true
}

func (ctx *context) processKeyEvent(e key.Event, textbuffer *bytes.Buffer) {
	if e.Direction == key.DirRelease {
		return
	}

	evinNotext := func() {
		for _, k := range ctx.Input.Keyboard.Keys {
			if k.Code == e.Code {
				k.Modifiers |= e.Modifiers
				return
			}
		}
		ctx.Input.Keyboard.Keys = append(ctx.Input.Keyboard.Keys, e)
	}
	evinText := func() {
		if e.Modifiers == 0 || e.Modifiers == key.ModShift {
			io.WriteString(textbuffer, string(e.Rune))
		}

		evinNotext()
	}

	switch {
	case e.Code == key.CodeUnknown:
		if e.Rune > 0 {
			evinText()
		}
	case (e.Code >= key.CodeA && e.Code <= key.Code0) || e.Code == key.CodeSpacebar || e.Code == key.CodeHyphenMinus || e.Code == key.CodeEqualSign || e.Code == key.CodeLeftSquareBracket || e.Code == key.CodeRightSquareBracket || e.Code == key.CodeBackslash || e.Code == key.CodeSemicolon || e.Code == key.CodeApostrophe || e.Code == key.CodeGraveAccent || e.Code == key.CodeComma || e.Code == key.CodeFullStop || e.Code == key.CodeSlash || (e.Code >= key.CodeKeypadSlash && e.Code <= key.CodeKeypadPlusSign) || (e.Code >= key.CodeKeypad1 && e.Code <= key.CodeKeypadEqualSign):
		evinText()
	case e.Code == key.CodeTab:
		e.Rune = '\t'
		evinText()
	case e.Code == key.CodeReturnEnter || e.Code == key.CodeKeypadEnter:
		e.Rune = '\n'
		evinText()
	default:
		evinNotext()
	}
}

func (w *masterWindow) updater() {
	for {
		time.Sleep(20 * time.Millisecond)
		func() {
			w.uilock.Lock()
			defer w.uilock.Unlock()
			if w.closing {
				return
			}
			changed := atomic.LoadInt32(&w.ctx.changed)
			if changed > 0 {
				atomic.AddInt32(&w.ctx.changed, -1)
				w.updateLocked()
			} else {
				down := false
				for _, btn := range w.ctx.Input.Mouse.Buttons {
					if btn.Down {
						down = true
					}
				}
				if down {
					w.updateLocked()
				}
			}
		}()
	}
}

// Forces an update of the window.
func (mw *masterWindow) Changed() {
	atomic.AddInt32(&mw.ctx.changed, 1)
}

func (w *masterWindow) updateLocked() {
	w.ctx.Windows[0].Bounds = rect.FromRectangle(w.bounds)
	in := &w.ctx.Input
	in.Mouse.clip = nk_null_rect
	in.Keyboard.Text = w.textbuffer.String()
	w.textbuffer.Reset()

	var t0, t1, te time.Time
	if perfUpdate || w.Perf {
		t0 = time.Now()
	}

	w.ctx.Update()

	if perfUpdate || w.Perf {
		t1 = time.Now()
	}
	nprimitives := w.draw()
	if perfUpdate {
		te = time.Now()

		fps := 1.0 / te.Sub(t0).Seconds()

		fmt.Printf("Update %0.4f msec = %0.4f updatefn + %0.4f draw (%d primitives) [max fps %0.2f]\n", te.Sub(t0).Seconds()*1000, t1.Sub(t0).Seconds()*1000, te.Sub(t1).Seconds()*1000, nprimitives, fps)
	}
	if w.Perf && nprimitives > 0 {
		te = time.Now()

		fps := 1.0 / te.Sub(t0).Seconds()

		s := fmt.Sprintf("%0.4fms + %0.4fms (%0.2f)", t1.Sub(t0).Seconds()*1000, te.Sub(t1).Seconds()*1000, fps)
		img := w.wndb.RGBA()
		d := font.Drawer{
			Dst:  img,
			Src:  image.White,
			Face: w.ctx.Style.Font}

		width := d.MeasureString(s).Ceil()

		bounds := w.bounds
		bounds.Min.X = bounds.Max.X - width
		bounds.Min.Y = bounds.Max.Y - (w.ctx.Style.Font.Metrics().Ascent + w.ctx.Style.Font.Metrics().Descent).Ceil()
		draw.Draw(img, bounds, image.Black, bounds.Min, draw.Src)
		d.Dot = fixed.P(bounds.Min.X, bounds.Min.Y+w.ctx.Style.Font.Metrics().Ascent.Ceil())
		d.DrawString(s)
	}
	w.ctx.Reset()
	if nprimitives > 0 {
		w.wnd.Upload(w.bounds.Min, w.wndb, w.bounds)
		w.wnd.Publish()
	}
}

func (w *masterWindow) closeLocked() {
	w.closing = true
	if w.wndb != nil {
		w.wndb.Release()
	}
	w.wnd.Release()
}

// Programmatically closes window.
func (mw *masterWindow) Close() {
	mw.uilock.Lock()
	defer mw.uilock.Unlock()
	mw.closeLocked()
}

// Returns true if the window is closed.
func (mw *masterWindow) Closed() bool {
	mw.uilock.Lock()
	defer mw.uilock.Unlock()
	return mw.closing
}

func (w *masterWindow) setupBuffer(sz image.Point) {
	var err error
	oldb := w.wndb
	w.wndb, err = w.screen.NewBuffer(sz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not setup buffer: %v", err)
		w.wndb = oldb
	}
	w.bounds = w.wndb.Bounds()
}

func (w *masterWindow) draw() int {
	wimg := w.wndb.RGBA()

	contextAllCommands(w.ctx)

	if !w.drawChanged(w.ctx.cmds) {
		return 0
	}

	w.prevCmds = append(w.prevCmds[:0], w.ctx.cmds...)

	return w.ctx.Draw(wimg)
}

// compares cmds to the last draw frame, returns true if there is a change
func (w *masterWindow) drawChanged(cmds []command.Command) bool {
	if len(cmds) != len(w.prevCmds) {
		return true
	}

	for i := range cmds {
		if cmds[i].Kind != w.prevCmds[i].Kind {
			return true
		}

		cmd := &cmds[i]
		pcmd := &w.prevCmds[i]

		switch cmds[i].Kind {
		case command.ScissorCmd:
			if *pcmd != *cmd {
				return true
			}

		case command.LineCmd:
			if *pcmd != *cmd {
				return true
			}

		case command.RectFilledCmd:
			if i == 0 {
				cmd.RectFilled.Color.A = 0xff
			}
			if *pcmd != *cmd {
				return true
			}

		case command.TriangleFilledCmd:
			if *pcmd != *cmd {
				return true
			}

		case command.CircleFilledCmd:
			if *pcmd != *cmd {
				return true
			}

		case command.ImageCmd:
			if *pcmd != *cmd {
				return true
			}

		case command.TextCmd:
			if *pcmd != *cmd {
				return true
			}

		default:
			panic(UnknownCommandErr)
		}
	}

	return false
}

func (mw *masterWindow) ActivateEditor(ed *TextEditor) {
	mw.ctx.activateEditor = ed
}

func (mw *masterWindow) Style() *nstyle.Style {
	return &mw.ctx.Style
}

func (mw *masterWindow) SetStyle(style *nstyle.Style) {
	mw.ctx.Style = *style
	mw.ctx.Style.Defaults()
}

func (mw *masterWindow) GetPerf() bool {
	return mw.Perf
}

func (mw *masterWindow) SetPerf(perf bool) {
	mw.Perf = perf
}
