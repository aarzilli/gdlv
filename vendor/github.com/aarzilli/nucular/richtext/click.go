package richtext

import (
	"image"
	"time"
	"unicode/utf8"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"

	"golang.org/x/image/math/fixed"
	"golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/mouse"
)

func (rtxt *RichText) handleClick(w *nucular.Window, r rect.Rect, in *nucular.Input, styleSel styleSel, line *line, chunkIdx int, hovering *bool, linkClick *int32) {
	defer func() {
		if rtxt.Sel.S > rtxt.Sel.E {
			rtxt.Sel.E = rtxt.Sel.S
		}
	}()
	if rtxt.flags&Selectable == 0 && !styleSel.isLink && rtxt.flags&Clipboard == 0 && styleSel.Tooltip == nil && rtxt.flags&Keyboard == 0 {
		return
	}

	if rtxt.flags&Clipboard != 0 && r.W > 0 && r.H > 0 {
		fn := styleSel.ContextMenu
		if fn == nil {
			fn = func(w *nucular.Window) {
				w.Row(20).Dynamic(1)
				if w.MenuItem(label.TA("Copy", "LC")) {
					clipboard.Set(rtxt.Get(rtxt.Sel))
					w.Close()
				}
			}
		}
		if w := w.ContextualOpen(0, image.Point{}, r, fn); w != nil {
			rtxt.focused = true
		}
	}

	oldSel := rtxt.Sel

	if rtxt.down {
		rtxt.focused = true
		if in.Mouse.HoveringRect(r) {
			if !in.Mouse.Down(mouse.ButtonLeft) {
				if rtxt.isClick && styleSel.isLink && in.Mouse.HoveringRect(r) {
					if styleSel.link != nil {
						styleSel.link()
					} else if linkClick != nil {
						*linkClick = line.coordToIndex(in.Mouse.Pos, chunkIdx, rtxt.adv)
					}
				}
				rtxt.down = false
				rtxt.isClick = false
			} else {
				q := line.coordToIndex(in.Mouse.Pos, chunkIdx, rtxt.adv)
				if q < rtxt.dragStart {
					rtxt.Sel.S = q
					rtxt.Sel.E = rtxt.dragStart
				} else {
					rtxt.Sel.S = rtxt.dragStart
					rtxt.Sel.E = q
				}
				rtxt.expandSelection()
				if q != rtxt.dragStart {
					rtxt.isClick = false
				}
			}
		}
	} else {
		if in.Mouse.Down(mouse.ButtonLeft) && in.Mouse.HoveringRect(r) {
			rtxt.focused = true
			q := line.coordToIndex(in.Mouse.Pos, chunkIdx, rtxt.adv)
			if time.Since(rtxt.lastClickTime) < 200*time.Millisecond && q == rtxt.dragStart {
				rtxt.clickCount++
			} else {
				rtxt.clickCount = 1
			}
			if rtxt.clickCount > 3 {
				rtxt.clickCount = 3
			}
			rtxt.lastClickTime = time.Now()
			rtxt.dragStart = q
			rtxt.Sel.S = rtxt.dragStart
			rtxt.Sel.E = rtxt.Sel.S
			rtxt.expandSelection()
			rtxt.down = true
			rtxt.isClick = true
		}
		if (styleSel.isLink || styleSel.Tooltip != nil) && hovering != nil && in.Mouse.HoveringRect(r) {
			*hovering = true
		}
	}

	if rtxt.flags&Selectable == 0 {
		rtxt.Sel = oldSel
	}
	return
}

func (rtxt *RichText) expandSelection() {
	switch rtxt.clickCount {
	case 2:
		sline := rtxt.findLine(rtxt.Sel.S)
		eline := rtxt.findLine(rtxt.Sel.E)

		var citer citer
		for citer.Init(sline, rtxt.Sel.S); citer.Valid(); citer.Prev() {
			if citer.Char() == ' ' {
				citer.Next()
				break
			}

		}
		rtxt.Sel.S = citer.off

		for citer.Init(eline, rtxt.Sel.E); citer.Valid(); citer.Next() {
			if citer.Char() == ' ' {
				break
			}
		}
		rtxt.Sel.E = citer.off
	case 3:
		sline := rtxt.findLine(rtxt.Sel.S)
		eline := rtxt.findLine(rtxt.Sel.E)
		if len(sline.off) > 0 {
			rtxt.Sel.S = sline.off[0]
			rtxt.Sel.E = eline.endoff()
		}
	}
}

