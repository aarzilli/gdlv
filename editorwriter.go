package main

import (
	"github.com/aarzilli/nucular"
)

type editorWriter struct {
	ed *nucular.TextEditor
}

func (w *editorWriter) Write(b []byte) (int, error) {
	atend := w.ed.Cursor == len(w.ed.Buffer) || w.ed.Cursor == len(w.ed.Buffer)-1
	w.ed.Buffer = append(w.ed.Buffer, []rune(string(b))...)
	if atend {
		w.ed.Cursor = len(w.ed.Buffer)
		if b[len(b)-1] == '\n' {
			w.ed.Cursor--
		}
		w.ed.CursorFollow = true
		w.ed.Redraw = true
	}
	return len(b), nil
}
