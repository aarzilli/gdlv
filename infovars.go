package main

import (
	"bytes"
	"fmt"
	"image"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"

	"github.com/derekparker/delve/service/api"
)

type Variable struct {
	*api.Variable
	Value    string
	Children []*Variable
}

func wrapApiVariable(v *api.Variable) *Variable {
	r := &Variable{Variable: v}
	r.Value = v.Value
	if f := varFormat[v.Addr]; f != nil {
		r.Value = f(r.Value)
	} else if (v.Kind == reflect.Int || v.Kind == reflect.Uint) && ((v.Type == "uint8") || (v.Type == "int32")) {
		n, _ := strconv.Atoi(v.Value)
		r.Value = fmt.Sprintf("%s %q", v.Value, n)
	}
	r.Children = wrapApiVariables(v.Children)
	return r
}

func wrapApiVariables(vs []api.Variable) []*Variable {
	r := make([]*Variable, len(vs))
	for i := range vs {
		r[i] = wrapApiVariable(&vs[i])
	}
	return r
}

var globalsPanel = struct {
	asyncLoad    asyncLoad
	filterEditor nucular.TextEditor
	showAddr     bool
	globals      []*Variable
}{
	filterEditor: nucular.TextEditor{Filter: spacefilter},
}

var localsPanel = struct {
	asyncLoad    asyncLoad
	filterEditor nucular.TextEditor
	showAddr     bool
	args         []*Variable
	locals       []*Variable

	expressions []string
	selected    int
	ed          nucular.TextEditor
	v           []*Variable
}{
	filterEditor: nucular.TextEditor{Filter: spacefilter},
	selected:     -1,
	ed:           nucular.TextEditor{Flags: nucular.EditSelectable | nucular.EditSigEnter | nucular.EditClipboard},
}

func loadGlobals(p *asyncLoad) {
	globals, err := client.ListPackageVariables("", LongLoadConfig)
	globalsPanel.globals = wrapApiVariables(globals)
	sort.Sort(variablesByName(globalsPanel.globals))
	p.done(err)
}

func updateGlobals(container *nucular.Window) {
	w := globalsPanel.asyncLoad.showRequest(container, 0, "globals", loadGlobals)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	w.MenubarBegin()
	w.Row(varRowHeight).Static(90, 0, 100)
	w.Label("Filter:", "LC")
	globalsPanel.filterEditor.Edit(w)
	filter := string(globalsPanel.filterEditor.Buffer)
	w.CheckboxText("Address", &globalsPanel.showAddr)
	w.MenubarEnd()

	globals := globalsPanel.globals

	for i := range globals {
		if strings.Index(globals[i].Name, filter) >= 0 {
			showVariable(w, 0, globalsPanel.showAddr, -1, globals[i].Name, globals[i])
		}
	}
}

type variablesByName []*Variable

func (vars variablesByName) Len() int { return len(vars) }
func (vars variablesByName) Swap(i, j int) {
	temp := vars[i]
	vars[i] = vars[j]
	vars[j] = temp
}
func (vars variablesByName) Less(i, j int) bool { return vars[i].Name < vars[j].Name }

func loadLocals(p *asyncLoad) {
	args, errloc := client.ListFunctionArgs(api.EvalScope{curGid, curFrame}, LongLoadConfig)
	localsPanel.args = wrapApiVariables(args)
	sort.Sort(variablesByName(localsPanel.args))
	locals, errarg := client.ListLocalVariables(api.EvalScope{curGid, curFrame}, LongLoadConfig)
	localsPanel.locals = wrapApiVariables(locals)
	sort.Sort(variablesByName(localsPanel.locals))
	for i := range localsPanel.expressions {
		loadOneExpr(i)
	}

	m := map[string]int{}

	changename := func(v *Variable) {
		if n, ok := m[v.Name]; ok {
			n++
			m[v.Name] = n
			v.Name = fmt.Sprintf("%s(%d)", v.Name, n)
		} else {
			m[v.Name] = 0
		}
	}

	for i := range localsPanel.args {
		changename(localsPanel.args[i])
	}
	for i := range localsPanel.locals {
		changename(localsPanel.locals[i])
	}

	for _, err := range []error{errarg, errloc} {
		if err != nil {
			p.done(err)
			return
		}
	}
	p.done(nil)
}

