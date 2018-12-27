package main

import (
	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/rect"
)

var silenced bool
var scrollbackEditor nucular.TextEditor

var scrollbackEditorRect rect.Rect

type editorWriter struct {
	ed   *nucular.TextEditor
	lock bool
}

const (
	scrollbackHighMark = 64 * 1024
	scrollbackLowMark  = 32 * 1024
)

func (w *editorWriter) Write(b []byte) (int, error) {
	if w.lock {
		wnd.Lock()
		defer wnd.Unlock()
		defer wnd.Changed()
	}

	ncols := -1
	if spaceWidth > 0 && scrollbackEditorRect.W > 0 {
		ncols = (scrollbackEditorRect.W / spaceWidth) - 1
		if ncols < 80 {
			ncols = 80
		}
	}

	w.ed.Buffer = autowrappend(w.ed.Buffer, []rune(expandTabs(string(b))), ncols)
	if len(w.ed.Buffer) > scrollbackHighMark {
		copy(w.ed.Buffer, w.ed.Buffer[scrollbackLowMark:])
		w.ed.Buffer = w.ed.Buffer[:len(w.ed.Buffer)-scrollbackLowMark]
		w.ed.Cursor = len(w.ed.Buffer) - 256
	}
	oldcursor := w.ed.Cursor
	for w.ed.Cursor = len(w.ed.Buffer) - 2; w.ed.Cursor > oldcursor; w.ed.Cursor-- {
		if w.ed.Buffer[w.ed.Cursor] == '\n' {
			break
		}
	}
	if w.ed.Cursor > 0 {
		w.ed.Cursor++
	}
	if w.ed.Cursor > len(w.ed.Buffer) {
		w.ed.Cursor = len(w.ed.Buffer)
	}
	w.ed.CursorFollow = true
	w.ed.Redraw = true
	return len(b), nil
}

func currentColumn(buf []rune) int {
	for i := len(buf) - 1; i >= 0; i-- {
		if buf[i] == '\n' {
			return len(buf) - i - 1
		}
	}
	return len(buf)
}

func autowrappend(r, r1 []rune, ncols int) []rune {
	if ncols <= 0 {
		return append(r, r1...)
	}
	curcol := currentColumn(r)
	for _, ch := range r1 {
		if curcol >= ncols {
			r = append(r, '\n')
			curcol = 0
		}
		r = append(r, ch)
		curcol++
		if ch == '\n' {
			curcol = 0
		}
	}
	return r
}
