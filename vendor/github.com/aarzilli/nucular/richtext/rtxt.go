package richtext

import (
	"fmt"
	"image"
	"image/color"
	"math/rand"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/font"
	"golang.org/x/image/math/fixed"
)

type RichText struct {
	name       string
	chunks     []chunk    // the full text of this widget, divided into chunks as they were added by the caller
	styleSels  []styleSel // styling for the text in non-overlapping selections of increasing S
	Sel        Sel        // selected text if this widget is selectable, cursor position will have S == E
	Flags      Flags
	flags      Flags      // flags for this widget
	SelFgColor color.RGBA // foreground color for selection, zero value specifies that it should be copied from the window style
	SelColor   color.RGBA // background color for selection, zero value specifies that it should be copied from the window style

	txtColor, selFgColor, selColor color.RGBA // default foreground color and background selected color

	face font.Face // default face

	first bool
	ctor  Ctor

	changed  bool            // must recalculate text advances
	width    int             // currently flowed width
	maxWidth int             // maximum line width
	adv      []fixed.Int26_6 // advance for each rune in chunks
	lines    []line          // line of text, possibly wrapped

	down          bool  // a click/selection is in progress
	isClick       bool  // the click/selection in progress could be a click (unless the mouse moves too much)
	dragStart     int32 // stat of drag
	lastClickTime time.Time
	clickCount    int // number of consecutive clicks

	followCursor bool // scroll to show cursor on screen

	focused bool
}

type Sel struct {
	S, E int32
}

func (sel Sel) contains(p int32) bool {
	return p >= sel.S && p < sel.E
}

type Flags uint16

const (
	AutoWrap Flags = 1 << iota
	Selectable
	Clipboard
	ShowTick
	Keyboard
)

type FaceFlags uint16

const (
	Underline FaceFlags = 1 << iota
	Strikethrough
)

type Align uint8

const (
	AlignLeftDumb Align = iota
	AlignLeft
	AlignRight
	AlignCenter
	AlignJustified
)

type chunk struct {
	b []byte
	s string
}

func (chunk chunk) len() int32 {
	if chunk.b != nil {
		return int32(len(chunk.b))
	}
	return int32(len(chunk.s))
}

func (c chunk) sub(start, end int32) chunk {
	if c.b != nil {
		return chunk{b: c.b[start:end]}
	}
	return chunk{s: c.s[start:end]}
}

func (chunk chunk) isspacing() bool {
	if chunk.b != nil {
		return len(chunk.b) == 0 || (len(chunk.b) == 1 && (chunk.b[0] == '\t' || chunk.b[0] == ' '))
	}
	return chunk.s == "\t" || chunk.s == " " || chunk.s == ""
}

func (chunk chunk) runeCount() int {
	if chunk.b != nil {
		return utf8.RuneCount(chunk.b)
	}
	return utf8.RuneCountInString(chunk.s)
}

func (chunk chunk) str() string {
	if chunk.b != nil {
		return string(chunk.b)
	}
	return chunk.s
}

type TextStyle struct {
	Face  font.Face
	Flags FaceFlags

	Color, SelFgColor, BgColor color.RGBA // foreground color, selected foreground color, background color

	Tooltip      func(*nucular.Window)
	TooltipWidth int

	ContextMenu func(*nucular.Window)
}

type styleSel struct {
	Sel
	TextStyle

	align     Align
	paraColor color.RGBA

	link   func()
	isLink bool

	hoverColor color.RGBA // hover color for links
}

type line struct {
	chunks  []chunk     // each chunk has a uniform style
	w       []int       // width of each chunk
	off     []int32     // character offset of the start of each chunk
	runeoff int         // offset in number of runes of the first character in the line
	h       int         // height of the line
	asc     int         // ascent of the biggest font of the line
	p       image.Point // insertion point for the line

	leftMargin int // displacement from the left of the first chunk, used to implement right and center alignment
}

func (ln line) width() int {
	w := 0
	for i := range ln.w {
		w += ln.w[i]
	}
	return w
}

func (ln line) endoff() int32 {
	if len(ln.chunks) == 0 {
		return ln.off[0]
	}
	return ln.off[len(ln.chunks)-1] + ln.chunks[len(ln.chunks)-1].len()
}