const (
	varRowHeight = 20
	moreBtnWidth = 70
)

func updateLocals(container *nucular.Window) {
	w := localsPanel.asyncLoad.showRequest(container, 0, "locals", loadLocals)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	w.MenubarBegin()
	w.Row(varRowHeight).Static(90, 0, 100)
	w.Label("Filter:", "LC")
	localsPanel.filterEditor.Edit(w)
	filter := string(localsPanel.filterEditor.Buffer)
	w.CheckboxText("Address", &localsPanel.showAddr)
	w.MenubarEnd()

	args, locals := localsPanel.args, localsPanel.locals

	for i := range args {
		if strings.Index(args[i].Name, filter) >= 0 {
			showVariable(w, 0, localsPanel.showAddr, -1, args[i].Name, args[i])
		}
	}

	if len(args) > 0 {
		w.Row(varRowHeight / 2).Dynamic(1)
		w.Spacing(1)
	}

	for i := range locals {
		if strings.Index(locals[i].Name, filter) >= 0 {
			showVariable(w, 0, localsPanel.showAddr, -1, locals[i].Name, locals[i])
		}
	}

	if len(locals) > 0 {
		w.Row(varRowHeight / 2).Dynamic(1)
		w.Spacing(1)
	}

	editorShown := false

	for i := 0; i < len(localsPanel.expressions); i++ {
		if i == localsPanel.selected {
			exprsEditor(false, w)
			editorShown = true
		} else {
			if localsPanel.v[i] == nil {
				w.Row(varRowHeight).Dynamic(1)
				w.Label(fmt.Sprintf("loading %s", localsPanel.expressions[i]), "LC")
			} else {
				showVariable(w, 0, false, i, localsPanel.v[i].Name, localsPanel.v[i])
			}
		}
	}

	if !editorShown {
		exprsEditor(true, w)
	}
}

func loadOneExpr(i int) {
	v, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, localsPanel.expressions[i], LongLoadConfig)
	if err != nil {
		v = &api.Variable{Name: localsPanel.expressions[i], Unreadable: err.Error()}
	}
	localsPanel.v[i] = wrapApiVariable(v)
}

func exprsEditor(isnew bool, w *nucular.Window) {
	if isnew {
		w.Row(varRowHeight).Static(50, 0)
		w.Label("New: ", "LC")
		if w.Input().Mouse.HoveringRect(w.LastWidgetBounds) {
			w.Tooltip("Evaluate new expression")
		}
	} else {
		w.Row(varRowHeight).Dynamic(1)
	}
	active := localsPanel.ed.Edit(w)
	if active&nucular.EditCommitted == 0 {
		return
	}

	newexpr := string(localsPanel.ed.Buffer)
	localsPanel.ed.Buffer = localsPanel.ed.Buffer[:0]
	localsPanel.ed.Cursor = 0
	localsPanel.ed.Active = true
	localsPanel.ed.CursorFollow = true

	if localsPanel.selected < 0 {
		addExpression(newexpr)
	} else {
		localsPanel.expressions[localsPanel.selected] = newexpr
		go func(i int) {
			additionalLoadMu.Lock()
			defer additionalLoadMu.Unlock()
			loadOneExpr(i)
		}(localsPanel.selected)
		localsPanel.selected = -1
	}
}

func addExpression(newexpr string) {
	localsPanel.expressions = append(localsPanel.expressions, newexpr)
	localsPanel.v = append(localsPanel.v, nil)
	i := len(localsPanel.v) - 1
	go func(i int) {
		additionalLoadMu.Lock()
		defer additionalLoadMu.Unlock()
		loadOneExpr(i)
	}(i)
}

