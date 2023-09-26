package richtext

import (
	"image"
	"time"
	"unicode"
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
			rtxt.Group.grab(rtxt, w)
			if !in.Mouse.Down(mouse.ButtonLeft) {
				if rtxt.isClick && in.Mouse.HoveringRect(r) {
					if styleSel.isLink {
						if styleSel.link != nil {
							styleSel.link()
						} else if linkClick != nil {
							*linkClick = line.coordToIndex(in.Mouse.Pos, chunkIdx, rtxt.adv)
						}
					} else {
						if !rtxt.hadSelection {
							rtxt.Events |= Clicked
						}
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
		if in.Mouse.Down(mouse.ButtonLeft) && in.Mouse.HoveringRect(r) && in.Mouse.HasClickInRect(mouse.ButtonLeft, r) {
			otherHadSelection := rtxt.Group.grab(rtxt, w)
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
			rtxt.hadSelection = rtxt.Sel.S != rtxt.Sel.E || otherHadSelection
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
		rtxt.Sel.S = rtxt.towd(rtxt.Sel.S, -1, false)
		rtxt.Sel.E = rtxt.towd(rtxt.Sel.E, +1, false)
	case 3:
		sline := rtxt.findLine(rtxt.Sel.S)
		eline := rtxt.findLine(rtxt.Sel.E)
		if len(sline.off) > 0 {
			rtxt.Sel.S = sline.off[0]
			rtxt.Sel.E = eline.endoff()
		}
	}
}

func (rtxt *RichText) towd(start int32, dir int, forceAdvance bool) int32 {
	first := true
	line := rtxt.findLine(start)
	var citer citer
	citer.Init(line, start)
	for {
		if dir < 0 {
			citer.PrevRune()
		} else {
			citer.NextRune()
		}
		if !citer.Valid() {
			break
		}
		c := citer.Rune()
		if !(unicode.IsLetter(c) || unicode.IsDigit(c) || (c == '_')) {
			if first {
				if dir > 0 {
					break
				}
				if !forceAdvance {
					citer.NextRune()
				}
			} else {
				if dir < 0 {
					citer.NextRune()
				}
			}
			break
		}
		first = false
	}
	if !citer.Valid() {
		if dir < 0 {
			return 0
		}
		if len(rtxt.lines) == 0 {
			return 0
		}
		return rtxt.lines[len(rtxt.lines)-1].endoff()
	}
	return citer.off
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
	if !citer.Valid() {
		return
	}
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
	if !citer.Valid() {
		return
	}
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

func (citer *citer) NextRune() {
	for {
		citer.Next()
		if !citer.Valid() {
			return
		}
		if citer.Char()&0b1100_0000 != 0b1000_0000 {
			return
		}
	}
}

func (citer *citer) PrevRune() {
	for {
		citer.Prev()
		if !citer.Valid() {
			return
		}
		if citer.Char()&0b1100_0000 != 0b1000_0000 {
			return
		}
	}
}

func (citer *citer) Rune() rune {
	chunk := citer.line.chunks[citer.i]
	if chunk.b != nil {
		c, _ := utf8.DecodeRune(chunk.b[citer.j:])
		return c
	}
	c, _ := utf8.DecodeRuneInString(chunk.s[citer.j:])
	return c
}

func (rtxt *RichText) handleKeyboard(in *nucular.Input, changed *bool) (arrowKey, pageKey int) {
	if !rtxt.focused {
		return
	}
	if rtxt.flags&Keyboard != 0 {
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
	}
	if rtxt.flags&Editable != 0 {
		if in.Keyboard.Text != "" {
			rtxt.replace(in.Keyboard.Text, changed)
		}
		for _, k := range in.Keyboard.Keys {
			switch k.Code {
			case key.CodeUpArrow:
				//TODO: implement
			case key.CodeDownArrow:
				//TODO: implement
			case key.CodeLeftArrow:
				if k.Modifiers == 0 {
					rtxt.Sel.E--
					rtxt.Sel.S = rtxt.Sel.E
				} else if k.Modifiers == key.ModControl {
					rtxt.Sel.E = rtxt.towd(rtxt.Sel.E, -1, true)
					rtxt.Sel.S = rtxt.Sel.E
				}
				rtxt.clampSel()
			case key.CodeRightArrow:
				if k.Modifiers == 0 {
					rtxt.Sel.E++
					rtxt.Sel.S = rtxt.Sel.E
				} else {
					rtxt.Sel.E = rtxt.towd(rtxt.Sel.E, +1, true)
					rtxt.Sel.S = rtxt.Sel.E
				}
				rtxt.clampSel()
			case key.CodePageDown:
				if k.Modifiers == 0 {
					return 0, +1
				}
			case key.CodePageUp:
				if k.Modifiers == 0 {
					return 0, -1
				}
			case key.CodeDeleteForward:
				if rtxt.Sel.S == rtxt.Sel.E {
					rtxt.Sel.E++
				}
				rtxt.clampSel()
				rtxt.replace("", changed)
				rtxt.Sel.S = rtxt.Sel.E
			case key.CodeDeleteBackspace:
				if rtxt.Sel.S == rtxt.Sel.E {
					if k.Modifiers == 0 {
						rtxt.Sel.S--
					} else if k.Modifiers == key.ModControl {
						rtxt.Sel.S = rtxt.towd(rtxt.Sel.S, -1, true)
					}
				}
				rtxt.clampSel()
				rtxt.replace("", changed)
				rtxt.Sel.S = rtxt.Sel.E
			case key.CodeHome:
				rtxt.Sel.S = 0
				rtxt.Sel.E = 0
			case key.CodeA:
				if k.Modifiers == key.ModControl {
					rtxt.Sel.S = 0
					rtxt.Sel.E = 0
				}
			case key.CodeEnd:
				rtxt.Sel.S = int32(^uint32(0) >> 1)
				rtxt.Sel.E = rtxt.Sel.S
				rtxt.clampSel()
			case key.CodeE:
				if k.Modifiers == key.ModControl {
					rtxt.Sel.S = int32(^uint32(0) >> 1)
					rtxt.Sel.E = rtxt.Sel.S
					rtxt.clampSel()
				}
			case key.CodeZ:
				if k.Modifiers == key.ModControl {
					rtxt.undo(changed)
				}
			case key.CodeReturnEnter:
				if k.Modifiers == 0 {
					rtxt.replace("\n", changed)
				}
			case key.CodeC:
				if k.Modifiers == key.ModControl && rtxt.flags&Clipboard != 0 {
					clipboard.Set(rtxt.Get(rtxt.Sel))
				}
			case key.CodeX:
				if k.Modifiers == key.ModControl && rtxt.flags&Clipboard != 0 {
					clipboard.Set(rtxt.Get(rtxt.Sel))
					rtxt.replace("", changed)
				}
			case key.CodeV:
				if k.Modifiers == key.ModControl && rtxt.flags&Clipboard != 0 {
					rtxt.replace(clipboard.Get(), changed)
				}
			}
		}
	}
	return 0, 0
}

func (rtxt *RichText) clampSel() {
	endoff := int32(0)
	if len(rtxt.lines) > 0 {
		endoff = rtxt.lines[len(rtxt.lines)-1].endoff()
	}

	clampOne := func(x *int32) {
		if *x > endoff {
			*x = endoff
		}
		if *x < 0 {
			*x = 0
		}
	}

	clampOne(&rtxt.Sel.S)
	clampOne(&rtxt.Sel.E)

	if rtxt.Sel.E < rtxt.Sel.S {
		rtxt.Sel.S, rtxt.Sel.E = rtxt.Sel.E, rtxt.Sel.S
	}
}

func (rtxt *RichText) replace(str string, changed *bool) {
	if rtxt.Replace == nil {
		return
	}
	if !rtxt.Replace(rtxt.Sel, str) {
		return
	}
	*changed = true
	rtxt.undoEdit = &undoEdit{Sel{rtxt.Sel.S, rtxt.Sel.S + int32(len(str))}, rtxt.Get(rtxt.Sel)}
	rtxt.Sel.S = rtxt.Sel.S + int32(len(str))
	rtxt.Sel.E = rtxt.Sel.S
	rtxt.clampSel()
}

func (rtxt *RichText) undo(changed *bool) {
	if rtxt.undoEdit != nil {
		rtxt.Sel = rtxt.undoEdit.sel
		rtxt.replace(rtxt.undoEdit.str, changed)
	}
}

type undoEdit struct {
	sel Sel
	str string
}

func (grp *SelectionGroup) grab(rtxt *RichText, w *nucular.Window) bool {
	if grp == nil {
		return false
	}
	if grp.cur != nil && grp.cur != rtxt {
		w.Master().Changed()
	}
	oldCur := grp.cur
	grp.cur = rtxt
	return oldCur != nil && oldCur.Sel.S != oldCur.Sel.E
}
