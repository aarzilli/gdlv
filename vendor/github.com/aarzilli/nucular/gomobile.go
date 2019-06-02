// +build android nucular_mobile

package nucular

import (
	"encoding/binary"
	"fmt"
	"image"
	"math/bits"
	"sync/atomic"
	"time"

	"github.com/aarzilli/nucular/rect"

	"golang.org/x/mobile/app"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/mouse"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
	"golang.org/x/mobile/event/touch"
	"golang.org/x/mobile/exp/f32"
	"golang.org/x/mobile/exp/gl/glutil"
	"golang.org/x/mobile/gl"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
	"image/draw"
)

type masterWindow struct {
	masterWindowCommon

	a     app.App
	sz    size.Event // size in pixels of screen
	glctx gl.Context // OpenGL ES context

	closing bool

	screenTextureProgram       gl.Program // gpu program to render a single image that fills the entire screen
	screenTriangleBuf          gl.Buffer  // vertices of two triangles filling the entire screen
	screenTextureProjectionBuf gl.Buffer  // maps screenTriangleBuf triangles to a single texture
	positionAttrib             gl.Attrib  // vertex shitter position attribute
	texCoordAttrib             gl.Attrib  // vertex shitter a_texCoord attribute (pass-through attribute with texture coordinates)
	sampleUniform              gl.Uniform // fragment shitter 'sample' uniform (the screen texture)

	screenTexture          gl.Texture // texture that will fill the screen
	screenTextureAllocated bool       // was screenTexture allocated?

	wndbuf *image.RGBA // image of the whole screen for software renderer, gets copied into screenTexture on draw
}

func NewMasterWindowSize(flags WindowFlags, title string, sz image.Point, updatefn UpdateFn) MasterWindow {
	ctx := &context{}
	wnd := &masterWindow{}

	wnd.masterWindowCommonInit(ctx, flags, updatefn, wnd)

	return wnd
}

// Shows window, runs event loop
func (mw *masterWindow) Main() {
	app.Main(mw.main)
}

func (mw *masterWindow) Close() {
	mw.a.Send(lifecycle.Event{From: lifecycle.StageAlive, To: lifecycle.StageDead})
}

func (mw *masterWindow) Closed() bool {
	mw.uilock.Lock()
	defer mw.uilock.Unlock()
	return mw.closing
}

func (mw *masterWindow) main(a app.App) {
	mw.a = a
	go mw.updater()
	for e := range a.Events() {
		mw.uilock.Lock()
		mw.handleEventLocked(e)
		mw.uilock.Unlock()
	}
}

func (mw *masterWindow) handleEventLocked(e interface{}) {
	switch e := mw.a.Filter(e).(type) {
	case lifecycle.Event:
		switch e.Crosses(lifecycle.StageVisible) {
		case lifecycle.CrossOn:
			mw.glctx, _ = e.DrawContext.(gl.Context)
			mw.onStart()
			if (mw.sz != size.Event{}) {
				mw.a.Send(mw.sz)
			}
			mw.a.Send(paint.Event{})
		case lifecycle.CrossOff:
			mw.closeLocked()
			mw.glctx = nil
		}
	case size.Event:
		mw.sz = e
		mw.onResize()
		mw.prevCmds = mw.prevCmds[:0]
		if changed := atomic.LoadInt32(&mw.ctx.changed); changed < 2 {
			atomic.StoreInt32(&mw.ctx.changed, 2)
		}
	case paint.Event:
		if mw.glctx == nil || e.External || mw.wndbuf == nil {
			// As we are actively painting as fast as
			// we can (usually 60 FPS), skip any paint
			// events sent by the system.
			return
		}

		mw.prevCmds = mw.prevCmds[:0]
		mw.updateLocked()
	case touch.Event:
		changed := atomic.LoadInt32(&mw.ctx.changed)
		if changed < 2 {
			atomic.StoreInt32(&mw.ctx.changed, 2)
		}

		mw.ctx.Input.Mouse.Pos.X = int(e.X)
		mw.ctx.Input.Mouse.Pos.Y = int(e.Y)
		mw.ctx.Input.Mouse.Delta = mw.ctx.Input.Mouse.Pos.Sub(mw.ctx.Input.Mouse.Prev)

		switch e.Type {
		case touch.TypeBegin, touch.TypeEnd:
			down := e.Type == touch.TypeBegin

			btn := &mw.ctx.Input.Mouse.Buttons[mouse.ButtonLeft]
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
			if w.closing {
				return
			}
			changed := atomic.LoadInt32(&w.ctx.changed)
			if changed > 0 {
				atomic.AddInt32(&w.ctx.changed, -1)
				w.a.Send(paint.Event{})
			} else if gomobileBackendKeepPainting {
				w.a.Send(paint.Event{})
			} else {
				down = false
				for _, btn := range w.ctx.Input.Mouse.Buttons {
					if btn.Down {
						down = true
					}
				}
				if down {
					w.a.Send(paint.Event{})
				}
			}
		}()
	}
}