func showExprMenu(parentw *nucular.Window, exprMenuIdx int, v *Variable, clipb string) {
	if running {
		return
	}
	w := parentw.ContextualOpen(0, image.Point{}, parentw.LastWidgetBounds, nil)
	if w == nil {
		return
	}
	w.Row(20).Dynamic(1)
	if fn := detailsAvailable(v); fn != nil {
		if w.MenuItem(label.TA("Details", "LC")) {
			fn(w.Master(), v)
		}
	}

	if v.Kind == reflect.Func {
		if w.MenuItem(label.TA("Go to definition", "LC")) {
			locs, err := client.FindLocation(api.EvalScope{curGid, curFrame}, v.Value)
			if err == nil && len(locs) == 1 {
				listingPanel.pinnedLoc = &locs[0]
				go refreshState(refreshToSameFrame, clearNothing, nil)
			}
		}
	}

	if w.MenuItem(label.TA("Copy to clipboard", "LC")) {
		clipboard.Set(clipb)
	}

	if exprMenuIdx >= 0 && exprMenuIdx < len(localsPanel.expressions) {
		if w.MenuItem(label.TA("Edit", "LC")) {
			localsPanel.selected = exprMenuIdx
			localsPanel.ed.Buffer = []rune(localsPanel.expressions[localsPanel.selected])
			localsPanel.ed.Cursor = len(localsPanel.ed.Buffer)
			localsPanel.ed.CursorFollow = true
		}
		if w.MenuItem(label.TA("Remove", "LC")) {
			if exprMenuIdx+1 < len(localsPanel.expressions) {
				copy(localsPanel.expressions[exprMenuIdx:], localsPanel.expressions[exprMenuIdx+1:])
				copy(localsPanel.v[exprMenuIdx:], localsPanel.v[exprMenuIdx+1:])
			}
			localsPanel.expressions = localsPanel.expressions[:len(localsPanel.expressions)-1]
			localsPanel.v = localsPanel.v[:len(localsPanel.v)-1]
		}
	}
}