func (ln line) sel() Sel {
	if len(ln.chunks) == 0 {
		return Sel{ln.off[0], ln.off[0] + 1}
	}
	return Sel{ln.off[0], ln.endoff()}
}

type Ctor struct {
	rtxt *RichText
	mode ctorMode
	w    *nucular.Window
	off  int32 // offset for append mode
	len  int32

	chunks    []chunk
	styleSels []styleSel // not ordered, possibly overlapping
	alignSels []styleSel // not ordered, possibly overlapping
	linkSels  []styleSel

	savedStyleOk bool
	savedStyle   styleSel

	selsDone bool // all sels are closed

	linkClick int32 // position of click in link mode

	appendScroll bool
}

type ctorMode uint8

const (
	ctorWidget ctorMode = iota
	ctorRows
	ctorAppend
	ctorLink
)

func New(flags Flags) *RichText {
	n := rand.Int()
	return &RichText{
		name:  fmt.Sprintf("richtext%d", n),
		Flags: flags,
		flags: flags,
		first: true,
	}
}

// Widget adds this rich text widget as a widget to w. The returned
// constructor allows populating the rich text widget with text and
// responding to events.
// The constructor will be returned under this circumstances:
// 1. the first time this function or Rows is called
// 2. changed is true
// 3. a link was clicked
func (rtxt *RichText) Widget(w *nucular.Window, changed bool) *Ctor {
	rtxt.initialize(w)
	if rtxt.first || changed {
		rtxt.changed = true
		rtxt.ctor = Ctor{rtxt: rtxt, mode: ctorWidget, w: w}
		return &rtxt.ctor
	}
	return rtxt.drawWidget(w)
}

// Rows is like Widget but adds the contents as a series of rows instead of
// a single widget.
func (rtxt *RichText) Rows(w *nucular.Window, changed bool) *Ctor {
	rtxt.initialize(w)
	if rtxt.first || changed {
		rtxt.changed = true
		rtxt.ctor = Ctor{rtxt: rtxt, mode: ctorRows, w: w}
		return &rtxt.ctor
	}
	return rtxt.drawRows(w, 0)
}

// Append allows adding text at the end of the widget. Calling Link or
// LinkBytes on the returned constructor is illegal if callback is nil.
// Selections for SetStyleForSel and SetAlignForSel are interpreted to be
// relative to the newly added text.
func (rtxt *RichText) Append(scroll bool) *Ctor {
	if rtxt.first {
		panic("Append called on a RichText widget that isn't initialized")
	}
	off := int32(0)
	for _, chunk := range rtxt.chunks {
		off += chunk.len()
	}
	rtxt.ctor = Ctor{rtxt: rtxt, mode: ctorAppend, off: off, appendScroll: scroll}

	lastSel := rtxt.styleSels[len(rtxt.styleSels)-1]

	rtxt.ctor.alignSels = append(rtxt.ctor.alignSels, styleSel{Sel: Sel{S: int32(rtxt.ctor.off + rtxt.ctor.len)}, align: lastSel.align, paraColor: lastSel.paraColor})
	return &rtxt.ctor
}

// Look searches from the next occurence of text inside rtxt, starting at
// rtxt.Sel.S. Restarts the search from the start if nothing is found.
func (rtxt *RichText) Look(text string, wraparound bool) bool {
	insensitive := true

	for _, ch := range text {
		if unicode.IsUpper(ch) {
			insensitive = false
			break
		}
	}

	var titer textIter
	titer.Init(rtxt)
	if titer.Advance(rtxt.Sel.S) {
		for {
			if ok, sel := textIterMatch(titer, text, insensitive); ok {
				rtxt.Sel = sel
				return true
			}
			if !titer.Next() {
				break
			}
		}
	}

	if !wraparound {
		return false
	}

	titer.Init(rtxt)
	for {
		if ok, sel := textIterMatch(titer, text, insensitive); ok {
			rtxt.Sel = sel
			return true
		}
		if !titer.Next() {
			break
		}
	}

	rtxt.Sel.S = rtxt.Sel.E
	return false
}

func (rtxt *RichText) FollowCursor() {
	rtxt.followCursor = true
}

