package richtext

import (
	"image"
	"image/color"
	"unicode/utf8"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/command"
	"github.com/aarzilli/nucular/font"
	"github.com/aarzilli/nucular/rect"
	nstyle "github.com/aarzilli/nucular/style"
	"golang.org/x/image/math/fixed"
	"golang.org/x/mobile/event/mouse"
)

const debugDrawBoundingBoxes = false

func (rtxt *RichText) drawWidget(w *nucular.Window) *Ctor {
	rtxt.first = false

	var flags nucular.WindowFlags = 0
	if rtxt.flags&AutoWrap != 0 {
		flags = nucular.WindowNoHScrollbar
	}

	wp := w
	if w := w.GroupBegin(rtxt.name, flags); w != nil {
		r := rtxt.drawRows(w, wp.LastWidgetBounds.H)
		w.GroupEnd()
		return r
	}

	return nil
}

func (rtxt *RichText) drawRows(w *nucular.Window, viewporth int) *Ctor {
	arrowKey, pageKey := 0, 0
	if rtxt.focused && rtxt.flags&Keyboard != 0 {
		arrowKey, pageKey = rtxt.handleKeyboard(w.Input())
	}
	if viewporth == 0 {
		viewporth = w.Bounds.H - w.At().Y
	}

	wasFocused := rtxt.focused
	if rtxt.flags&Keyboard != 0 && (w.Input().Mouse.Buttons[mouse.ButtonLeft].Down || w.Input().Mouse.Buttons[mouse.ButtonRight].Down) {
		rtxt.focused = false
	}

	rtxt.first = false
	// this small row is necessary so that LayoutAvailableWidth will give us
	// the correct available width for our shit.
	w.RowScaled(1).Dynamic(1)
	width := rtxt.Width
	if width <= 0 {
		bounds := w.WidgetBounds()
		width = bounds.W
	}
	if rtxt.changed {
		rtxt.calcAdvances(0)
	}
	if width != rtxt.width || rtxt.changed {
		rtxt.width = width
		if rtxt.changed || rtxt.flags&AutoWrap != 0 {
			rtxt.reflow()
		}
	}

	if rtxt.Sel.S > rtxt.Sel.E {
		rtxt.Sel.E = rtxt.Sel.S
	}

	rtxt.changed = false

	in := w.Input()
	rowSpacing := w.WindowStyle().Spacing.Y

	var siter styleIterator
	siter.Init(rtxt)

	const (
		selBefore = iota
		selInside
		selAfter
		selTick
	)

	insel := selBefore

	linkClick := int32(-1)
	scrollbary := w.Scrollbar.Y

	for i := range rtxt.lines {
		line := &rtxt.lines[i]
		lineidx := i
		if rtxt.flags&AutoWrap != 0 {
			w.RowScaled(line.h).Dynamic(1)
		} else {
			w.RowScaled(line.h).StaticScaled(line.width())
		}

		bounds, out := w.Custom(nstyle.WidgetStateActive)
		if rtxt.followCursor && (line.sel().contains(rtxt.Sel.S) || line.endoff() == rtxt.Sel.S) {
			rtxt.followCursor = false
			r := bounds
			r.Intersect(&w.Bounds)
			if out == nil || r.H < line.h {
				scrollbary = w.At().Y - w.Bounds.H/2
				if scrollbary < 0 {
					scrollbary = 0
				}
			}
		}
		if out == nil {
			continue
		}
		if debugDrawBoundingBoxes {
			out.FillRect(bounds, 0, color.RGBA{0, 0xff, 0, 0xff})
			r := bounds
			r.W = rtxt.width
			out.FillRect(r, 0, color.RGBA{0, 0, 0xff, 0xff})
		}
		line.p = image.Point{bounds.X, bounds.Y}
		p := line.p

		if len(line.off) > 0 {
			siter.AdvanceTo(line.off[0])
		}

		if siter.styleSel.paraColor != (color.RGBA{}) {
			r := bounds
			r.W = rtxt.maxWidth
			r.H += rowSpacing
			out.FillRect(r, 0, siter.styleSel.paraColor)
		}

		if line.leftMargin > 0 {
			// click before the first chunk of the line
			rtxt.handleClick(w, rect.Rect{X: p.X, Y: p.Y, W: line.leftMargin, H: line.h + rowSpacing}, in, siter.styleSel, line, 0, nil, nil)
		}

		p.X += line.leftMargin

		if rtxt.Sel.S == rtxt.Sel.E {
			insel = selTick
		}

		for i, chunk := range line.chunks {
			siter.AdvanceTo(line.off[i])

			hovering := false

			rtxt.handleClick(w, rect.Rect{X: p.X, Y: p.Y, H: line.h + rowSpacing, W: line.w[i]}, in, siter.styleSel, line, i, &hovering, &linkClick)

			if hovering {
				if siter.styleSel.isLink {
					siter.styleSel.Color = siter.styleSel.hoverColor
				}
				if siter.styleSel.Tooltip != nil {
					w.TooltipOpen(siter.styleSel.TooltipWidth, false, siter.styleSel.Tooltip)
				}
			}

			chunkrng := Sel{line.off[i], line.off[i] + chunk.len()}

			simpleDrawChunk := false

			switch insel {
			case selBefore:
				if chunkrng.contains(rtxt.Sel.S) {
					s := rtxt.Sel.S - line.off[i]
					chunk1 := chunk.sub(0, s)
					w1 := line.chunkWidthEx(i, 0, chunk1, rtxt.adv)
					drawChunk(w, out, &p, chunk1, siter.styleSel, w1, line.h, line.asc)

					if chunkrng.contains(rtxt.Sel.E) {
						e := rtxt.Sel.E - line.off[i]
						chunk2 := chunk.sub(s, e)
						w2 := line.chunkWidthEx(i, s, chunk2, rtxt.adv)
						rtxt.drawSelectedChunk(w, out, &p, chunk2, siter.styleSel, w2, line.h, line.asc)

						chunk3 := chunk.sub(e, chunk.len())
						w3 := line.chunkWidthEx(i, e, chunk3, rtxt.adv)
						drawChunk(w, out, &p, chunk3, siter.styleSel, w3, line.h, line.asc)
						insel = selAfter
					} else {
						chunk2 := chunk.sub(s, chunk.len())
						w2 := line.chunkWidthEx(i, s, chunk2, rtxt.adv)
						rtxt.drawSelectedChunk(w, out, &p, chunk2, siter.styleSel, w2, line.h, line.asc)
						insel = selInside
					}
				} else {
					simpleDrawChunk = true
				}

			case selInside:
				if chunkrng.contains(rtxt.Sel.E) {
					e := rtxt.Sel.E - line.off[i]
					chunk1 := chunk.sub(0, e)
					w1 := line.chunkWidthEx(i, 0, chunk1, rtxt.adv)
					rtxt.drawSelectedChunk(w, out, &p, chunk1, siter.styleSel, w1, line.h, line.asc)

					chunk2 := chunk.sub(e, chunk.len())
					w2 := line.chunkWidthEx(i, e, chunk2, rtxt.adv)
					drawChunk(w, out, &p, chunk2, siter.styleSel, w2, line.h, line.asc)
					insel = selAfter
				} else if chunkrng.S >= rtxt.Sel.E {
					insel = selAfter
					simpleDrawChunk = true
				} else {
					rtxt.drawSelectedChunk(w, out, &p, chunk, siter.styleSel, line.w[i], line.h, line.asc)
				}

			case selAfter:
				simpleDrawChunk = true

			case selTick:
				simpleDrawChunk = true
			}

			if simpleDrawChunk {
				drawChunk(w, out, &p, chunk, siter.styleSel, line.w[i], line.h, line.asc)
				if insel == selTick && (rtxt.flags&ShowTick != 0) && (wasFocused || (rtxt.flags&Keyboard == 0)) && chunkrng.contains(rtxt.Sel.S) {
					x := p.X - line.w[i] + line.chunkWidth(i, rtxt.Sel.S-line.off[i], rtxt.adv)
					rtxt.drawTick(w, out, image.Point{x, p.Y}, line.h, siter.styleSel.Color, lineidx)
				}
			}
		}

		if len(line.chunks) == 0 && line.sel().contains(rtxt.Sel.S) && insel != selTick {
			insel = selInside
		}

		// click after the last chunk of text on the line
		rtxt.handleClick(w, rect.Rect{X: p.X, Y: p.Y, W: rtxt.width + bounds.X - p.X, H: line.h + rowSpacing}, in, siter.styleSel, line, len(line.chunks)-1, nil, nil)

		if insel == selTick && (rtxt.flags&ShowTick != 0) && (wasFocused || (rtxt.flags&Keyboard == 0)) && (line.endoff() == rtxt.Sel.S) {
			rtxt.drawTick(w, out, p, line.h, siter.styleSel.Color, lineidx)
		}

		if insel == selInside && rtxt.Sel.contains(line.endoff()) {
			spacewidth := int(4 * w.Master().Style().Scaling)
			out.FillRect(rect.Rect{X: p.X, Y: p.Y, W: spacewidth, H: line.h}, 0, rtxt.selColor)
		}

		if insel == selInside && line.sel().contains(rtxt.Sel.E) {
			insel = selAfter
		}
	}

	if rtxt.down && !in.Mouse.Down(mouse.ButtonLeft) {
		rtxt.down = false
	}

	if rtxt.followCursor {
		rtxt.followCursor = false
		if above, below := w.Invisible(0); above || below {
			scrollbary = w.At().Y - w.Bounds.H/2
			if scrollbary < 0 {
				scrollbary = 0
			}
		}
	}

	if pageKey != 0 && viewporth > 0 {
		scrollbary += (pageKey * viewporth) - nucular.FontHeight(rtxt.face)
		if scrollbary < 0 {
			scrollbary = 0
		}
	} else if arrowKey != 0 {
		scrollbary += arrowKey * nucular.FontHeight(rtxt.face)
		if scrollbary < 0 {
			scrollbary = 0
		}
	}

	if scrollbary != w.Scrollbar.Y {
		w.Scrollbar.Y = scrollbary
		w.Master().Changed()
	} else if wasFocused != rtxt.focused {
		w.Master().Changed()
	}

	if linkClick >= 0 {
		rtxt.ctor = Ctor{rtxt: rtxt, mode: ctorLink, w: w, linkClick: linkClick}
		return &rtxt.ctor
	}
	return nil
}