func showVariable(w *nucular.Window, depth int, addr bool, exprMenu int, name string, v *Variable) {
	varname := name
	const minInlineKeyValueLen = 20
	if v.Type != "" {
		if addr {
			if name != "" {
				name = fmt.Sprintf("%#x %s %s", v.Addr, name, v.Type)
			} else {
				name = fmt.Sprintf("%#x %s", v.Addr, v.Type)
			}
		} else {
			if name != "" {
				name = fmt.Sprintf("%s %s", name, v.Type)
			} else {
				name = v.Type
			}
		}
	} else {
		if addr {
			if name != "" {
				name = fmt.Sprintf("%#x %s", v.Addr, name)
			} else {
				name = fmt.Sprintf("%#x", v.Addr)
			}
		}
	}

	cblbl := func(fmtstr string, args ...interface{}) {
		s := fmt.Sprintf(fmtstr, args...)
		w.Label(s, "LC")
		showExprMenu(w, exprMenu, v, s)
	}

	w.Row(varRowHeight).StaticScaled(84 * zeroWidth)
	if v.Unreadable != "" {
		cblbl("%s = (unreadable %s)", name, v.Unreadable)
		return
	}

	if depth > 0 && v.Addr == 0 {
		cblbl("%s = nil", name, v.Type)
		return
	}

	switch v.Kind {
	case reflect.Slice:
		if !w.TreeIsOpen(varname) {
			name += " = " + v.SinglelineString()
		}
		if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
			showExprMenu(w, exprMenu, v, name)
			w.Label(fmt.Sprintf("len: %d cap: %d", v.Len, v.Cap), "LC")
			showArrayOrSliceContents(w, depth, addr, v)
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu, v, name)
		}
	case reflect.Array:
		if !w.TreeIsOpen(varname) {
			name += " = " + v.SinglelineString()
		}
		if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
			showExprMenu(w, exprMenu, v, name)
			w.Label(fmt.Sprintf("len: %d", v.Len), "LC")
			showArrayOrSliceContents(w, depth, addr, v)
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu, v, name)
		}
	case reflect.Ptr:
		if len(v.Children) == 0 {
			cblbl("%s ?", name)
		} else if v.Type == "" || v.Children[0].Addr == 0 {
			cblbl("%s = nil", name)
		} else {
			if !w.TreeIsOpen(varname) {
				name += " = " + v.SinglelineString()
			}
			if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
				if v.Children[0].OnlyAddr {
					loadMoreStruct(v.Children[0])
					w.Label("Loading...", "LC")
				} else {
					showExprMenu(w, exprMenu, v, name)
					showVariable(w, depth+1, addr, -1, "", v.Children[0])
				}
				w.TreePop()
			} else {
				showExprMenu(w, exprMenu, v, name)
			}
		}
	case reflect.UnsafePointer:
		cblbl("%s = unsafe.Pointer(%#x)", name, v.Children[0].Addr)
	case reflect.String:
		if v.Len == int64(len(v.Value)) {
			cblbl("%s = %q", name, v.Value)
		} else {
			cblbl("%s = %q...", name, v.Value)
		}
	case reflect.Chan:
		if len(v.Children) == 0 {
			cblbl("%s = nil", name)
		} else {
			if !w.TreeIsOpen(varname) {
				name += " = " + v.SinglelineString()
			}
			if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
				showExprMenu(w, exprMenu, v, name)
				showStructContents(w, depth, addr, v)
				w.TreePop()
			} else {
				showExprMenu(w, exprMenu, v, name)
			}
		}
	case reflect.Struct:
		if !w.TreeIsOpen(varname) {
			name += " = " + v.SinglelineString()
		}
		if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
			showExprMenu(w, exprMenu, v, name)
			if int(v.Len) != len(v.Children) && len(v.Children) == 0 {
				loadMoreStruct(v)
				w.Label("Loading...", "LC")
			} else {
				showStructContents(w, depth, addr, v)
			}
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu, v, name)
		}
	case reflect.Interface:
		if v.Children[0].Kind == reflect.Invalid {
			cblbl("%s = nil", name)
		} else {
			if !w.TreeIsOpen(varname) {
				name += " = " + v.SinglelineString()
			}
			if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
				showExprMenu(w, exprMenu, v, name)
				if v.Children[0].Kind == reflect.Ptr {
					if len(v.Children[0].Children) > 0 {
						showVariable(w, depth+1, addr, -1, "data", v.Children[0].Children[0])
					} else {
						loadMoreStruct(v)
						w.Label("Loading...", "LC")
					}
				} else {
					showVariable(w, depth+1, addr, -1, "data", v.Children[0])
				}
				w.TreePop()
			} else {
				showExprMenu(w, exprMenu, v, name)
			}
		}
	case reflect.Map:
		if !w.TreeIsOpen(varname) {
			name += " = " + v.SinglelineString()
		}
		if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
			showExprMenu(w, exprMenu, v, name)
			for i := 0; i < len(v.Children); i += 2 {
				key, value := v.Children[i], v.Children[i+1]
				if len(key.Children) == 0 && len(key.Value) < minInlineKeyValueLen {
					var keyname string
					if key.Kind == reflect.String {
						keyname = fmt.Sprintf("[%q]", key.Value)
					} else {
						keyname = fmt.Sprintf("[%s]", key.Value)
					}
					showVariable(w, depth+1, addr, -1, keyname, value)
				} else {
					showVariable(w, depth+1, addr, -1, fmt.Sprintf("[%d key]", i/2), key)
					showVariable(w, depth+1, addr, -1, fmt.Sprintf("[%d value]", i/2), value)
				}
			}
			if len(v.Children)/2 != int(v.Len) {
				w.Row(varRowHeight).Static(moreBtnWidth)
				if w.ButtonText(fmt.Sprintf("%d more", int(v.Len)-(len(v.Children)/2))) {
					loadMoreMap(v)
				}
			}
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu, v, name)
		}
	case reflect.Func:
		if v.Value == "" {
			cblbl("%s = nil", name)
		} else {
			cblbl(fmt.Sprintf("%s = %s", name, v.Value))
		}
	case reflect.Complex64, reflect.Complex128:
		cblbl("%s = (%s + %si)", name, v.Children[0].Value, v.Children[1].Value)
	case reflect.Float32, reflect.Float64:
		cblbl("%s = %s", name, v.Value)
	default:
		if v.Value != "" {
			cblbl("%s = %s", name, v.Value)
		} else {
			cblbl("%s = (unknown %s)", name, v.Kind)
		}
	}
}