func (rtxt *RichText) Get(sel Sel) string {
	var titer textIter
	titer.Init(rtxt)
	if !titer.Advance(rtxt.Sel.S) {
		return ""
	}

	startChunkIdx := titer.chunkIdx
	startByteIdx := titer.j

	var endChunkIdx int
	var endByteIdx int32
	if titer.Advance(rtxt.Sel.E - rtxt.Sel.S) {
		endChunkIdx = titer.chunkIdx
		endByteIdx = titer.j
	} else {
		endChunkIdx = len(rtxt.chunks)
		endByteIdx = 0
	}

	var out strings.Builder
	out.WriteString(rtxt.chunks[startChunkIdx].sub(startByteIdx, rtxt.chunks[startChunkIdx].len()).str())

	for i := startChunkIdx + 1; i < endChunkIdx; i++ {
		out.WriteString(rtxt.chunks[i].str())
	}

	if endChunkIdx < len(rtxt.chunks) {
		out.WriteString(rtxt.chunks[endChunkIdx].sub(0, endByteIdx).str())
	}

	return out.String()
}

// Tail removes everything but the last n physical lines
func (rtxt *RichText) Tail(n int) {
	if len(rtxt.lines) <= n {
		return
	}

	off := rtxt.lines[len(rtxt.lines)-n].off[0]
	runeoff := rtxt.lines[len(rtxt.lines)-n].off[0]

	off2 := int32(0)
	for i, chunk := range rtxt.chunks {
		if off2+chunk.len() > off {
			rtxt.chunks = append(rtxt.chunks[:0], rtxt.chunks[i:]...)
			s := off - off2
			if s >= chunk.len() {
				rtxt.chunks = append(rtxt.chunks[:0], rtxt.chunks[1:]...)
			} else {
				if rtxt.chunks[0].b != nil {
					rtxt.chunks[0].b = rtxt.chunks[0].b[s:]
				} else {
					rtxt.chunks[0].s = rtxt.chunks[0].s[s:]
				}
			}
			break
		}
		off2 += chunk.len()
	}

	rtxt.adv = append(rtxt.adv[:0], rtxt.adv[runeoff:]...)
	rtxt.reflow()

}

func (rtxt *RichText) initialize(w *nucular.Window) {
	style := w.Master().Style()
	if (rtxt.SelColor != color.RGBA{}) {
		rtxt.selColor = rtxt.SelColor
	} else {
		rtxt.selColor = style.Edit.SelectedHover
	}
	if (rtxt.SelFgColor != color.RGBA{}) {
		rtxt.selFgColor = rtxt.SelFgColor
	} else {
		rtxt.selFgColor = style.Edit.SelectedTextHover
	}
	rtxt.txtColor = style.Edit.TextActive
	if rtxt.face != style.Font {
		rtxt.changed = true
	}
	rtxt.face = style.Font
	if rtxt.flags != rtxt.Flags {
		rtxt.flags = rtxt.Flags
		rtxt.changed = true
	}
}

func (ctor *Ctor) End() {
	if !ctor.selsDone {
		ctor.closeSels()
	}
	ctor.styleSels = removeEmptySels(ctor.styleSels)
	ctor.alignSels = removeEmptySels(ctor.alignSels)
	ctor.linkSels = removeEmptySels(ctor.linkSels)
	ctor.styleSels = separateStyles(ctor.styleSels)
	ctor.alignSels = separateStyles(ctor.alignSels)
	styleSels := mergeStyles(ctor.styleSels, ctor.alignSels, ctor.linkSels)
	styleSels = removeEmptySels(styleSels)
	switch ctor.mode {
	case ctorAppend:
		n := len(ctor.rtxt.chunks)
		ctor.rtxt.chunks = append(ctor.rtxt.chunks, ctor.chunks...)
		ctor.rtxt.styleSels = append(ctor.rtxt.styleSels, styleSels...)
		ctor.rtxt.calcAdvances(n)
		ctor.rtxt.reflow()
		if ctor.appendScroll {
			ctor.rtxt.Sel = Sel{ctor.off + ctor.len, ctor.off + ctor.len}
			ctor.rtxt.followCursor = true
		}
	case ctorRows:
		ctor.rtxt.styleSels = styleSels
		ctor.rtxt.chunks = ctor.chunks
		ctor.rtxt.drawRows(ctor.w, 0)
	case ctorWidget:
		ctor.rtxt.styleSels = styleSels
		ctor.rtxt.chunks = ctor.chunks
		ctor.rtxt.drawWidget(ctor.w)
	case ctorLink:
		// nothing to do
	}
}

