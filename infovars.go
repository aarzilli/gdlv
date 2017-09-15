package main

import (
	"fmt"
	"image"
	"image/color"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/label"

	"github.com/derekparker/delve/service/api"
)

type numberMode int

const (
	decMode numberMode = iota
	hexMode
	octMode
)

type Variable struct {
	*api.Variable
	Width    int
	Value    string
	IntMode  numberMode
	FloatFmt string
	Varname  string
	loading  bool

	DisplayName, DisplayNameAddr string
	Expression                   string

	Children []*Variable
}

func wrapApiVariable(v *api.Variable, name, expr string) *Variable {
	r := &Variable{Variable: v}
	r.Value = v.Value
	r.Expression = expr
	if f := varFormat[v.Addr]; f != nil {
		f(r)
	} else if (v.Kind == reflect.Int || v.Kind == reflect.Uint) && ((v.Type == "uint8") || (v.Type == "int32")) {
		n, _ := strconv.Atoi(v.Value)
		r.Value = fmt.Sprintf("%s %q", v.Value, n)
	} else if f := conf.CustomFormatters[v.Type]; f != nil {
		f.Format(r)
	}

	if v.Type != "" {
		if name != "" {
			r.DisplayName = fmt.Sprintf("%s %s", name, v.Type)
		} else {
			r.DisplayName = v.Type
		}
	} else {
		r.DisplayName = name
	}

	r.DisplayNameAddr = fmt.Sprintf("%#x %s", v.Addr, r.DisplayName)

	r.Children = wrapApiVariables(v.Children, v.Kind, 0, r.Expression)

	if v.Kind == reflect.Interface {
		if len(r.Children) > 0 && r.Children[0].Kind == reflect.Ptr {
			if len(r.Children[0].Children) > 0 {
				r.Children[0].Children[0].DisplayName = r.Children[0].DisplayName
				r.Children[0].Children[0].DisplayNameAddr = r.Children[0].DisplayNameAddr
			}
		}
	}
	return r
}

func wrapApiVariables(vs []api.Variable, kind reflect.Kind, start int, expr string) []*Variable {
	r := make([]*Variable, 0, len(vs))

	const minInlineKeyValueLen = 20

	if kind == reflect.Map {
		for i := 0; i < len(vs); i += 2 {
			ok := false
			key, value := &vs[i], &vs[i+1]
			if len(key.Children) == 0 && len(key.Value) < minInlineKeyValueLen {
				var keyname string
				switch key.Kind {
				case reflect.String:
					keyname = fmt.Sprintf("[%q]", key.Value)
				case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Complex64, reflect.Complex128, reflect.Float32, reflect.Float64:
					keyname = fmt.Sprintf("[%s]", key.Value)
				}
				if keyname != "" {
					r = append(r, wrapApiVariable(value, keyname, ""))
					r = append(r, nil)
					ok = true
				}
			}

			if !ok {
				r = append(r, wrapApiVariable(key, fmt.Sprintf("[%d key]", start+i/2), ""))
				r = append(r, wrapApiVariable(value, fmt.Sprintf("[%d value]", start+i/2), ""))
			}
		}
		return r
	}

	for i := range vs {
		var childName, childExpr string
		switch kind {
		case reflect.Interface:
			childName = "data"
			childExpr = ""
		case reflect.Slice, reflect.Array:
			childName = fmt.Sprintf("[%d]", start+i)
			if expr != "" {
				childExpr = fmt.Sprintf("%s[%d]", expr, start+i)
			}
		case reflect.Ptr:
			childName = vs[i].Name
			if expr != "" {
				childExpr = fmt.Sprintf("(*(%s))", expr)
			}
		case reflect.Struct, reflect.Chan:
			childName = vs[i].Name
			if expr != "" {
				x := expr
				if strings.HasPrefix(x, "(*(") && strings.HasSuffix(x, "))") {
					x = x[3 : len(x)-2]
				}
				childExpr = fmt.Sprintf("%s.%s", x, vs[i].Name)
			}
		case 0:
			childName = vs[i].Name
			childExpr = vs[i].Name

		default:
			childName = vs[i].Name
			childExpr = ""
		}
		r = append(r, wrapApiVariable(&vs[i], childName, childExpr))
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
	globalsPanel.globals = wrapApiVariables(globals, 0, 0, "")
	sort.Sort(variablesByName(globalsPanel.globals))
	p.done(err)
}

func updateGlobals(container *nucular.Window) {
	w := globalsPanel.asyncLoad.showRequest(container, 0, "globals")
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
			showVariable(w, 0, globalsPanel.showAddr, -1, globals[i])
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
	localsPanel.args = wrapApiVariables(args, 0, 0, "")
	locals, errarg := client.ListLocalVariables(api.EvalScope{curGid, curFrame}, LongLoadConfig)
	for i := range locals {
		v := &locals[i]
		if v.Kind == reflect.Ptr && len(v.Name) > 1 && v.Name[0] == '&' && len(v.Children) > 0 {
			name := v.Name[1:]
			locals[i] = v.Children[0]
			locals[i].Name = name
		}
	}
	localsPanel.locals = wrapApiVariables(locals, 0, 0, "")

	for i := range localsPanel.expressions {
		loadOneExpr(i)
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
	varRowHeight    = 20
	varEditorHeight = 25
	posRowHeight    = 36
	moreBtnWidth    = 70
)

func updateLocals(container *nucular.Window) {
	w := localsPanel.asyncLoad.showRequest(container, 0, "locals")
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

	if w.TreePush(nucular.TreeTab, "Arguments", true) {
		for i := range args {
			if strings.Index(args[i].Name, filter) >= 0 {
				showVariable(w, 0, localsPanel.showAddr, -1, args[i])
			}
		}
		w.TreePop()
	}

	if w.TreePush(nucular.TreeTab, "Locals", true) {
		for i := range locals {
			if strings.Index(locals[i].Name, filter) >= 0 {
				showVariable(w, 0, localsPanel.showAddr, -1, locals[i])
			}
		}
		w.TreePop()
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
				showVariable(w, 0, localsPanel.showAddr, i, localsPanel.v[i])
			}
		}
	}

	if !editorShown {
		exprsEditor(true, w)
	}
}

