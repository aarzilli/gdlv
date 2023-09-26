//go:build (darwin && !nucular_shiny) || nucular_gio
// +build darwin,!nucular_shiny nucular_gio

package nucular

import (
	"bytes"
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

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/profile"
	"gioui.org/io/system"
	"gioui.org/op"
	gioclip "gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"

	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/command"
	"github.com/aarzilli/nucular/font"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"

	"golang.org/x/image/math/fixed"
	mkey "golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/mouse"
)

type masterWindow struct {
	masterWindowCommon

	Title       string
	initialSize image.Point
	size        image.Point
	onClose     func()

	w   *app.Window
	ops op.Ops

	textbuffer bytes.Buffer

	closed bool

	charAtlas map[charAtlasKey]map[rune]renderedGlyph
}

var clipboardStarted bool = false
var clipboardMu sync.Mutex

func NewMasterWindowSize(flags WindowFlags, title string, sz image.Point, updatefn UpdateFn) MasterWindow {
	ctx := &context{}
	wnd := &masterWindow{}

	wnd.masterWindowCommonInit(ctx, flags, updatefn, wnd)

	wnd.Title = title
	wnd.initialSize = sz

	clipboardMu.Lock()
	if !clipboardStarted {
		clipboardStarted = true
		clipboard.Start()
	}
	clipboardMu.Unlock()

	return wnd
}

func (mw *masterWindow) Main() {
	go func() {
		mw.w = app.NewWindow(app.Title(mw.Title), func(u unit.Metric, cfg *app.Config) {
			cfg.Size = image.Point{
				X: mw.ctx.scale(mw.initialSize.X),
				Y: mw.ctx.scale(mw.initialSize.Y),
			}
		})
		mw.main()
		if mw.onClose != nil {
			mw.onClose()
		} else {
			os.Exit(0)
		}
	}()
	go mw.updater()
	app.Main()
}

func (mw *masterWindow) Lock() {
	mw.uilock.Lock()
}

func (mw *masterWindow) Unlock() {
	mw.uilock.Unlock()
}

func (mw *masterWindow) Close() {
	mw.w.Perform(system.ActionClose)
}

func (mw *masterWindow) Closed() bool {
	mw.uilock.Lock()
	defer mw.uilock.Unlock()
	return mw.closed
}

func (mw *masterWindow) OnClose(onClose func()) {
	mw.onClose = onClose
}

func (mw *masterWindow) main() {
	perfString := ""
	for e := range mw.w.Events() {
		switch e := e.(type) {
		case system.DestroyEvent:
			mw.uilock.Lock()
			mw.closed = true
			mw.uilock.Unlock()
			if e.Err != nil {
				fmt.Fprintf(os.Stderr, "error: %v\n", e.Err)
			}
			return

		case system.FrameEvent:
			mw.size = e.Size

			for _, e := range e.Queue.Events(mw.ctx) {
				switch e := e.(type) {
				case pointer.Event:
					mw.uilock.Lock()
					mw.processPointerEvent(e)
					mw.uilock.Unlock()
				case key.EditEvent:
					changed := atomic.LoadInt32(&mw.ctx.changed)
					if changed < 2 {
						atomic.StoreInt32(&mw.ctx.changed, 2)
					}
					mw.uilock.Lock()
					io.WriteString(&mw.textbuffer, e.Text)
					mw.uilock.Unlock()

				case key.Event:
					changed := atomic.LoadInt32(&mw.ctx.changed)
					if changed < 2 {
						atomic.StoreInt32(&mw.ctx.changed, 2)
					}
					if e.State == key.Press {
						mw.uilock.Lock()
						switch e.Name {
						case key.NameEnter, key.NameReturn:
							io.WriteString(&mw.textbuffer, "\n")
						}
						mw.ctx.Input.Keyboard.Keys = append(mw.ctx.Input.Keyboard.Keys, gio2mobileKey(e))
						mw.uilock.Unlock()
					}
				case profile.Event:
					perfString = e.Timings
				}
			}

			mw.uilock.Lock()
			mw.prevCmds = mw.prevCmds[:0]
			mw.updateLocked(perfString)
			mw.uilock.Unlock()

			e.Frame(&mw.ops)

		}
	}
}