// Align changes current text alignment. It is only legal to call Align
// before inserting text or after a newline character is inserted.
func (ctor *Ctor) Align(align Align) {
	ctor.ParagraphStyle(align, color.RGBA{})
}

func (ctor *Ctor) ParagraphStyle(align Align, color color.RGBA) {
	const badAlign = "Align call not at the start of a line"
	var lastChunk chunk
	if len(ctor.chunks) > 0 {
		lastChunk = ctor.chunks[len(ctor.chunks)-1]
	} else if ctor.mode == ctorAppend && len(ctor.rtxt.chunks) > 0 {
		lastChunk = ctor.rtxt.chunks[len(ctor.rtxt.chunks)-1]
	}
	if lastChunk.b != nil && lastChunk.b[len(lastChunk.b)-1] != '\n' {
		panic(badAlign)
	} else if len(lastChunk.s) > 0 && lastChunk.s[len(lastChunk.s)-1] != '\n' {
		panic(badAlign)
	}

	if ctor.mode == ctorLink {
		return
	}

	if len(ctor.alignSels) > 0 {
		ctor.alignSels[len(ctor.alignSels)-1].E = int32(ctor.off + ctor.len)
	}
	ctor.alignSels = append(ctor.alignSels, styleSel{Sel: Sel{S: int32(ctor.off + ctor.len)}, align: align, paraColor: color})
}

// SetStyle changes current text style. If color or selColor are the zero
// value the default value (copied from the window) will be used.
// If face is nil the default font face from the window style will be used.
func (ctor *Ctor) SetStyle(s TextStyle) {
	if ctor.mode == ctorLink {
		return
	}
	if len(ctor.styleSels) > 0 {
		ctor.styleSels[len(ctor.styleSels)-1].E = ctor.off + ctor.len
	}
	ctor.styleSels = append(ctor.styleSels, styleSel{Sel: Sel{S: int32(ctor.off + ctor.len)}, TextStyle: s})
}

// SaveStyle saves the current text style.
func (ctor *Ctor) SaveStyle() {
	if ctor.mode == ctorLink {
		return
	}
	if len(ctor.styleSels) <= 0 || ctor.styleSels[len(ctor.styleSels)-1].E != 0 {
		ctor.savedStyleOk = false
	} else {
		ctor.savedStyle = ctor.styleSels[len(ctor.styleSels)-1]
		ctor.savedStyleOk = true
	}
}

// ClearStyle resets the text style to the default.
func (ctor *Ctor) ClearStyle() {
	if len(ctor.styleSels) > 0 {
		ctor.styleSels[len(ctor.styleSels)-1].E = ctor.off + ctor.len
	}
}

// RestoreStyle restores the last saved text style.
func (ctor *Ctor) RestoreStyle() {
	if ctor.mode == ctorLink {
		return
	}
	if ctor.savedStyleOk {
		ctor.SetStyle(ctor.savedStyle.TextStyle)
	} else {
		ctor.ClearStyle()
	}
}

// SetAlignForSel changes the alignment for the specified region.
func (ctor *Ctor) SetAlignForSel(sel Sel, align Align) {
	ctor.SetParagraphStyleForSel(sel, align, color.RGBA{})
}

func (ctor *Ctor) SetParagraphStyleForSel(sel Sel, align Align, color color.RGBA) {
	if !ctor.selsDone {
		ctor.closeSels()
	}
	if ctor.mode == ctorLink {
		return
	}
	const badAlign = "Align call not at the start of a line"
	if sel.S > 0 {
		if ctor.findByte(sel.S-1) != '\n' {
			panic(badAlign)
		}
	}
	if sel.E > 0 && sel.E < ctor.len {
		if ctor.findByte(sel.E-1) != '\n' {
			panic(badAlign)
		}
	}
	ctor.alignSels = append(ctor.alignSels, styleSel{Sel: sel, align: align, paraColor: color})
}

func (ctor *Ctor) findByte(off int32) byte {
	cur := int32(0)
	for _, chunk := range ctor.chunks {
		if off < cur+chunk.len() {
			chunkoff := off - cur
			if chunk.b != nil {
				return chunk.b[chunkoff]
			}
			return chunk.s[chunkoff]
		}
		cur += chunk.len()
	}
	return 0
}

