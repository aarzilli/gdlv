package nucular

import (
	"image"
	"image/color"

	"golang.org/x/mobile/event/key"
	"golang.org/x/mobile/event/mouse"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/command"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"
	nstyle "github.com/aarzilli/nucular/style"
)

///////////////////////////////////////////////////////////////////////////////////
// TEXT WIDGETS
///////////////////////////////////////////////////////////////////////////////////

type textWidget struct {
	Padding    image.Point
	Background color.RGBA
	Text       color.RGBA
}

func textClamp(f font.Face, text []rune, space int) []rune {
	text_width := 0
	for i, ch := range text {
		_, _, _, xwfixed, _ := f.Glyph(fixed.P(0, 0), ch)
		xw := xwfixed.Ceil()
		if text_width+xw >= space {
			return text[:i]
		}
		text_width += xw
	}
	return text
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

func widgetTextWrap(o *command.Buffer, b rect.Rect, str []rune, t *textWidget, f font.Face) {
	var done int = 0
	var line rect.Rect
	var text textWidget

	text.Padding = image.Point{0, 0}
	text.Background = t.Background
	text.Text = t.Text

	b.W = max(b.W, 2*t.Padding.X)
	b.H = max(b.H, 2*t.Padding.Y)
	b.H = b.H - 2*t.Padding.Y

	line.X = b.X + t.Padding.X
	line.Y = b.Y + t.Padding.Y
	line.W = b.W - 2*t.Padding.X
	line.H = 2*t.Padding.Y + FontHeight(f)

	fitting := textClamp(f, str, line.W)
	for done < len(str) {
		if len(fitting) == 0 || line.Y+line.H >= (b.Y+b.H) {
			break
		}
		widgetText(o, line, string(fitting), &text, "LC", f)
		done += len(fitting)
		line.Y += FontHeight(f) + 2*t.Padding.Y
		fitting = textClamp(f, str[done:], line.W)
	}
}

///////////////////////////////////////////////////////////////////////////////////
// TEXT EDITOR
///////////////////////////////////////////////////////////////////////////////////

type propertyStatus int

const (
	propertyDefault = propertyStatus(iota)
	propertyEdit
	propertyDrag
)

// TextEditor stores the state of a text editor.
// To add a text editor to a window create a TextEditor object with
// &TextEditor{}, store it somewhere then in the update function call
// the Edit method passing the window to it.
type TextEditor struct {
	win            *Window
	propertyStatus propertyStatus
	Cursor         int
	Buffer         []rune
	Filter         FilterFunc
	Flags          EditFlags
	CursorFollow   bool
	Redraw         bool

	Maxlen int

	Initialized            bool
	Active                 bool
	InsertMode             bool
	Scrollbar              image.Point
	SelectStart, SelectEnd int
	HasPreferredX          bool
	SingleLine             bool
	PreferredX             int
	Undo                   textUndoState
}

func (ed *TextEditor) init(win *Window) {
	if ed.Filter == nil {
		ed.Filter = FilterDefault
	}
	if !ed.Initialized {
		if ed.Flags&EditMultiline != 0 {
			ed.clearState(TextEditMultiLine)
		} else {
			ed.clearState(TextEditSingleLine)
		}

	}
	if ed.win == nil || ed.win != win {
		if ed.win == nil {
			if ed.Buffer == nil {
				ed.Buffer = []rune{}
			}
			ed.Filter = nil
			ed.Cursor = 0
		}
		ed.Redraw = true
		ed.win = win
	}
}

type EditFlags int

const (
	EditDefault  EditFlags = 0
	EditReadOnly EditFlags = 1 << iota
	EditAutoSelect
	EditSigEnter
	EditAllowTab
	EditNoCursor
	EditSelectable
	EditClipboard
	EditCtrlEnterNewline
	EditNoHorizontalScroll
	EditAlwaysInsertMode
	EditMultiline
	EditNeverInsertMode
	EditFocusFollowsMouse

	EditSimple = EditAlwaysInsertMode
	EditField  = EditAlwaysInsertMode | EditSelectable
	EditBox    = EditAlwaysInsertMode | EditSelectable | EditMultiline | EditAllowTab
)

type EditEvents int

const (
	EditActive EditEvents = 1 << iota
	EditInactive
	EditActivated
	EditDeactivated
	EditCommitted
)

type TextEditType int

const (
	TextEditSingleLine TextEditType = iota
	TextEditMultiLine
)

type textFind struct {
	X         int
	Y         int
	Height    int
	FirstChar int
	Length    int
	PrevFirst int
}

type textEditRow struct {
	X0             int
	X1             int
	BaselineYDelta int
	Ymin           int
	Ymax           int
	NumChars       int
}

type textUndoRecord struct {
	Where        int
	InsertLength int
	DeleteLength int
	Text         []rune
}

const _TEXTEDIT_UNDOSTATECOUNT = 99

type textUndoState struct {
	UndoRec   [_TEXTEDIT_UNDOSTATECOUNT]textUndoRecord
	UndoPoint int16
	RedoPoint int16
}

func strInsertText(str []rune, pos int, runes []rune) []rune {
	if cap(str) < len(str)+len(runes) {
		newcap := (cap(str) + 1) * 2
		if newcap < len(str)+len(runes) {
			newcap = len(str) + len(runes)
		}
		newstr := make([]rune, len(str), newcap)
		copy(newstr, str)
		str = newstr
	}
	str = str[:len(str)+len(runes)]
	copy(str[pos+len(runes):], str[pos:])
	copy(str[pos:], runes)
	return str
}

func strDeleteText(s []rune, pos int, dlen int) []rune {
	copy(s[pos:], s[pos+dlen:])
	s = s[:len(s)-dlen]
	return s
}

func textHasSelection(s *TextEditor) bool {
	return s.SelectStart != s.SelectEnd
}

func (edit *TextEditor) getWidth(line_start int, char_id int, font font.Face) int {
	return FontWidth(font, string(edit.Buffer[line_start+char_id:line_start+char_id+1]))
}

func textCalculateTextBounds(font font.Face, text []rune, rowHeight int, stopOnNewline bool) (textSize image.Point, n int) {
	lineStart := 0

	flushLine := func(lineEnd int) {
		if lineStart >= len(text) {
			return
		}
		w := FontWidth(font, string(text[lineStart:lineEnd]))
		if w > textSize.X {
			textSize.X = w
		}
		textSize.Y += rowHeight
	}

	done := false
	for i := range text {
		if text[i] == '\n' {
			flushLine(i)
			if stopOnNewline {
				n++
				done = true
				break
			}
			lineStart = i + 1
		}
		n++
	}

	if !done {
		flushLine(len(text))
	}

	return
}

func texteditLayoutRow(r *textEditRow, edit *TextEditor, line_start_id int, row_height int, font font.Face) {
	size, glyphs := textCalculateTextBounds(font, edit.Buffer[line_start_id:], row_height, true)

	r.X0 = 0.0
	r.X1 = size.X
	r.BaselineYDelta = size.Y
	r.Ymin = 0.0
	r.Ymax = size.Y
	r.NumChars = glyphs
}

func (edit *TextEditor) locateCoord(p image.Point, font font.Face, row_height int) int {
	var r textEditRow
	var base_y int = 0
	var prev_x int
	var i int = 0
	var k int

	x, y := p.X, p.Y

	r.X1 = 0
	r.X0 = r.X1
	r.Ymax = 0
	r.Ymin = r.Ymax
	r.NumChars = 0

	/* search rows to find one that straddles 'y' */
	for i < len(edit.Buffer) {
		texteditLayoutRow(&r, edit, i, row_height, font)
		if r.NumChars <= 0 {
			return len(edit.Buffer)
		}

		if y < base_y+r.Ymax {
			break
		}

		i += r.NumChars
		base_y += r.BaselineYDelta
	}

	/* below all text, return 'after' last character */
	if i >= len(edit.Buffer) {
		return len(edit.Buffer)
	}

	/* check if it's before the beginning of the line */
	if x < r.X0 {
		return i
	}

	/* check if it's before the end of the line */
	if x < r.X1 {
		/* search characters in row for one that straddles 'x' */
		k = i

		prev_x = r.X0
		for i = 0; i < r.NumChars; i++ {
			w := edit.getWidth(k, i, font)
			if x < prev_x+w {
				if x < prev_x+w {
					return k + i
				} else {
					return k + i + 1
				}
			}

			prev_x += w
		}
	}

	/* shouldn't happen, but if it does, fall through to end-of-line case */

	/* if the last character is a newline, return that.
	 * otherwise return 'after' the last character */
	if edit.Buffer[i+r.NumChars-1] == '\n' {
		return i + r.NumChars - 1
	} else {
		return i + r.NumChars
	}
}

func (state *TextEditor) click(coord image.Point, font font.Face, row_height int) {
	/* API click: on mouse down, move the cursor to the clicked location,
	 * and reset the selection */
	state.Cursor = state.locateCoord(coord, font, row_height)

	state.SelectStart = state.Cursor
	state.SelectEnd = state.Cursor
	state.HasPreferredX = false
}

func (state *TextEditor) drag(coord image.Point, font font.Face, row_height int) {
	/* API drag: on mouse drag, move the cursor and selection endpoint
	 * to the clicked location */
	var p int = state.locateCoord(coord, font, row_height)
	if state.SelectStart == state.SelectEnd {
		state.SelectStart = state.Cursor
	}
	state.SelectEnd = p
	state.Cursor = state.SelectEnd
}

func (state *TextEditor) findCharpos(n int, single_line bool, font font.Face, row_height int) (find textFind) {
	var r textEditRow
	var prev_start int = 0
	/* find the x/y location of a character, and remember info about the previous
	 * row in case we get a move-up event (for page up, we'll have to rescan) */

	var i int = 0
	var first int

	if n == len(state.Buffer) {
		/* if it's at the end, then find the last line -- simpler than trying to
		explicitly handle this case in the regular code */
		if single_line {
			texteditLayoutRow(&r, state, 0, row_height, font)
			find.Y = 0
			find.FirstChar = 0
			find.Length = len(state.Buffer)
			find.Height = r.Ymax - r.Ymin
			find.X = r.X1
		} else {
			find.Y = 0
			find.X = 0
			find.Height = 1

			for {
				texteditLayoutRow(&r, state, i, row_height, font)
				if i+r.NumChars >= len(state.Buffer) {
					break
				}
				prev_start = i
				i += r.NumChars
				find.Y += r.BaselineYDelta
			}

			find.Length = r.NumChars
			find.Height = r.Ymax - r.Ymin
			find.FirstChar = i
			find.Length = 0
			find.X = r.X1
			find.PrevFirst = prev_start
		}

		return
	}

	/* search rows to find the one that straddles character n */
	find.Y = 0

	for {
		texteditLayoutRow(&r, state, i, row_height, font)
		if n < i+r.NumChars {
			break
		}
		prev_start = i
		i += r.NumChars
		find.Y += r.BaselineYDelta
	}

	first = i
	find.FirstChar = first
	find.Length = r.NumChars
	find.Height = r.Ymax - r.Ymin
	find.PrevFirst = prev_start

	/* now scan to find xpos */
	find.X = r.X0

	for i = 0; first+i < n; i++ {
		find.X += state.getWidth(first, i, font)
	}

	return
}

func (state *TextEditor) clamp() {
	/* make the selection/cursor state valid if client altered the string */
	if textHasSelection(state) {
		if state.SelectStart > len(state.Buffer) {
			state.SelectStart = len(state.Buffer)
		}
		if state.SelectEnd > len(state.Buffer) {
			state.SelectEnd = len(state.Buffer)
		}

		/* if clamping forced them to be equal, move the cursor to match */
		if state.SelectStart == state.SelectEnd {
			state.Cursor = state.SelectStart
		}
	}

	if state.Cursor > len(state.Buffer) {
		state.Cursor = len(state.Buffer)
	}
}

// Deletes a chunk of text in the editor.
func (edit *TextEditor) Delete(where int, len int) {
	/* delete characters while updating undo */
	edit.makeundoDelete(where, len)

	edit.Buffer = strDeleteText(edit.Buffer, where, len)
	edit.HasPreferredX = false
}

// Deletes selection.
func (edit *TextEditor) DeleteSelection() {
	/* delete the section */
	edit.clamp()

	if textHasSelection(edit) {
		if edit.SelectStart < edit.SelectEnd {
			edit.Delete(edit.SelectStart, edit.SelectEnd-edit.SelectStart)
			edit.Cursor = edit.SelectStart
			edit.SelectEnd = edit.Cursor
		} else {
			edit.Delete(edit.SelectEnd, edit.SelectStart-edit.SelectEnd)
			edit.Cursor = edit.SelectEnd
			edit.SelectStart = edit.Cursor
		}

		edit.HasPreferredX = false
	}
}

func (state *TextEditor) sortselection() {
	/* canonicalize the selection so start <= end */
	if state.SelectEnd < state.SelectStart {
		var temp int = state.SelectEnd
		state.SelectEnd = state.SelectStart
		state.SelectStart = temp
	}
}

func (state *TextEditor) moveToFirst() {
	/* move cursor to first character of selection */
	if textHasSelection(state) {
		state.sortselection()
		state.Cursor = state.SelectStart
		state.SelectEnd = state.SelectStart
		state.HasPreferredX = false
	}
}

func (state *TextEditor) moveToLast() {
	/* move cursor to last character of selection */
	if textHasSelection(state) {
		state.sortselection()
		state.clamp()
		state.Cursor = state.SelectEnd
		state.SelectStart = state.SelectEnd
		state.HasPreferredX = false
	}
}

func isWordBoundary(state *TextEditor, idx int) bool {
	if idx <= 0 {
		return true
	}
	c := state.Buffer[idx]
	return c == ' ' || c == '\t' || c == 0x3000 || c == ',' || c == ';' || c == '(' || c == ')' || c == '{' || c == '}' || c == '[' || c == ']' || c == '|'
}

func (state *TextEditor) moveToWordPrevious() int {
	var c int = state.Cursor - 1
	for c >= 0 && !isWordBoundary(state, c) {
		c--
	}

	if c < 0 {
		c = 0
	}

	return c
}

func (state *TextEditor) moveToWordNext() int {
	var c int = state.Cursor + 1
	for c < len(state.Buffer) && !isWordBoundary(state, c) {
		c++
	}

	if c > len(state.Buffer) {
		c = len(state.Buffer)
	}

	return c
}

func (state *TextEditor) prepSelectionAtCursor() {
	/* update selection and cursor to match each other */
	if !textHasSelection(state) {
		state.SelectEnd = state.Cursor
		state.SelectStart = state.SelectEnd
	} else {
		state.Cursor = state.SelectEnd
	}
}

func (edit *TextEditor) Cut() int {
	if edit.Flags&EditReadOnly != 0 {
		return 0
	}
	/* API cut: delete selection */
	if textHasSelection(edit) {
		edit.DeleteSelection() /* implicitly clamps */
		edit.HasPreferredX = false
		return 1
	}

	return 0
}

// Paste from clipboard
func (edit *TextEditor) Paste(ctext string) {
	if edit.Flags&EditReadOnly != 0 {
		return
	}

	/* if there's a selection, the paste should delete it */
	edit.clamp()

	edit.DeleteSelection()

	text := []rune(ctext)

	edit.Buffer = strInsertText(edit.Buffer, edit.Cursor, text)

	edit.makeundoInsert(edit.Cursor, len(text))
	edit.Cursor += len(text)
	edit.HasPreferredX = false
}

func (edit *TextEditor) Text(text []rune) {
	if edit.Flags&EditReadOnly != 0 {
		return
	}

	for i := range text {
		/* can't add newline in single-line mode */
		if text[i] == '\n' && edit.SingleLine {
			break
		}

		/* filter incoming text */
		if edit.Filter != nil && !edit.Filter(text[i]) {
			continue
		}

		if edit.InsertMode && !textHasSelection(edit) && edit.Cursor < len(edit.Buffer) {
			edit.makeundoReplace(edit.Cursor, 1, 1)
			edit.Buffer = strDeleteText(edit.Buffer, edit.Cursor, 1)
			edit.Buffer = strInsertText(edit.Buffer, edit.Cursor, text[i:i+1])
			edit.Cursor++
			edit.HasPreferredX = false
		} else {
			edit.DeleteSelection() /* implicitly clamps */
			edit.Buffer = strInsertText(edit.Buffer, edit.Cursor, text[i:i+1])
			edit.makeundoInsert(edit.Cursor, 1)
			edit.Cursor++
			edit.HasPreferredX = false
		}
	}
}

func (state *TextEditor) key(e key.Event, font font.Face, row_height int) {
	readOnly := state.Flags&EditReadOnly != 0
retry:
	switch e.Code {
	case key.CodeZ:
		if readOnly {
			return
		}
		if e.Modifiers&key.ModControl != 0 {
			if e.Modifiers&key.ModShift != 0 {
				state.DoRedo()
				state.HasPreferredX = false

			} else {
				state.DoUndo()
				state.HasPreferredX = false
			}
		}

	case key.CodeInsert:
		state.InsertMode = !state.InsertMode

	case key.CodeLeftArrow:
		if e.Modifiers&key.ModControl != 0 {
			if e.Modifiers&key.ModShift != 0 {
				if !textHasSelection(state) {
					state.prepSelectionAtCursor()
				}
				state.Cursor = state.moveToWordPrevious()
				state.SelectEnd = state.Cursor
				state.clamp()
			} else {
				if textHasSelection(state) {
					state.moveToFirst()
				} else {
					state.Cursor = state.moveToWordPrevious()
					state.clamp()
				}
			}
		} else {
			if e.Modifiers&key.ModShift != 0 {
				state.clamp()
				state.prepSelectionAtCursor()

				/* move selection left */
				if state.SelectEnd > 0 {
					state.SelectEnd--
				}
				state.Cursor = state.SelectEnd
				state.HasPreferredX = false
			} else {
				/* if currently there's a selection,
				 * move cursor to start of selection */
				if textHasSelection(state) {
					state.moveToFirst()
				} else if state.Cursor > 0 {
					state.Cursor--
				}
				state.HasPreferredX = false
			}
		}

	case key.CodeRightArrow:
		if e.Modifiers&key.ModControl != 0 {
			if e.Modifiers&key.ModShift != 0 {
				if !textHasSelection(state) {
					state.prepSelectionAtCursor()
				}
				state.Cursor = state.moveToWordNext()
				state.SelectEnd = state.Cursor
				state.clamp()
			} else {
				if textHasSelection(state) {
					state.moveToLast()
				} else {
					state.Cursor = state.moveToWordNext()
					state.clamp()
				}
			}
		} else {
			if e.Modifiers&key.ModShift != 0 {
				state.prepSelectionAtCursor()

				/* move selection right */
				state.SelectEnd++

				state.clamp()
				state.Cursor = state.SelectEnd
				state.HasPreferredX = false
			} else {
				/* if currently there's a selection,
				 * move cursor to end of selection */
				if textHasSelection(state) {
					state.moveToLast()
				} else {
					state.Cursor++
				}
				state.clamp()
				state.HasPreferredX = false
			}
		}
	case key.CodeDownArrow:
		var row textEditRow
		var i int

		if state.SingleLine {
			/* on windows, up&down in single-line behave like left&right */
			e.Code = key.CodeRightArrow
			goto retry
		}

		if e.Modifiers&key.ModShift != 0 {
			state.prepSelectionAtCursor()
		} else if textHasSelection(state) {
			state.moveToLast()
		}

		/* compute current position of cursor point */
		state.clamp()

		find := state.findCharpos(state.Cursor, state.SingleLine, font, row_height)

		/* now find character position down a row */
		if find.Length != 0 {
			var x int
			var goal_x int
			if state.HasPreferredX {
				goal_x = state.PreferredX
			} else {
				goal_x = find.X
			}
			var start int = find.FirstChar + find.Length

			state.Cursor = start
			texteditLayoutRow(&row, state, state.Cursor, row_height, font)
			x = row.X0

			found := false
			for i = 0; i < row.NumChars; i++ {
				dx := state.getWidth(start, i, font)
				x += dx
				if x > goal_x {
					found = true
					break
				}
				state.Cursor++
			}

			if !found && row.NumChars > 0 {
				state.Cursor--
			}

			state.clamp()

			state.HasPreferredX = true
			state.PreferredX = goal_x
			if e.Modifiers&key.ModShift != 0 {
				state.SelectEnd = state.Cursor
			}
		}

	case key.CodeUpArrow:
		var row textEditRow
		var i int

		if state.SingleLine {
			/* on windows, up&down become left&right */
			e.Code = key.CodeLeftArrow

			goto retry
		}

		if e.Modifiers&key.ModShift != 0 {
			state.prepSelectionAtCursor()
		} else if textHasSelection(state) {
			state.moveToFirst()
		}

		/* compute current position of cursor point */
		state.clamp()

		find := state.findCharpos(state.Cursor, state.SingleLine, font, row_height)

		/* can only go up if there's a previous row */
		if find.PrevFirst != find.FirstChar {
			var x int
			/* now find character position up a row */

			var goal_x int
			if state.HasPreferredX {
				goal_x = state.PreferredX
			} else {
				goal_x = find.X
			}

			state.Cursor = find.PrevFirst
			texteditLayoutRow(&row, state, state.Cursor, row_height, font)
			x = row.X0

			found := false
			for i = 0; i < row.NumChars; i++ {
				dx := state.getWidth(find.PrevFirst, i, font)
				x += dx
				if x > goal_x {
					found = true
					break
				}
				state.Cursor++
			}

			if !found && row.NumChars > 0 {
				state.Cursor--
			}

			state.clamp()

			state.HasPreferredX = true
			state.PreferredX = goal_x
			if e.Modifiers&key.ModShift != 0 {
				state.SelectEnd = state.Cursor
			}
		}

	case key.CodeDeleteForward:
		if readOnly {
			return
		}
		if textHasSelection(state) {
			state.DeleteSelection()
		} else {
			if state.Cursor < len(state.Buffer) {
				state.Delete(state.Cursor, 1)
			}
		}

		state.HasPreferredX = false

	case key.CodeDeleteBackspace:
		if readOnly {
			return
		}
		if textHasSelection(state) {
			state.DeleteSelection()
		} else {
			state.clamp()
			if state.Cursor > 0 {
				state.Delete(state.Cursor-1, 1)
				state.Cursor--
			}
		}

		state.HasPreferredX = false

	case key.CodeHome:
		if e.Modifiers&key.ModControl != 0 {
			if e.Modifiers&key.ModShift != 0 {
				state.prepSelectionAtCursor()
				state.SelectEnd = 0
				state.Cursor = state.SelectEnd
				state.HasPreferredX = false
			} else {
				state.SelectEnd = 0
				state.SelectStart = state.SelectEnd
				state.Cursor = state.SelectStart
				state.HasPreferredX = false
			}
		} else {
			if e.Modifiers&key.ModShift != 0 {
				state.clamp()
				state.prepSelectionAtCursor()
				find := state.findCharpos(state.Cursor, state.SingleLine, font, row_height)
				state.SelectEnd = find.FirstChar
				state.Cursor = state.SelectEnd
				state.HasPreferredX = false
			} else {
				state.clamp()
				state.moveToFirst()
				find := state.findCharpos(state.Cursor, state.SingleLine, font, row_height)
				state.Cursor = find.FirstChar
				state.HasPreferredX = false
			}

		}

	case key.CodeEnd:
		if e.Modifiers&key.ModControl != 0 {
			if e.Modifiers&key.ModShift != 0 {
				state.prepSelectionAtCursor()
				state.SelectEnd = len(state.Buffer)
				state.Cursor = state.SelectEnd
				state.HasPreferredX = false
			} else {
				state.Cursor = len(state.Buffer)
				state.SelectEnd = 0
				state.SelectStart = state.SelectEnd
				state.HasPreferredX = false
			}
		} else {
			if e.Modifiers&key.ModShift != 0 {
				state.clamp()
				state.prepSelectionAtCursor()
				find := state.findCharpos(state.Cursor, state.SingleLine, font, row_height)
				state.HasPreferredX = false
				state.Cursor = find.FirstChar + find.Length
				if find.Length > 0 && state.Buffer[state.Cursor-1] == '\n' {
					state.Cursor--
				}
				state.SelectEnd = state.Cursor
			} else {
				state.clamp()
				state.moveToFirst()
				find := state.findCharpos(state.Cursor, state.SingleLine, font, row_height)

				state.HasPreferredX = false
				state.Cursor = find.FirstChar + find.Length
				if find.Length > 0 && state.Buffer[state.Cursor-1] == '\n' {
					state.Cursor--
				}
			}
		}
	}
}

func texteditFlushRedo(state *textUndoState) {
	state.RedoPoint = int16(_TEXTEDIT_UNDOSTATECOUNT)
}

func texteditDiscardUndo(state *textUndoState) {
	/* discard the oldest entry in the undo list */
	if state.UndoPoint > 0 {
		state.UndoPoint--
		copy(state.UndoRec[:], state.UndoRec[1:])
	}
}

func texteditCreateUndoRecord(state *textUndoState, numchars int) *textUndoRecord {
	/* any time we create a new undo record, we discard redo*/
	texteditFlushRedo(state)

	/* if we have no free records, we have to make room,
	 * by sliding the existing records down */
	if int(state.UndoPoint) == _TEXTEDIT_UNDOSTATECOUNT {
		texteditDiscardUndo(state)
	}

	r := &state.UndoRec[state.UndoPoint]
	state.UndoPoint++
	return r
}

func texteditCreateundo(state *textUndoState, pos int, insert_len int, delete_len int) *textUndoRecord {
	r := texteditCreateUndoRecord(state, insert_len)

	r.Where = pos
	r.InsertLength = insert_len
	r.DeleteLength = delete_len
	r.Text = nil

	return r
}

func (edit *TextEditor) DoUndo() {
	var s *textUndoState = &edit.Undo
	var u textUndoRecord
	var r *textUndoRecord
	if s.UndoPoint == 0 {
		return
	}

	/* we need to do two things: apply the undo record, and create a redo record */
	u = s.UndoRec[s.UndoPoint-1]

	r = &s.UndoRec[s.RedoPoint-1]
	r.Text = nil

	r.InsertLength = u.DeleteLength
	r.DeleteLength = u.InsertLength
	r.Where = u.Where

	if u.DeleteLength != 0 {
		r.Text = make([]rune, u.DeleteLength)
		copy(r.Text, edit.Buffer[u.Where:u.Where+u.DeleteLength])
		edit.Buffer = strDeleteText(edit.Buffer, u.Where, u.DeleteLength)
	}

	/* check type of recorded action: */
	if u.InsertLength != 0 {
		/* easy case: was a deletion, so we need to insert n characters */
		edit.Buffer = strInsertText(edit.Buffer, u.Where, u.Text)
	}

	edit.Cursor = u.Where + u.InsertLength

	s.UndoPoint--
	s.RedoPoint--
}

func (edit *TextEditor) DoRedo() {
	var s *textUndoState = &edit.Undo
	var u *textUndoRecord
	var r textUndoRecord
	if int(s.RedoPoint) == _TEXTEDIT_UNDOSTATECOUNT {
		return
	}

	/* we need to do two things: apply the redo record, and create an undo record */
	u = &s.UndoRec[s.UndoPoint]

	r = s.UndoRec[s.RedoPoint]

	/* we KNOW there must be room for the undo record, because the redo record
	was derived from an undo record */
	u.DeleteLength = r.InsertLength

	u.InsertLength = r.DeleteLength
	u.Where = r.Where
	u.Text = nil

	if r.DeleteLength != 0 {
		u.Text = make([]rune, r.DeleteLength)
		copy(u.Text, edit.Buffer[r.Where:r.Where+r.DeleteLength])
		edit.Buffer = strDeleteText(edit.Buffer, r.Where, r.DeleteLength)
	}

	if r.InsertLength != 0 {
		/* easy case: need to insert n characters */
		edit.Buffer = strInsertText(edit.Buffer, r.Where, r.Text)
	}

	edit.Cursor = r.Where + r.InsertLength

	s.UndoPoint++
	s.RedoPoint++
}

func (state *TextEditor) makeundoInsert(where int, length int) {
	texteditCreateundo(&state.Undo, where, 0, length)
}

func (state *TextEditor) makeundoDelete(where int, length int) {
	u := texteditCreateundo(&state.Undo, where, length, 0)
	u.Text = make([]rune, length)
	copy(u.Text, state.Buffer[where:where+length])
}

func (state *TextEditor) makeundoReplace(where int, old_length int, new_length int) {
	u := texteditCreateundo(&state.Undo, where, old_length, new_length)
	u.Text = make([]rune, old_length)
	copy(u.Text, state.Buffer[where:where+old_length])
}

func (state *TextEditor) clearState(type_ TextEditType) {
	/* reset the state to default */
	state.Undo.UndoPoint = 0

	state.Undo.RedoPoint = int16(_TEXTEDIT_UNDOSTATECOUNT)
	state.HasPreferredX = false
	state.PreferredX = 0
	//state.CursorAtEndOfLine = 0
	state.Initialized = true
	state.SingleLine = type_ == TextEditSingleLine
	state.InsertMode = false
}

func (edit *TextEditor) SelectAll() {
	edit.SelectStart = 0
	edit.SelectEnd = len(edit.Buffer) + 1
}

func editDrawText(out *command.Buffer, style *nstyle.Edit, pos image.Point, x_margin int, text []rune, row_height int, f font.Face, background color.RGBA, foreground color.RGBA, is_selected bool) (posOut image.Point) {
	if len(text) == 0 {
		return pos
	}
	var line_offset int = 0
	var line_count int = 0
	var txt textWidget
	txt.Background = background
	txt.Text = foreground

	pos_x, pos_y := pos.X, pos.Y
	start := 0

	d := font.Drawer{Face: f}

	flushLine := func(index int) rect.Rect {
		// new line sepeator so draw previous line
		var lblrect rect.Rect
		lblrect.Y = pos_y + line_offset
		lblrect.H = row_height
		lblrect.W = nk_null_rect.W
		lblrect.X = pos_x

		if is_selected { // selection needs to draw different background color
			if index == len(text) || (index == start && start == 0) {
				// XXX calculating text width here is slow figure out why
				lblrect.W = d.MeasureString(string(text[start:index])).Ceil()
			}
			out.FillRect(lblrect, 0, background)
		}
		widgetText(out, lblrect, string(text[start:index]), &txt, "LC", f)

		if line_count == 0 {
			pos_x = x_margin
		}

		return lblrect
	}

	for index, glyph := range text {
		if glyph == '\n' {
			flushLine(index)
			line_count++
			start = index + 1
			line_offset += row_height
			continue
		}

		if glyph == '\r' {
			continue
		}
	}

	if start >= len(text) {
		return image.Point{pos_x, pos_y + line_offset}
	}

	// draw last line
	lblrect := flushLine(len(text))
	lblrect.W = d.MeasureString(string(text[start:])).Ceil()

	return image.Point{lblrect.X + lblrect.W, lblrect.Y}
}

func (ed *TextEditor) doEdit(bounds rect.Rect, style *nstyle.Edit, inp *Input) (ret EditEvents) {
	font := ed.win.ctx.Style.Font
	state := ed.win.widgets.PrevState(bounds)

	ed.clamp()

	// visible text area calculation
	var area rect.Rect
	area.X = bounds.X + style.Padding.X + style.Border
	area.Y = bounds.Y + style.Padding.Y + style.Border
	area.W = bounds.W - (2.0*style.Padding.X + 2*style.Border)
	area.H = bounds.H - (2.0*style.Padding.Y + 2*style.Border)
	if ed.Flags&EditMultiline != 0 {
		area.H = area.H - style.ScrollbarSize.Y
	}
	var row_height int
	if ed.Flags&EditMultiline != 0 {
		row_height = FontHeight(font) + style.RowPadding
	} else {
		row_height = area.H
	}

	/* update edit state */
	prev_state := ed.Active

	if ed.win.ctx.activateEditor != nil {
		ed.Active = ed.win.ctx.activateEditor == ed

	}

	is_hovered := inp.Mouse.HoveringRect(bounds)

	if ed.Flags&EditFocusFollowsMouse != 0 {
		if inp != nil {
			ed.Active = is_hovered
		}
	} else {
		if inp != nil && inp.Mouse.Buttons[mouse.ButtonLeft].Clicked && inp.Mouse.Buttons[mouse.ButtonLeft].Down {
			ed.Active = inp.Mouse.HoveringRect(bounds)
		}
	}

	/* (de)activate text editor */
	var select_all bool
	if !prev_state && ed.Active {
		type_ := TextEditSingleLine
		if ed.Flags&EditMultiline != 0 {
			type_ = TextEditMultiLine
		}
		ed.clearState(type_)
		if ed.Flags&EditAlwaysInsertMode != 0 {
			ed.InsertMode = true
		}
		if ed.Flags&EditAutoSelect != 0 {
			select_all = true
		}
	} else if !ed.Active {
		ed.InsertMode = false
	}

	if ed.Flags&EditNeverInsertMode != 0 {
		ed.InsertMode = false
	}

	if ed.Active {
		ret = EditActive
	} else {
		ret = EditInactive
	}
	if prev_state != ed.Active {
		if ed.Active {
			ret |= EditActivated
		} else {
			ret |= EditDeactivated
		}
	}

	/* handle user input */
	cursor_follow := ed.CursorFollow
	ed.CursorFollow = false
	if ed.Active && inp != nil {
		inpos := inp.Mouse.Pos
		indelta := inp.Mouse.Delta
		coord := image.Point{(inpos.X - area.X) + ed.Scrollbar.X, (inpos.Y - area.Y) + ed.Scrollbar.Y}
		areaWithoutScrollbar := area
		areaWithoutScrollbar.W -= style.ScrollbarSize.X
		is_hovered := inp.Mouse.HoveringRect(areaWithoutScrollbar)

		/* mouse click handler */
		if select_all {
			ed.SelectAll()
		} else if is_hovered && inp.Mouse.Buttons[mouse.ButtonLeft].Down && inp.Mouse.Buttons[mouse.ButtonLeft].Clicked {
			ed.click(coord, font, row_height)
		} else if is_hovered && inp.Mouse.Buttons[mouse.ButtonLeft].Down && (indelta.X != 0.0 || indelta.Y != 0.0) {
			ed.drag(coord, font, row_height)
			cursor_follow = true
		}

		/* text input */
		if inp.Keyboard.Text != "" {
			ed.Text([]rune(inp.Keyboard.Text))
			cursor_follow = true
		}

		copy := false
		cut := false
		paste := false

		for _, e := range inp.Keyboard.Keys {
			switch e.Code {
			case key.CodeReturnEnter:
				if ed.Flags&EditCtrlEnterNewline != 0 && e.Modifiers&key.ModShift != 0 {
					ed.Text([]rune{'\n'})
					cursor_follow = true
				} else if ed.Flags&EditSigEnter != 0 {
					ret = EditInactive
					ret |= EditDeactivated
					if ed.Flags&EditReadOnly == 0 {
						ret |= EditCommitted
					}
					ed.Active = false
				} else {
					ed.Text([]rune{'\n'})
					cursor_follow = true
				}

			case key.CodeX:
				if e.Modifiers&key.ModControl != 0 {
					cut = true
				}

			case key.CodeC:
				if e.Modifiers&key.ModControl != 0 {
					copy = true
				}

			case key.CodeV:
				if e.Modifiers&key.ModControl != 0 {
					paste = true
				}

			default:
				ed.key(e, font, row_height)
				cursor_follow = true
			}

		}

		/* cut & copy handler */
		if (copy || cut) && (ed.Flags&EditClipboard != 0) {
			var begin, end int
			if ed.SelectStart > ed.SelectEnd {
				begin = ed.SelectEnd
				end = ed.SelectStart
			} else {
				begin = ed.SelectStart
				end = ed.SelectEnd
			}
			clipboard.Set(string(ed.Buffer[begin:end]))
			if cut {
				ed.Cut()
				cursor_follow = true
			}
		}

		/* paste handler */
		if paste && (ed.Flags&EditClipboard != 0) {
			ed.Paste(clipboard.Get())
			cursor_follow = true
		}

	}

	/* set widget state */
	if ed.Active {
		state = nstyle.WidgetStateActive
	} else {
		state = nstyle.WidgetStateInactive
	}
	if is_hovered {
		state |= nstyle.WidgetStateHovered
	}

	var d drawableTextEditor

	/* text pointer positions */
	var selection_begin, selection_end int
	if ed.SelectStart < ed.SelectEnd {
		selection_begin = ed.SelectStart
		selection_end = ed.SelectEnd
	} else {
		selection_begin = ed.SelectEnd
		selection_end = ed.SelectStart
	}

	d.SelectionBegin, d.SelectionEnd = selection_begin, selection_end

	d.Edit = ed
	d.State = state
	d.Style = style
	d.Bounds = bounds
	d.Area = area
	d.RowHeight = row_height
	ed.win.widgets.Add(state, bounds)
	d.Draw(&ed.win.ctx.Style, &ed.win.cmds)

	/* scrollbar */
	if cursor_follow {
		cursor_pos := d.CursorPos
		/* update scrollbar to follow cursor */
		if ed.Flags&EditNoHorizontalScroll == 0 {
			/* horizontal scroll */
			scroll_increment := area.W / 2
			if (cursor_pos.X < ed.Scrollbar.X) || ((ed.Scrollbar.X+area.W)-cursor_pos.X < style.CursorSize) {
				ed.Scrollbar.X = max(0, cursor_pos.X-scroll_increment)
			}
		} else {
			ed.Scrollbar.X = 0
		}

		if ed.Flags&EditMultiline != 0 {
			/* vertical scroll */
			if cursor_pos.Y < ed.Scrollbar.Y {
				ed.Scrollbar.Y = max(0, cursor_pos.Y-row_height)
			}
			for (ed.Scrollbar.Y+area.H)-cursor_pos.Y < row_height {
				ed.Scrollbar.Y = ed.Scrollbar.Y + row_height
			}
		} else {
			ed.Scrollbar.Y = 0
		}
	}

	if !ed.SingleLine {
		/* scrollbar widget */
		var scroll rect.Rect
		scroll.X = (area.X + area.W) - style.ScrollbarSize.X
		scroll.Y = area.Y
		scroll.W = style.ScrollbarSize.X
		scroll.H = area.H

		scroll_offset := float64(ed.Scrollbar.Y)
		scroll_step := float64(scroll.H) * 0.1
		scroll_inc := float64(scroll.H) * 0.01
		scroll_target := float64(d.TextSize.Y + row_height)
		ed.Scrollbar.Y = int(doScrollbarv(ed.win, scroll, bounds, scroll_offset, scroll_target, scroll_step, scroll_inc, &style.Scrollbar, inp, font))
	}

	return ret
}

type drawableTextEditor struct {
	Edit      *TextEditor
	State     nstyle.WidgetStates
	Style     *nstyle.Edit
	Bounds    rect.Rect
	Area      rect.Rect
	RowHeight int

	SelectionBegin, SelectionEnd int

	TextSize  image.Point
	CursorPos image.Point
}

func (d *drawableTextEditor) Draw(z *nstyle.Style, out *command.Buffer) {
	edit := d.Edit
	state := d.State
	style := d.Style
	bounds := d.Bounds
	font := z.Font
	area := d.Area
	row_height := d.RowHeight
	selection_begin := d.SelectionBegin
	selection_end := d.SelectionEnd

	/* select background colors/images  */
	var old_clip rect.Rect = out.Clip
	{
		var background *nstyle.Item
		if state&nstyle.WidgetStateActive != 0 {
			background = &style.Active
		} else if state&nstyle.WidgetStateHovered != 0 {
			background = &style.Hover
		} else {
			background = &style.Normal
		}

		/* draw background frame */
		if background.Type == nstyle.ItemColor {
			out.FillRect(bounds, style.Rounding, style.BorderColor)
			out.FillRect(shrinkRect(bounds, style.Border), style.Rounding, background.Data.Color)
		} else {
			out.DrawImage(bounds, background.Data.Image)
		}
	}

	area.W -= style.CursorSize
	clip := unify(old_clip, area)
	out.PushScissor(clip)
	/* draw text */
	var background_color color.RGBA
	var text_color color.RGBA
	var sel_background_color color.RGBA
	var sel_text_color color.RGBA
	var cursor_color color.RGBA
	var cursor_text_color color.RGBA
	var background *nstyle.Item

	/* select correct colors to draw */
	if state&nstyle.WidgetStateActive != 0 {
		background = &style.Active
		text_color = style.TextActive
		sel_text_color = style.SelectedTextHover
		sel_background_color = style.SelectedHover
		cursor_color = style.CursorHover
		cursor_text_color = style.CursorTextHover
	} else if state&nstyle.WidgetStateHovered != 0 {
		background = &style.Hover
		text_color = style.TextHover
		sel_text_color = style.SelectedTextHover
		sel_background_color = style.SelectedHover
		cursor_text_color = style.CursorTextHover
		cursor_color = style.CursorHover
	} else {
		background = &style.Normal
		text_color = style.TextNormal
		sel_text_color = style.SelectedTextNormal
		sel_background_color = style.SelectedNormal
		cursor_color = style.CursorNormal
		cursor_text_color = style.CursorTextNormal
	}

	if background.Type == nstyle.ItemImage {
		background_color = color.RGBA{0, 0, 0, 0}
	} else {
		background_color = background.Data.Color
	}

	startPos := image.Point{area.X - edit.Scrollbar.X, area.Y - edit.Scrollbar.Y}
	pos := startPos
	x_margin := pos.X
	if edit.SelectStart == edit.SelectEnd {
		drawEolCursor := func() {
			cursor_pos := d.CursorPos
			/* draw cursor at end of line */
			var cursor rect.Rect
			cursor.W = style.CursorSize
			cursor.H = row_height
			cursor.X = area.X + cursor_pos.X - edit.Scrollbar.X
			cursor.Y = area.Y + cursor_pos.Y + row_height/2.0 - cursor.H/2.0
			cursor.Y -= edit.Scrollbar.Y
			out.FillRect(cursor, 0, cursor_color)

		}

		/* no selection so just draw the complete text */
		pos = editDrawText(out, style, pos, x_margin, edit.Buffer[:edit.Cursor], row_height, font, background_color, text_color, false)
		d.CursorPos = pos.Sub(startPos)
		if edit.Active {
			if edit.Cursor < len(edit.Buffer) {
				pos = editDrawText(out, style, pos, x_margin, edit.Buffer[edit.Cursor:edit.Cursor+1], row_height, font, cursor_color, cursor_text_color, true)
				if edit.Buffer[edit.Cursor] == '\n' {
					drawEolCursor()
				}
				pos = editDrawText(out, style, pos, x_margin, edit.Buffer[edit.Cursor+1:], row_height, font, background_color, text_color, false)
			} else {
				drawEolCursor()
			}
		} else if edit.Cursor < len(edit.Buffer) {
			pos = editDrawText(out, style, pos, x_margin, edit.Buffer[edit.Cursor:], row_height, font, background_color, text_color, false)
		}
	} else {
		/* edit has selection so draw 1-3 text chunks */
		if selection_begin > 0 {
			/* draw unselected text before selection */
			pos = editDrawText(out, style, pos, x_margin, edit.Buffer[:selection_begin], row_height, font, background_color, text_color, false)
		}

		if selection_begin == edit.SelectEnd {
			d.CursorPos = pos.Sub(startPos)
		}

		pos = editDrawText(out, style, pos, x_margin, edit.Buffer[selection_begin:selection_end], row_height, font, sel_background_color, sel_text_color, true)

		if selection_begin != edit.SelectEnd {
			d.CursorPos = pos.Sub(startPos)
		}

		if selection_end < len(edit.Buffer) {
			pos = editDrawText(out, style, pos, x_margin, edit.Buffer[selection_end:], row_height, font, background_color, text_color, false)
		}
	}
	d.TextSize = pos.Sub(startPos)

	out.PushScissor(old_clip)
	return
}

// Adds text editor edit to win.
// Initial contents of the text editor will be set to text. If
// alwaysSet is specified the contents of the editor will be reset
// to text.
func (edit *TextEditor) Edit(win *Window) EditEvents {
	edit.init(win)
	if edit.Maxlen > 0 {
		if len(edit.Buffer) > edit.Maxlen {
			edit.Buffer = edit.Buffer[:edit.Maxlen]
		}
	}

	if edit.Flags&EditNoCursor != 0 {
		edit.Cursor = len(edit.Buffer)
	}
	if edit.Flags&EditSelectable == 0 {
		edit.SelectStart = edit.Cursor
		edit.SelectEnd = edit.Cursor
	}

	var bounds rect.Rect

	style := &edit.win.ctx.Style
	widget_state, bounds := edit.win.widget()
	if widget_state == 0 {
		return 0
	}
	in := edit.win.inputMaybe(widget_state)

	return edit.doEdit(bounds, &style.Edit, in)
}