func isPinned(expr string) bool {
	return expr[0] == '['
}

// parseFramePin parses a frame pin expression:
// [g 12 f -543] expr
// pins expr to goroutine 12 and frame -543
func parseFramePin(expr string) (pin bool, gid, frameOffset int, varExpr string) {
	if !isPinned(expr) {
		return false, 0, 0, expr
	}
	close := strings.Index(expr, "]")
	if close < 0 {
		return false, 0, 0, expr
	}
	pinexpr := strings.TrimSpace(expr[1:close])
	varExpr = strings.TrimSpace(expr[close+1:])

	if pinexpr[0] != 'g' {
		return false, 0, 0, expr
	}
	gidexpr := pinexpr[1:]
	fpos := strings.Index(gidexpr, "f")
	if fpos < 0 {
		return false, 0, 0, expr
	}
	pinexpr = gidexpr[fpos:]
	gidexpr = strings.TrimSpace(gidexpr[:fpos])
	var err error
	gid, err = strconv.Atoi(gidexpr)
	if err != nil {
		return false, 0, 0, expr
	}

	if pinexpr[0] != 'f' {
		return false, 0, 0, expr
	}
	pinexpr = strings.TrimSpace(pinexpr[1:])
	frameOffset, err = strconv.Atoi(pinexpr)
	if err != nil {
		return false, 0, 0, expr
	}
	pin = true
	return
}

func findFrameOffset(gid, frameOffset int) (frame int) {
	frames, err := client.Stacktrace(gid, 100, nil)
	if err != nil {
		return -1
	}

	for i := range frames {
		if frames[i].FrameOffset == int64(frameOffset) {
			return i
		}
	}
	return -1
}

func loadOneExpr(i int) {
	pin, gid, frameOffset, expr := parseFramePin(localsPanel.expressions[i])
	var frame int
	if !pin {
		gid = curGid
		frame = curFrame
	} else {
		frame = findFrameOffset(gid, frameOffset)
		if frame < 0 {
			localsPanel.v[i] = wrapApiVariable(&api.Variable{Unreadable: "could not find frame"}, "", "")
			return
		}
	}

	v, err := client.EvalVariable(api.EvalScope{gid, frame}, expr, LongLoadConfig)
	if err != nil {
		v = &api.Variable{Name: localsPanel.expressions[i], Unreadable: err.Error()}
	}
	v.Name = localsPanel.expressions[i]
	localsPanel.v[i] = wrapApiVariable(v, v.Name, v.Name)
}

