package nucular

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/command"
	"github.com/aarzilli/nucular/internal/assets"
	"github.com/aarzilli/nucular/rect"

	"github.com/golang/freetype"
	"github.com/golang/freetype/raster"
	"github.com/golang/freetype/truetype"

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

type MasterWindow struct {
	screen screen.Screen
	wnd    screen.Window
	wndb   screen.Buffer
	img    *image.RGBA
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

var ttfontDefault *truetype.Font
var defaultFontInit sync.Once

// Returns default font (DroidSansMono) with specified size and scaling
func DefaultFont(size int, scaling float64) font.Face {
	defaultFontInit.Do(func() {
		fontData, _ := assets.Asset("DroidSansMono.ttf")
		ttfontDefault, _ = freetype.ParseFont(fontData)
	})

	sz := int(float64(size) * scaling)

	return truetype.NewFace(ttfontDefault, &truetype.Options{Size: float64(sz), Hinting: font.HintingFull, DPI: 72})
}

// Creates new master window
func NewMasterWindow(updatefn UpdateFn, flags WindowFlags) *MasterWindow {
	ctx := &context{Scaling: 1.0}
	ctx.Input.Mouse.valid = true
	wnd := &MasterWindow{ctx: ctx}
	wnd.layout.Flags = flags

	clipboardMu.Lock()
	if !clipboardStarted {
		clipboardStarted = true
		clipboard.Start()
	}
	clipboardMu.Unlock()

	ctx.Windows = append(ctx.Windows, createWindow(ctx, ""))
	ctx.Windows[0].idx = 0
	ctx.Windows[0].layout = &wnd.layout
	ctx.Windows[0].flags = wnd.layout.Flags
	ctx.Windows[0].cmds.UseClipping = true
	ctx.Windows[0].updateFn = updatefn
	ctx.mw = wnd

	return wnd
}

// Shows window, runs event loop
func (mw *MasterWindow) Main() {
	driver.Main(mw.main)
}

func (mw *MasterWindow) main(s screen.Screen) {
	var err error
	mw.screen = s
	width, height := int(640*mw.ctx.Scaling), int(480*mw.ctx.Scaling)
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

func (w *MasterWindow) handleEventLocked(ei interface{}) bool {
	switch e := ei.(type) {
	case paint.Event:
		w.updateLocked()

	case lifecycle.Event:
		if e.Crosses(lifecycle.StageDead) == lifecycle.CrossOn || e.To == lifecycle.StageDead {
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
			w.updateLocked()
		} else {
			if w.wndb != nil {
				w.wndb.Release()
			}
			w.setupBuffer(sz)
			w.updateLocked()
		}

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
		if e.Direction == key.DirRelease {
			break
		}
		evinNotext := func() {
			for _, k := range w.ctx.Input.Keyboard.Keys {
				if k.Code == e.Code {
					k.Modifiers |= e.Modifiers
					return
				}
			}
			w.ctx.Input.Keyboard.Keys = append(w.ctx.Input.Keyboard.Keys, e)
		}
		evinText := func() {
			if e.Modifiers == 0 || e.Modifiers == key.ModShift {
				io.WriteString(&w.textbuffer, string(e.Rune))
			}

			evinNotext()
		}

		switch {
		case e.Code == key.CodeUnknown:
			if e.Rune > 0 {
				evinText()
			}
		case (e.Code >= key.CodeA && e.Code <= key.Code0) || e.Code == key.CodeTab || e.Code == key.CodeSpacebar || e.Code == key.CodeHyphenMinus || e.Code == key.CodeEqualSign || e.Code == key.CodeLeftSquareBracket || e.Code == key.CodeRightSquareBracket || e.Code == key.CodeBackslash || e.Code == key.CodeSemicolon || e.Code == key.CodeApostrophe || e.Code == key.CodeGraveAccent || e.Code == key.CodeComma || e.Code == key.CodeFullStop || e.Code == key.CodeSlash || (e.Code >= key.CodeKeypadSlash && e.Code <= key.CodeKeypadPlusSign) || (e.Code >= key.CodeKeypad1 && e.Code <= key.CodeKeypadEqualSign):
			evinText()
		case e.Code == key.CodeReturnEnter || e.Code == key.CodeKeypadEnter:
			e.Rune = '\n'
			evinText()
		default:
			evinNotext()
		}
	}

	return true
}

func (w *MasterWindow) updater() {
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
func (mw *MasterWindow) Changed() {
	atomic.AddInt32(&mw.ctx.changed, 1)
}

func (w *MasterWindow) updateLocked() {
	contextBegin(w.ctx, &w.layout)
	in := &w.ctx.Input
	in.Mouse.clip = nk_null_rect
	in.Keyboard.Text = w.textbuffer.String()
	w.ctx.Windows[0].Bounds = rect.FromRectangle(w.bounds)
	var t0, t1, te time.Time
	if perfUpdate || w.Perf {
		t0 = time.Now()
	}
	for i := 0; i < len(w.ctx.Windows); i++ { // this must not use range or tooltips won't work
		win := w.ctx.Windows[i]
		if win.flags&windowContextual != 0 {
			prevbody := win.Bounds
			prevbody.H = win.layout.Height
			// if the contextual menu ended up with its bottom right corner outside
			// the main window's bounds and it could be moved to be inside the main
			// window by popping it a different way do it.
			// Since the size of the contextual menu is only knowable after displaying
			// it once this must be done on the second frame.
			max := w.ctx.Windows[0].Bounds.Max()
			if win.triggerBounds.Contains(prevbody.Min()) && ((prevbody.Max().X > max.X) || (prevbody.Max().Y > max.Y)) && (win.Bounds.X-prevbody.W >= 0) && (win.Bounds.Y-prevbody.H >= 0) {
				win.Bounds.X = win.Bounds.X - prevbody.W
				win.Bounds.Y = win.Bounds.Y - prevbody.H
			} else {
				win.Bounds.X = win.Bounds.X
				win.Bounds.Y = win.Bounds.Y
			}
		}

		if win.flags&windowCombo != 0 && win.flags&WindowDynamic != 0 {
			prevbody := win.Bounds
			prevbody.H = win.layout.Height
			// If the combo window ends up with the right corner below the
			// main winodw's lower bound make it non-dynamic and resize it to its
			// maximum possible size that will show the whole combo box.
			max := w.ctx.Windows[0].Bounds.Max()
			if prevbody.Y+prevbody.H > max.Y {
				prevbody.H = max.Y - prevbody.Y
				win.Bounds = prevbody
				win.flags &= ^windowCombo
			}
		}

		if win.flags&windowNonblock != 0 && !win.first {
			/* check if user clicked outside the popup and close if so */
			in_panel := w.ctx.Input.Mouse.IsClickInRect(mouse.ButtonLeft, win.ctx.Windows[0].layout.Bounds)
			prevbody := win.Bounds
			prevbody.H = win.layout.Height
			in_body := w.ctx.Input.Mouse.IsClickInRect(mouse.ButtonLeft, prevbody)
			in_header := w.ctx.Input.Mouse.IsClickInRect(mouse.ButtonLeft, win.header)
			if !in_body && in_panel || in_header {
				win.close = true
			}
		}

		if win.flags&windowPopup != 0 {
			win.cmds.PushScissor(nk_null_rect)

			if !panelBegin(w.ctx, win, win.title) {
				win.close = true
			}
			win.layout.Offset = &win.Scrollbar
		}

		win.first = false

		win.updateFn(win)

		if win.title == tooltipWindowTitle {
			win.close = true
		}

		if win.flags&windowPopup != 0 {
			panelEnd(w.ctx, win)
		}
	}
	contextEnd(w.ctx)
	b := w.bounds
	if perfUpdate || w.Perf {
		t1 = time.Now()
	}
	nwidgets, nprimitives := w.draw()
	if perfUpdate {
		te = time.Now()

		fps := 1.0 / te.Sub(t0).Seconds()

		fmt.Printf("Update %0.4f msec = %0.4f updatefn (%d widgets)+ %0.4f draw (%d primitives) [max fps %0.2f]\n", te.Sub(t0).Seconds()*1000, t1.Sub(t0).Seconds()*1000, nwidgets, te.Sub(t1).Seconds()*1000, nprimitives, fps)
	}
	if w.Perf && nprimitives > 0 {
		te = time.Now()

		fps := 1.0 / te.Sub(t0).Seconds()

		s := fmt.Sprintf("%0.4fms + %0.4fms (%0.2f)", t1.Sub(t0).Seconds()*1000, te.Sub(t1).Seconds()*1000, fps)
		d := font.Drawer{
			Dst:  w.img,
			Src:  image.White,
			Face: w.ctx.Style.Font}

		width := d.MeasureString(s).Ceil()

		bounds := w.img.Bounds()
		bounds.Min.X = bounds.Max.X - width
		bounds.Min.Y = bounds.Max.Y - (w.ctx.Style.Font.Metrics().Ascent + w.ctx.Style.Font.Metrics().Descent).Ceil()
		draw.Draw(w.img, bounds, image.Black, bounds.Min, draw.Src)
		d.Dot = fixed.P(bounds.Min.X, bounds.Min.Y+w.ctx.Style.Font.Metrics().Ascent.Ceil())
		d.DrawString(s)
	}
	for i := 0; i < len(w.ctx.Windows); i++ {
		if w.ctx.Windows[i].close {
			if i != len(w.ctx.Windows)-1 {
				copy(w.ctx.Windows[i:], w.ctx.Windows[i+1:])
				i--
			}
			w.ctx.Windows = w.ctx.Windows[:len(w.ctx.Windows)-1]
		}
	}
	for i := range w.ctx.Windows {
		w.ctx.Windows[i].idx = i
	}
	w.ctx.activateEditor = nil
	in.Mouse.Buttons[mouse.ButtonLeft].Clicked = false
	in.Mouse.Buttons[mouse.ButtonMiddle].Clicked = false
	in.Mouse.Buttons[mouse.ButtonRight].Clicked = false
	in.Mouse.ScrollDelta = 0
	in.Mouse.Prev.X = in.Mouse.Pos.X
	in.Mouse.Prev.Y = in.Mouse.Pos.Y
	in.Mouse.Delta = image.Point{}
	w.textbuffer.Reset()
	in.Keyboard.Keys = in.Keyboard.Keys[0:0]
	draw.Draw(w.wndb.RGBA(), b, w.img, b.Min, draw.Src)
	w.wnd.Upload(b.Min, w.wndb, b)
	w.wnd.Publish()
}

func (w *MasterWindow) closeLocked() {
	w.closing = true
	if w.wndb != nil {
		w.wndb.Release()
	}
	w.wnd.Release()
}

// Programmatically closes window.
func (mw *MasterWindow) Close() {
	mw.uilock.Lock()
	defer mw.uilock.Unlock()
	mw.closeLocked()
}

// Returns true if the window is closed.
func (mw *MasterWindow) Closed() bool {
	mw.uilock.Lock()
	defer mw.uilock.Unlock()
	return mw.closing
}

func (w *MasterWindow) setupBuffer(sz image.Point) {
	var err error
	oldb := w.wndb
	w.wndb, err = w.screen.NewBuffer(sz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not setup buffer: %v", err)
		w.wndb = oldb
	}
	w.img = image.NewRGBA(w.wndb.Bounds())
	w.bounds = w.wndb.Bounds()
}

var cnt = 0
var ln, frect, brrect, frrect, ftri, circ, fcirc, txt int

func (w *MasterWindow) draw() (int, int) {
	img := w.img

	var painter *myRGBAPainter
	var rasterizer *raster.Rasterizer

	roundAngle := func(cx, cy int, radius uint16, startAngle, angle float64, c color.Color) {
		rasterizer.Clear()
		rasterizer.Start(fixed.P(cx, cy))
		traceArc(rasterizer, float64(cx), float64(cy), float64(radius), float64(radius), startAngle, angle, false)
		rasterizer.Add1(fixed.P(cx, cy))
		painter.SetColor(c)
		rasterizer.Rasterize(painter)

	}

	setupRasterizer := func() {
		rasterizer = raster.NewRasterizer(img.Bounds().Dx(), img.Bounds().Dy())
		painter = &myRGBAPainter{Image: img}
	}

	nwidgets, cmds := contextAllCommands(w.ctx)

	var txttim, tritim, brecttim, frecttim, frrecttim time.Duration
	var t0 time.Time

	if !w.drawChanged(cmds) {
		return 0, 0
	}

	w.prevCmds = cmds

	for i, icmd := range cmds {
		switch cmd := icmd.(type) {
		case *command.Scissor:
			img = w.img.SubImage(cmd.Rect.Rectangle()).(*image.RGBA)
			painter = nil
			rasterizer = nil

		case *command.Line:
			colimg := image.NewUniform(cmd.Color)
			op := draw.Over
			if cmd.Color.A == 0xff {
				op = draw.Src
			}

			h1 := int(cmd.LineThickness / 2)
			h2 := int(cmd.LineThickness) - h1

			if cmd.Begin.X == cmd.End.X {
				// draw vertical line
				r := image.Rect(cmd.Begin.X-h1, cmd.Begin.Y, cmd.Begin.X+h2, cmd.End.Y)
				draw.Draw(img, r, colimg, r.Min, op)
			} else if cmd.Begin.Y == cmd.End.Y {
				// draw horizontal line
				r := image.Rect(cmd.Begin.X, cmd.Begin.Y-h1, cmd.End.X, cmd.Begin.Y+h2)
				draw.Draw(img, r, colimg, r.Min, op)
			} else {
				if rasterizer == nil {
					setupRasterizer()
				}

				unzw := rasterizer.UseNonZeroWinding
				rasterizer.UseNonZeroWinding = true

				var p raster.Path
				p.Start(fixed.P(cmd.Begin.X-img.Bounds().Min.X, cmd.Begin.Y-img.Bounds().Min.Y))
				p.Add1(fixed.P(cmd.End.X-img.Bounds().Min.X, cmd.End.Y-img.Bounds().Min.Y))

				rasterizer.Clear()
				rasterizer.AddStroke(p, fixed.I(int(cmd.LineThickness)), nil, nil)
				painter.SetColor(cmd.Color)
				rasterizer.Rasterize(painter)

				rasterizer.UseNonZeroWinding = unzw
			}
			ln++

		case *command.RectFilled:
			if i == 0 {
				// first command draws the background, insure that it's always fully opaque
				cmd.Color.A = 0xff
			}
			if perfUpdate {
				t0 = time.Now()
			}
			colimg := image.NewUniform(cmd.Color)
			op := draw.Over
			if cmd.Color.A == 0xff {
				op = draw.Src
			}

			body := cmd.Rect.Rectangle()

			var lwing, rwing image.Rectangle

			// rounding is true if rounding has been requested AND we can draw it
			rounding := cmd.Rounding > 0 && int(cmd.Rounding*2) < cmd.W && int(cmd.Rounding*2) < cmd.H

			if rounding {
				body.Min.X += int(cmd.Rounding)
				body.Max.X -= int(cmd.Rounding)

				lwing = image.Rect(cmd.Rect.X, cmd.Rect.Y+int(cmd.Rounding), cmd.Rect.X+int(cmd.Rounding), cmd.Rect.Y+cmd.Rect.H-int(cmd.Rounding))
				rwing = image.Rect(cmd.Rect.X+cmd.Rect.W-int(cmd.Rounding), lwing.Min.Y, cmd.Rect.X+cmd.Rect.W, lwing.Max.Y)
			}

			bordopt := false

			if ok, border := borderOptimize(cmd, cmds, i+1); ok {
				// only draw parts of body if this command can be optimized to a border with the next command

				bordopt = true
				cmd2, _ := cmds[i+1].(*command.RectFilled)
				border += int(cmd2.Rounding)

				top := image.Rect(body.Min.X, body.Min.Y, body.Max.X, body.Min.Y+border)
				bot := image.Rect(body.Min.X, body.Max.Y-border, body.Max.X, body.Max.Y)

				draw.Draw(img, top, colimg, top.Min, op)
				draw.Draw(img, bot, colimg, bot.Min, op)

				if border < int(cmd.Rounding) {
					// wings need shrinking
					d := int(cmd.Rounding) - border
					lwing.Max.Y -= d
					rwing.Min.Y += d
				} else {
					// display extra wings
					d := border - int(cmd.Rounding)

					xlwing := image.Rect(top.Min.X, top.Max.Y, top.Min.X+d, bot.Min.Y)
					xrwing := image.Rect(top.Max.X-d, top.Max.Y, top.Max.X, bot.Min.Y)

					draw.Draw(img, xlwing, colimg, xlwing.Min, op)
					draw.Draw(img, xrwing, colimg, xrwing.Min, op)
				}

				brrect++
			} else {
				draw.Draw(img, body, colimg, body.Min, op)
				if cmd.Rounding == 0 {
					frect++
				} else {
					frrect++
				}
			}

			if rounding {
				draw.Draw(img, lwing, colimg, lwing.Min, op)
				draw.Draw(img, rwing, colimg, rwing.Min, op)

				rangle := math.Pi / 2

				if rasterizer == nil {
					setupRasterizer()
				}

				minx := img.Bounds().Min.X
				miny := img.Bounds().Min.Y

				roundAngle(cmd.X+cmd.W-int(cmd.Rounding)-minx, cmd.Y+int(cmd.Rounding)-miny, cmd.Rounding, -math.Pi/2, rangle, cmd.Color)
				roundAngle(cmd.X+cmd.W-int(cmd.Rounding)-minx, cmd.Y+cmd.H-int(cmd.Rounding)-miny, cmd.Rounding, 0, rangle, cmd.Color)
				roundAngle(cmd.X+int(cmd.Rounding)-minx, cmd.Y+cmd.H-int(cmd.Rounding)-miny, cmd.Rounding, math.Pi/2, rangle, cmd.Color)
				roundAngle(cmd.X+int(cmd.Rounding)-minx, cmd.Y+int(cmd.Rounding)-miny, cmd.Rounding, math.Pi, rangle, cmd.Color)
			}

			if perfUpdate {
				if bordopt {
					brecttim += time.Now().Sub(t0)
				} else {
					if cmd.Rounding > 0 {
						frrecttim += time.Now().Sub(t0)
					} else {
						frecttim += time.Now().Sub(t0)
					}
				}
			}

		case *command.TriangleFilled:
			if perfUpdate {
				t0 = time.Now()
			}
			if rasterizer == nil {
				setupRasterizer()
			}
			minx := img.Bounds().Min.X
			miny := img.Bounds().Min.Y
			rasterizer.Clear()
			rasterizer.Start(fixed.P(cmd.A.X-minx, cmd.A.Y-miny))
			rasterizer.Add1(fixed.P(cmd.B.X-minx, cmd.B.Y-miny))
			rasterizer.Add1(fixed.P(cmd.C.X-minx, cmd.C.Y-miny))
			rasterizer.Add1(fixed.P(cmd.A.X-minx, cmd.A.Y-miny))
			painter.SetColor(cmd.Color)
			rasterizer.Rasterize(painter)
			ftri++

			if perfUpdate {
				tritim += time.Now().Sub(t0)
			}

		case *command.CircleFilled:
			if rasterizer == nil {
				setupRasterizer()
			}
			rasterizer.Clear()
			startp := traceArc(rasterizer, float64(cmd.X-img.Bounds().Min.X)+float64(cmd.W/2), float64(cmd.Y-img.Bounds().Min.Y)+float64(cmd.H/2), float64(cmd.W/2), float64(cmd.H/2), 0, -math.Pi*2, true)
			rasterizer.Add1(startp) // closes path
			painter.SetColor(cmd.Color)
			rasterizer.Rasterize(painter)
			fcirc++

		case *command.Image:
			draw.Draw(img, cmd.Rect.Rectangle(), cmd.Img, image.Point{}, draw.Src)

		case *command.Text:
			if perfUpdate {
				t0 = time.Now()
			}
			dstimg := w.img.SubImage(img.Bounds().Intersect(cmd.Rect.Rectangle())).(*image.RGBA)
			d := font.Drawer{
				Dst:  dstimg,
				Src:  image.NewUniform(cmd.Foreground),
				Face: cmd.Face,
				Dot:  fixed.P(cmd.X, cmd.Y+cmd.Face.Metrics().Ascent.Ceil())}

			start := 0
			for i := range cmd.String {
				if cmd.String[i] == '\n' {
					d.DrawString(cmd.String[start:i])
					d.Dot.X = fixed.I(cmd.X)
					d.Dot.Y += fixed.I(FontHeight(cmd.Face))
					start = i + 1
				}
			}
			if start < len(cmd.String) {
				d.DrawString(cmd.String[start:])
			}
			txt++
			if perfUpdate {
				txttim += time.Now().Sub(t0)
			}
		default:
			panic(UnknownCommandErr)
		}
	}

	if perfUpdate {
		fmt.Printf("triangle: %0.4fms text: %0.4fms brect: %0.4fms frect: %0.4fms frrect %0.4f\n", tritim.Seconds()*1000, txttim.Seconds()*1000, brecttim.Seconds()*1000, frecttim.Seconds()*1000, frrecttim.Seconds()*1000)
	}

	cnt++
	if perfUpdate && (cnt%100) == 0 {
		fmt.Printf("ln %d, frect %d, frrect %d, brrect %d, ftri %d, circ %d, fcirc %d, txt %d\n", ln, frect, frrect, brrect, ftri, circ, fcirc, txt)
		ln, frect, frrect, brrect, ftri, circ, fcirc, txt = 0, 0, 0, 0, 0, 0, 0, 0
	}

	return nwidgets, len(cmds)
}

// Returns true if cmds[idx] is a shrunk version of CommandFillRect and its
// color is not semitransparent and the border isn't greater than 128
func borderOptimize(cmd *command.RectFilled, cmds []command.Command, idx int) (ok bool, border int) {
	if idx >= len(cmds) {
		return false, 0
	}

	cmd2, ok := cmds[idx].(*command.RectFilled)
	if !ok {
		return false, 0
	}

	if cmd2.Color.A != 0xff {
		return false, 0
	}

	border = cmd2.Rect.X - cmd.Rect.X
	if border <= 0 || border > 128 {
		return false, 0
	}

	if shrinkRect(cmd.Rect, border) != cmd2.Rect {
		return false, 0
	}

	return true, border
}

func floatP(x, y float64) fixed.Point26_6 {
	return fixed.Point26_6{X: fixed.Int26_6(x * 64), Y: fixed.Int26_6(y * 64)}
}

// TraceArc trace an arc using a Liner
func traceArc(t *raster.Rasterizer, x, y, rx, ry, start, angle float64, first bool) fixed.Point26_6 {
	end := start + angle
	clockWise := true
	if angle < 0 {
		clockWise = false
	}
	if !clockWise {
		for start < end {
			start += math.Pi * 2
		}
		end = start + angle
	}
	ra := (math.Abs(rx) + math.Abs(ry)) / 2
	da := math.Acos(ra/(ra+0.125)) * 2
	//normalize
	if !clockWise {
		da = -da
	}
	angle = start
	var curX, curY float64
	var startX, startY float64
	for {
		if (angle < end-da/4) != clockWise {
			curX = x + math.Cos(end)*rx
			curY = y + math.Sin(end)*ry
			t.Add1(floatP(curX, curY))
			return floatP(startX, startY)
		}
		curX = x + math.Cos(angle)*rx
		curY = y + math.Sin(angle)*ry

		angle += da
		if first {
			first = false
			startX, startY = curX, curY
			t.Start(floatP(curX, curY))
		} else {
			t.Add1(floatP(curX, curY))
		}
	}
}

type myRGBAPainter struct {
	Image *image.RGBA
	// cr, cg, cb and ca are the 16-bit color to paint the spans.
	cr, cg, cb, ca uint32
}

// SetColor sets the color to paint the spans.
func (r *myRGBAPainter) SetColor(c color.Color) {
	r.cr, r.cg, r.cb, r.ca = c.RGBA()
}

func (r *myRGBAPainter) Paint(ss []raster.Span, done bool) {
	b := r.Image.Bounds()
	cr8 := uint8(r.cr >> 8)
	cg8 := uint8(r.cg >> 8)
	cb8 := uint8(r.cb >> 8)
	for _, s := range ss {
		s.Y += b.Min.Y
		s.X0 += b.Min.X
		s.X1 += b.Min.X
		if s.Y < b.Min.Y {
			continue
		}
		if s.Y >= b.Max.Y {
			return
		}
		if s.X0 < b.Min.X {
			s.X0 = b.Min.X
		}
		if s.X1 > b.Max.X {
			s.X1 = b.Max.X
		}
		if s.X0 >= s.X1 {
			continue
		}
		// This code mimics drawGlyphOver in $GOROOT/src/image/draw/draw.go.
		ma := s.Alpha
		const m = 1<<16 - 1
		i0 := (s.Y-r.Image.Rect.Min.Y)*r.Image.Stride + (s.X0-r.Image.Rect.Min.X)*4
		i1 := i0 + (s.X1-s.X0)*4
		if ma != m || r.ca != m {
			for i := i0; i < i1; i += 4 {
				dr := uint32(r.Image.Pix[i+0])
				dg := uint32(r.Image.Pix[i+1])
				db := uint32(r.Image.Pix[i+2])
				da := uint32(r.Image.Pix[i+3])
				a := (m - (r.ca * ma / m)) * 0x101
				r.Image.Pix[i+0] = uint8((dr*a + r.cr*ma) / m >> 8)
				r.Image.Pix[i+1] = uint8((dg*a + r.cg*ma) / m >> 8)
				r.Image.Pix[i+2] = uint8((db*a + r.cb*ma) / m >> 8)
				r.Image.Pix[i+3] = uint8((da*a + r.ca*ma) / m >> 8)
			}
		} else {
			for i := i0; i < i1; i += 4 {
				r.Image.Pix[i+0] = cr8
				r.Image.Pix[i+1] = cg8
				r.Image.Pix[i+2] = cb8
				r.Image.Pix[i+3] = 0xff
			}
		}
	}
}

// compares cmds to the last draw frame, returns true if there is a change
func (w *MasterWindow) drawChanged(cmds []command.Command) bool {
	if len(cmds) != len(w.prevCmds) {
		return true
	}

	for i := range cmds {
		switch cmd := cmds[i].(type) {
		case *command.Scissor:
			pcmd, ok := w.prevCmds[i].(*command.Scissor)
			if !ok {
				return true
			}
			if *pcmd != *cmd {
				return true
			}

		case *command.Line:
			pcmd, ok := w.prevCmds[i].(*command.Line)
			if !ok {
				return true
			}
			if *pcmd != *cmd {
				return true
			}

		case *command.RectFilled:
			if i == 0 {
				cmd.Color.A = 0xff
			}
			pcmd, ok := w.prevCmds[i].(*command.RectFilled)
			if !ok {
				return true
			}
			if *pcmd != *cmd {
				return true
			}

		case *command.TriangleFilled:
			pcmd, ok := w.prevCmds[i].(*command.TriangleFilled)
			if !ok {
				return true
			}
			if *pcmd != *cmd {
				return true
			}

		case *command.CircleFilled:
			pcmd, ok := w.prevCmds[i].(*command.CircleFilled)
			if !ok {
				return true
			}
			if *pcmd != *cmd {
				return true
			}

		case *command.Image:
			pcmd, ok := w.prevCmds[i].(*command.Image)
			if !ok {
				return true
			}
			if *pcmd != *cmd {
				return true
			}

		case *command.Text:
			pcmd, ok := w.prevCmds[i].(*command.Text)
			if !ok {
				return true
			}
			if *pcmd != *cmd {
				return true
			}

		default:
			panic(UnknownCommandErr)
		}
	}

	return false
}

func (mw *MasterWindow) ActivateEditor(ed *TextEditor) {
	mw.ctx.activateEditor = ed
}