func (w *masterWindow) onStart() {
	var err error
	w.screenTextureProgram, err = glutil.CreateProgram(w.glctx, screenTextureVertexShitter, screenTextureFragmentShitter)
	if err != nil {
		panic(err)
	}

	w.screenTriangleBuf = w.glctx.CreateBuffer()
	w.glctx.BindBuffer(gl.ARRAY_BUFFER, w.screenTriangleBuf)
	w.glctx.BufferData(gl.ARRAY_BUFFER, f32.Bytes(binary.LittleEndian,
		-1.0, 1.0, 0.0,
		1.0, 1.0, 0.0,
		-1.0, -1.0, 0.0,
		1.0, -1.0, 0.0,
	), gl.STATIC_DRAW)

	w.screenTextureProjectionBuf = w.glctx.CreateBuffer()
	w.glctx.BindBuffer(gl.ARRAY_BUFFER, w.screenTextureProjectionBuf)
	w.glctx.BufferData(gl.ARRAY_BUFFER, f32.Bytes(binary.LittleEndian,
		0.0, 0.0,
		1.0, 0.0,
		0.0, 1.0,
		1.0, 1.0,
	), gl.STATIC_DRAW)

	w.positionAttrib = w.glctx.GetAttribLocation(w.screenTextureProgram, "position")
	w.texCoordAttrib = w.glctx.GetAttribLocation(w.screenTextureProgram, "a_texCoord")
	w.sampleUniform = w.glctx.GetUniformLocation(w.screenTextureProgram, "sample")
}

func (w *masterWindow) closeLocked() {
	w.closing = true

	w.glctx.DeleteProgram(w.screenTextureProgram)
	w.glctx.DeleteBuffer(w.screenTriangleBuf)
	w.glctx.DeleteBuffer(w.screenTextureProjectionBuf)

	if w.screenTextureAllocated {
		w.glctx.DeleteTexture(w.screenTexture)
	}
}

func (w *masterWindow) onResize() {
	if w.glctx == nil {
		return
	}
	if w.screenTextureAllocated {
		w.glctx.DeleteTexture(w.screenTexture)
	}

	w.screenTextureAllocated = true

	w2 := roundToPower2(w.sz.WidthPx)
	h2 := roundToPower2(w.sz.HeightPx)

	w.wndbuf = image.NewRGBA(image.Rect(0, 0, w2, h2))

	fx := float32(w.sz.WidthPx) / float32(w2)
	fy := float32(w.sz.HeightPx) / float32(h2)

	w.glctx.BindBuffer(gl.ARRAY_BUFFER, w.screenTextureProjectionBuf)
	w.glctx.BufferData(gl.ARRAY_BUFFER, f32.Bytes(binary.LittleEndian,
		0.0, 0.0,
		fx, 0.0,
		0.0, fy,
		fx, fy), gl.STATIC_DRAW)

	w.screenTexture = w.glctx.CreateTexture()
	w.glctx.BindTexture(gl.TEXTURE_2D, w.screenTexture)
	w.glctx.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, w2, h2, gl.RGBA, gl.UNSIGNED_BYTE, w.wndbuf.Pix)
	w.glctx.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.CLAMP_TO_EDGE)
	w.glctx.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.CLAMP_TO_EDGE)
	w.glctx.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.LINEAR)
	w.glctx.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)
}

func roundToPower2(x int) int {
	return 1 << (bits.LeadingZeros(0) - bits.LeadingZeros(uint(x)))
}