func (rtxt *RichText) drawTick(w *nucular.Window, out *command.Buffer, p image.Point, lineh int, color color.RGBA, lineidx int) {
	linethick := int(w.Master().Style().Scaling)
	out.StrokeLine(image.Point{p.X, p.Y}, image.Point{p.X, p.Y + lineh}, linethick, color)
}

func (rtxt *RichText) drawSelectedChunk(w *nucular.Window, out *command.Buffer, p *image.Point, chunk chunk, styleSel styleSel, width, lineh, lineasc int) {
	styleSel.BgColor = rtxt.selColor
	styleSel.Color = styleSel.SelFgColor
	drawChunk(w, out, p, chunk, styleSel, width, lineh, lineasc)
}

func drawChunk(w *nucular.Window, out *command.Buffer, p *image.Point, chunk chunk, styleSel styleSel, width, lineh, lineasc int) {
	if chunk.isspacing() {
		if debugDrawBoundingBoxes && width > 0 {
			yoff := alignBaseline(lineh, lineasc, styleSel.Face)
			r := rect.Rect{X: p.X, Y: p.Y + yoff, H: lineh - yoff, W: width}
			out.FillRect(r, 0, color.RGBA{0xff, 0xff, 0x00, 0xff})
		}
		if styleSel.BgColor != (color.RGBA{}) && width > 0 {
			r := rect.Rect{X: p.X, Y: p.Y, H: lineh, W: width}
			out.FillRect(r, 0, styleSel.BgColor)
		}
	} else {
		r := rect.Rect{X: p.X, Y: p.Y, H: lineh, W: width}

		if styleSel.BgColor != (color.RGBA{}) {
			out.FillRect(r, 0, styleSel.BgColor)
		}

		yoff := alignBaseline(lineh, lineasc, styleSel.Face)
		r.Y += yoff
		r.H -= yoff

		if debugDrawBoundingBoxes {
			out.FillRect(r, 0, color.RGBA{0xff, 0, 0, 0xff})
		}

		if chunk.b != nil {
			//TODO: DrawTextBytes
			panic("not implemented")
		} else {
			out.DrawText(r, chunk.s, styleSel.Face, styleSel.Color)
		}

		if styleSel.Flags&Underline != 0 {
			linethick := int(w.Master().Style().Scaling)
			y := p.Y + lineh
			out.StrokeLine(image.Point{p.X, y}, image.Point{p.X + width, y}, linethick, styleSel.Color)
		}

		if styleSel.Flags&Strikethrough != 0 {
			linethick := int(w.Master().Style().Scaling)
			m := styleSel.Face.Metrics()
			y := p.Y + lineasc + m.Descent.Ceil() - m.Ascent.Ceil()/2
			out.StrokeLine(image.Point{p.X, y}, image.Point{p.X + width, y}, linethick, styleSel.Color)
		}
	}

	p.X += width
}

