package main

import (
	"fmt"
	"image"
	"image/color"
	"os/exec"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aarzilli/nucular/font"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/clipboard"
	ncommand "github.com/aarzilli/nucular/command"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"
	nstyle "github.com/aarzilli/nucular/style"
	"golang.org/x/mobile/event/mouse"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/prettyprint"
)

type numberMode int

const (
	decMode numberMode = iota
	hexMode
	octMode
	binMode
)

var changedVariableOpacity uint8

const maxChangedVariableOpacity = 0xd0
const minChangedVariableOpacity = 0x10

var drawStartTime time.Time

type Variable struct {
	fnname string
	*api.Variable
	Width   int
	Value   string
	loading bool
	Varname string

	ShortType   string
	DisplayName string
	Expression  string

	changed bool

	Children []*Variable

	ed   *nucular.TextEditor
	sfmt *prettyprint.SimpleFormat
}

// SinglelineString returns a representation of v on a single line.
func (v *Variable) SinglelineString(includeType, fullTypes bool) string {
	return prettyprint.Singleline(v.Variable, includeType, fullTypes)
}

// MultilineString returns a representation of v on multiple lines.
func (v *Variable) MultilineString(indent string, sfmt *prettyprint.SimpleFormat) string {
	return prettyprint.Multiline(v.Variable, indent, sfmt)
}

func wrapApiVariableSimple(v *api.Variable) *Variable {
	return wrapApiVariable("", v, v.Name, v.Name, false, nil, 0)
}

func wrapApiVariable(fnname string, v *api.Variable, name, expr string, customFormatters bool, sfmt *prettyprint.SimpleFormat, depth int) *Variable {
	r := &Variable{Variable: v}
	r.Value = v.Value
	r.Expression = expr
	if r.Expression == "" {
		r.Expression = fmt.Sprintf("(*(*%q)(%#x))", v.Type, v.Addr)
	}

	if name != "" {
		r.DisplayName = name
	} else {
		r.DisplayName = v.Type
	}

	r.ShortType = prettyprint.ShortenType(v.Type)

	r.Varname = r.DisplayName

	r.Children = wrapApiVariables("", v.Children, v.Kind, 0, r.Expression, customFormatters, sfmt, depth+1)

	if v.Kind == reflect.Interface {
		if len(r.Children) > 0 && r.Children[0].Kind == reflect.Ptr {
			if len(r.Children[0].Children) > 0 {
				r.Children[0].Children[0].DisplayName = r.Children[0].DisplayName
			}
		}
	}

	r.fnname = fnname
	if sfmt != nil && *sfmt != (prettyprint.SimpleFormat{}) {
		r.Value = sfmt.Apply(v)
	} else if f := varFormat[getVarFormatKey(r)]; f != (prettyprint.SimpleFormat{}) {
		sfmt = &f
		r.Value = sfmt.Apply(v)
	} else if (v.Kind == reflect.Int || v.Kind == reflect.Uint) && ((v.Type == "uint8") || (v.Type == "int32")) {
		n, _ := strconv.Atoi(v.Value)
		if n >= ' ' && n <= '~' {
			r.Value = fmt.Sprintf("%s %q", v.Value, n)
		}
	} else if f := conf.CustomFormatters[v.Type]; f != nil && customFormatters && depth < 10 {
		f.Format(r)
	} else if v.Type == "time.Time" {
		r.Value = formatTime(v)
	}

	r.sfmt = sfmt
	if r.sfmt == nil {
		r.sfmt = &prettyprint.SimpleFormat{}
	}

	return r
}

func formatTime(v *api.Variable) string {
	const (
		timeTimeWallHasMonotonicBit uint64        = (1 << 63)                                                  // hasMonotonic bit of time.Time.wall
		maxAddSeconds               time.Duration = (time.Duration(^uint64(0)>>1) / time.Second) * time.Second // maximum number of seconds that can be added with (time.Time).Add, measured in nanoseconds
		wallNsecShift                             = 30                                                         // size of the nanoseconds field of time.Time.wall
		unixTimestampOfWallEpoch                  = -2682288000                                                // number of seconds between the unix epoch and the epoch for time.Time.wall (1 jan 1885)
	)

	wallv := fieldVariable(v, "wall")
	extv := fieldVariable(v, "ext")
	if wallv == nil || extv == nil || wallv.Unreadable != "" || extv.Unreadable != "" || wallv.Value == "" || extv.Value == "" {
		return v.Value
	}
	wall, err1 := strconv.ParseUint(wallv.Value, 10, 64)
	ext, err2 := strconv.ParseInt(extv.Value, 10, 64)
	if err1 != nil || err2 != nil {
		return v.Value
	}
	_ = ext
	hasMonotonic := (wall & timeTimeWallHasMonotonicBit) != 0
	if hasMonotonic {
		// the 33-bit field of wall holds a 33-bit unsigned wall
		// seconds since Jan 1 year 1885, and ext holds a signed 64-bit monotonic
		// clock reading, nanoseconds since process start
		sec := int64(wall << 1 >> (wallNsecShift + 1)) // seconds since 1 Jan 1885
		t := time.Unix(sec+unixTimestampOfWallEpoch, 0).UTC()
		return fmt.Sprintf("time.Time(%s, %+d)", t.Format(time.RFC3339), ext)
	} else {
		// the full signed 64-bit wall seconds since Jan 1 year 1 is stored in ext
		var t time.Time
		for ext > int64(maxAddSeconds/time.Second) {
			t = t.Add(maxAddSeconds)
			ext -= int64(maxAddSeconds / time.Second)
		}
		t = t.Add(time.Duration(ext) * time.Second)
		return t.Format(time.RFC3339)
	}
}