func (mw *masterWindow) processPointerEvent(e pointer.Event) {
	changed := atomic.LoadInt32(&mw.ctx.changed)
	if changed < 2 {
		atomic.StoreInt32(&mw.ctx.changed, 2)
	}
	switch e.Type {
	case pointer.Release, pointer.Cancel:
		for i := range mw.ctx.Input.Mouse.Buttons {
			btn := &mw.ctx.Input.Mouse.Buttons[i]
			if btn.Down {
				btn.Down = false
				btn.Clicked = true
			}
		}

	case pointer.Press:
		var button mouse.Button

		switch {
		case e.Buttons.Contain(pointer.ButtonPrimary):
			button = mouse.ButtonLeft
		case e.Buttons.Contain(pointer.ButtonSecondary):
			button = mouse.ButtonRight
		case e.Buttons.Contain(pointer.ButtonTertiary):
			button = mouse.ButtonMiddle
		}

		if button == mouse.ButtonRight && e.Modifiers.Contain(key.ModCtrl) {
			button = mouse.ButtonLeft
		}

		down := e.Type == pointer.Press
		btn := &mw.ctx.Input.Mouse.Buttons[button]
		if btn.Down == down {
			break
		}

		if down {
			btn.ClickedPos.X = int(e.Position.X)
			btn.ClickedPos.Y = int(e.Position.Y)
		}
		btn.Clicked = true
		btn.Down = down

	case pointer.Move, pointer.Drag, pointer.Scroll:
		mw.ctx.Input.Mouse.Pos.X = int(e.Position.X)
		mw.ctx.Input.Mouse.Pos.Y = int(e.Position.Y)
		mw.ctx.Input.Mouse.Delta = mw.ctx.Input.Mouse.Pos.Sub(mw.ctx.Input.Mouse.Prev)

		mw.ctx.Input.Mouse.ScrollDelta += -e.Scroll.Y / 10
	}
}

var runeToCode = map[string]mkey.Code{}

func init() {
	for i := byte('a'); i <= 'z'; i++ {
		c := mkey.Code((i - 'a') + 4)
		runeToCode[string([]byte{i})] = c
		runeToCode[string([]byte{i - 0x20})] = c
	}

	runeToCode["\t"] = mkey.CodeTab
	runeToCode[" "] = mkey.CodeSpacebar
	runeToCode["-"] = mkey.CodeHyphenMinus
	runeToCode["="] = mkey.CodeEqualSign
	runeToCode["["] = mkey.CodeLeftSquareBracket
	runeToCode["]"] = mkey.CodeRightSquareBracket
	runeToCode["\\"] = mkey.CodeBackslash
	runeToCode[";"] = mkey.CodeSemicolon
	runeToCode["\""] = mkey.CodeApostrophe
	runeToCode["`"] = mkey.CodeGraveAccent
	runeToCode[","] = mkey.CodeComma
	runeToCode["."] = mkey.CodeFullStop
	runeToCode["/"] = mkey.CodeSlash

	runeToCode[key.NameLeftArrow] = mkey.CodeLeftArrow
	runeToCode[key.NameRightArrow] = mkey.CodeRightArrow
	runeToCode[key.NameUpArrow] = mkey.CodeUpArrow
	runeToCode[key.NameDownArrow] = mkey.CodeDownArrow
	runeToCode[key.NameReturn] = mkey.CodeReturnEnter
	runeToCode[key.NameEnter] = mkey.CodeReturnEnter
	runeToCode[key.NameEscape] = mkey.CodeEscape
	runeToCode[key.NameHome] = mkey.CodeHome
	runeToCode[key.NameEnd] = mkey.CodeEnd
	runeToCode[key.NameDeleteBackward] = mkey.CodeDeleteBackspace
	runeToCode[key.NameDeleteForward] = mkey.CodeDeleteForward
	runeToCode[key.NamePageUp] = mkey.CodePageUp
	runeToCode[key.NamePageDown] = mkey.CodePageDown
	runeToCode[key.NameTab] = mkey.CodeTab

	runeToCode["F1"] = mkey.CodeF1
	runeToCode["F2"] = mkey.CodeF2
	runeToCode["F3"] = mkey.CodeF3
	runeToCode["F4"] = mkey.CodeF4
	runeToCode["F5"] = mkey.CodeF5
	runeToCode["F6"] = mkey.CodeF6
	runeToCode["F7"] = mkey.CodeF7
	runeToCode["F8"] = mkey.CodeF8
	runeToCode["F9"] = mkey.CodeF9
	runeToCode["F10"] = mkey.CodeF10
	runeToCode["F11"] = mkey.CodeF11
	runeToCode["F12"] = mkey.CodeF12
}