func alignBaseline(h int, asc int, face font.Face) int {
	d := asc - face.Metrics().Ascent.Ceil()
	if d < 0 {
		d = 0
	}
	return d
}

func (rtxt *RichText) reflow() {
	if rtxt.lines != nil {
		rtxt.lines = rtxt.lines[:0]
	}

	rtxt.maxWidth = rtxt.width

	var ln line

	var siter styleIterator
	siter.Init(rtxt)

	runeoff := 0
	var splitruneoff int
	if (rtxt.flags&AutoWrap != 0) && (siter.styleSel.align != AlignLeftDumb) {
		splitruneoff = rtxt.wordwrap(rtxt.chunks, 0, runeoff)
	}

	h := []int{}
	asc := []int{}

	var linew fixed.Int26_6

	maxint := func(v []int) int {
		m := 0
		for i := range v {
			if v[i] > m {
				m = v[i]
			}
		}
		return m
	}

	lastEmptyChunkOff := int32(0)

	flushLine := func(runedelta int) {
		lnwidth := ln.width()
		diff := rtxt.width - lnwidth
		switch siter.styleSel.align {
		case AlignRight:
			if diff > 0 {
				ln.leftMargin = diff
			}
		case AlignCenter:
			if diff > 2 {
				ln.leftMargin = diff / 2
			}
		case AlignJustified:
			if runeoff+runedelta == splitruneoff && rtxt.flags&AutoWrap != 0 {
				justifyLine(ln, diff)
			}
		}
		if len(ln.chunks) == 0 {
			ln.h = nucular.FontHeight(siter.styleSel.Face)
			ln.off = append(ln.off, lastEmptyChunkOff)
		} else {
			ln.h = maxint(h)
			ln.asc = maxint(asc)
		}
		if rtxt.maxWidth < lnwidth {
			rtxt.maxWidth = lnwidth
		}
		rtxt.lines = append(rtxt.lines, ln)
		ln = line{}
		h = h[:0]
		asc = asc[:0]
		ln.runeoff = runeoff + runedelta
		linew = fixed.I(0)
	}

	off := int32(0)

	for i, chunk := range rtxt.chunks {
		// Note chunk is a copy of the element in the slice so we can modify it with impunity

		start := int32(0)
		j := int32(0)
		var chunkw fixed.Int26_6

		flushChunk := func(end int32, styleSel styleSel) {
			if start != end {
				ln.chunks = append(ln.chunks, rtxt.chunks[i].sub(start, end))
				ln.off = append(ln.off, off+start)
				ln.w = append(ln.w, chunkw.Ceil())
				h = append(h, nucular.FontHeight(styleSel.Face))
				asc = append(asc, styleSel.Face.Metrics().Ascent.Ceil())
				linew += chunkw
				chunkw = fixed.I(0)
				start = end
			} else {
				lastEmptyChunkOff = off + start
			}
		}

		for j < chunk.len() {
			var ch rune
			var rsz int
			if chunk.b != nil {
				ch, rsz = utf8.DecodeRune(chunk.b[j:])
				j += int32(rsz)
			} else {
				ch, rsz = utf8.DecodeRuneInString(chunk.s[j:])
				j += int32(rsz)
			}
			a := rtxt.adv[runeoff]
			runeoff++

			doWordWrap := false

			switch ch {
			case '\t':
				flushChunk(j-1, siter.styleSel)
				chunkw = a
				flushChunk(j, siter.styleSel)
			case '\n':
				flushChunk(j-1, siter.styleSel)
				start++ // skip newline
				flushLine(0)
				doWordWrap = true
			default:
				chunkw += a
			}

			if rtxt.Flags&AutoWrap != 0 {
				if siter.styleSel.align == AlignLeftDumb && (linew+chunkw).Ceil() > rtxt.width && (j-int32(rsz)-start) > 0 {
					chunkw -= a
					flushChunk(j-int32(rsz), siter.styleSel)
					flushLine(-1)
					doWordWrap = true
					chunkw += a
				} else if runeoff == splitruneoff {
					if ch == ' ' && siter.styleSel.align == AlignJustified {
						chunkw -= a
						flushChunk(j-1, siter.styleSel)
						chunkw += a
					}
					flushChunk(j, siter.styleSel)
					flushLine(0)
					doWordWrap = true
				} else if ch == ' ' && siter.styleSel.align == AlignJustified {
					chunkw -= a
					flushChunk(j-1, siter.styleSel)
					chunkw += a
					flushChunk(j, siter.styleSel)
				}
			}

			styleSel := siter.styleSel
			if siter.AdvanceRune(rsz) {
				flushChunk(j, styleSel)
			}

			if doWordWrap && (rtxt.flags&AutoWrap != 0) && siter.styleSel.align != AlignLeftDumb {
				splitruneoff = rtxt.wordwrap(rtxt.chunks[i:], j, runeoff)
			}
		}

		flushChunk(rtxt.chunks[i].len(), siter.styleSel)
		off += rtxt.chunks[i].len()
	}

	if len(ln.chunks) > 0 {
		flushLine(0)
	}
}