func showArrayOrSliceContents(w *nucular.Window, depth int, addr bool, v *Variable) {
	for i := range v.Children {
		showVariable(w, depth+1, addr, -1, fmt.Sprintf("[%d]", i), v.Children[i])
	}
	if len(v.Children) != int(v.Len) {
		w.Row(varRowHeight).Static(moreBtnWidth)
		if w.ButtonText(fmt.Sprintf("%d more", int(v.Len)-len(v.Children))) {
			loadMoreArrayOrSlice(v)
		}
	}
}

func showStructContents(w *nucular.Window, depth int, addr bool, v *Variable) {
	for i := range v.Children {
		showVariable(w, depth+1, addr, -1, v.Children[i].Name, v.Children[i])
	}
}

var additionalLoadMu sync.Mutex
var additionalLoadRunning bool

func loadMoreMap(v *Variable) {
	additionalLoadMu.Lock()
	defer additionalLoadMu.Unlock()

	if !additionalLoadRunning {
		additionalLoadRunning = true
		go func() {
			expr := fmt.Sprintf("(*(*%q)(%#x))[%d:]", v.Type, v.Addr, len(v.Children)/2)
			lv, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, expr, LongLoadConfig)
			if err != nil {
				out := editorWriter{&scrollbackEditor, true}
				fmt.Fprintf(&out, "Error loading array contents %s: %v\n", expr, err)
				// prevent further attempts at loading
				v.Len = int64(len(v.Children) / 2)
			} else {
				v.Children = append(v.Children, wrapApiVariables(lv.Children)...)
			}
			wnd.Changed()
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
		}()
	}
}

func loadMoreArrayOrSlice(v *Variable) {
	additionalLoadMu.Lock()
	defer additionalLoadMu.Unlock()
	if !additionalLoadRunning {
		additionalLoadRunning = true
		go func() {
			expr := fmt.Sprintf("(*(*%q)(%#x))[%d:]", v.Type, v.Addr, len(v.Children))
			lv, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, expr, LongLoadConfig)
			if err != nil {
				out := editorWriter{&scrollbackEditor, true}
				fmt.Fprintf(&out, "Error loading array contents %s: %v\n", expr, err)
				// prevent further attempts at loading
				v.Len = int64(len(v.Children))
			} else {
				v.Children = append(v.Children, wrapApiVariables(lv.Children)...)
			}
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
			wnd.Changed()
		}()
	}
}

func loadMoreStruct(v *Variable) {
	additionalLoadMu.Lock()
	defer additionalLoadMu.Unlock()
	if !additionalLoadRunning {
		additionalLoadRunning = true
		go func() {
			lv, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, fmt.Sprintf("*(*%q)(%#x)", v.Type, v.Addr), LongLoadConfig)
			if err != nil {
				v.Unreadable = err.Error()
			} else {
				lv.Name = v.Name
				*v = *wrapApiVariable(lv)
			}
			wnd.Changed()
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
		}()
	}
}

type openDetailsWindowFn func(nucular.MasterWindow, *Variable)

func detailsAvailable(v *Variable) openDetailsWindowFn {
	if v == nil {
		return nil
	}
	switch v.Type {
	case "string", "[]uint8", "[]int32":
		return newStringViewer
	case "[]int", "[]int8", "[]int16", "[]int64", "[]uint", "[]uint16", "[]uint32", "[]uint64":
		return newIntArrayViewer
	case "int", "int8", "int16", "int32", "uint", "uint8", "uint16", "uint32", "uint64":
		return newIntViewer
	}
	return nil
}

var varFormat = map[uintptr]func(string) string{}

type stringViewerMode int

const (
	viewString stringViewerMode = iota
	viewByteArray
	viewRuneArray
)

type numberMode int

const (
	hexMode = iota
	decMode = iota
	octMode = iota
)

type stringViewer struct {
	v          *Variable
	mode       stringViewerMode
	numberMode numberMode
	ed         nucular.TextEditor
	mu         sync.Mutex
}