func fieldVariable(v *api.Variable, name string) *api.Variable {
	for i := range v.Children {
		if v.Children[i].Name == name {
			return &v.Children[i]
		}
	}
	return nil
}

func wrapApiVariables(fnname string, vs []api.Variable, kind reflect.Kind, start int, expr string, customFormatters bool, sfmt *prettyprint.SimpleFormat, depth int) []*Variable {
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
					value.Name = keyname[1 : len(keyname)-1]
					r = append(r, wrapApiVariable("", value, keyname, "", customFormatters, sfmt, depth))
					r = append(r, nil)
					ok = true
				}
			}

			if !ok {
				r = append(r, wrapApiVariable("", key, fmt.Sprintf("[%d key]", start+i/2), "", customFormatters, sfmt, depth))
				r = append(r, wrapApiVariable("", value, fmt.Sprintf("[%d value]", start+i/2), "", customFormatters, sfmt, depth))
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
		r = append(r, wrapApiVariable(fnname, &vs[i], childName, childExpr, customFormatters, sfmt, depth))
	}
	return r
}

var globalsPanel = struct {
	asyncLoad    asyncLoad
	filterEditor nucular.TextEditor
	showAddr     bool
	fullTypes    bool
	globals      []*Variable
}{
	filterEditor: nucular.TextEditor{Filter: spacefilter},
}

var localsPanel = struct {
	asyncLoad    asyncLoad
	filterEditor nucular.TextEditor
	showAddr     bool
	fullTypes    bool
	locals       []*Variable

	expressions []Expr
	selected    int
	ed          nucular.TextEditor
	v           []*Variable
}{
	filterEditor: nucular.TextEditor{Filter: spacefilter},
	selected:     -1,
	ed:           nucular.TextEditor{Flags: nucular.EditSelectable | nucular.EditSigEnter | nucular.EditClipboard},
}

type Expr struct {
	Expr                         string
	maxArrayValues, maxStringLen int
	traced                       bool
}

func loadGlobals(p *asyncLoad) {
	globals, err := client.ListPackageVariables("", getVariableLoadConfig())
	globalsPanel.globals = wrapApiVariables("", globals, 0, 0, "", true, nil, 0)
	sort.Sort(variablesByName(globalsPanel.globals))
	p.done(err)
}

func updateGlobals(container *nucular.Window) {
	if container.HelpClicked {
		showHelp(container.Master(), "Globals Panel Help", globalsPanelHelp)
	}
	w := globalsPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	additionalLoadMu.Lock()
	defer additionalLoadMu.Unlock()

	w.MenubarBegin()
	w.Row(varRowHeight).Static(90, 0, 110, 110)
	w.Label("Filter:", "LC")
	globalsPanel.filterEditor.Edit(w)
	filter := string(globalsPanel.filterEditor.Buffer)
	w.CheckboxText("Full Types", &globalsPanel.fullTypes)
	w.CheckboxText("Address", &globalsPanel.showAddr)
	w.MenubarEnd()

	globals := globalsPanel.globals

	for i := range globals {
		if strings.Index(globals[i].Name, filter) >= 0 {
			showVariable(w, 0, newShowVariableFlags(globalsPanel.showAddr, globalsPanel.fullTypes), -1, globals[i])
		}
	}
}

type variablesByName []*Variable

func (vars variablesByName) Len() int           { return len(vars) }
func (vars variablesByName) Swap(i, j int)      { vars[i], vars[j] = vars[j], vars[i] }
func (vars variablesByName) Less(i, j int) bool { return vars[i].Name < vars[j].Name }