func (w *masterWindow) updateLocked() {
	w.ctx.Windows[0].Bounds = rect.Rect{0, 0, w.sz.WidthPx, w.sz.HeightPx}
	in := &w.ctx.Input
	in.Mouse.clip = nk_null_rect
	//TODO: when we can pop up a soft keyboard fill in.Keyboard.Text here

	var t0, t1, te time.Time
	if perfUpdate || w.Perf {
		t0 = time.Now()
	}

	if dumpFrame && !perfUpdate {
		panic("dumpFrame")
	}

	w.ctx.Update()

	if perfUpdate || w.Perf {
		t1 = time.Now()
	}
	nprimitives := w.draw()
	if perfUpdate && nprimitives > 0 {
		te = time.Now()

		fps := 1.0 / te.Sub(t0).Seconds()

		fmt.Printf("Update %0.4f msec = %0.4f updatefn + %0.4f draw (%d primitives) [max fps %0.2f]\n", te.Sub(t0).Seconds()*1000, t1.Sub(t0).Seconds()*1000, te.Sub(t1).Seconds()*1000, nprimitives, fps)
	}
	if w.Perf && nprimitives > 0 {
		te = time.Now()
		w.drawPerfCounter(w.wndbuf, image.Rect(0, 0, w.sz.WidthPx, w.sz.HeightPx), t0, t1, te)
	}
	{
		s := fmt.Sprintf("Last size event %v", w.sz)
		d := font.Drawer{
			Dst:  w.wndbuf,
			Src:  image.White,
			Face: w.ctx.Style.Font}

		width := d.MeasureString(s).Ceil()

		bounds := image.Rect(0, 0, w.sz.WidthPx, w.sz.HeightPx)
		bounds.Min.X = bounds.Max.X - width
		bounds.Min.Y = bounds.Max.Y - (w.ctx.Style.Font.Metrics().Ascent + w.ctx.Style.Font.Metrics().Descent).Ceil()
		draw.Draw(w.wndbuf, bounds, image.Black, bounds.Min, draw.Src)
		d.Dot = fixed.P(bounds.Min.X, bounds.Min.Y+w.ctx.Style.Font.Metrics().Ascent.Ceil())
		d.DrawString(s)
	}
	if dumpFrame && frameCnt < 1000 && nprimitives > 0 {
		w.dumpFrame(w.wndbuf.SubImage(image.Rect(0, 0, w.sz.WidthPx, w.sz.HeightPx)).(*image.RGBA), t0, t1, te, nprimitives)
	}
	if nprimitives > 0 {
		w.glctx.UseProgram(w.screenTextureProgram)

		bounds := w.wndbuf.Bounds()

		w.glctx.BindTexture(gl.TEXTURE_2D, w.screenTexture)
		w.glctx.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, bounds.Dx(), bounds.Dy(), gl.RGBA, gl.UNSIGNED_BYTE, w.wndbuf.Pix)

		// set uniform 'sample'
		w.glctx.ActiveTexture(gl.TEXTURE0)
		w.glctx.BindTexture(gl.TEXTURE_2D, w.screenTexture)
		w.glctx.Uniform1i(w.sampleUniform, 0)

		// position input
		w.glctx.BindBuffer(gl.ARRAY_BUFFER, w.screenTriangleBuf)
		w.glctx.EnableVertexAttribArray(w.positionAttrib)
		w.glctx.VertexAttribPointer(w.positionAttrib, 3, gl.FLOAT, false, 0, 0)

		// a_texCoord input
		w.glctx.BindBuffer(gl.ARRAY_BUFFER, w.screenTextureProjectionBuf)
		w.glctx.EnableVertexAttribArray(w.texCoordAttrib)
		w.glctx.VertexAttribPointer(w.texCoordAttrib, 2, gl.FLOAT, false, 0, 0)

		w.glctx.DrawArrays(gl.TRIANGLE_STRIP, 0, 4)
		w.glctx.DisableVertexAttribArray(w.positionAttrib)
		w.glctx.DisableVertexAttribArray(w.texCoordAttrib)

		w.a.Publish()
	}
}

func (w *masterWindow) draw() int {
	contextAllCommands(w.ctx)
	w.ctx.Reset()

	if !w.drawChanged() {
		return 0
	}

	w.prevCmds = append(w.prevCmds[:0], w.ctx.cmds...)

	return w.ctx.Draw(w.wndbuf.SubImage(image.Rect(0, 0, w.sz.WidthPx, w.sz.HeightPx)).(*image.RGBA))
}

const screenTextureVertexShitter = `#version 100
attribute vec4 position;
attribute vec2 a_texCoord;
varying vec2 v_texCoord;
void main() {
	gl_Position = position;
	v_texCoord = a_texCoord;
}`

const screenTextureFragmentShitter = `#version 100
precision mediump float;
varying vec2 v_texCoord;
uniform sampler2D sample;
void main() {
	gl_FragColor = texture2D(sample, v_texCoord);
}`