func newStringViewer(mw nucular.MasterWindow, v *Variable) {
	sv := &stringViewer{v: v}
	switch v.Type {
	case "string":
		sv.mode = viewString
	case "[]uint8":
		sv.mode = viewByteArray
	case "[]int32":
		sv.mode = viewRuneArray
	}
	sv.ed.Flags = nucular.EditReadOnly | nucular.EditMultiline | nucular.EditSelectable | nucular.EditClipboard
	sv.setupView()
	mw.PopupOpen("Viewing string: "+v.Name, popupFlags|nucular.WindowScalable, rect.Rect{100, 100, 550, 400}, true, sv.Update)
}

func (sv *stringViewer) Update(w *nucular.Window) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	w.Row(20).Dynamic(1)
	w.Label(sv.v.Name, "LC")

	w.Row(20).Static(100, 80, 80, 80)
	w.Label("View as:", "LC")
	newmode := sv.mode
	if w.OptionText("string", newmode == viewString) {
		newmode = viewString
	}
	if w.OptionText("[]byte", newmode == viewByteArray) {
		newmode = viewByteArray
	}
	if w.OptionText("[]rune", newmode == viewRuneArray) {
		newmode = viewRuneArray
	}
	if newmode != sv.mode {
		sv.mode = newmode
		sv.setupView()
	}

	switch sv.mode {
	case viewString:
		// nothing to choose
	case viewByteArray, viewRuneArray:
		numberMode := sv.numberMode
		w.Row(20).Static(120, 120, 120)
		if w.OptionText("Decimal", numberMode == decMode) {
			numberMode = decMode
		}
		if w.OptionText("Hexadecimal", numberMode == hexMode) {
			numberMode = hexMode
		}
		if w.OptionText("Octal", numberMode == octMode) {
			numberMode = octMode
		}
		if numberMode != sv.numberMode {
			sv.numberMode = numberMode
			sv.setupView()
		}
	}

	w.Row(0).Dynamic(1)
	sv.ed.Edit(w)

	w.Row(20).Static(0, 100, 100)
	l := int64(sv.len())
	w.Label(fmt.Sprintf("Loaded %d/%d", l, sv.v.Len), "LC")
	if sv.v.Len != l {
		if w.ButtonText("Load more") {
			sv.loadMore()
		}
	} else {
		w.Spacing(1)
	}
	if w.ButtonText("OK") {
		w.Close()
	}
}

func (sv *stringViewer) len() int {
	switch sv.v.Kind {
	case reflect.String:
		return len(sv.v.Value)
	case reflect.Array, reflect.Slice:
		return len(sv.v.Children)
	default:
		return 0
	}
}

func (sv *stringViewer) setupView() {
	var bytes []byte
	var runes []rune

	switch sv.v.Type {
	case "string":
		switch sv.mode {
		case viewString:
			sv.ed.Buffer = []rune(sv.v.Value)
		case viewByteArray:
			bytes = []byte(sv.v.Value)
		case viewRuneArray:
			runes = []rune(sv.v.Value)
		}
	case "[]uint8":
		bytes = make([]byte, len(sv.v.Children))
		for i := range sv.v.Children {
			n, _ := strconv.Atoi(sv.v.Children[i].Variable.Value)
			bytes[i] = byte(n)
		}
		switch sv.mode {
		case viewString:
			sv.ed.Buffer = []rune(string(bytes))
		case viewByteArray:
			// nothing to do
		case viewRuneArray:
			runes = []rune(string(bytes))
		}
	case "[]int32":
		runes = make([]rune, len(sv.v.Children))
		for i := range sv.v.Children {
			n, _ := strconv.Atoi(sv.v.Children[i].Variable.Value)
			runes[i] = rune(n)
		}
		switch sv.mode {
		case viewString:
			sv.ed.Buffer = runes
		case viewByteArray:
			bytes = []byte(string(runes))
		case viewRuneArray:
			// nothing to do
		}
	}

	switch sv.mode {
	case viewString:
		// nothing more to do
	case viewByteArray:
		array := make([]int64, len(bytes))
		for i := range bytes {
			array[i] = int64(bytes[i])
		}
		sv.ed.Buffer = []rune(formatArray(array, true, sv.numberMode, true, 1, 16))
	case viewRuneArray:
		array := make([]int64, len(runes))
		for i := range runes {
			array[i] = int64(runes[i])
		}
		sv.ed.Buffer = []rune(formatArray(array, sv.numberMode != decMode, sv.numberMode, false, 2, 10))
	}
}