func exprsEditor(isnew bool, w *nucular.Window) {
	if isnew {
		w.Row(varEditorHeight).Static(50, 0)
		w.Label("New: ", "LC")
		if w.Input().Mouse.HoveringRect(w.LastWidgetBounds) {
			w.Tooltip("Evaluate new expression")
		}
	} else {
		w.Row(varEditorHeight).Dynamic(1)
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

	if w.MenuItem(label.TA("Copy to clipboard", "LC")) {
		clipboard.Set(clipb)
	}

	if w.MenuItem(label.TA("Copy address to clipboard", "LC")) {
		clipboard.Set(fmt.Sprintf("%#x", v.Addr))
	}

	if v.Expression != "" {
		if w.MenuItem(label.TA("Add as expression", "LC")) {
			addExpression(v.Expression)

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

	switch v.Kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		mode := v.IntMode
		oldmode := mode
		if w.OptionText("Hexadecimal", mode == hexMode) {
			mode = hexMode
		}
		if w.OptionText("Octal", mode == octMode) {
			mode = octMode
		}
		if w.OptionText("Decimal", mode == decMode) {
			mode = decMode
		}
		if mode != oldmode {
			f := intFormatter[mode]
			varFormat[v.Addr] = f
			f(v)
			v.Width = 0
		}

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		mode := v.IntMode
		oldmode := mode
		if w.OptionText("Hexadecimal", mode == hexMode) {
			mode = hexMode
		}
		if w.OptionText("Octal", mode == octMode) {
			mode = octMode
		}
		if w.OptionText("Decimal", mode == decMode) {
			mode = decMode
		}
		if mode != oldmode {
			f := uintFormatter[mode]
			varFormat[v.Addr] = f
			f(v)
			v.Width = 0
		}

	case reflect.Float32, reflect.Float64:
		if w.MenuItem(label.TA("Format...", "LC")) {
			newFloatViewer(w, v)
		}
	}

	switch v.Type {
	case "bool", "int", "int8", "int16", "int32", "int64", "byte", "rune":
	case "uintptr", "uint", "uint8", "uint16", "uint32", "uint64":
	case "float32", "float64", "complex64", "complex128":
	case "string":
	default:
		if cfmt := conf.CustomFormatters[v.Type]; cfmt != nil {
			if w.MenuItem(label.TA("Edit custom formatter...", "LC")) {
				viewCustomFormatterMaker(w, v, cfmt.Fmtstr, cfmt.Argstr)
			}
			if w.MenuItem(label.TA("Remove custom formatter", "LC")) {
				delete(conf.CustomFormatters, v.Type)
				saveConfiguration()
				go refreshState(refreshToSameFrame, clearFrameSwitch, nil)
			}
		} else {
			if w.MenuItem(label.TA("Custom format for type...", "LC")) {
				viewCustomFormatterMaker(w, v, "", []string{})
			}
		}
	}

	if exprMenuIdx >= 0 && exprMenuIdx < len(localsPanel.expressions) {
		pinned := isPinned(localsPanel.expressions[exprMenuIdx])
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
		if w.CheckboxText("Pin to frame", &pinned) {
			if pinned && curFrame < len(stackPanel.stack) {
				localsPanel.expressions[exprMenuIdx] = fmt.Sprintf("[g %d f %d] %s", curGid, stackPanel.stack[curFrame].FrameOffset, localsPanel.expressions[exprMenuIdx])
			} else {
				_, _, _, localsPanel.expressions[exprMenuIdx] = parseFramePin(localsPanel.expressions[exprMenuIdx])
			}
			go func(i int) {
				additionalLoadMu.Lock()
				defer additionalLoadMu.Unlock()
				loadOneExpr(i)
			}(exprMenuIdx)
		}
	}

	switch v.Kind {
	case reflect.Slice, reflect.Array:
		if v.Addr != 0 {
			if w.MenuItem(label.TA("Find element...", "LC")) {
				viewFindElement(w, v)
			}
		}
	}

	if w.MenuItem(label.TA("Location...", "LC")) {
		out := editorWriter{&scrollbackEditor, false}
		fmt.Fprintf(&out, "location: %s\n", v.LocationExpr)
	}
}

func showVariable(w *nucular.Window, depth int, addr bool, exprMenu int, v *Variable) {
	name := v.DisplayName
	if addr {
		name = v.DisplayNameAddr
	}
	varname := v.Varname
	if varname == "" {
		varname = name
	}

	style := w.Master().Style()

	if v.Flags&api.VariableShadowed != 0 {
		savedStyle := *style
		defer func() {
			*style = savedStyle
		}()
		const darken = 0.75
		for _, p := range []*color.RGBA{&style.Text.Color, &style.Tab.NodeButton.TextNormal, &style.Tab.NodeButton.TextHover, &style.Tab.NodeButton.TextActive, &style.Tab.Text} {
			p.R = uint8(float64(p.R) * darken)
			p.G = uint8(float64(p.G) * darken)
			p.B = uint8(float64(p.B) * darken)
		}
	}

	const maxWidth = 4096

	hdrCollapsedName := func() string {
		if v.Value != "" {
			return name + " = " + v.Value
		}
		return name + " = " + v.SinglelineStringNoType()
	}

	hdr := func() bool {
		if v.Width == 0 {
			v.Width = nucular.FontWidth(style.Font, hdrCollapsedName()) + nucular.FontHeight(style.Font) + style.Tab.Padding.X*3 + style.GroupWindow.Padding.X*2 + style.Tab.NodeButton.Padding.X*2 + style.Tab.NodeButton.Border*2
			if !addr {
				v.Width += nucular.FontWidth(style.Font, fmt.Sprintf("%#x ", v.Addr))
			}
			if v.Width > maxWidth {
				v.Width = maxWidth
			}
		}
		w.LayoutSetWidthScaled(v.Width)
		if !w.TreeIsOpen(varname) {
			name = hdrCollapsedName()
		}
		r := w.TreePushNamed(nucular.TreeNode, varname, name, false)
		showExprMenu(w, exprMenu, v, name)
		return r
	}

	cblbl := func(fmtstr string, args ...interface{}) {
		s := fmt.Sprintf(fmtstr, args...)
		if v.Width == 0 {
			v.Width = nucular.FontWidth(style.Font, s) + style.Text.Padding.X*2
			if !addr {
				v.Width += nucular.FontWidth(style.Font, fmt.Sprintf("%#x ", v.Addr))
			}
			if v.Width > maxWidth {
				v.Width = maxWidth
			}
		}
		w.LayoutSetWidthScaled(w.Master().Style().Tab.Indent)
		w.Spacing(1)
		w.LayoutSetWidthScaled(v.Width)
		w.Label(s, "LC")
		showExprMenu(w, exprMenu, v, s)
	}

	dynlbl := func(s string) {
		w.Row(varRowHeight).Dynamic(1)
		w.Label(s, "LC")
	}

	w.Row(varRowHeight).Static()
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
		if hdr() {
			dynlbl(fmt.Sprintf("len: %d cap: %d", v.Len, v.Cap))
			showArrayOrSliceContents(w, depth, addr, v)
			w.TreePop()
		}
	case reflect.Array:
		if hdr() {
			dynlbl(fmt.Sprintf("len: %d", v.Len))
			showArrayOrSliceContents(w, depth, addr, v)
			w.TreePop()
		}
	case reflect.Ptr:
		if len(v.Children) == 0 {
			cblbl("%s ?", name)
		} else if v.Type == "" || v.Children[0].Addr == 0 {
			cblbl("%s = nil", name)
		} else {
			if hdr() {
				if v.Children[0].OnlyAddr {
					loadMoreStruct(v.Children[0])
					dynlbl("Loading...")
				} else {
					showVariable(w, depth+1, addr, -1, v.Children[0])
				}
				w.TreePop()
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
			if hdr() {
				showStructContents(w, depth, addr, v)
				w.TreePop()
			}
		}
	case reflect.Struct:
		if hdr() {
			if int(v.Len) != len(v.Children) && len(v.Children) == 0 {
				loadMoreStruct(v)
				dynlbl("Loading...")
			} else {
				showStructContents(w, depth, addr, v)
			}
			w.TreePop()
		}
	case reflect.Interface:
		if v.Children[0].Kind == reflect.Invalid {
			cblbl("%s = nil", name)
		} else {
			if hdr() {
				showInterfaceContents(w, depth, addr, v)
				w.TreePop()
			}
		}
	case reflect.Map:
		if hdr() {
			if depth < 10 && !v.loading && len(v.Children) > 0 && autoloadMore(v.Children[0]) {
				v.loading = true
				loadMoreStruct(v)
			}
			for i := range v.Children {
				if v.Children[i] != nil {
					showVariable(w, depth+1, addr, -1, v.Children[i])
				}
			}
			if len(v.Children)/2 != int(v.Len) && v.Addr != 0 {
				w.Row(varRowHeight).Static(moreBtnWidth)
				if w.ButtonText(fmt.Sprintf("%d more", int(v.Len)-(len(v.Children)/2))) {
					loadMoreMap(v)
				}
			}
			w.TreePop()
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
	if depth < 10 && !v.loading && len(v.Children) > 0 && autoloadMore(v.Children[0]) {
		v.loading = true
		loadMoreStruct(v)
	}
	for i := range v.Children {
		showVariable(w, depth+1, addr, -1, v.Children[i])
	}
	if len(v.Children) != int(v.Len) && v.Addr != 0 {
		w.Row(varRowHeight).Static(moreBtnWidth)
		if w.ButtonText(fmt.Sprintf("%d more", int(v.Len)-len(v.Children))) {
			loadMoreArrayOrSlice(v)
		}
	}
}

func autoloadMore(v *Variable) bool {
	if v.OnlyAddr {
		return true
	}
	if v.Kind == reflect.Struct && len(v.Children) == 0 {
		return true
	}
	if v.Kind == reflect.Ptr && len(v.Children) == 1 && v.Children[0].OnlyAddr {
		return true
	}
	return false
}

func showStructContents(w *nucular.Window, depth int, addr bool, v *Variable) {
	for i := range v.Children {
		showVariable(w, depth+1, addr, -1, v.Children[i])
	}
}

func showInterfaceContents(w *nucular.Window, depth int, addr bool, v *Variable) {
	if len(v.Children) <= 0 {
		return
	}
	data := v.Children[0]
	if data.OnlyAddr {
		loadMoreStruct(v)
		w.Row(varRowHeight).Dynamic(1)
		w.Label("Loading...", "LC")
		return
	}
	if data.Kind == reflect.Ptr {
		if len(data.Children) <= 0 {
			loadMoreStruct(v)
			w.Row(varRowHeight).Dynamic(1)
			w.Label("Loading...", "LC")
			return
		}
		data = data.Children[0]
	}

	switch data.Kind {
	case reflect.Struct:
		w.Row(varRowHeight).Dynamic(1)
		w.Label("concrete type: "+v.Children[0].Type, "LC")
		showStructContents(w, depth, addr, data)
	case reflect.Array, reflect.Slice:
		w.Row(varRowHeight).Dynamic(1)
		w.Label("concrete type: "+v.Children[0].Type, "LC")
		showArrayOrSliceContents(w, depth, addr, data)
	default:
		showVariable(w, depth+1, addr, -1, data)
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
			lv, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, expr, LongArrayLoadConfig)
			if err != nil {
				out := editorWriter{&scrollbackEditor, true}
				fmt.Fprintf(&out, "Error loading array contents %s: %v\n", expr, err)
				// prevent further attempts at loading
				v.Len = int64(len(v.Children) / 2)
			} else {
				v.Children = append(v.Children, wrapApiVariables(lv.Children, reflect.Map, len(v.Children), v.Expression)...)
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
			lv, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, expr, LongArrayLoadConfig)
			if err != nil {
				out := editorWriter{&scrollbackEditor, true}
				fmt.Fprintf(&out, "Error loading array contents %s: %v\n", expr, err)
				// prevent further attempts at loading
				v.Len = int64(len(v.Children))
			} else {
				v.Children = append(v.Children, wrapApiVariables(lv.Children, v.Kind, len(v.Children), v.Expression)...)
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
				dn := v.DisplayName
				dna := v.DisplayNameAddr
				lv.Name = v.Name
				*v = *wrapApiVariable(lv, lv.Name, v.Expression)
				v.DisplayName = dn
				v.DisplayNameAddr = dna
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
	}
	return nil
}