func loadLocals(p *asyncLoad) {
	changedVariableOpacity = maxChangedVariableOpacity
	oldlocals := append([]*Variable{}, localsPanel.locals...)
	oldv := append([]*Variable{}, localsPanel.v...)
	drawStartTime = time.Now()

	scope := currentEvalScope()

	frames, _ := client.Stacktrace(scope.GoroutineID, 1, 0, nil)
	var fnname = ""
	if len(frames) > 0 {
		fnname = frames[0].Function.Name()
	}

	args, errloc := client.ListFunctionArgs(scope, getVariableLoadConfig())
	localsPanel.locals = wrapApiVariables(fnname, args, 0, 0, "", true, nil, 0)
	locals, errarg := client.ListLocalVariables(scope, getVariableLoadConfig())
	localsPanel.locals = append(localsPanel.locals, wrapApiVariables(fnname, locals, 0, 0, "", true, nil, 0)...)

	sort.SliceStable(localsPanel.locals, func(i, j int) bool { return localsPanel.locals[i].DeclLine < localsPanel.locals[j].DeclLine })

	varmap := map[string]int{}

	for i := range localsPanel.locals {
		varname := localsPanel.locals[i].Varname
		d := varmap[varname]
		localsPanel.locals[i].Varname += fmt.Sprintf(" %d", d)
		d++
		varmap[varname] = d
	}

	markChangedVariables(localsPanel.locals, oldlocals)

	var scrollbackOut = editorWriter{true}
	for i := range localsPanel.expressions {
		loadOneExpr(i)
		if localsPanel.expressions[i].traced {
			fmt.Fprintf(&scrollbackOut, "%s = %s\n", localsPanel.v[i].Name, localsPanel.v[i].SinglelineString(true, false))
		}
	}

	markChangedVariables(localsPanel.v, oldv)

	if LogOutputNice != nil {
		logf("Local variables (%#v):\n", currentEvalScope())
		for i := range localsPanel.locals {
			fmt.Fprintf(LogOutputNice, "\t%s = %s\n", localsPanel.locals[i].Name, localsPanel.locals[i].MultilineString("\t", nil))
		}
		for i := range localsPanel.v {
			fmt.Fprintf(LogOutputNice, "\t%s = %s\n", localsPanel.v[i].Name, localsPanel.v[i].MultilineString("\t", nil))
		}
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
	if container.HelpClicked {
		showHelp(container.Master(), "Locals Panel Help", localsPanelHelp)
	}
	w := localsPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	additionalLoadMu.Lock()
	defer additionalLoadMu.Unlock()

	w.MenubarBegin()
	w.Row(varRowHeight).Static(90, 0, 110, 110)
	w.Label("Filter:", "LC")
	localsPanel.filterEditor.Edit(w)
	filter := string(localsPanel.filterEditor.Buffer)
	w.CheckboxText("Full Types", &localsPanel.fullTypes)
	w.CheckboxText("Address", &localsPanel.showAddr)
	w.MenubarEnd()

	locals := localsPanel.locals

	if len(localsPanel.expressions) > 0 {
		if w.TreePush(nucular.TreeTab, "Expression", true) {
			for i := 0; i < len(localsPanel.expressions); i++ {
				if i == localsPanel.selected {
					exprsEditor(w)
				} else {
					if localsPanel.v[i] == nil {
						w.Row(varRowHeight).Dynamic(1)
						w.Label(fmt.Sprintf("loading %s", localsPanel.expressions[i].Expr), "LC")
					} else {
						showVariable(w, 0, newShowVariableFlags(localsPanel.showAddr, localsPanel.fullTypes), i, localsPanel.v[i])
					}
				}
			}
			w.TreePop()
		}
	}

	if len(locals) > 0 {
		if w.TreePush(nucular.TreeTab, "Local variables and arguments", true) {
			for i := range locals {
				if strings.Index(locals[i].Name, filter) >= 0 {
					showVariable(w, 0, newShowVariableFlags(localsPanel.showAddr, localsPanel.fullTypes), -1, locals[i])
				}
			}
			w.TreePop()
		}
	}

	if changedVariableOpacity > minChangedVariableOpacity {
		opacityReductionPerMillisecond := float64(maxChangedVariableOpacity-minChangedVariableOpacity) / 1500
		elapsed := time.Since(drawStartTime)
		elapsedms := elapsed.Nanoseconds() / 1e6
		changedVariableOpacity = maxChangedVariableOpacity - byte(float64(elapsedms)*opacityReductionPerMillisecond)
		if changedVariableOpacity > maxChangedVariableOpacity || changedVariableOpacity < minChangedVariableOpacity {
			changedVariableOpacity = minChangedVariableOpacity
		}
		w.Master().Changed()
	}
}

func isPinned(expr string) bool {
	return expr[0] == '['
}

func loadOneExpr(i int) {
	cfg := getVariableLoadConfig()
	if localsPanel.expressions[i].maxArrayValues > 0 {
		cfg.MaxArrayValues = localsPanel.expressions[i].maxArrayValues
		cfg.MaxStringLen = localsPanel.expressions[i].maxStringLen
	}

	localsPanel.v[i], _ = evalScopedExpr(localsPanel.expressions[i].Expr, cfg, true)

	localsPanel.v[i].Name = localsPanel.expressions[i].Expr
	localsPanel.v[i].DisplayName = localsPanel.expressions[i].Expr
}

func exprsEditor(w *nucular.Window) {
	w.Row(varEditorHeight).Dynamic(1)
	active := localsPanel.ed.Edit(w)
	if active&nucular.EditCommitted == 0 {
		return
	}
	if localsPanel.selected < 0 {
		return
	}

	localsPanel.expressions[localsPanel.selected].Expr = string(localsPanel.ed.Buffer)
	go func(i int) {
		additionalLoadMu.Lock()
		defer additionalLoadMu.Unlock()
		loadOneExpr(i)
	}(localsPanel.selected)
	localsPanel.selected = -1
}

func addExpression(newexpr string) {
	wnd.Lock()
	localsPanel.expressions = append(localsPanel.expressions, Expr{Expr: newexpr})
	localsPanel.v = append(localsPanel.v, nil)
	wnd.Unlock()
	i := len(localsPanel.v) - 1
	go func(i int) {
		additionalLoadMu.Lock()
		defer additionalLoadMu.Unlock()
		loadOneExpr(i)
	}(i)
}

func editExpression(exprMenuIdx int) {
	localsPanel.selected = exprMenuIdx
	localsPanel.ed.Buffer = []rune(localsPanel.expressions[localsPanel.selected].Expr)
	localsPanel.ed.Cursor = len(localsPanel.ed.Buffer)
	localsPanel.ed.SelectStart = localsPanel.ed.Cursor
	localsPanel.ed.SelectEnd = localsPanel.ed.Cursor
	localsPanel.ed.CursorFollow = true
	localsPanel.ed.Active = true
	commandLineEditor.Active = false
}

func showExprMenu(parentw *nucular.Window, exprMenuIdx int, v *Variable, clipb []byte) {
	if client.Running() {
		return
	}
	if parentw.LastWidgetBounds.W > 0 && parentw.LastWidgetBounds.H > 0 {
		if parentw.Input().Mouse.Clicked(mouse.ButtonMiddle, parentw.LastWidgetBounds) {
			editExpression(exprMenuIdx)
			return
		}
	}
	w := parentw.ContextualOpen(0, image.Point{}, parentw.LastWidgetBounds, nil)
	if w == nil {
		return
	}
	w.Row(20).Dynamic(1)
	isExpression := exprMenuIdx >= 0 && exprMenuIdx < len(localsPanel.expressions)
	if isExpression {
		if w.MenuItem(label.TA("Edit expression", "LC")) {
			editExpression(exprMenuIdx)
		}
		if w.MenuItem(label.TA("Remove expression", "LC")) {
			removeExpression(exprMenuIdx)
			return
		}
		if w.MenuItem(label.TA("Load parameters...", "LC")) {
			w.Master().PopupOpen(fmt.Sprintf("Load parameters for %s", localsPanel.expressions[exprMenuIdx].Expr), dynamicPopupFlags, rect.Rect{100, 100, 400, 700}, true, configureLoadParameters(exprMenuIdx))
		}
		pinned := exprIsScoped(localsPanel.expressions[exprMenuIdx].Expr)
		if w.CheckboxText("Pin to frame", &pinned) {
			se := ParseScopedExpr(localsPanel.expressions[exprMenuIdx].Expr)
			if pinned && curFrame < len(stackPanel.stack) {
				se.Kind = FrameOffsetScopeExpr
				se.Gid = curGid
				se.Foff = int(stackPanel.stack[curFrame].FrameOffset)
				se.DeferredCall = curDeferredCall
			} else {
				se.Kind = NormalScopeExpr
				se.Gid = -1
				se.Foff = -1
				se.Fid = -1
				se.DeferredCall = -1
			}
			localsPanel.expressions[exprMenuIdx].Expr = se.String()
			go func(i int) {
				additionalLoadMu.Lock()
				defer additionalLoadMu.Unlock()
				loadOneExpr(i)
			}(exprMenuIdx)
		}
		w.CheckboxText("Traced", &localsPanel.expressions[exprMenuIdx].traced)
		w.MenuItem(label.TA("---", "CC"))
	}
	if isExpression {
		if w.MenuItem(label.TA("Details", "LC")) {
			newDetailViewer(w.Master(), localsPanel.expressions[exprMenuIdx].Expr)
			removeExpression(exprMenuIdx)
			return
		}
	} else {
		if w.MenuItem(label.TA("Details", "LC")) {
			newDetailViewer(w.Master(), v.Expression)
		}
	}

	if v.Kind == reflect.Chan {
		if w.MenuItem(label.TA("Channel goroutines", "LC")) {
			go chanGoroutines(v)
		}
	}

	if w.MenuItem(label.TA("Copy to clipboard", "LC")) {
		clipboard.Set(string(clipb))
	}

	if w.MenuItem(label.TA("Go doc", "LC")) {
		go goDocCommand(v.Type)
	}

	if v.Kind == reflect.Interface && len(v.Children) > 0 {
		if w.MenuItem(label.TA("Go doc (concrete type)", "LC")) {
			go goDocCommand(v.Children[0].Type)
		}
	}

	if !isExpression && v.Expression != "" {
		if w.MenuItem(label.TA("Add as expression", "LC")) {
			go addExpression(v.Expression)
		}
	}

	if v.Kind == reflect.Func {
		if w.MenuItem(label.TA("Go to definition", "LC")) {
			locs, _, err := client.FindLocation(currentEvalScope(), fmt.Sprintf("*%#x", v.Base), true, nil)
			if err == nil && len(locs) == 1 {
				listingPanel.pinnedLoc = &locs[0]
				go refreshState(refreshToSameFrame, clearNothing, nil)
			}
		}
	}

	setVarFormat := func(f *prettyprint.SimpleFormat) {
		if exprMenuIdx >= 0 && exprMenuIdx < len(localsPanel.expressions) {
			expr := ParseScopedExpr(localsPanel.expressions[exprMenuIdx].Expr)
			expr.Fmt = *f
			localsPanel.expressions[exprMenuIdx].Expr = expr.String()
		} else {
			varFormat[getVarFormatKey(v)] = *f
		}
	}

	switch v.Kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		fallthrough
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		mode := v.sfmt.IntFormat
		if w.OptionText("Hexadecimal", v.sfmt.IntFormat == "%#x") {
			v.sfmt.IntFormat = "%#x"
		}
		if w.OptionText("Octal", v.sfmt.IntFormat == "%#o") {
			v.sfmt.IntFormat = "%#o"
		}
		if w.OptionText("Binary", v.sfmt.IntFormat == "%#b") {
			v.sfmt.IntFormat = "%#b"
		}
		if w.OptionText("Decimal", v.sfmt.IntFormat == "" || v.sfmt.IntFormat == "%d") {
			v.sfmt.IntFormat = ""
		}
		if mode != v.sfmt.IntFormat {
			setVarFormat(v.sfmt)
			v.Value = v.sfmt.Apply(v.Variable)
			v.Width = 0
		}

	case reflect.Float32, reflect.Float64:
		if w.MenuItem(label.TA("Format...", "LC")) {
			newFloatViewer(w, v, setVarFormat)
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

	if w.MenuItem(label.TA("Location...", "LC")) {
		out := editorWriter{false}
		fmt.Fprintf(&out, "location of %q at %#x: %s\n", v.Name, curPC, v.LocationExpr)
	}
}

func removeExpression(exprMenuIdx int) {
	if exprMenuIdx+1 < len(localsPanel.expressions) {
		copy(localsPanel.expressions[exprMenuIdx:], localsPanel.expressions[exprMenuIdx+1:])
		copy(localsPanel.v[exprMenuIdx:], localsPanel.v[exprMenuIdx+1:])
	}
	localsPanel.expressions = localsPanel.expressions[:len(localsPanel.expressions)-1]
	localsPanel.v = localsPanel.v[:len(localsPanel.v)-1]
}

func variableHeader(w *nucular.Window, flags showVariableFlags, exprMenu int, v *Variable) bool {
	fullTypes := flags.fullTypes()
	style := w.Master().Style()

	w.LayoutSetWidthScaled(w.LayoutAvailableWidth())
	initialOpen := false
	if flags.parentIsPtr() || flags.alwaysExpand() {
		initialOpen = true
	}
	lblrect, out, isopen := w.TreePushCustom(nucular.TreeNode, v.Varname, initialOpen)
	if out == nil {
		return isopen
	}
	lblrect.W = w.Bounds.W
	if v.changed {
		out.FillRect(lblrect, 0, changedVariableColor())
	}

	clipb := []byte{}

	print := func(str string, font font.Face) {
		clipb = append(clipb, []byte(str)...)
		clipb = append(clipb, ' ')
		out.DrawText(lblrect, str, font, style.Tab.Text)
		width := nucular.FontWidth(font, str) + spaceWidth
		lblrect.X += width
		lblrect.W -= width
	}

	if flags.withAddr() {
		print(fmt.Sprintf("%#x", v.Addr), style.Font)
	}
	if isopen {
		print(v.DisplayName, boldFace)

		switch v.Kind {
		case reflect.Slice:
			print(getDisplayType(v, fullTypes), style.Font)
			print(fmt.Sprintf("(len: %d cap: %d)", v.Len, v.Cap), style.Font)
		case reflect.Interface:
			if len(v.Children) > 0 && v.Children[0] != nil {
				print(fmt.Sprintf("%s (%v)", getDisplayType(v, fullTypes), getDisplayType(v.Children[0], fullTypes)), style.Font)
			} else {
				print(v.Type, style.Font)
			}
		default:
			print(getDisplayType(v, fullTypes), style.Font)
		}
	} else {
		print(v.DisplayName, boldFace)
		print(getDisplayType(v, fullTypes), style.Font)
		if v.Value != "" && v.Kind != reflect.Ptr {
			print("= "+v.Value, style.Font)
		} else {
			print("= "+v.SinglelineString(false, fullTypes), style.Font)
		}
	}
	showExprMenu(w, exprMenu, v, clipb)
	return isopen
}

func variableNoHeaderSpacing(w *nucular.Window, n int) int {
	style := w.Master().Style()
	symX := style.Tab.Padding.X
	symW := nucular.FontHeight(style.Font)
	item_spacing := style.NormalWindow.Spacing
	return (n + 1) * (symX + symW + item_spacing.X + 2*style.Tab.Spacing.X)
}

func variableNoHeader(w *nucular.Window, flags showVariableFlags, exprMenu int, v *Variable, value string, wrap bool) {
	fullTypes := flags.fullTypes()
	style := w.Master().Style()
	var lblrect rect.Rect
	var out *ncommand.Buffer
	start := func() {
		w.LayoutSetWidthScaled(variableNoHeaderSpacing(w, 0))
		w.Spacing(1)
		w.LayoutSetWidthScaled(w.LayoutAvailableWidth())
		lblrect, out = w.Custom(nstyle.WidgetStateActive)
	}

	changed := func() {
		if v.changed && out != nil {
			out.FillRect(lblrect, 0, changedVariableColor())
		}
	}

	start()
	if out == nil {
		return
	}
	changed()

	lblrect.Y += style.Text.Padding.Y

	clipb := []byte{}

	print := func(str string, font font.Face) {
		clipb = append(clipb, []byte(str)...)
		clipb = append(clipb, ' ')
		if out != nil {
			out.DrawText(lblrect, str, font, style.Text.Color)
		}
		width := nucular.FontWidth(font, str) + spaceWidth
		lblrect.X += width
		lblrect.W -= width
	}

	if flags.withAddr() {
		print(fmt.Sprintf("%#x", v.Addr), style.Font)
	}
	print(v.DisplayName, boldFace)
	print(getDisplayType(v, fullTypes), style.Font)
	print("= ", style.Font)

	valuex := lblrect.X
	valuew := lblrect.W

	clipb = append(clipb, []byte(value)...)
	clipb = append(clipb, ' ')

	textClamp := func(text []rune, space int) []rune {
		if !wrap || nucular.FontWidth(style.Font, string(text)) < space {
			return text
		}
		text_width := 0
		for i, ch := range text {
			xw := nucular.FontWidth(style.Font, string(ch))
			if text_width+xw >= space {
				if i == 0 {
					i = 1
				}
				return text[:i]
			}
			text_width += xw
		}
		return text
	}

	valstr := []rune(value)
	first := true
	for len(valstr) > 0 {
		if !first {
			showExprMenu(w, exprMenu, v, clipb)
			w.Row(varRowHeight).Static()
			start()
			if out == nil {
				return
			}
			changed()
			lblrect.X = valuex
			lblrect.W = valuew
		}
		cur := textClamp(valstr, lblrect.W)
		valstr = valstr[len(cur):]
		out.DrawText(lblrect, string(cur), style.Font, style.Text.Color)
		first = false
	}

	showExprMenu(w, exprMenu, v, clipb)
}

func getDisplayType(v *Variable, fullTypes bool) string {
	if fullTypes {
		return v.Type
	}
	return v.ShortType
}

func darken(p *color.RGBA) {
	const darken = 0.5
	p.A = uint8(float64(p.A) * darken)
	p.R = uint8(float64(p.R) * darken)
	p.G = uint8(float64(p.G) * darken)
	p.B = uint8(float64(p.B) * darken)
}

type showVariableFlags uint8

const (
	showVariableFullTypes showVariableFlags = 1 << iota
	showVariableWithAddr
	showVariableParentIsPtr
	showVariableAlwaysExpand
)

func newShowVariableFlags(showAddr, fullTypes bool) (r showVariableFlags) {
	if showAddr {
		r |= showVariableWithAddr
	}
	if fullTypes {
		r |= showVariableFullTypes
	}
	return r
}

func (flags showVariableFlags) fullTypes() bool    { return (flags & showVariableFullTypes) != 0 }
func (flags showVariableFlags) withAddr() bool     { return (flags & showVariableWithAddr) != 0 }
func (flags showVariableFlags) parentIsPtr() bool  { return (flags & showVariableParentIsPtr) != 0 }
func (flags showVariableFlags) alwaysExpand() bool { return (flags & showVariableAlwaysExpand) != 0 }

func showVariable(w *nucular.Window, depth int, flags showVariableFlags, exprMenu int, v *Variable) {
	style := w.Master().Style()

	if v.Flags&api.VariableShadowed != 0 || v.Unreadable != "" {
		savedStyle := *style
		defer func() {
			*style = savedStyle
		}()
		for _, p := range []*color.RGBA{&style.Text.Color, &style.Tab.NodeButton.TextNormal, &style.Tab.NodeButton.TextHover, &style.Tab.NodeButton.TextActive, &style.Tab.Text} {
			darken(p)
		}
	}

	curflags := flags
	flags = flags &^ showVariableParentIsPtr
	flags = flags &^ showVariableAlwaysExpand

	hdr := func() bool {
		return variableHeader(w, curflags, exprMenu, v)
	}

	cblbl := func(value string) {
		variableNoHeader(w, curflags, exprMenu, v, value, false)
	}

	cblblfmt := func(fmtstr string, args ...interface{}) {
		variableNoHeader(w, curflags, exprMenu, v, fmt.Sprintf(fmtstr, args...), false)
	}

	dynlbl := func(s string) {
		w.Row(varRowHeight).Dynamic(1)
		w.Label(s, "LC")
	}

	w.Row(varRowHeight).Static()
	if v.Unreadable != "" {
		cblblfmt("(unreadable %s)", v.Unreadable)
		return
	}

	if depth > 0 && v.Addr == 0 {
		cblbl("nil")
		return
	}

	switch v.Kind {
	case reflect.Slice:
		if hdr() {
			showArrayOrSliceContents(w, depth, flags, v)
			w.TreePop()
		}
	case reflect.Array:
		if hdr() {
			showArrayOrSliceContents(w, depth, flags, v)
			w.TreePop()
		}
	case reflect.Ptr:
		if len(v.Children) == 0 {
			cblbl("?")
		} else if v.Type == "" || v.Children[0].Addr == 0 {
			cblbl("nil")
		} else {
			if hdr() {
				if v.Children[0].OnlyAddr {
					loadMoreStruct(v.Children[0])
					dynlbl("Loading...")
				} else {
					showVariable(w, depth+1, flags|showVariableParentIsPtr, -1, v.Children[0])
				}
				w.TreePop()
			}
		}
	case reflect.UnsafePointer:
		cblblfmt("unsafe.Pointer(%#x)", v.Children[0].Addr)
	case reflect.String:
		if v.Value != v.Variable.Value {
			cblblfmt("(%d/%d)", len(v.Variable.Value), v.Len)
			hexdumpWindow(w, v)
		} else if v.Len == int64(len(v.Value)) {
			variableNoHeader(w, curflags, exprMenu, v, fmt.Sprintf("%q", v.Value), true)
		} else {
			variableNoHeader(w, curflags, exprMenu, v, fmt.Sprintf("%q...", v.Value), true)
		}
	case reflect.Chan:
		if len(v.Children) == 0 {
			cblbl("nil")
		} else {
			if hdr() {
				showStructContents(w, depth, flags, v)
				w.TreePop()
			}
		}
	case reflect.Struct:
		if hdr() {
			if int(v.Len) != len(v.Children) && len(v.Children) == 0 {
				loadMoreStruct(v)
				dynlbl("Loading...")
			} else {
				showStructContents(w, depth, flags, v)
			}
			w.TreePop()
		}
	case reflect.Interface:
		if v.Children[0].Kind == reflect.Invalid {
			cblbl("nil")
		} else {
			if hdr() {
				showInterfaceContents(w, depth, flags, v)
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
					showVariable(w, depth+1, flags, -1, v.Children[i])
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
			cblbl("nil")
		} else {
			cblbl(v.Value)
		}
	case reflect.Complex64, reflect.Complex128:
		cblblfmt("(%s + %si)", v.Children[0].Value, v.Children[1].Value)
	case reflect.Float32, reflect.Float64:
		cblbl(v.Value)
	default:
		if v.Value != "" && v.Kind != reflect.Ptr {
			cblbl(v.Value)
		} else {
			cblblfmt("(unknown %s)", v.Kind)
		}
	}
}

func showArrayOrSliceContents(w *nucular.Window, depth int, flags showVariableFlags, v *Variable) {
	if depth < 10 && !v.loading && len(v.Children) > 0 && autoloadMore(v.Children[0]) {
		v.loading = true
		loadMoreStruct(v)
	}
	for i := range v.Children {
		showVariable(w, depth+1, flags, -1, v.Children[i])
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

func showStructContents(w *nucular.Window, depth int, flags showVariableFlags, v *Variable) {
	for i := range v.Children {
		showVariable(w, depth+1, flags, -1, v.Children[i])
	}
}

func showInterfaceContents(w *nucular.Window, depth int, flags showVariableFlags, v *Variable) {
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
		showStructContents(w, depth, flags, data)
	case reflect.Array, reflect.Slice:
		showArrayOrSliceContents(w, depth, flags, data)
	default:
		showVariable(w, depth+1, flags, -1, data)
	}
}

var additionalLoadMu sync.Mutex
var additionalLoadRunning bool

func loadMoreMap(v *Variable) {
	if !additionalLoadRunning {
		additionalLoadRunning = true
		go func() {
			expr := fmt.Sprintf("(*(*%q)(%#x))[%d:]", v.Type, v.Addr, len(v.Children)/2)
			lv, err := client.EvalVariable(currentEvalScope(), expr, LongArrayLoadConfig)
			if err != nil {
				out := editorWriter{true}
				fmt.Fprintf(&out, "Error loading array contents %s: %v\n", expr, err)
				// prevent further attempts at loading
				v.Len = int64(len(v.Children) / 2)
			} else {
				v.Children = append(v.Children, wrapApiVariables("", lv.Children, reflect.Map, len(v.Children), v.Expression, true, nil, 0)...)
			}
			wnd.Changed()
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
		}()
	}
}

func loadMoreArrayOrSlice(v *Variable) {
	if !additionalLoadRunning {
		additionalLoadRunning = true
		go func() {
			expr := fmt.Sprintf("(*(*%q)(%#x))[%d:]", v.Type, v.Addr, len(v.Children))
			lv, err := client.EvalVariable(currentEvalScope(), expr, LongArrayLoadConfig)
			if err != nil {
				out := editorWriter{true}
				fmt.Fprintf(&out, "Error loading array contents %s: %v\n", expr, err)
				// prevent further attempts at loading
				v.Len = int64(len(v.Children))
			} else {
				v.Children = append(v.Children, wrapApiVariables("", lv.Children, v.Kind, len(v.Children), v.Expression, true, nil, 0)...)
			}
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
			wnd.Changed()
		}()
	}
}

func loadMoreStruct(v *Variable) {
	if !additionalLoadRunning {
		additionalLoadRunning = true
		go func() {
			lv, err := client.EvalVariable(currentEvalScope(), fmt.Sprintf("*(*%q)(%#x)", v.Type, v.Addr), getVariableLoadConfig())
			if err != nil {
				v.Unreadable = err.Error()
			} else {
				dn := v.DisplayName
				vn := v.Varname
				lv.Name = v.Name
				*v = *wrapApiVariable("", lv, lv.Name, v.Expression, true, nil, 0)
				v.Varname = vn
				v.DisplayName = dn
			}
			wnd.Changed()
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
		}()
	}
}

func configureLoadParameters(exprMenuIdx int) func(w *nucular.Window) {
	expr := &localsPanel.expressions[exprMenuIdx]
	maxArrayValues := expr.maxArrayValues
	maxStringLen := expr.maxStringLen
	if maxArrayValues <= 0 {
		cfg := getVariableLoadConfig()
		maxArrayValues = cfg.MaxArrayValues
		maxStringLen = cfg.MaxStringLen
	}

	return func(w *nucular.Window) {
		commit := false
		w.Row(30).Static(0)
		w.PropertyInt("Max array load:", 0, &maxArrayValues, 4096, 1, 1)

		w.Row(30).Static(0)
		w.PropertyInt("Max string load:", 0, &maxStringLen, 4096, 1, 1)

		w.Row(30).Static(0, 100, 100)
		w.Spacing(1)
		if w.ButtonText("Cancel") {
			w.Close()
		}
		if w.ButtonText("OK") {
			commit = true
		}
		if commit {
			expr.maxArrayValues = maxArrayValues
			expr.maxStringLen = maxStringLen
			loadOneExpr(exprMenuIdx)
			w.Close()
		}
	}
}

func markChangedVariables(newvars []*Variable, oldvars []*Variable) {
	m := make(map[string]*Variable)
	for i := range oldvars {
		m[oldvars[i].Varname] = oldvars[i]
	}

	for i := range newvars {
		oldvar, _ := m[newvars[i].Varname]
		if oldvar == nil {
			newvars[i].changed = true
		} else {
			markChangedVariable(newvars[i], oldvar)
		}
	}
}

func markChangedVariable(newvar *Variable, oldvar *Variable) {
	if newvar == nil {
		return
	}
	if oldvar == nil {
		newvar.changed = true
		return
	}
	if newvar.Value != oldvar.Value {
		newvar.changed = true
		return
	}

	for i := range newvar.Children {
		if newvar.Children[i] == nil {
			continue
		}
		if i >= len(oldvar.Children) {
			newvar.Children[i].changed = true
		} else {
			markChangedVariable(newvar.Children[i], oldvar.Children[i])
		}
		if newvar.Children[i].changed {
			newvar.changed = true
		}
	}
}

func changedVariableColor() color.RGBA {
	return color.RGBA{changedVariableOpacity, 0, 0, changedVariableOpacity}
}

func goDocCommand(typ string) {
	for len(typ) > 0 && typ[0] == '*' {
		typ = typ[1:]
	}
	cmd := exec.Command("go", "doc", typ)
	cmd.Dir = BackendServer.builddir
	out, _ := cmd.CombinedOutput()

	var scrollbackOut = &editorWriter{true}
	fmt.Fprintf(scrollbackOut, "Go doc for %s\n", typ)
	scrollbackOut.Write(out)
}

func hexdumpWindow(w *nucular.Window, v *Variable) {
	w.Row(5*varRowHeight).StaticScaled(variableNoHeaderSpacing(w, 1), 0)
	w.Spacing(1)
	if v.ed == nil {
		v.ed = &nucular.TextEditor{}
		v.ed.Flags = nucular.EditMultiline | nucular.EditClipboard | nucular.EditSelectable
		v.ed.Buffer = []rune(v.Value)
	}
	v.ed.Edit(w)
}