func gio2mobileKey(e key.Event) mkey.Event {
	var mod mkey.Modifiers

	if e.Modifiers.Contain(key.ModCommand) {
		mod |= mkey.ModMeta
	}
	if e.Modifiers.Contain(key.ModCtrl) {
		mod |= mkey.ModControl
	}
	if e.Modifiers.Contain(key.ModAlt) {
		mod |= mkey.ModAlt
	}
	if e.Modifiers.Contain(key.ModSuper) {
		mod |= mkey.ModMeta
	}
	if e.Modifiers.Contain(key.ModShift) {
		mod |= mkey.ModShift
	}

	var name rune

	for _, ch := range e.Name {
		name = ch
		break
	}

	return mkey.Event{
		Rune:      name,
		Code:      runeToCode[e.Name],
		Modifiers: mod,
		Direction: mkey.DirRelease,
	}
}

func (w *masterWindow) updater() {
	var down bool
	for {
		if down {
			time.Sleep(10 * time.Millisecond)
		} else {
			time.Sleep(20 * time.Millisecond)
		}
		func() {
			w.uilock.Lock()
			defer w.uilock.Unlock()
			if w.closed {
				return
			}
			changed := atomic.LoadInt32(&w.ctx.changed)
			if changed > 0 {
				atomic.AddInt32(&w.ctx.changed, -1)
				w.w.Invalidate()
			}
		}()
	}
}

func (mw *masterWindow) updateLocked(perfString string) {
	mw.ctx.Windows[0].Bounds = rect.Rect{X: 0, Y: 0, W: mw.size.X, H: mw.size.Y}
	in := &mw.ctx.Input
	in.Mouse.clip = nk_null_rect
	in.Keyboard.Text = mw.textbuffer.String()
	mw.textbuffer.Reset()

	var t0, t1, te time.Time
	if perfUpdate || mw.Perf {
		t0 = time.Now()
	}

	if dumpFrame && !perfUpdate {
		panic("dumpFrame")
	}

	mw.ctx.Update()

	if perfUpdate || mw.Perf {
		t1 = time.Now()
	}
	nprimitives := mw.draw(perfString)
	if perfUpdate && nprimitives > 0 {
		te = time.Now()

		fps := 1.0 / te.Sub(t0).Seconds()

		fmt.Printf("Update %0.4f msec = %0.4f updatefn + %0.4f draw (%d primitives) [max fps %0.2f]\n", te.Sub(t0).Seconds()*1000, t1.Sub(t0).Seconds()*1000, te.Sub(t1).Seconds()*1000, nprimitives, fps)
	}
	if mw.Perf && nprimitives > 0 {
		te = time.Now()
		fps := 1.0 / te.Sub(t0).Seconds()

		s0 := fmt.Sprintf("%0.4fms + %0.4fms (%0.2f) [ca]", t1.Sub(t0).Seconds()*1000, te.Sub(t1).Seconds()*1000, fps)

		font := mw.Style().Font

		w := FontWidth(font, s0)
		if w2 := FontWidth(font, perfString); w2 > w {
			w = w2
		}

		bounds := image.Point{X: w, Y: (mw.ctx.Style.Font.Metrics().Ascent + mw.ctx.Style.Font.Metrics().Descent).Ceil() * 2}

		pos := mw.size
		pos.Y -= bounds.Y
		pos.X -= bounds.X

		paintRect := image.Rect(pos.X, pos.Y, pos.X+bounds.X, pos.Y+bounds.Y)

		paint.FillShape(&mw.ops, color.NRGBA{0xff, 0xff, 0xff, 0xff}, gioclip.UniformRRect(paintRect, 0).Op(&mw.ops))

		drawText(&mw.ops, mw.charAtlas, font, color.RGBA{0x00, 0x00, 0x00, 0xff}, rect.FromRectangle(paintRect), s0+"\n"+perfString)
	}
}

