package richtext

import (
	"image"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aarzilli/nucular"
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
					w.SetClipboard(rtxt.Get(rtxt.Sel))
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

func (rtxt *RichText) handleKeyboard(w *nucular.Window, in *nucular.Input, changed *bool) (arrowKey, pageKey int) {
	if !rtxt.focused {
		return
	}
	if rtxt.flags&Keyboard != 0 {
		for e := range in.Keyboard.Events() {
			switch {
			case e.HandleKey(key.CodeC, key.ModControl):
				if rtxt.flags&Clipboard != 0 {
					w.SetClipboard(rtxt.Get(rtxt.Sel))
				}
			case e.HandleKey(key.CodeUpArrow, 0):
				return -1, 0
			case e.HandleKey(key.CodeDownArrow, 0):
				return +1, 0
			case e.HandleKey(key.CodePageDown, 0):
				return 0, +1
			case e.HandleKey(key.CodePageUp, 0):
				return 0, -1
			}
		}
	}
	if rtxt.flags&Editable != 0 {
		edited := false
		for e := range in.Keyboard.Events() {
			switch {
			case e.HandleText():
				rtxt.replace(e.Text(), changed)
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
				edited = true
			case rtxt.flags&Clipboard != 0 && e.HandleClipboard():
				rtxt.replace(e.Text(), changed)
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeUpArrow, 0):
				//TODO: implement
			case e.HandleKey(key.CodeDownArrow, 0):
				//TODO: implement
			case e.HandleKey(key.CodeLeftArrow, 0):
				rtxt.Sel.E = rtxt.prevRuneOff(rtxt.Sel.E)
				rtxt.Sel.S = rtxt.Sel.E
				rtxt.clampSel()
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeLeftArrow, key.ModControl):
				rtxt.Sel.E = rtxt.towd(rtxt.Sel.E, -1, true)
				rtxt.Sel.S = rtxt.Sel.E
				rtxt.clampSel()
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeRightArrow, 0):
				rtxt.Sel.E = rtxt.nextRuneOff(rtxt.Sel.E)
				rtxt.Sel.S = rtxt.Sel.E
				rtxt.clampSel()
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeRightArrow, key.ModControl):
				rtxt.Sel.E = rtxt.towd(rtxt.Sel.E, +1, true)
				rtxt.Sel.S = rtxt.Sel.E
				rtxt.clampSel()
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodePageDown, 0):
				return 0, +1
			case e.HandleKey(key.CodePageUp, 0):
				return 0, -1
			case e.HandleKey(key.CodeDeleteForward, 0):
				if rtxt.Sel.S == rtxt.Sel.E {
					rtxt.Sel.E = rtxt.nextRuneOff(rtxt.Sel.E)
				}
				rtxt.clampSel()
				rtxt.replace("", changed)
				rtxt.Sel.S = rtxt.Sel.E
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeDeleteBackspace, 0):
				fallthrough
			case e.HandleKey(key.CodeDeleteBackspace, key.ModControl):
				k := e.Key()
				if rtxt.Sel.S == rtxt.Sel.E {
					if k.Modifiers == 0 {
						rtxt.Sel.S = rtxt.prevRuneOff(rtxt.Sel.S)
					} else if k.Modifiers == key.ModControl {
						rtxt.Sel.S = rtxt.towd(rtxt.Sel.S, -1, true)
					}
				}
				rtxt.clampSel()
				rtxt.replace("", changed)
				rtxt.Sel.S = rtxt.Sel.E
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeHome, 0):
				rtxt.Sel.S = 0
				rtxt.Sel.E = 0
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeA, key.ModControl):
				rtxt.Sel.S = 0
				rtxt.Sel.E = 0
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeEnd, 0):
				rtxt.Sel.S = int32(^uint32(0) >> 1)
				rtxt.Sel.E = rtxt.Sel.S
				rtxt.clampSel()
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeE, key.ModControl):
				rtxt.Sel.S = int32(^uint32(0) >> 1)
				rtxt.Sel.E = rtxt.Sel.S
				rtxt.clampSel()
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeTab, 0):
				rtxt.replace("\t", changed)
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeZ, key.ModControl):
				rtxt.undo(changed)
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case e.HandleKey(key.CodeReturnEnter, 0):
				rtxt.replace("\n", changed)
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case rtxt.flags&Clipboard != 0 && e.HandleKey(key.CodeC, key.ModControl):
				w.SetClipboard(rtxt.Get(rtxt.Sel))
			case rtxt.flags&Clipboard != 0 && e.HandleKey(key.CodeX, key.ModControl):
				w.SetClipboard(rtxt.Get(rtxt.Sel))
				rtxt.replace("", changed)
				rtxt.updateIMESnippet(rtxt.Sel)
				rtxt.ime.updateSel = true
			case rtxt.flags&Clipboard != 0 && e.HandleKey(key.CodeV, key.ModControl):
				w.GetClipboard()

			case e.HandleSnippet():
				rng := e.Range()
				rtxt.ime.updateSnippetWithRuneSel = &rng
				rtxt.ime.updateSnippet = false

			case e.HandleSelection():
				if !edited {
					rtxt.Sel = rtxt.runeSelToSel(e.Range())
					rtxt.clampSel()
				}
			}
		}
	}
	return 0, 0
}

