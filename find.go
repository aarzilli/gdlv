package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"reflect"
	"sync"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/rect"
)

type findElementWindowSingleton struct {
	v         *Variable
	searching bool
	cnt       int
	cancel    chan struct{}
	done      bool
	exprEd    nucular.TextEditor
	mu        sync.Mutex
}

var findElementWindow findElementWindowSingleton

func viewFindElement(w *nucular.Window, v *Variable) {
	findElementWindow.v = v
	findElementWindow.searching = false
	findElementWindow.exprEd.Flags = nucular.EditSelectable | nucular.EditClipboard | nucular.EditSigEnter
	findElementWindow.exprEd.Active = true
	w.Master().PopupOpen(fmt.Sprintf("Find Element in %s", v.Name), dynamicPopupFlags, rect.Rect{20, 100, 480, 500}, true, findElementWindow.Update)
}

func (fw *findElementWindowSingleton) Update(w *nucular.Window) {
	if fw.searching {
		fw.updateSearching(w)
	} else {
		fw.updateSetup(w)
	}
}

func (fw *findElementWindowSingleton) updateSetup(w *nucular.Window) {
	w.Row(30).Static(0)
	w.Label("Expression:", "LC")
	w.Row(30).Static(0)
	ok := false
	if ev := fw.exprEd.Edit(w); ev&nucular.EditCommitted != 0 {
		ok = true
	}
	w.Row(30).Static(0, 80, 80, 80)
	w.Spacing(1)
	if w.ButtonText("Help") {
		out := editorWriter{&scrollbackEditor, false}
		fmt.Fprintf(&out, `"Find Element" searches for the first element satisfying some
expression inside a slice or an array.

The expression is any boolean expression accepted by "print" where
the 'x' variable will represent the current element.

Example:

The target program contains this:

	type Item struct {
		Name string
		Value int
	}
	
	var list []Item
	
You want to find the first element of list with Name == "some_name":
- right click on the "list" slice,
- enter the expression: x.Name == "some_name"
- click OK
`)

		w.Close()
	}
	if w.ButtonText("Cancel") {
		w.Close()
	}
	if w.ButtonText("OK") {
		ok = true
	}
	if ok {
		fw.cnt = 0
		fw.cancel = make(chan struct{})
		fw.searching = true
		fw.done = false
		go fw.search(fw.cancel, int(fw.v.Len), string(fw.exprEd.Buffer), fw.v)
	}
}

func (fw *findElementWindowSingleton) updateSearching(w *nucular.Window) {
	fw.mu.Lock()
	defer fw.mu.Unlock()
	w.Row(30).Static(0)
	w.Label(fmt.Sprintf("Searching %d/%d", fw.cnt, fw.v.Len), "LC")
	w.Row(30).Static(0)
	w.Progress(&fw.cnt, int(fw.v.Len), false)
	w.Row(30).Static(0, 80)
	w.Spacing(1)
	if w.ButtonText("Cancel") {
		close(fw.cancel)
		w.Close()
	}
	if fw.done {
		w.Close()
	}
}

func (fw *findElementWindowSingleton) search(cancel chan struct{}, n int, strexpr string, v *Variable) {
	defer func() {
		fw.mu.Lock()
		fw.done = true
		fw.mu.Unlock()
	}()
	out := editorWriter{&scrollbackEditor, true}
	for i := 0; i < int(n); i++ {
		select {
		case <-cancel:
			return
		default:
		}
		fw.mu.Lock()
		fw.cnt = i
		fw.mu.Unlock()
		wnd.Changed()

		var cfw CustomFormatterWalker
		cfw.replace = fmt.Sprintf("(*(*%q)(%#x))[%d]", v.Type, v.Addr, i)

		expr, err := parser.ParseExpr(strexpr)
		if err != nil {
			fmt.Fprintf(&out, "error evaluating expression %q: %v", strexpr, err)
			return
		}

		ast.Walk(&cfw, expr)
		var buf bytes.Buffer
		printer.Fprint(&buf, token.NewFileSet(), expr)

		ret, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, buf.String(), LongLoadConfig)
		if err != nil {
			fmt.Fprintf(&out, "error evaluating expression %q: %v", buf.String(), err)
			return
		}

		if ret.Kind != reflect.Bool {
			fmt.Fprintf(&out, "error: value of expression %q is not a boolean\n", buf.String())
			return
		}

		if ret.Value == "true" {
			fmt.Fprintf(&out, "element satisfying expression %q found at %d\n", strexpr, i)
			return
		}
	}

	fmt.Fprintf(&out, "no element satisfying expression %q found\n", strexpr)
}