func (rtxt *RichText) wordwrap(chunks []chunk, start int32, startRuneoff int) int {
	runeoff := startRuneoff
	spaceruneoff := -1
	advance := fixed.I(0)
	for _, chunk := range chunks {
		if chunk.b != nil {
			chunk.b = chunk.b[start:]
		} else {
			chunk.s = chunk.s[start:]
		}
		start = 0

		for chunk.len() > 0 {
			var ch rune
			var rsz int
			if chunk.b != nil {
				ch, rsz = utf8.DecodeRune(chunk.b)
				chunk.b = chunk.b[rsz:]
			} else {
				ch, rsz = utf8.DecodeRuneInString(chunk.s)
				chunk.s = chunk.s[rsz:]
			}
			a := rtxt.adv[runeoff]
			runeoff++

			if ch == '\n' {
				return -1
			}
			if ch == ' ' {
				spaceruneoff = runeoff
			}

			advance += a

			if advance.Ceil() > rtxt.width {
				if spaceruneoff > 0 {
					return spaceruneoff
				} else {
					return runeoff - 1
				}
			}
		}
	}
	return -1
}

func justifyLine(line line, diff int) {
	if len(line.chunks) == 0 {
		return
	}

	nspaces := 0
	for i := range line.chunks {
		if line.chunks[i].b != nil {
			if len(line.chunks[i].b) == 1 {
				switch line.chunks[i].b[0] {
				case '\t':
					return
				case ' ':
					nspaces++
				}
			}
		} else {
			switch line.chunks[i].s {
			case "\t":
				return
			case " ":
				nspaces++
			}
		}
	}

	isspc := func(chunk chunk) bool {
		if chunk.b != nil {
			return (len(chunk.b) == 1) && (chunk.b[0] == ' ')
		} else {
			return chunk.s == " "
		}
	}

	for i := len(line.chunks) - 1; i > 0; i-- {
		if isspc(line.chunks[i]) {
			diff += line.w[i]
			line.w[i] = 0
			nspaces--
		} else {
			break
		}
	}

	if nspaces == 0 {
		return
	}

	deltaw := float64(diff) / float64(nspaces)
	udiff := 0
	deltaerr := deltaw - float64(int(deltaw))
	error := float64(0)

	for i := range line.chunks {
		if isspc(line.chunks[i]) && line.w[i] > 0 {
			line.w[i] += int(deltaw)
			udiff += int(deltaw)
			error += deltaerr
			if error > 1 {
				line.w[i]++
				udiff++
				error -= 1
			}
		}
	}

	if diff > udiff {
		for i := range line.chunks {
			if isspc(line.chunks[i]) && line.w[i] > 0 {
				line.w[i] += (diff - udiff)
				break
			}
		}
	}
}

