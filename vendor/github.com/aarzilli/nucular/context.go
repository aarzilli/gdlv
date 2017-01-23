package nucular

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"time"

	"github.com/aarzilli/nucular/command"
	nstyle "github.com/aarzilli/nucular/style"

	"github.com/golang/freetype/raster"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"golang.org/x/mobile/event/mouse"
)

type context struct {
	mw             MasterWindow
	Input          Input
	Style          nstyle.Style
	Windows        []*Window
	changed        int32
	activateEditor *TextEditor
	cmds           []command.Command
	trashFrame     bool
}

func contextAllCommands(ctx *context) {
	ctx.cmds = ctx.cmds[:0]
	for _, w := range ctx.Windows {
		ctx.cmds = append(ctx.cmds, w.cmds.Commands...)
	}
	return
}

func (ctx *context) setupMasterWindow(layout *panel, updatefn UpdateFn) {
	ctx.Windows = append(ctx.Windows, createWindow(ctx, ""))
	ctx.Windows[0].idx = 0
	ctx.Windows[0].layout = layout
	ctx.Windows[0].flags = layout.Flags
	ctx.Windows[0].cmds.UseClipping = true
	ctx.Windows[0].updateFn = updatefn
}

func (ctx *context) Update() {
	for count := 0; count < 2; count++ {
		contextBegin(ctx, ctx.Windows[0].layout)
		for i := 0; i < len(ctx.Windows); i++ {
			ctx.Windows[i].began = false
		}
		for i := 0; i < len(ctx.Windows); i++ { // this must not use range or tooltips won't work
			win := ctx.Windows[i]
			if win.updateFn != nil {
				win.specialPanelBegin()
				win.updateFn(win)
			}

			if !win.began {
				win.close = true
				continue
			}

			if win.title == tooltipWindowTitle {
				win.close = true
			}

			if win.flags&windowPopup != 0 {
				panelEnd(ctx, win)
			}
		}
		contextEnd(ctx)
		if !ctx.trashFrame {
			break
		}
	}
}

func contextBegin(ctx *context, layout *panel) {
	for _, w := range ctx.Windows {
		w.usingSub = false
		w.curNode = w.rootNode
		w.close = false
		w.widgets.reset()
		w.cmds.Reset()
	}

	ctx.trashFrame = false
	ctx.Windows[0].layout = layout
	panelBegin(ctx, ctx.Windows[0], "")
	layout.Offset = &ctx.Windows[0].Scrollbar
}

func contextEnd(ctx *context) {
	panelEnd(ctx, ctx.Windows[0])
}

func (ctx *context) Reset() {
	for i := 0; i < len(ctx.Windows); i++ {
		if ctx.Windows[i].close {
			if i != len(ctx.Windows)-1 {
				copy(ctx.Windows[i:], ctx.Windows[i+1:])
				i--
			}
			ctx.Windows = ctx.Windows[:len(ctx.Windows)-1]
		}
	}
	for i := range ctx.Windows {
		ctx.Windows[i].idx = i
	}
	ctx.activateEditor = nil
	in := &ctx.Input
	in.Mouse.Buttons[mouse.ButtonLeft].Clicked = false
	in.Mouse.Buttons[mouse.ButtonMiddle].Clicked = false
	in.Mouse.Buttons[mouse.ButtonRight].Clicked = false
	in.Mouse.ScrollDelta = 0
	in.Mouse.Prev.X = in.Mouse.Pos.X
	in.Mouse.Prev.Y = in.Mouse.Pos.Y
	in.Mouse.Delta = image.Point{}
	in.Keyboard.Keys = in.Keyboard.Keys[0:0]
}

var cnt = 0
var ln, frect, brrect, frrect, ftri, circ, fcirc, txt int

