// +build linux,!android,!nucular_mobile darwin,!nucular_mobile windows,!nucular_mobile freebsd,!nucular_mobile

package nucular

import (
	"bytes"
	"fmt"
	"image"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/rect"

	"golang.org/x/exp/shiny/driver"
	"golang.org/x/exp/shiny/screen"
	"golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/lifecycle"
	"golang.org/x/mobile/event/mouse"
	"golang.org/x/mobile/event/paint"
	"golang.org/x/mobile/event/size"
)

//go:generate go-bindata -o internal/assets/assets.go -pkg assets DroidSansMono.ttf

var clipboardStarted bool = false
var clipboardMu sync.Mutex

type masterWindow struct {
	masterWindowCommon

	Title  string
	screen screen.Screen
	wnd    screen.Window
	wndb   screen.Buffer
	bounds image.Rectangle

	initialSize image.Point

	// window is focused
	Focus bool

	textbuffer bytes.Buffer

	closing     bool
	focusedOnce bool
}

// Creates new master window
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

// Shows window, runs event loop
func (mw *masterWindow) Main() {
	driver.Main(mw.main)
}

func (mw *masterWindow) Lock() {
	mw.uilock.Lock()
}

func (mw *masterWindow) Unlock() {
	mw.uilock.Unlock()
}

func (mw *masterWindow) main(s screen.Screen) {
	var err error
	mw.screen = s
	width, height := mw.ctx.scale(mw.initialSize.X), mw.ctx.scale(mw.initialSize.Y)
	mw.wnd, err = s.NewWindow(&screen.NewWindowOptions{width, height, mw.Title})
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
		// On darwin we must respond to a paint.Event by reuploading the buffer or
		// the appplication will freeze.
		// On windows when the window goes off screen part of the window contents
		// will be discarded and must be redrawn.
		w.prevCmds = w.prevCmds[:0]
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
			if !w.focusedOnce {
				// on linux uploads that happen before this event don't get displayed
				// for some reason, force a reupload
				w.focusedOnce = true
				w.prevCmds = w.prevCmds[:0]
			}
			w.Focus = true
			c = true
		case lifecycle.CrossOff:
			w.Focus = false
			c = true
		}
		if c {
			if changed := atomic.LoadInt32(&w.ctx.changed); changed < 2 {
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
		if changed := atomic.LoadInt32(&w.ctx.changed); changed < 2 {
			atomic.StoreInt32(&w.ctx.changed, 2)
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
		w.ctx.processKeyEvent(e, &w.textbuffer)
	}

	return true
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
				w.updateLocked()
			} else {
				down = false
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
		w.drawPerfCounter(w.wndb.RGBA(), w.bounds, t0, t1, te)
	}
	if dumpFrame && frameCnt < 1000 && nprimitives > 0 {
		w.dumpFrame(w.wndb.RGBA(), t0, t1, te, nprimitives)
	}
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
	mw.wnd.Send(lifecycle.Event{From: lifecycle.StageAlive, To: lifecycle.StageDead})
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
	if !w.drawChanged() {
		return 0
	}

	w.prevCmds = append(w.prevCmds[:0], w.ctx.cmds...)

	return w.ctx.Draw(w.wndb.RGBA())
}