type styleIterator struct {
	rtxt      *RichText
	styleSels []styleSel
	styleSel  styleSel
	cur       int32
}

func (siter *styleIterator) Init(rtxt *RichText) {
	siter.styleSels = rtxt.styleSels
	siter.rtxt = rtxt
	siter.setStyle()
}

func (siter *styleIterator) setStyle() {
	if len(siter.styleSels) == 0 || siter.styleSels[0].S > siter.cur {
		siter.defaultStyle()
	} else {
		siter.styleSel = siter.styleSels[0]
		siter.fixDefaults()
	}
}

func (siter *styleIterator) defaultStyle() {
	if len(siter.styleSels) > 0 {
		siter.styleSel.E = siter.styleSels[0].S
	} else {
		siter.styleSel.E = int32(^uint32(0) >> 1)
	}
	siter.styleSel.align = AlignLeftDumb
	siter.styleSel.Face = siter.rtxt.face
	siter.styleSel.Flags = 0
	siter.styleSel.link = nil
	siter.styleSel.Color = siter.rtxt.txtColor
	siter.styleSel.SelFgColor = siter.rtxt.selFgColor
	siter.styleSel.BgColor = color.RGBA{0, 0, 0, 0}
}

func (siter *styleIterator) fixDefaults() {
	zero := color.RGBA{}
	if siter.styleSel.Color == zero {
		siter.styleSel.Color = siter.rtxt.txtColor
	}
	if siter.styleSel.SelFgColor == zero {
		siter.styleSel.SelFgColor = siter.rtxt.selFgColor
	}
	if siter.styleSel.BgColor == zero {
		siter.styleSel.BgColor = color.RGBA{0, 0, 0, 0}
	}
	if siter.styleSel.Face == (font.Face{}) {
		siter.styleSel.Face = siter.rtxt.face
	}
}

