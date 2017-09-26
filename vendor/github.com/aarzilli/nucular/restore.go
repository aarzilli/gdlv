package nucular

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"io"
	"os"
	"strconv"

	"github.com/aarzilli/nucular/rect"
)

type OpenWindowFn func(title string, flags WindowFlags, updateFn UpdateFn, saveFn SaveFn)
type RestoreFn func(data []byte, openWndFn OpenWindowFn) error

func (w *masterWindow) Save() ([]byte, error) {
	var out bytes.Buffer
	err := w.ctx.Save(&out)
	return out.Bytes(), err
}

func (w *masterWindow) Restore(data []byte, restoreFn RestoreFn) {
	go func() {
		w.uilock.Lock()
		defer w.uilock.Unlock()
		err := w.ctx.Restore(bytes.NewBuffer(data), restoreFn)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v", err)
		}
		w.Changed()
	}()
}

/*
Save format ::= <docked_conf><floating_configs>
floating_configs ::= <x>,<y>,<w>,<h>,<window>[<floating_configs>]*
<docked_conf> ::= 0 | "|"<size><docked_conf><docked_conf> | "_"<size><docked_conf><docked_conf> | <window>
window ::= a single character other than '0', '|', '_' or '!'. Or '!' followed by a base64 encoding terminated by another '!'
*/

func (ctx *context) Save(w io.Writer) error {
	err := ctx.DockedWindows.Save(w)
	if err != nil {
		return err
	}
	for _, wnd := range ctx.Windows {
		if wnd.flags&WindowNonmodal == 0 {
			return fmt.Errorf("can not save when non-modal windows are open")
		}
		fmt.Fprintf(w, "%d,%d,%d,%d,", wnd.Bounds.X, wnd.Bounds.Y, wnd.Bounds.W, wnd.Bounds.H)
		saveWindow(w, wnd)
	}
	return nil
}

func (t *dockedTree) Save(w io.Writer) error {
	switch t.Type {
	case dockedNodeVert:
		fmt.Fprintf(w, "|%d", t.Split.Size)
		err := t.Child[0].Save(w)
		if err != nil {
			return err
		}
		err = t.Child[1].Save(w)
		if err != nil {
			return err
		}
	case dockedNodeHoriz:
		fmt.Fprintf(w, "_%d", t.Split.Size)
		err := t.Child[0].Save(w)
		if err != nil {
			return err
		}
		err = t.Child[1].Save(w)
		if err != nil {
			return err
		}
	case dockedNodeLeaf:
		if t.W == nil {
			fmt.Fprintf(w, "0")
		} else {
			if t.W.saveFn == nil {
				return fmt.Errorf("one docked window doesn't have a save function")
			}
			saveWindow(w, t.W)
		}
	}
	return nil
}

func saveWindow(w io.Writer, wnd *Window) {
	if wnd.saveFn != nil {
		data := wnd.saveFn()
		switch {
		case len(data) == 0:
			fmt.Fprintf(w, "0")
		case len(data) == 1 && data[0] != '0' && data[0] != '|' && data[0] != '_' && data[0] != '!' && data[0] >= '0' && data[0] <= 'z':
			fmt.Fprintf(w, "%c", data[0])
		default:
			encodedata := base64.StdEncoding.EncodeToString(data)
			fmt.Fprintf(w, "!%s!", encodedata)
		}
	} else {
		fmt.Fprintf(w, "0")
	}
}

func (ctx *context) Restore(in io.Reader, restoreFn RestoreFn) error {
	rd := bufio.NewReader(in)
	ctx.DockedWindows = dockedTree{}
	layout0 := ctx.Windows[0].layout
	ctx.Windows = ctx.Windows[:0]
	ctx.dockedCnt = 0
	ctx.DockedWindows = *parseDockedTree(rd, ctx, restoreFn)

	for {
		var rect rect.Rect
		rect.X = readIntComma(rd)
		rect.Y = readIntComma(rd)
		rect.W = readIntComma(rd)
		rect.H = readIntComma(rd)
		if !readWindow(layout0, rd, ctx, rect, restoreFn) {
			break
		}
		layout0 = nil
	}

	for i, w := range ctx.Windows {
		w.idx = i
	}

	return nil
}

func parseDockedTree(rd *bufio.Reader, ctx *context, restoreFn RestoreFn) *dockedTree {
	switch b, _ := rd.ReadByte(); b {
	case '0':
		return &dockedTree{}
	case '_', '|':
		t := &dockedTree{}
		t.Split.Size = readIntNoComma(rd)
		t.Type = dockedNodeHoriz
		if b == '|' {
			t.Type = dockedNodeVert
		}
		t.Child[0] = parseDockedTree(rd, ctx, restoreFn)
		t.Child[1] = parseDockedTree(rd, ctx, restoreFn)
		return t
	default:
		t := &dockedTree{}
		t.Type = dockedNodeLeaf
		finishReadWindow(b, nil, rd, ctx, rect.Rect{0, 0, 200, 200}, restoreFn)
		t.W = ctx.Windows[len(ctx.Windows)-1]
		ctx.Windows = ctx.Windows[:len(ctx.Windows)-1]
		t.W.flags |= windowDocked
		ctx.dockedCnt--
		t.W.idx = ctx.dockedCnt
		t.W.undockedSz = image.Point{t.W.Bounds.W, t.W.Bounds.H}
		return t
	}
}

func readIntComma(rd *bufio.Reader) int {
	bs, _ := rd.ReadBytes(',')
	if len(bs) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(string(bs[:len(bs)-1]))
	return n
}

func readIntNoComma(rd *bufio.Reader) int {
	r := 0
	for {
		b, err := rd.ReadByte()
		if err != nil {
			break
		}
		if b < '0' || b > '9' {
			rd.UnreadByte()
			break
		}
		r = r * 10
		r += int(b - '0')
	}
	return r
}

func readWindow(layout0 *panel, rd *bufio.Reader, ctx *context, rect rect.Rect, restoreFn RestoreFn) bool {
	b, err := rd.ReadByte()
	if err != nil {
		return false
	}
	finishReadWindow(b, layout0, rd, ctx, rect, restoreFn)
	return true
}

func finishReadWindow(b byte, layout0 *panel, rd *bufio.Reader, ctx *context, rect rect.Rect, restoreFn RestoreFn) {
	if b == '0' {
		if layout0 != nil {
			ctx.setupMasterWindow(layout0, func(*Window) {})
		} else {
			ctx.popupOpen("", WindowDefaultFlags, rect, false, func(*Window) {}, nil)
		}
		return
	}
	data := []byte{b}
	if b == '!' {
		data, _ = rd.ReadBytes('!')
		data, _ = base64.StdEncoding.DecodeString(string(data))
	}
	called := false
	restoreFn(data, func(title string, flags WindowFlags, updateFn UpdateFn, saveFn SaveFn) {
		if called {
			return
		}
		called = true
		ctx.popupOpen(title, flags, rect, false, updateFn, saveFn)
	})
}