// byteAt returns the byte at offset off of the text, ok is false if off is
// out of range.
func (rtxt *RichText) byteAt(off int32) (c byte, ok bool) {
	if off < 0 {
		return 0, false
	}
	base := int32(0)
	for _, chunk := range rtxt.chunks {
		n := chunk.len()
		if off < base+n {
			if chunk.b != nil {
				return chunk.b[off-base], true
			}
			return chunk.s[off-base], true
		}
		base += n
	}
	return 0, false
}

func isContinuationByte(c byte) bool {
	return c&0b1100_0000 == 0b1000_0000
}

// nextRuneOff returns the offset of the rune following the one starting at off.
func (rtxt *RichText) nextRuneOff(off int32) int32 {
	off++
	for i := 1; i < utf8.UTFMax; i++ {
		c, ok := rtxt.byteAt(off)
		if !ok || !isContinuationByte(c) {
			break
		}
		off++
	}
	return off
}

// prevRuneOff returns the offset of the rune preceding the one starting at off.
func (rtxt *RichText) prevRuneOff(off int32) int32 {
	off--
	for i := 1; i < utf8.UTFMax; i++ {
		c, ok := rtxt.byteAt(off)
		if !ok || !isContinuationByte(c) {
			break
		}
		off--
	}
	return off
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
	if !rtxt.Replace(rtxt.Sel, &str) {
		return
	}
	*changed = true
	rtxt.undoEdit = &undoEdit{Sel{rtxt.Sel.S, rtxt.Sel.S + int32(len(str))}, rtxt.Get(rtxt.Sel)}
	rtxt.Sel.S = rtxt.Sel.S + int32(len(str))
	rtxt.Sel.E = rtxt.Sel.S
}

func (rtxt *RichText) undo(changed *bool) {
	if rtxt.undoEdit != nil {
		rtxt.Sel = rtxt.undoEdit.sel
		rtxt.replace(rtxt.undoEdit.str, changed)
	}
}

// updateIMESnippet schedules sel to be sent to the IME as the current
// snippet. The snippet is sent by flushIMESnippet, during drawRows, because
// when an edit happens the new text isn't known until the caller has
// re-populated the widget.
func (rtxt *RichText) updateIMESnippet(sel Sel) {
	rtxt.ime.updateSnippet = true
	rtxt.ime.snippetSel = sel
}

// flushIMESnippet sends the pending snippet to the IME, if it changed.
func (rtxt *RichText) flushIMESnippet(w *nucular.Window) {
	if rtxt.ime.updateSnippetWithRuneSel != nil {
		if !rtxt.ime.updateSnippet {
			rtxt.ime.updateSnippet = true
			rtxt.ime.snippetSel = rtxt.runeSelToSel(*rtxt.ime.updateSnippetWithRuneSel)
		}
		rtxt.ime.updateSnippetWithRuneSel = nil
	}
	if !rtxt.ime.updateSnippet {
		return
	}
	rtxt.ime.updateSnippet = false
	if rtxt.flags&Editable == 0 || !rtxt.focused {
		return
	}
	sel := rtxt.ime.snippetSel
	if sel.S > sel.E {
		sel.S, sel.E = sel.E, sel.S
	}
	length := rtxt.length()
	if sel.S > length {
		sel.S = length
	}
	if sel.E > length {
		sel.E = length
	}
	start, end := rtxt.offToRune(sel.S), rtxt.offToRune(sel.E)
	text := rtxt.Get(sel)
	if start == rtxt.ime.start && end == rtxt.ime.end && text == rtxt.ime.text {
		return
	}
	rtxt.ime.start, rtxt.ime.end, rtxt.ime.text = start, end, text
	w.IMESnippet(start, end, text)
}

// sendIMESelection sends the current selection to the IME, if needed. Caret
// is the position of the cursor (top of the line).
func (rtxt *RichText) sendIMESelection(w *nucular.Window, caret image.Point, lineh int) {
	if !rtxt.ime.updateSel || rtxt.flags&Editable == 0 || !rtxt.focused {
		return
	}
	rtxt.ime.updateSel = false
	caret.Y += lineh
	w.IMESelection(rtxt.offToRune(rtxt.Sel.S), rtxt.offToRune(rtxt.Sel.E), caret, lineh, w.Bounds)
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