func formatArray(array []int64, hexaddr bool, mode numberMode, canonical bool, size, stride int) string {
	var fmtstr, emptyfield string
	switch mode {
	case decMode:
		fmtstr = fmt.Sprintf("%%%dd ", size*3)
		emptyfield = fmt.Sprintf("%*s", size*3+1, "")
	case hexMode:
		fmtstr = fmt.Sprintf("%%0%dx ", size*2)
		emptyfield = fmt.Sprintf("%*s", size*2+1, "")
	case octMode:
		fmtstr = fmt.Sprintf("%%0%do ", size*3)
		emptyfield = fmt.Sprintf("%*s", size*3+1, "")
	}

	var addrfmtstr string
	if hexaddr {
		d := hexdigits(uint64(len(array)))
		if d < 2 {
			d = 2
		}
		addrfmtstr = fmt.Sprintf("%%0%dx  ", d)
	} else {
		addrfmtstr = fmt.Sprintf("[%%%dd]  ", digits(len(array)))
	}

	var buf bytes.Buffer
	i := 0
	for i < len(array) {
		fmt.Fprintf(&buf, addrfmtstr, i)
		start := i
		for c := 0; c < stride; i, c = i+1, c+1 {
			if stride%8 == 0 && c%8 == 0 && c != 0 && c != stride-1 {
				fmt.Fprintf(&buf, " ")
			}
			if i < len(array) {
				fmt.Fprintf(&buf, fmtstr, array[i])
			} else {
				fmt.Fprintf(&buf, emptyfield)
			}
		}

		if canonical {
			fmt.Fprintf(&buf, " |")
			for j := start; j < i; j++ {
				if j < len(array) {
					if array[j] >= 0x20 && array[j] <= 0x7e {
						fmt.Fprintf(&buf, "%c", byte(array[j]))
					} else {
						fmt.Fprintf(&buf, ".")
					}
				} else {
					fmt.Fprintf(&buf, " ")
				}
			}
			fmt.Fprintf(&buf, "|\n")
		} else {
			fmt.Fprintf(&buf, "\n")
		}
	}

	return buf.String()
}

func (sv *stringViewer) loadMore() {
	additionalLoadMu.Lock()
	defer additionalLoadMu.Unlock()
	if !additionalLoadRunning {
		additionalLoadRunning = true
		go func() {
			expr := fmt.Sprintf("(*(*%q)(%#x))[%d:]", sv.v.RealType, sv.v.Addr, sv.len())
			lv, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, expr, LongLoadConfig)
			if err != nil {
				out := editorWriter{&scrollbackEditor, true}
				fmt.Fprintf(&out, "Error loading string contents %s: %v\n", expr, err)
			} else {
				switch sv.v.Kind {
				case reflect.String:
					sv.v.Value += lv.Value
				case reflect.Array, reflect.Slice:
					sv.v.Children = append(sv.v.Children, wrapApiVariables(lv.Children)...)
				}
			}
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
			sv.mu.Lock()
			sv.setupView()
			sv.mu.Unlock()
			wnd.Changed()
		}()
	}
}

type intArrayViewer struct {
	v          *Variable
	displayLen int
	mode       numberMode
	ed         nucular.TextEditor
	mu         sync.Mutex
}

func newIntArrayViewer(mw nucular.MasterWindow, v *Variable) {
	av := &intArrayViewer{v: v}
	av.mode = decMode
	av.ed.Flags = nucular.EditReadOnly | nucular.EditMultiline | nucular.EditSelectable | nucular.EditClipboard
	av.setupView()
	mw.PopupOpen("Viewing array: "+v.Name, popupFlags|nucular.WindowScalable, rect.Rect{100, 100, 550, 400}, true, av.Update)
}