func (rtxt *RichText) findLine(q int32) line {
	for _, line := range rtxt.lines {
		if len(line.off) <= 0 {
			continue
		}
		if line.sel().contains(q) {
			return line
		}
	}
	return rtxt.lines[len(rtxt.lines)-1]
}

func (line line) coordToIndex(p image.Point, chunkIdx int, adv []fixed.Int26_6) int32 {
	advance, runeoff := line.chunkAdvance(chunkIdx)
	if len(line.chunks) == 0 {
		return line.off[0]
	}
	chunk := line.chunks[chunkIdx]

	x := advance + line.leftMargin + line.p.X

	w := fixed.I(0)

	off := line.off[chunkIdx]
	for chunk.len() > 0 {
		w += adv[runeoff]

		if x+w.Ceil() > p.X {
			return off
		}

		var rsz int
		if chunk.b != nil {
			_, rsz = utf8.DecodeRune(chunk.b)
			chunk.b = chunk.b[rsz:]
		} else {
			_, rsz = utf8.DecodeRuneInString(chunk.s)
			chunk.s = chunk.s[rsz:]
		}
		off += int32(rsz)
		runeoff++
	}

	return off
}

type citer struct {
	valid bool
	off   int32
	line  line
	i, j  int
}

func (citer *citer) Init(line line, off int32) {
	citer.valid = true
	citer.off = off
	citer.line = line
	found := false
	for i := range citer.line.chunks {
		if citer.line.off[i] <= off && off < citer.line.off[i]+citer.line.chunks[i].len() {
			citer.i = i
			citer.j = int(off - citer.line.off[i])
			found = true
			break
		}
	}
	if !found {
		citer.i = len(citer.line.chunks)
		citer.j = 0
		citer.valid = false
	}
	if len(citer.line.chunks) <= 0 {
		citer.valid = false
	}
}

func (citer *citer) Valid() bool {
	if !citer.valid {
		return false
	}
	if citer.i < 0 || citer.i >= len(citer.line.chunks) {
		return false
	}
	chunk := citer.line.chunks[citer.i]
	if citer.j < 0 {
		return false
	}
	if chunk.b != nil {
		return citer.j < len(chunk.b)
	}
	return citer.j < len(chunk.s)
}

func (citer *citer) Char() byte {
	chunk := citer.line.chunks[citer.i]
	if chunk.b != nil {
		return chunk.b[citer.j]
	}
	return chunk.s[citer.j]
}

func (citer *citer) Prev() {
	citer.j--
	citer.off--
	if citer.j < 0 {
		citer.i--
		if citer.i < 0 {
			citer.i = 0
			citer.off++
			citer.valid = false
		} else {
			chunk := citer.line.chunks[citer.i]
			if chunk.b != nil {
				citer.j = len(chunk.b) - 1
			} else {
				citer.j = len(chunk.s) - 1
			}
		}
	}
}

func (citer *citer) Next() {
	citer.j++
	citer.off++
	for citer.j >= int(citer.line.chunks[citer.i].len()) {
		citer.j = 0
		citer.i++
		if citer.i >= len(citer.line.chunks) {
			citer.valid = false
			return
		}
	}
}

func (rtxt *RichText) handleKeyboard(in *nucular.Input) (arrowKey, pageKey int) {
	if !rtxt.focused || rtxt.flags&Keyboard == 0 {
		return
	}

	for _, k := range in.Keyboard.Keys {
		switch {
		case k.Modifiers == key.ModControl && k.Code == key.CodeC:
			if rtxt.flags&Clipboard != 0 {
				clipboard.Set(rtxt.Get(rtxt.Sel))
			}
		case k.Code == key.CodeUpArrow:
			return -1, 0
		case k.Code == key.CodeDownArrow:
			return +1, 0
		case k.Code == key.CodePageDown:
			return 0, +1
		case k.Code == key.CodePageUp:
			return 0, -1
		}
	}

	return 0, 0
}