func (w *masterWindow) draw(perfString string) int {
	if !w.drawChanged() {
		return 0
	}

	w.prevCmds = append(w.prevCmds[:0], w.ctx.cmds...)

	return w.ctx.Draw(&w.ops, w.size, w.Perf, perfString)
}

func (ctx *context) Draw(ops *op.Ops, size image.Point, perf bool, perfString string) int {
	ops.Reset()

	if perf {
		profile.Op{ctx}.Add(ops)
	}

	areaStack := gioclip.Rect(image.Rectangle{Max: size}).Push(ops)
	// Register for all pointer inputs on the current clip area.
	pointer.InputOp{ctx, false, pointer.Cancel | pointer.Press | pointer.Release | pointer.Move | pointer.Drag | pointer.Scroll, image.Rect(-4096, -4096, 4096, 4096)}.Add(ops)
	key.InputOp{ctx, key.HintAny, ""}.Add(ops)
	key.FocusOp{ctx}.Add(ops)
	areaStack.Pop()

	var scissorStack gioclip.Stack
	scissorless := true

	ctx.cmds = append(ctx.cmds, command.Command{Kind: command.TextCmd, Text: command.Text{String: perfString + "( )0123456789+.ms", Face: ctx.mw.Style().Font, Foreground: color.RGBA{0x00, 0x00, 0x00, 0xff}}})
	charAtlas := ctx.mw.(*masterWindow).charAtlas
	updateCharAtlas(ctx.cmds, &charAtlas)
	ctx.mw.(*masterWindow).charAtlas = charAtlas
	ctx.cmds = ctx.cmds[:len(ctx.cmds)-1]

	for i := range ctx.cmds {
		icmd := &ctx.cmds[i]
		switch icmd.Kind {
		case command.ScissorCmd:
			if !scissorless {
				scissorStack.Pop()
			}
			scissorStack = gioclip.Rect(icmd.Rect.Rectangle()).Push(ops)
			scissorless = false

		case command.LineCmd:
			cmd := icmd.Line

			paint.ColorOp{Color: toNRGBA(cmd.Color)}.Add(ops)

			h1 := int(cmd.LineThickness / 2)
			h2 := int(cmd.LineThickness) - h1

			if cmd.Begin.X == cmd.End.X {
				y0, y1 := cmd.Begin.Y, cmd.End.Y
				if y0 > y1 {
					y0, y1 = y1, y0
				}
				stack := gioclip.Rect{image.Point{cmd.Begin.X - h1, y0}, image.Point{cmd.Begin.X + h2, y1}}.Push(ops)
				paint.PaintOp{}.Add(ops)
				stack.Pop()
			} else if cmd.Begin.Y == cmd.End.Y {
				x0, x1 := cmd.Begin.X, cmd.End.X
				if x0 > x1 {
					x0, x1 = x1, x0
				}
				stack := gioclip.Rect{image.Point{x0, cmd.Begin.Y - h1}, image.Point{x1, cmd.Begin.Y + h2}}.Push(ops)
				paint.PaintOp{}.Add(ops)
				stack.Pop()
			} else {
				m := float32(cmd.Begin.Y-cmd.End.Y) / float32(cmd.Begin.X-cmd.End.X)
				invm := -1 / m

				xadv := float32(math.Sqrt(float64(cmd.LineThickness*cmd.LineThickness) / (4 * float64((invm*invm + 1)))))
				yadv := xadv * invm

				var p gioclip.Path
				p.Begin(ops)

				pa := f32.Point{float32(cmd.Begin.X) - xadv, float32(cmd.Begin.Y) - yadv}
				p.Move(pa)
				pb := f32.Point{2 * xadv, 2 * yadv}
				p.Line(pb)
				pc := f32.Point{float32(cmd.End.X - cmd.Begin.X), float32(cmd.End.Y - cmd.Begin.Y)}
				p.Line(pc)
				pd := f32.Point{-2 * xadv, -2 * yadv}
				p.Line(pd)
				p.Line(f32.Point{float32(cmd.Begin.X - cmd.End.X), float32(cmd.Begin.Y - cmd.End.Y)})
				p.Close()

				stack := gioclip.Outline{Path: p.End()}.Op().Push(ops)

				pb = pb.Add(pa)
				pc = pc.Add(pb)
				pd = pd.Add(pc)

				paint.PaintOp{}.Add(ops)
				stack.Pop()
			}

		case command.RectFilledCmd:
			cmd := icmd.RectFilled
			// rounding is true if rounding has been requested AND we can draw it
			rounding := cmd.Rounding > 0 && int(cmd.Rounding*2) < icmd.W && int(cmd.Rounding*2) < icmd.H

			paint.ColorOp{Color: toNRGBA(cmd.Color)}.Add(ops)

			if rounding {
				const c = 0.55228475 // 4*(sqrt(2)-1)/3

				x, y, w, h := float32(icmd.X), float32(icmd.Y), float32(icmd.W), float32(icmd.H)
				r := float32(cmd.Rounding)

				var b gioclip.Path
				b.Begin(ops)
				b.Move(f32.Point{X: x + w, Y: y + h - r})
				b.Cube(f32.Point{X: 0, Y: r * c}, f32.Point{X: -r + r*c, Y: r}, f32.Point{X: -r, Y: r}) // SE
				b.Line(f32.Point{X: r - w + r, Y: 0})
				b.Cube(f32.Point{X: -r * c, Y: 0}, f32.Point{X: -r, Y: -r + r*c}, f32.Point{X: -r, Y: -r}) // SW
				b.Line(f32.Point{X: 0, Y: r - h + r})
				b.Cube(f32.Point{X: 0, Y: -r * c}, f32.Point{X: r - r*c, Y: -r}, f32.Point{X: r, Y: -r}) // NW
				b.Line(f32.Point{X: w - r - r, Y: 0})
				b.Cube(f32.Point{X: r * c, Y: 0}, f32.Point{X: r, Y: r - r*c}, f32.Point{X: r, Y: r}) // NE
				b.Close()
				stack := gioclip.Outline{Path: b.End()}.Op().Push(ops)
				paint.PaintOp{}.Add(ops)
				stack.Pop()
			} else {
				stack := gioclip.Rect(icmd.Rect.Rectangle()).Push(ops)
				paint.PaintOp{}.Add(ops)
				stack.Pop()
			}

		case command.TriangleFilledCmd:
			cmd := icmd.TriangleFilled

			paint.ColorOp{toNRGBA(cmd.Color)}.Add(ops)

			var p gioclip.Path
			p.Begin(ops)
			p.Move(f32.Point{float32(cmd.A.X), float32(cmd.A.Y)})
			p.Line(f32.Point{float32(cmd.B.X - cmd.A.X), float32(cmd.B.Y - cmd.A.Y)})
			p.Line(f32.Point{float32(cmd.C.X - cmd.B.X), float32(cmd.C.Y - cmd.B.Y)})
			p.Line(f32.Point{float32(cmd.A.X - cmd.C.X), float32(cmd.A.Y - cmd.C.Y)})
			p.Close()
			stack := gioclip.Outline{Path: p.End()}.Op().Push(ops)

			paint.PaintOp{}.Add(ops)

			stack.Pop()

		case command.CircleFilledCmd:
			paint.ColorOp{toNRGBA(icmd.CircleFilled.Color)}.Add(ops)

			r := min2(float32(icmd.W), float32(icmd.H)) / 2

			const c = 0.55228475 // 4*(sqrt(2)-1)/3
			var b gioclip.Path
			b.Begin(ops)
			b.Move(f32.Point{X: float32(icmd.X) + r*2, Y: float32(icmd.Y) + r})
			b.Cube(f32.Point{X: 0, Y: r * c}, f32.Point{X: -r + r*c, Y: r}, f32.Point{X: -r, Y: r})    // SE
			b.Cube(f32.Point{X: -r * c, Y: 0}, f32.Point{X: -r, Y: -r + r*c}, f32.Point{X: -r, Y: -r}) // SW
			b.Cube(f32.Point{X: 0, Y: -r * c}, f32.Point{X: r - r*c, Y: -r}, f32.Point{X: r, Y: -r})   // NW
			b.Cube(f32.Point{X: r * c, Y: 0}, f32.Point{X: r, Y: r - r*c}, f32.Point{X: r, Y: r})      // NE
			stack := gioclip.Outline{Path: b.End()}.Op().Push(ops)

			paint.PaintOp{}.Add(ops)

			stack.Pop()

		case command.ImageCmd:
			//TODO: this should be retained between frames somehow...
			paint.NewImageOp(icmd.Image.Img).Add(ops)
			stack1 := op.Offset(image.Point{icmd.Rect.X, icmd.Rect.Y}).Push(ops)
			stack2 := gioclip.Rect{image.Point{0, 0}, image.Point{icmd.Rect.W, icmd.Rect.H}}.Push(ops)
			paint.PaintOp{}.Add(ops)
			stack2.Pop()
			stack1.Pop()

		case command.TextCmd:
			stack := gioclip.Rect(icmd.Rect.Rectangle()).Push(ops)
			drawText(ops, charAtlas, icmd.Text.Face, icmd.Text.Foreground, icmd.Rect, icmd.Text.String)
			stack.Pop()

		default:
			panic(UnknownCommandErr)
		}
	}

	return len(ctx.cmds)
}