func (av *intArrayViewer) Update(w *nucular.Window) {
	av.mu.Lock()
	defer av.mu.Unlock()

	if av.displayLen != len(av.v.Children) {
		av.setupView()
	}

	w.Row(20).Static(100, 120, 120, 120)
	w.Label("View as:", "LC")
	mode := av.mode
	if w.OptionText("Decimal", mode == decMode) {
		mode = decMode
	}
	if w.OptionText("Hexadecimal", mode == hexMode) {
		mode = hexMode
	}
	if w.OptionText("Octal", mode == octMode) {
		mode = octMode
	}
	if mode != av.mode {
		av.mode = mode
		av.setupView()
	}

	w.Row(0).Dynamic(1)
	av.ed.Edit(w)

	w.Row(20).Static(0, 100, 100)
	w.Label(fmt.Sprintf("Loaded %d/%d", len(av.v.Children), av.v.Len), "LC")
	if av.v.Len != int64(len(av.v.Children)) {
		if w.ButtonText("Load more") {
			loadMoreArrayOrSlice(av.v)
		}
	} else {
		w.Spacing(1)
	}
	if w.ButtonText("OK") {
		w.Close()
	}
}

func (av *intArrayViewer) setupView() {
	array := make([]int64, len(av.v.Children))
	max := int64(0)
	for i := range av.v.Children {
		array[i], _ = strconv.ParseInt(av.v.Children[i].Variable.Value, 10, 64)
		x := array[i]
		if x < 0 {
			x = -x
		}
		if x > max {
			max = x
		}
	}

	if max < 1 {
		max = 1
	}

	size := int(math.Ceil((math.Log(float64(max)) / math.Log(2)) / 8))
	av.ed.Buffer = []rune(formatArray(array, av.mode != decMode, av.mode, false, size, 10))
}

type intViewer struct {
	v    *Variable
	mode numberMode
	ed   nucular.TextEditor
}

var intFormatter = map[numberMode]func(v string) string{
	decMode: func(v string) string {
		n, _ := strconv.ParseInt(v, 0, 64)
		return fmt.Sprintf("%d", n)
	},
	hexMode: func(v string) string {
		n, _ := strconv.ParseInt(v, 0, 64)
		return fmt.Sprintf("%#x", n)
	},
	octMode: func(v string) string {
		n, _ := strconv.ParseInt(v, 0, 64)
		return fmt.Sprintf("%#o", n)
	},
}

func newIntViewer(mw nucular.MasterWindow, v *Variable) {
	iv := &intViewer{v: v}
	switch {
	case strings.HasPrefix(v.Value, "0x"):
		iv.mode = hexMode
	case strings.HasPrefix(v.Value, "0") && v.Value != "0":
		iv.mode = octMode
	default:
		iv.mode = decMode
	}
	iv.ed.Flags = nucular.EditReadOnly | nucular.EditSelectable | nucular.EditClipboard
	iv.setupView()
	mw.PopupOpen("Viewing array: "+v.Name, dynamicPopupFlags, rect.Rect{100, 100, 500, 400}, true, iv.Update)
}

func (iv *intViewer) Update(w *nucular.Window) {
	w.Row(20).Static(100, 120, 120, 120)
	w.Label("View as:", "LC")
	mode := iv.mode
	if w.OptionText("Decimal", mode == decMode) {
		mode = decMode
	}
	if w.OptionText("Hexadecimal", mode == hexMode) {
		mode = hexMode
	}
	if w.OptionText("Octal", mode == octMode) {
		mode = octMode
	}
	if mode != iv.mode {
		iv.mode = mode
		iv.setupView()
	}

	w.Row(30).Dynamic(1)
	iv.ed.Edit(w)
	w.Row(20).Static(0, 100)
	w.Spacing(1)
	if w.ButtonText("OK") {
		iv.v.Value = string(iv.ed.Buffer)
		varFormat[iv.v.Addr] = intFormatter[iv.mode]
		w.Close()
	}
}

func (iv *intViewer) setupView() {
	iv.ed.Buffer = []rune(intFormatter[iv.mode](iv.v.Value))
}

func formatLocation2(loc api.Location) string {
	name := "(nil)"
	if loc.Function != nil {
		name = loc.Function.Name
	}
	return fmt.Sprintf("%s\nat %s:%d", name, ShortenFilePath(loc.File), loc.Line)
}