// SetStyleForSel changes the text style for the specified region.
func (ctor *Ctor) SetStyleForSel(sel Sel, s TextStyle) {
	if !ctor.selsDone {
		ctor.closeSels()
	}
	if ctor.mode == ctorLink {
		return
	}
	ctor.styleSels = append(ctor.styleSels, styleSel{Sel: sel, TextStyle: s})
}

// Text adds text to the widget.
func (ctor *Ctor) Text(text string) {
	ctor.textChunk(chunk{s: text})
}

// TextBytes adds text to the widget.
//TODO: reenable when implemetned
// func (ctor *Ctor) TextBytes(text []byte) {
// 	ctor.textChunk(chunk{b: text})
// }

func (ctor *Ctor) textChunk(chunk chunk) {
	if ctor.selsDone {
		panic("Text add after selections were finalized")
	}
	if chunk.len() <= 0 {
		return
	}
	ctor.chunks = append(ctor.chunks, chunk)
	ctor.len += chunk.len()
}

// Link adds a link to the widget. It will return true if the link was
// clicked and callback is nil.
func (ctor *Ctor) Link(text string, hoverColor color.RGBA, callback func()) bool {
	return ctor.linkChunk(chunk{s: text}, hoverColor, callback)
}

// LinkBytes adds a link to the widget. It will return true if the link was
// clicked and callback is nil.
//TODO: reenable when implemented
// func (ctor *Ctor) LinkBytes(text []byte, callback func(string)) bool {
// 	panic("not implemented")
// 	return ctor.linkChunk(chunk{b: text}, callback)
// }

func (ctor *Ctor) linkChunk(chunk chunk, hoverColor color.RGBA, callback func()) bool {
	if ctor.mode == ctorAppend && callback == nil {
		panic("Link added in append mode without callback")
	}
	if ctor.selsDone {
		panic("Link added after selections were finalized")
	}
	if chunk.len() <= 0 {
		return false
	}
	sel := Sel{S: ctor.len + ctor.off}
	ctor.textChunk(chunk)
	sel.E = ctor.len + ctor.off
	ctor.linkSels = append(ctor.linkSels, styleSel{Sel: sel, link: callback, isLink: true, hoverColor: hoverColor})
	return ctor.mode == ctorLink && sel.contains(ctor.linkClick)
}

func (ctor *Ctor) closeSels() {
	ctor.selsDone = true
	if len(ctor.alignSels) > 0 {
		ctor.alignSels[len(ctor.alignSels)-1].E = int32(ctor.off + ctor.len)
	}
	if len(ctor.styleSels) > 0 {
		ctor.styleSels[len(ctor.styleSels)-1].E = int32(ctor.off + ctor.len)
	}
}

// separateStyles takes a slice of possibly overlapping styles and makes it
// non-overlapping.
func separateStyles(ssels []styleSel) []styleSel {
	sort.SliceStable(ssels, func(i, j int) bool { return ssels[i].S < ssels[j].S })

	sstack := []styleSel{}
	cur := int32(0)
	r := make([]styleSel, 0, len(ssels))

	for len(ssels) > 0 || len(sstack) > 0 {
		switch {
		case len(sstack) == 0:
			sstack = append(sstack, ssels[0])
			cur = ssels[0].S
			ssels = ssels[1:]

		case len(ssels) == 0 || sstack[len(sstack)-1].E < ssels[0].S:
			s := sstack[len(sstack)-1]
			if cur < s.Sel.E {
				s.Sel.S = cur
				r = append(r, s)
				cur = s.Sel.E
			}
			sstack = sstack[:len(sstack)-1]

		default:
			s := sstack[len(sstack)-1]
			s.Sel.S = cur
			cur = ssels[0].S
			s.Sel.E = cur
			r = append(r, s)
			sstack = append(sstack, ssels[0])
			ssels = ssels[1:]
		}
	}

	return r
}