func drawText(ops *op.Ops, charAtlas map[charAtlasKey]map[rune]renderedGlyph, fontFace font.Face, foreground color.RGBA, rect rect.Rect, str string) {
	m := charAtlas[charAtlasKey{fontFace, foreground}]
	if m == nil {
		// this should never happen
		m = make(map[rune]renderedGlyph)
	}
	face := fontFace.Face
	dot := image.Point{rect.X, rect.Y}
	prevC := rune(-1)
	for _, ch := range str {
		if ch == '\n' {
			prevC = rune(-1)
			dot.X = rect.X
			dot.Y += FontHeight(fontFace)
			continue
		}
		if prevC >= 0 {
			dot.X += face.Kern(prevC, ch).Floor()
		}
		rg := m[ch]
		if rg.img != nil && len(rg.img.Pix) > 0 {
			if rg.op == nil {
				imgop := paint.NewImageOp(rg.img)
				rg.op = &imgop
				m[ch] = rg
			}
			stack := op.Offset(image.Point{dot.X + rg.x, dot.Y + rg.y}).Push(ops)
			rg.op.Add(ops)
			paint.PaintOp{}.Add(ops)
			stack.Pop()
		}
		dot.X += rg.advance.Floor()
		prevC = ch
	}
}

func n2iRect(r rect.Rect) image.Rectangle {
	return image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)
}