func (siter *styleIterator) AdvanceRune(sz int) bool {
	siter.cur += int32(sz)
	return siter.AdvanceTo(siter.cur)
}

func (siter *styleIterator) AdvanceTo(pos int32) bool {
	siter.cur = pos
	if siter.cur < siter.styleSel.E {
		return false
	}
	for len(siter.styleSels) > 0 && siter.cur >= siter.styleSels[0].E {
		siter.styleSels = siter.styleSels[1:]
	}
	siter.setStyle()
	return true
}

func (line line) chunkWidth(chunkIdx int, byteIdx int32, adv []fixed.Int26_6) int {
	_, runeoff := line.chunkAdvance(chunkIdx)
	if len(line.chunks) == 0 {
		return 0
	}
	chunk := line.chunks[chunkIdx]

	w := fixed.I(0)
	off := int32(0)
	for chunk.len() > 0 {
		if off >= byteIdx {
			return w.Ceil()
		}

		w += adv[runeoff]

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

	return w.Ceil()
}

func (line line) chunkWidthEx(chunkIdx int, startByteIdx int32, tgtChunk chunk, adv []fixed.Int26_6) int {
	_, runeoff := line.chunkAdvance(chunkIdx)
	if len(line.chunks) == 0 {
		return 0
	}
	off := int32(0)

	{
		chunk := line.chunks[chunkIdx]

		for chunk.len() > 0 {
			if off >= startByteIdx {
				break
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
	}
	w := fixed.I(0)

	for tgtChunk.len() > 0 {
		w += adv[runeoff]

		var rsz int
		if tgtChunk.b != nil {
			_, rsz = utf8.DecodeRune(tgtChunk.b)
			tgtChunk.b = tgtChunk.b[rsz:]
		} else {
			_, rsz = utf8.DecodeRuneInString(tgtChunk.s)
			tgtChunk.s = tgtChunk.s[rsz:]
		}

		runeoff++
	}

	return w.Ceil()
}

func (line line) chunkAdvance(chunkIdx int) (int, int) {
	runeoff := line.runeoff
	w := 0
	for i := 0; i < chunkIdx; i++ {
		w += line.w[i]
		runeoff += line.chunks[i].runeCount()
	}
	return w, runeoff
}