func mergeStyles(styleSels, alignSels, linkSels []styleSel) []styleSel {
	minPosAfter := func(p0 int32) int32 {
		min := int32((^uint32(0)) >> 1)

		if len(styleSels) > 0 && styleSels[0].S > p0 && styleSels[0].S < min {
			min = styleSels[0].S
		}
		if len(alignSels) > 0 && alignSels[0].S > p0 && alignSels[0].S < min {
			min = alignSels[0].S
		}
		if len(linkSels) > 0 && linkSels[0].S > p0 && linkSels[0].S < min {
			min = linkSels[0].S
		}
		if len(styleSels) > 0 && styleSels[0].E > p0 && styleSels[0].E < min {
			min = styleSels[0].E
		}
		if len(alignSels) > 0 && alignSels[0].E > p0 && alignSels[0].E < min {
			min = alignSels[0].E
		}
		if len(linkSels) > 0 && linkSels[0].E > p0 && linkSels[0].E < min {
			min = linkSels[0].E
		}
		return min
	}

	r := make([]styleSel, 0, len(styleSels))

	appendCurrentStyle := func(sel Sel) {
		s := styleSel{Sel: sel}
		ok := false

		if len(styleSels) > 0 && styleSels[0].contains(sel.S) {
			s = styleSels[0]
			s.Sel = sel
			ok = true
		}
		if len(alignSels) > 0 && alignSels[0].contains(sel.S) {
			s.align = alignSels[0].align
			s.paraColor = alignSels[0].paraColor
			ok = true
		}
		if len(linkSels) > 0 && linkSels[0].contains(sel.S) {
			s.link = linkSels[0].link
			s.isLink = linkSels[0].isLink
			s.hoverColor = linkSels[0].hoverColor
			ok = true
		}
		if ok {
			r = append(r, s)
		}
	}

	removeBefore := func(p0 int32) {
		if len(styleSels) > 0 && styleSels[0].E <= p0 {
			styleSels = styleSels[1:]
		}
		if len(alignSels) > 0 && alignSels[0].E <= p0 {
			alignSels = alignSels[1:]
		}
		if len(linkSels) > 0 && linkSels[0].E <= p0 {
			linkSels = linkSels[1:]
		}
	}

	cur := minPosAfter(-1)

	for {
		next := minPosAfter(cur)
		appendCurrentStyle(Sel{cur, next})
		cur = next
		removeBefore(cur)
		if len(styleSels) == 0 && len(alignSels) == 0 && len(linkSels) == 0 {
			break
		}
	}

	return r
}

func removeEmptySels(styleSels []styleSel) []styleSel {
	r := styleSels[:0]
	for _, styleSel := range styleSels {
		if styleSel.S < styleSel.E {
			r = append(r, styleSel)
		}
	}
	return r
}

type textIter struct {
	rtxt     *RichText
	chunkIdx int
	j        int32
	off      int32
	valid    bool
}

func (titer *textIter) Init(rtxt *RichText) {
	titer.rtxt = rtxt
	titer.chunkIdx = 0
	titer.j = 0
	titer.off = 0
	titer.valid = true
}

func (titer *textIter) Next() bool {
	if !titer.valid {
		return false
	}
	titer.j++
	titer.off++

	if titer.j >= titer.rtxt.chunks[titer.chunkIdx].len() {
		titer.j = 0
		titer.chunkIdx++
		if titer.chunkIdx >= len(titer.rtxt.chunks) {
			titer.valid = false
			return false
		}
	}

	return true
}

func (titer *textIter) Char() byte {
	if !titer.valid {
		return 0
	}
	chunk := titer.rtxt.chunks[titer.chunkIdx]
	if chunk.b != nil {
		return chunk.b[titer.j]
	}
	return chunk.s[titer.j]
}

func (titer *textIter) Advance(off int32) bool {
	titer.j += off
	titer.off += off

	for titer.j >= titer.rtxt.chunks[titer.chunkIdx].len() {
		titer.j -= titer.rtxt.chunks[titer.chunkIdx].len()
		titer.chunkIdx++
		if titer.chunkIdx >= len(titer.rtxt.chunks) {
			titer.valid = false
			return false
		}
	}

	return true
}

func (titer *textIter) Valid() bool {
	return titer.valid
}

func textIterMatch(titer textIter, needle string, insensitive bool) (bool, Sel) {
	start := titer.off
	for i := 0; i < len(needle); i++ {
		ch := titer.Char()
		if insensitive {
			ch = byte(unicode.ToLower(rune(ch)))
		}
		if !titer.Valid() || needle[i] != ch {
			return false, Sel{}
		}
		titer.Next()
	}
	return true, Sel{start, titer.off}
}

func (rtxt *RichText) length() int32 {
	r := int32(0)
	for _, chunk := range rtxt.chunks {
		r += chunk.len()
	}
	return r
}