func min4(a, b, c, d float32) float32 {
	return min2(min2(a, b), min2(c, d))
}

func min2(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func max4(a, b, c, d float32) float32 {
	return max2(max2(a, b), max2(c, d))
}

func max2(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

type textLine struct {
	Ascent, Descent         fixed.Int26_6
	Bounds                  fixed.Rectangle26_6
	Width                   fixed.Int26_6
	RunesOffset, RunesCount int
	Glyphs                  []text.Glyph
}

type linesIterator struct {
	txt          []text.Glyph
	line         []text.Glyph
	start, count int
}

func (it *linesIterator) Next() bool {
	it.start += it.count
	if len(it.txt) == 0 {
		return false
	}

	found := false
	for i := range it.txt {
		if it.txt[i].Y != it.txt[0].Y {
			it.line = it.txt[:i]
			it.txt = it.txt[i:]
			found = true
			break
		}
	}
	if !found {
		it.line = it.txt
		it.txt = it.txt[:0]
	}
	it.count = 0
	for i := range it.line {
		it.count += int(it.line[i].Runes)
	}
	return true
}

func (it *linesIterator) Line() textLine {
	if len(it.line) == 0 {
		return textLine{RunesOffset: it.start}
	}
	lastX := it.line[len(it.line)-1].Bounds.Max.X + it.line[len(it.line)-1].X
	return textLine{
		Ascent:      it.line[0].Ascent,
		Descent:     it.line[0].Descent,
		Width:       lastX - it.line[0].X,
		RunesOffset: it.start,
		RunesCount:  it.count,
		Glyphs:      it.line,
	}
}

func textPadding(txt []text.Glyph) (padding image.Rectangle) {
	if len(txt) == 0 {
		return
	}
	it := linesIterator{txt: txt}
	it.Next()
	first := it.Line()
	if d := first.Ascent + first.Bounds.Min.Y; d < 0 {
		padding.Min.Y = d.Ceil()
	}
	var last textLine
	for it.Next() {
		last = it.Line()
	}
	if d := last.Bounds.Max.Y - last.Descent; d > 0 {
		padding.Max.Y = d.Ceil()
	}
	if d := first.Bounds.Min.X; d < 0 {
		padding.Min.X = d.Ceil()
	}
	if d := first.Bounds.Max.X - first.Width; d > 0 {
		padding.Max.X = d.Ceil()
	}
	return
}

func clipLine(line []text.Glyph, clip image.Rectangle) []text.Glyph {
	if len(line) < 0 {
		return line
	}

	for len(line) > 0 {
		if (line[0].X + line[0].Advance).Ceil() >= clip.Min.X {
			break
		}
		line = line[1:]
	}

	for len(line) > 0 {
		if line[len(line)-1].X.Ceil() < clip.Max.X {
			break
		}
		line = line[:len(line)-1]
	}

	return line
}

func maxLinesWidth(txt []text.Glyph) int {
	it := linesIterator{txt: txt}
	w := 0
	for it.Next() {
		line := it.Line()
		if line.Width.Ceil() > w {
			w = line.Width.Ceil()
		}
	}
	return w
}

///////////////////////////////////////////////////////////////////////////////////
// TEXT WIDGETS
///////////////////////////////////////////////////////////////////////////////////

const (
	tabSizeInSpaces = 8
)

type textWidget struct {
	Padding    image.Point
	Background color.RGBA
	Text       color.RGBA
}

func widgetText(o *command.Buffer, b rect.Rect, str string, t *textWidget, a label.Align, f font.Face) {
	b.H = max(b.H, 2*t.Padding.Y)
	lblrect := rect.Rect{X: 0, W: 0, Y: b.Y + t.Padding.Y, H: b.H - 2*t.Padding.Y}

	/* align in x-axis */
	switch a[0] {
	case 'L':
		lblrect.X = b.X + t.Padding.X
		lblrect.W = max(0, b.W-2*t.Padding.X)
	case 'C':
		text_width := FontWidth(f, str)
		text_width += (2.0 * t.Padding.X)
		lblrect.W = max(1, 2*t.Padding.X+text_width)
		lblrect.X = (b.X + t.Padding.X + ((b.W-2*t.Padding.X)-lblrect.W)/2)
		lblrect.X = max(b.X+t.Padding.X, lblrect.X)
		lblrect.W = min(b.X+b.W, lblrect.X+lblrect.W)
		if lblrect.W >= lblrect.X {
			lblrect.W -= lblrect.X
		}
	case 'R':
		text_width := FontWidth(f, str)
		text_width += (2.0 * t.Padding.X)
		lblrect.X = max(b.X+t.Padding.X, (b.X+b.W)-(2*t.Padding.X+text_width))
		lblrect.W = text_width + 2*t.Padding.X
	default:
		panic("unsupported alignment")
	}

	/* align in y-axis */
	if len(a) >= 2 {
		switch a[1] {
		case 'C':
			lblrect.Y = b.Y + b.H/2.0 - FontHeight(f)/2.0
		case 'B':
			lblrect.Y = b.Y + b.H - FontHeight(f)
		}
	}
	if lblrect.H < FontHeight(f)*2 {
		lblrect.H = FontHeight(f) * 2
	}

	o.DrawText(lblrect, str, f, t.Text)
}

func toNRGBA(c color.RGBA) color.NRGBA {
	if c.A == 0xff {
		return color.NRGBA{c.R, c.G, c.B, c.A}
	}
	r, g, b, a := c.RGBA()
	r = (r * 0xffff) / a
	g = (g * 0xffff) / a
	b = (b * 0xffff) / a
	return color.NRGBA{uint8(r >> 8), uint8(g >> 8), uint8(b >> 8), uint8(a >> 8)}
}

type charAtlasKey struct {
	face  font.Face
	color color.RGBA
}

type renderedGlyph struct {
	img     *image.RGBA
	x, y    int
	advance fixed.Int26_6
	op      *paint.ImageOp
}

func updateCharAtlas(cmds []command.Command, allm *map[charAtlasKey]map[rune]renderedGlyph) {
	seen := make(map[charAtlasKey]bool)
	if *allm == nil {
		*allm = make(map[charAtlasKey]map[rune]renderedGlyph)
	}
	for i := range cmds {
		cmd := &cmds[i]
		if cmd.Kind != command.TextCmd {
			continue
		}

		k := charAtlasKey{cmd.Text.Face, cmd.Text.Foreground}
		m := (*allm)[k]
		if m == nil {
			m = make(map[rune]renderedGlyph)
			(*allm)[k] = m
		}
		seen[k] = true

		for _, ch := range cmd.Text.String {
			if _, ok := m[ch]; ok {
				continue
			}
			face := cmd.Text.Face.Face
			dr, mask, maskp, advance, ok := face.Glyph(fixed.P(0, cmd.Text.Face.Metrics().Ascent.Ceil()), ch)
			img := image.NewRGBA(dr)
			if ok {
				draw.DrawMask(img, dr, image.NewUniform(cmd.Text.Foreground), image.Point{}, mask, maskp, draw.Over)
			}
			m[ch] = renderedGlyph{img: img, advance: advance, x: dr.Min.X, y: dr.Min.Y}
			for i := 0; i < len(img.Pix); i += 4 {
				c := color.RGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}
				c2 := color.NRGBAModel.Convert(c).(color.NRGBA)
				a := uint32(float64(c2.A) * 1.5)
				if a > 0xff {
					a = 0xff
				}
				c2.A = uint8(a)
				c = color.RGBAModel.Convert(c2).(color.RGBA)
				img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
			}
		}
	}
	for k := range *allm {
		if !seen[k] {
			delete(*allm, k)
		}
	}
}