func (ctx *context) Draw(wimg *image.RGBA) int {
	var txttim, tritim, brecttim, frecttim, frrecttim time.Duration
	var t0 time.Time

	img := wimg

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

	for i := range ctx.cmds {
		icmd := &ctx.cmds[i]
		switch icmd.Kind {
		case command.ScissorCmd:
			img = wimg.SubImage(icmd.Rectangle()).(*image.RGBA)
			painter = nil
			rasterizer = nil

		case command.LineCmd:
			cmd := icmd.Line
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

		case command.RectFilledCmd:
			cmd := icmd.RectFilled
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

			body := icmd.Rectangle()

			var lwing, rwing image.Rectangle

			// rounding is true if rounding has been requested AND we can draw it
			rounding := cmd.Rounding > 0 && int(cmd.Rounding*2) < icmd.W && int(cmd.Rounding*2) < icmd.H

			if rounding {
				body.Min.X += int(cmd.Rounding)
				body.Max.X -= int(cmd.Rounding)

				lwing = image.Rect(icmd.X, icmd.Y+int(cmd.Rounding), icmd.X+int(cmd.Rounding), icmd.Y+icmd.H-int(cmd.Rounding))
				rwing = image.Rect(icmd.X+icmd.W-int(cmd.Rounding), lwing.Min.Y, icmd.X+icmd.W, lwing.Max.Y)
			}

			bordopt := false

			if ok, border := borderOptimize(icmd, ctx.cmds, i+1); ok {
				// only draw parts of body if this command can be optimized to a border with the next command

				bordopt = true
				cmd2 := ctx.cmds[i+1]
				border += int(cmd2.RectFilled.Rounding)

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

				roundAngle(icmd.X+icmd.W-int(cmd.Rounding)-minx, icmd.Y+int(cmd.Rounding)-miny, cmd.Rounding, -math.Pi/2, rangle, cmd.Color)
				roundAngle(icmd.X+icmd.W-int(cmd.Rounding)-minx, icmd.Y+icmd.H-int(cmd.Rounding)-miny, cmd.Rounding, 0, rangle, cmd.Color)
				roundAngle(icmd.X+int(cmd.Rounding)-minx, icmd.Y+icmd.H-int(cmd.Rounding)-miny, cmd.Rounding, math.Pi/2, rangle, cmd.Color)
				roundAngle(icmd.X+int(cmd.Rounding)-minx, icmd.Y+int(cmd.Rounding)-miny, cmd.Rounding, math.Pi, rangle, cmd.Color)
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

		case command.TriangleFilledCmd:
			cmd := icmd.TriangleFilled
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

		case command.CircleFilledCmd:
			if rasterizer == nil {
				setupRasterizer()
			}
			rasterizer.Clear()
			startp := traceArc(rasterizer, float64(icmd.X-img.Bounds().Min.X)+float64(icmd.W/2), float64(icmd.Y-img.Bounds().Min.Y)+float64(icmd.H/2), float64(icmd.W/2), float64(icmd.H/2), 0, -math.Pi*2, true)
			rasterizer.Add1(startp) // closes path
			painter.SetColor(icmd.CircleFilled.Color)
			rasterizer.Rasterize(painter)
			fcirc++

		case command.ImageCmd:
			draw.Draw(img, icmd.Rectangle(), icmd.Image.Img, image.Point{}, draw.Src)

		case command.TextCmd:
			if perfUpdate {
				t0 = time.Now()
			}
			dstimg := wimg.SubImage(img.Bounds().Intersect(icmd.Rectangle())).(*image.RGBA)
			d := font.Drawer{
				Dst:  dstimg,
				Src:  image.NewUniform(icmd.Text.Foreground),
				Face: icmd.Text.Face,
				Dot:  fixed.P(icmd.X, icmd.Y+icmd.Text.Face.Metrics().Ascent.Ceil())}

			start := 0
			for i := range icmd.Text.String {
				if icmd.Text.String[i] == '\n' {
					d.DrawString(icmd.Text.String[start:i])
					d.Dot.X = fixed.I(icmd.X)
					d.Dot.Y += fixed.I(FontHeight(icmd.Text.Face))
					start = i + 1
				}
			}
			if start < len(icmd.Text.String) {
				d.DrawString(icmd.Text.String[start:])
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

	return len(ctx.cmds)
}

// Returns true if cmds[idx] is a shrunk version of CommandFillRect and its
// color is not semitransparent and the border isn't greater than 128
func borderOptimize(cmd *command.Command, cmds []command.Command, idx int) (ok bool, border int) {
	if idx >= len(cmds) {
		return false, 0
	}

	if cmds[idx].Kind != command.RectFilledCmd {
		return false, 0
	}

	cmd2 := cmds[idx]

	if cmd2.RectFilled.Color.A != 0xff {
		return false, 0
	}

	border = cmd2.X - cmd.X
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
