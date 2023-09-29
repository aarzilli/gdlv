package main

import (
	"sync"

	"github.com/aarzilli/nucular/font"
	"github.com/aarzilli/nucular/richtext"
)

var silenced bool
var onNewline bool = true
var scrollbackEditor = richtext.New(richtext.Selectable | richtext.ShowTick | richtext.AutoWrap | richtext.Clipboard | richtext.Keyboard)
var scrollbackClear bool
var scrollbackInitialized bool
var scrollbackMu sync.Mutex
var scrollbackPreInitWrite []byte

type editorWriter struct {
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

	if len(b) > 0 {
		onNewline = b[len(b)-1] == '\n'
	}

	scrollbackMu.Lock()
	if !scrollbackInitialized {
		scrollbackPreInitWrite = append(scrollbackPreInitWrite, b...)
		scrollbackMu.Unlock()
		return len(b), nil
	}
	scrollbackMu.Unlock()

	c := scrollbackEditor.Append(true)
	c.SetStyle(richtext.TextStyle{Cursor: font.TextCursor})
	c.Text(string(b))
	c.End()
	scrollbackEditor.Tail(10000)
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
