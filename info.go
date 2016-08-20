package main

import (
	"fmt"
	"image"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/label"
	ntypes "github.com/aarzilli/nucular/types"
	"github.com/derekparker/delve/service/api"
)

type asyncLoad struct {
	mu      sync.Mutex
	loaded  bool
	loading bool
	err     error
}

func (l *asyncLoad) clear() {
	l.mu.Lock()
	l.loaded = false
	l.mu.Unlock()
}

func (l *asyncLoad) done(err error) {
	l.mu.Lock()
	l.err = err
	l.loading = false
	l.loaded = true
	l.mu.Unlock()
	wnd.Changed()
}

func (l *asyncLoad) showRequest(mw *nucular.MasterWindow, container *nucular.Window, name string, load func(*asyncLoad)) *nucular.Window {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.loading {
		container.Label("Loading...", "LT")
		return nil
	}

	if !l.loaded {
		if client == nil {
			container.Label("Connecting...", "LT")
			return nil
		}
		if running {
			container.Label("Running...", "LT")
			return nil
		}

		l.loading = true
		go load(l)
		return nil
	}

	if l.err != nil {
		container.Label(fmt.Sprintf("Error: %v", l.err), "LT")
		return nil
	}

	if w := container.GroupBegin(name, 0); w != nil {
		return w
	}
	return nil
}

const (
	currentGoroutineLocation = "Current location"
	userGoroutineLocation    = "User location"
	goStatementLocation      = "Go statement location"
)

var goroutineLocations = []string{currentGoroutineLocation, userGoroutineLocation, goStatementLocation}
var goroutinesPanel = struct {
	asyncLoad         asyncLoad
	goroutineLocation int
	goroutines        []*api.Goroutine
}{
	goroutineLocation: 1,
}

var stackPanel = struct {
	asyncLoad asyncLoad
	stack     []api.Stackframe
	depth     int
}{
	depth: 20,
}

var threadsPanel = struct {
	asyncLoad asyncLoad
	threads   []*api.Thread
}{}

var localsPanel = struct {
	asyncLoad    asyncLoad
	filterEditor nucular.TextEditor
	showAddr     bool
	args         []api.Variable
	locals       []api.Variable
}{
	filterEditor: nucular.TextEditor{Filter: spacefilter},
}

var exprsPanel = struct {
	asyncLoad    asyncLoad
	expressions  []string
	selected     int
	menuSelected int
	ed           nucular.TextEditor
	v            []*api.Variable
}{
	selected: -1,
	ed:       nucular.TextEditor{Flags: nucular.EditSelectable | nucular.EditSigEnter | nucular.EditClipboard},
}

var regsPanel = struct {
	asyncLoad asyncLoad
	regs      string
}{}

var globalsPanel = struct {
	asyncLoad    asyncLoad
	filterEditor nucular.TextEditor
	showAddr     bool
	globals      []api.Variable
}{
	filterEditor: nucular.TextEditor{Filter: spacefilter},
}

var breakpointsPanel = struct {
	asyncLoad   asyncLoad
	selected    int
	breakpoints []*api.Breakpoint
	editedBp    *api.Breakpoint
	printEditor nucular.TextEditor
	condEditor  nucular.TextEditor
}{}

type stringSlicePanel struct {
	name         string
	filterEditor nucular.TextEditor
	slice        []string
}

var funcsPanel = stringSlicePanel{name: "functions"}
var typesPanel = stringSlicePanel{name: "types"}
var sourcesPanel = stringSlicePanel{name: "sources"}

func spacefilter(ch rune) bool {
	return ch != ' ' && ch != '\t'
}

type goroutinesByID []*api.Goroutine

func (gs goroutinesByID) Len() int { return len(gs) }
func (gs goroutinesByID) Swap(i, j int) {
	temp := gs[i]
	gs[i] = gs[j]
	gs[j] = temp
}
func (gs goroutinesByID) Less(i, j int) bool { return gs[i].ID < gs[j].ID }

func loadGoroutines(p *asyncLoad) {
	var err error
	goroutinesPanel.goroutines, err = client.ListGoroutines()
	if err == nil {
		sort.Sort(goroutinesByID(goroutinesPanel.goroutines))
	}
	p.done(err)
}

func updateGoroutines(mw *nucular.MasterWindow, container *nucular.Window) {
	w := goroutinesPanel.asyncLoad.showRequest(mw, container, "goroutines", loadGoroutines)
	if w == nil {
		return
	}
	defer w.GroupEnd()
	style, _ := mw.Style()

	goroutines := goroutinesPanel.goroutines

	w.MenubarBegin()
	w.Row(20).Static(180)
	w.ComboSimple(goroutineLocations, &goroutinesPanel.goroutineLocation, 22)
	w.MenubarEnd()

	pad := style.Selectable.Padding.X * 2
	d := 1
	if len(goroutines) > 0 {
		d = digits(goroutines[len(goroutines)-1].ID)
	}

	maxthreadid := 0
	for _, g := range goroutines {
		if g.ThreadID > maxthreadid {
			maxthreadid = g.ThreadID
		}
	}

	dthread := digits(maxthreadid)

	zerow := nucular.FontWidth(style.Font, "0")

	w.Row(20).StaticScaled(zerow*d+pad, zerow*dthread+pad, 0)
	for _, g := range goroutines {
		selected := curGid == g.ID
		w.SelectableLabel(fmt.Sprintf("%*d", d, g.ID), "LC", &selected)
		if g.ThreadID != 0 {
			w.SelectableLabel(fmt.Sprintf("%*d", dthread, g.ThreadID), "LC", &selected)
		} else {
			w.SelectableLabel(" ", "LC", &selected)
		}
		switch goroutineLocations[goroutinesPanel.goroutineLocation] {
		case currentGoroutineLocation:
			w.SelectableLabel(formatLocation(g.CurrentLoc), "LC", &selected)
		case userGoroutineLocation:
			w.SelectableLabel(formatLocation(g.UserCurrentLoc), "LC", &selected)
		case goStatementLocation:
			w.SelectableLabel(formatLocation(g.GoStatementLoc), "LC", &selected)
		}
		if selected && curGid != g.ID && !running {
			go func(gid int) {
				state, err := client.SwitchGoroutine(gid)
				if err != nil {
					out := editorWriter{&scrollbackEditor, true}
					fmt.Fprintf(&out, "Could not switch goroutine: %v\n", err)
				} else {
					go refreshState(false, clearGoroutineSwitch, state)
				}
			}(g.ID)
		}
	}
}

func loadStacktrace(p *asyncLoad) {
	var err error
	stackPanel.stack, err = client.Stacktrace(curGid, stackPanel.depth, nil)
	p.done(err)
}

func updateStacktrace(mw *nucular.MasterWindow, container *nucular.Window) {
	w := stackPanel.asyncLoad.showRequest(mw, container, "stack", loadStacktrace)
	if w == nil {
		return
	}
	defer w.GroupEnd()
	style, _ := mw.Style()

	w.MenubarBegin()
	w.Row(20).Static(120)
	if w.PropertyInt("depth:", 1, &stackPanel.depth, 200, 1, 5) {
		go func() {
			stackPanel.asyncLoad.clear()
			wnd.Changed()
		}()
	}
	w.MenubarEnd()

	stack := stackPanel.stack

	maxpc := uint64(0)
	for _, frame := range stack {
		if frame.PC > maxpc {
			maxpc = frame.PC
		}
	}

	pad := style.Selectable.Padding.X * 2
	didx := digits(len(stack))
	d := hexdigits(maxpc)

	w.Row(40).StaticScaled(nucular.FontWidth(style.Font, "0")*didx+pad, nucular.FontWidth(style.Font, fmt.Sprintf("%#0*x", d, 0))+pad, 0)

	for i, frame := range stack {
		selected := curFrame == i
		w.SelectableLabel(fmt.Sprintf("%*d", didx, i), "LT", &selected)
		w.SelectableLabel(fmt.Sprintf("%#0*x", d, frame.PC), "LT", &selected)
		name := "(nil)"
		if frame.Function != nil {
			name = frame.Function.Name
		}
		w.SelectableLabel(fmt.Sprintf("%s\nat %s:%d", name, ShortenFilePath(frame.File), frame.Line), "LT", &selected)
		if selected && curFrame != i && !running {
			curFrame = i
			go refreshState(true, clearFrameSwitch, nil)
		}
	}
}

type threadsByID []*api.Thread

func (threads threadsByID) Len() int { return len(threads) }
func (threads threadsByID) Swap(i, j int) {
	temp := threads[i]
	threads[i] = threads[j]
	threads[j] = temp
}
func (threads threadsByID) Less(i, j int) bool { return threads[i].ID < threads[j].ID }

func loadThreads(p *asyncLoad) {
	var err error
	threadsPanel.threads, err = client.ListThreads()
	if err == nil {
		sort.Sort(threadsByID(threadsPanel.threads))
	}
	p.done(err)
}

func updateThreads(mw *nucular.MasterWindow, container *nucular.Window) {
	w := threadsPanel.asyncLoad.showRequest(mw, container, "threads", loadThreads)
	if w == nil {
		return
	}
	defer w.GroupEnd()
	style, _ := mw.Style()

	threads := threadsPanel.threads

	pad := style.Selectable.Padding.X * 2
	d := 1
	if len(threads) > 0 {
		d = digits(threads[len(threads)-1].ID)
	}
	w.Row(20).StaticScaled(nucular.FontWidth(style.Font, "0")*d+pad, 0)

	for _, thread := range threads {
		selected := curThread == thread.ID
		w.SelectableLabel(fmt.Sprintf("%*d", d, thread.ID), "LC", &selected)
		loc := api.Location{thread.PC, thread.File, thread.Line, thread.Function}
		w.SelectableLabel(formatLocation(loc), "LC", &selected)
		if selected && curThread != thread.ID && !running {
			go func(tid int) {
				state, err := client.SwitchThread(tid)
				if err != nil {
					out := editorWriter{&scrollbackEditor, true}
					fmt.Fprintf(&out, "Could not switch thread: %v\n", err)
				} else {
					go refreshState(false, clearGoroutineSwitch, state)
				}
			}(thread.ID)
		}
	}
	w.GroupEnd()
}

type variablesByName []api.Variable

func (vars variablesByName) Len() int { return len(vars) }
func (vars variablesByName) Swap(i, j int) {
	temp := vars[i]
	vars[i] = vars[j]
	vars[j] = temp
}
func (vars variablesByName) Less(i, j int) bool { return vars[i].Name < vars[j].Name }

func loadLocals(p *asyncLoad) {
	var errloc, errarg error
	localsPanel.args, errloc = client.ListFunctionArgs(api.EvalScope{curGid, curFrame}, LongLoadConfig)
	if errarg == nil {
		sort.Sort(variablesByName(localsPanel.args))
	}
	localsPanel.locals, errarg = client.ListLocalVariables(api.EvalScope{curGid, curFrame}, LongLoadConfig)
	if errloc == nil {
		sort.Sort(variablesByName(localsPanel.locals))
	}

	m := map[string]int{}

	changename := func(v *api.Variable) {
		if n, ok := m[v.Name]; ok {
			n++
			m[v.Name] = n
			v.Name = fmt.Sprintf("%s(%d)", v.Name, n)
		} else {
			m[v.Name] = 0
		}
	}

	for i := range localsPanel.args {
		changename(&localsPanel.args[i])
	}
	for i := range localsPanel.locals {
		changename(&localsPanel.locals[i])
	}
	if errarg != nil {
		p.done(errarg)
		return
	}
	p.done(errloc)
}

const (
	varRowHeight = 20
	moreBtnWidth = 70
)

const variableIndent = 18

func updateLocals(mw *nucular.MasterWindow, container *nucular.Window) {
	w := localsPanel.asyncLoad.showRequest(mw, container, "locals", loadLocals)
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
	w.Row(varRowHeight).Dynamic(1)

	_, scaling := mw.Style()
	ind := int(variableIndent * scaling)

	args, locals := localsPanel.args, localsPanel.locals

	for i := range args {
		if strings.Index(args[i].Name, filter) >= 0 {
			showVariable(w, 0, localsPanel.showAddr, -1, args[i].Name, &args[i], ind)
		}
	}

	if len(args) > 0 {
		w.Row(varRowHeight / 2).Dynamic(1)
		w.Spacing(1)
		w.Row(varRowHeight).Dynamic(1)
	}

	for i := range locals {
		if strings.Index(locals[i].Name, filter) >= 0 {
			showVariable(w, 0, localsPanel.showAddr, -1, locals[i].Name, &locals[i], ind)
		}
	}
}

func loadOneExpr(i int) {
	var err error
	exprsPanel.v[i], err = client.EvalVariable(api.EvalScope{curGid, curFrame}, exprsPanel.expressions[i], LongLoadConfig)
	if err != nil {
		exprsPanel.v[i] = &api.Variable{Name: exprsPanel.expressions[i], Unreadable: err.Error()}
	}
}

func loadExprs(l *asyncLoad) {
	for i := range exprsPanel.expressions {
		loadOneExpr(i)
	}
	l.done(nil)
}

func updateExprs(mw *nucular.MasterWindow, container *nucular.Window) {
	w := exprsPanel.asyncLoad.showRequest(mw, container, "exprs", loadExprs)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	w.Row(varRowHeight).Dynamic(1)

	_, scaling := mw.Style()
	ind := int(variableIndent * scaling)
	editorShown := false

	for i := range exprsPanel.expressions {
		if i == exprsPanel.selected {
			exprsEditor(w)
			editorShown = true
		} else {
			showVariable(w, 0, false, i, exprsPanel.v[i].Name, exprsPanel.v[i], ind)
		}
	}

	if !editorShown {
		exprsEditor(w)
	}
}

func exprsEditor(w *nucular.Window) {
	active := exprsPanel.ed.Edit(w)
	if active&nucular.EditCommitted == 0 {
		return
	}

	newexpr := string(exprsPanel.ed.Buffer)
	exprsPanel.ed.Buffer = exprsPanel.ed.Buffer[:0]
	exprsPanel.ed.Cursor = 0
	exprsPanel.ed.Active = true
	exprsPanel.ed.CursorFollow = true

	if exprsPanel.selected < 0 {
		exprsPanel.expressions = append(exprsPanel.expressions, newexpr)
		exprsPanel.v = append(exprsPanel.v, nil)
		i := len(exprsPanel.v) - 1
		go func(i int) {
			additionalLoadMu.Lock()
			defer additionalLoadMu.Unlock()
			loadOneExpr(i)
		}(i)
	} else {
		exprsPanel.expressions[exprsPanel.selected] = newexpr
		go func(i int) {
			additionalLoadMu.Lock()
			defer additionalLoadMu.Unlock()
			loadOneExpr(i)
		}(exprsPanel.selected)
		exprsPanel.selected = -1
	}
}

func showExprMenu(w *nucular.Window, exprMenuIdx int) {
	if exprMenuIdx < 0 || running {
		return
	}
	if w.Input().Mouse.AnyClickInRect(w.LastWidgetBounds) {
		exprsPanel.menuSelected = exprMenuIdx
	}
	w.ContextualOpen(0, image.Point{150, 500}, w.LastWidgetBounds, exprMenu)
}

func exprMenu(mw *nucular.MasterWindow, w *nucular.Window) {
	w.Row(20).Dynamic(1)

	if w.MenuItem(label.TA("Edit", "LC")) {
		exprsPanel.selected = exprsPanel.menuSelected
		if exprsPanel.selected >= 0 && exprsPanel.selected < len(exprsPanel.expressions) {
			exprsPanel.ed.Buffer = []rune(exprsPanel.expressions[exprsPanel.selected])
			exprsPanel.ed.Cursor = len(exprsPanel.ed.Buffer)
			exprsPanel.ed.CursorFollow = true
		}
	}
	if w.MenuItem(label.TA("Remove", "LC")) {
		if exprsPanel.menuSelected+1 < len(exprsPanel.expressions) {
			copy(exprsPanel.expressions[exprsPanel.menuSelected:], exprsPanel.expressions[exprsPanel.menuSelected+1:])
			copy(exprsPanel.v[exprsPanel.menuSelected:], exprsPanel.v[exprsPanel.menuSelected+1:])
		}
		exprsPanel.expressions = exprsPanel.expressions[:len(exprsPanel.expressions)-1]
		exprsPanel.v = exprsPanel.v[:len(exprsPanel.v)-1]
	}
}

func loadRegs(p *asyncLoad) {
	var err error
	regsPanel.regs, err = client.ListRegisters()
	p.done(err)
}

func updateRegs(mw *nucular.MasterWindow, container *nucular.Window) {
	w := regsPanel.asyncLoad.showRequest(mw, container, "registers", loadRegs)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	regs := regsPanel.regs

	lines := 1
	for i := range regs {
		if regs[i] == '\n' {
			lines++
		}
	}
	w.Row(20 * lines).Dynamic(1)
	w.Label(regs, "LT")
}

func loadGlobals(p *asyncLoad) {
	var err error
	globalsPanel.globals, err = client.ListPackageVariables("", LongLoadConfig)
	sort.Sort(variablesByName(globalsPanel.globals))
	p.done(err)
}

func updateGlobals(mw *nucular.MasterWindow, container *nucular.Window) {
	w := globalsPanel.asyncLoad.showRequest(mw, container, "globals", loadGlobals)
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
	w.Row(varRowHeight).Dynamic(1)

	_, scaling := mw.Style()
	ind := int(18 * scaling)

	globals := globalsPanel.globals

	for i := range globals {
		if strings.Index(globals[i].Name, filter) >= 0 {
			showVariable(w, 0, globalsPanel.showAddr, -1, globals[i].Name, &globals[i], ind)
		}
	}
}

type breakpointsByID []*api.Breakpoint

func (bps breakpointsByID) Len() int { return len(bps) }
func (bps breakpointsByID) Swap(i, j int) {
	temp := bps[i]
	bps[i] = bps[j]
	bps[j] = temp
}
func (bps breakpointsByID) Less(i, j int) bool { return bps[i].ID < bps[i].ID }

func loadBreakpoints(p *asyncLoad) {
	var err error
	breakpointsPanel.breakpoints, err = client.ListBreakpoints()
	if err == nil {
		sort.Sort(breakpointsByID(breakpointsPanel.breakpoints))
	}
	p.done(err)
}

func updateBreakpoints(mw *nucular.MasterWindow, container *nucular.Window) {
	w := breakpointsPanel.asyncLoad.showRequest(mw, container, "breakpoints", loadBreakpoints)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	style, _ := mw.Style()

	breakpoints := breakpointsPanel.breakpoints

	pad := style.Selectable.Padding.X * 2
	d := 1
	if len(breakpoints) > 0 {
		d = digits(breakpoints[len(breakpoints)-1].ID)
	}
	if d < 3 {
		d = 3
	}

	w.Row(40).StaticScaled(nucular.FontWidth(style.Font, "0")*d+pad, 0)
	for _, breakpoint := range breakpoints {
		selected := breakpointsPanel.selected == breakpoint.ID
		w.SelectableLabel(fmt.Sprintf("%*d", d, breakpoint.ID), "LT", &selected)
		bounds := w.LastWidgetBounds
		bounds.W = w.Bounds.W
		if w.Input().Mouse.AnyClickInRect(bounds) {
			breakpointsPanel.selected = breakpoint.ID
		}
		w.SelectableLabel(fmt.Sprintf("%s in %s\nat %s:%d (%#v)", breakpoint.Name, breakpoint.FunctionName, breakpoint.File, breakpoint.Line, breakpoint.Addr), "LT", &selected)
		if !running {
			w.ContextualOpen(0, image.Point{200, 500}, bounds, breakpointsMenu)
		}
	}
}

func breakpointsMenu(mw *nucular.MasterWindow, w *nucular.Window) {
	w.Row(20).Dynamic(1)
	if breakpointsPanel.selected > 0 {
		if w.MenuItem(label.TA("Edit...", "LC")) {
			mw.PopupOpen(fmt.Sprintf("Editing breakpoint %d", breakpointsPanel.selected), nucular.WindowDynamic|nucular.WindowTitle|nucular.WindowNoScrollbar|nucular.WindowMovable|nucular.WindowBorder, ntypes.Rect{100, 100, 400, 700}, true, breakpointEditor)
		}
		if w.MenuItem(label.TA("Clear", "LC")) {
			go func() {
				scrollbackOut := editorWriter{&scrollbackEditor, true}
				_, err := client.ClearBreakpoint(breakpointsPanel.selected)
				if err != nil {
					fmt.Fprintf(&scrollbackOut, "Could not clear breakpoint: %v\n", err)
				}
				breakpointsPanel.asyncLoad.clear()
				wnd.Changed()
			}()
		}
	}
	if w.MenuItem(label.TA("Clear All", "LC")) {
		go func() {
			scrollbackOut := editorWriter{&scrollbackEditor, true}
			breakpoints := breakpointsPanel.breakpoints
			for i := range breakpoints {
				if breakpoints[i].ID < 0 {
					continue
				}
				_, err := client.ClearBreakpoint(breakpoints[i].ID)
				if err != nil {
					fmt.Fprintf(&scrollbackOut, "Could not clear breakpoint %d: %v\n", breakpoints[i].ID, err)
				}
			}
			breakpointsPanel.asyncLoad.clear()
			wnd.Changed()
		}()
	}
}

func breakpointEditor(mw *nucular.MasterWindow, w *nucular.Window) {
	if breakpointsPanel.editedBp == nil {
		breakpoints := breakpointsPanel.breakpoints
		for i := range breakpoints {
			if breakpoints[i].ID == breakpointsPanel.selected {
				breakpointsPanel.editedBp = breakpoints[i]
				break
			}
		}

		if breakpointsPanel.editedBp == nil {
			w.Close()
			return
		}

		breakpointsPanel.printEditor.Flags = nucular.EditMultiline | nucular.EditClipboard | nucular.EditSelectable
		breakpointsPanel.printEditor.Buffer = breakpointsPanel.printEditor.Buffer[:0]
		for i := range breakpointsPanel.editedBp.Variables {
			breakpointsPanel.printEditor.Buffer = append(breakpointsPanel.printEditor.Buffer, []rune(fmt.Sprintf("%s\n", breakpointsPanel.editedBp.Variables[i]))...)
		}

		breakpointsPanel.condEditor.Flags = nucular.EditClipboard | nucular.EditSelectable
		breakpointsPanel.condEditor.Buffer = []rune(breakpointsPanel.editedBp.Cond)
	}

	w.Row(20).Dynamic(2)
	if w.OptionText("breakpoint", !breakpointsPanel.editedBp.Tracepoint) {
		breakpointsPanel.editedBp.Tracepoint = false
	}
	if w.OptionText("tracepoint", breakpointsPanel.editedBp.Tracepoint) {
		breakpointsPanel.editedBp.Tracepoint = true
	}

	w.Row(20).Static(100, 100, 150)
	arguments := breakpointsPanel.editedBp.LoadArgs != nil
	w.CheckboxText("Arguments", &arguments)
	locals := breakpointsPanel.editedBp.LoadLocals != nil
	w.CheckboxText("Locals", &locals)
	w.PropertyInt("Stacktrace", 0, &breakpointsPanel.editedBp.Stacktrace, 200, 1, 10)

	verboseArguments, verboseLocals := false, false
	w.Row(20).Static(20, 100, 100)
	w.Spacing(1)
	if arguments {
		verboseArguments = breakpointsPanel.editedBp.LoadArgs != nil && *breakpointsPanel.editedBp.LoadArgs == LongLoadConfig
		w.CheckboxText("-v", &verboseArguments)
	} else {
		w.Spacing(1)
	}
	if locals {
		verboseLocals = breakpointsPanel.editedBp.LoadLocals != nil && *breakpointsPanel.editedBp.LoadLocals == LongLoadConfig
		w.CheckboxText("-v", &verboseLocals)
	} else {
		w.Spacing(1)
	}

	if arguments {
		if verboseArguments {
			breakpointsPanel.editedBp.LoadArgs = &LongLoadConfig
		} else {
			breakpointsPanel.editedBp.LoadArgs = &ShortLoadConfig
		}
	} else {
		breakpointsPanel.editedBp.LoadArgs = nil
	}

	if locals {
		if verboseLocals {
			breakpointsPanel.editedBp.LoadLocals = &LongLoadConfig
		} else {
			breakpointsPanel.editedBp.LoadLocals = &ShortLoadConfig
		}
	} else {
		breakpointsPanel.editedBp.LoadLocals = nil
	}

	w.Row(20).Dynamic(1)
	w.Label("Print:", "LC")
	w.Row(100).Dynamic(1)
	breakpointsPanel.printEditor.Edit(w)

	w.Row(20).Static(70, 0)
	w.Label("Condition:", "LC")
	breakpointsPanel.condEditor.Edit(w)

	w.Row(20).Static(0, 80, 80)
	w.Spacing(1)
	if w.ButtonText("Cancel") {
		breakpointsPanel.editedBp = nil
		breakpointsPanel.asyncLoad.clear()
		w.Close()
	}
	if w.ButtonText("OK") {
		breakpointsPanel.editedBp.Cond = string(breakpointsPanel.condEditor.Buffer)
		breakpointsPanel.editedBp.Variables = breakpointsPanel.editedBp.Variables[:0]
		for _, p := range strings.Split(string(breakpointsPanel.printEditor.Buffer), "\n") {
			if p == "" {
				continue
			}
			breakpointsPanel.editedBp.Variables = append(breakpointsPanel.editedBp.Variables, p)
		}
		go func(bp *api.Breakpoint) {
			err := client.AmendBreakpoint(bp)
			if err != nil {
				scrollbackOut := editorWriter{&scrollbackEditor, true}
				fmt.Fprintf(&scrollbackOut, "Could not amend breakpoint: %v\n", err)
			}
		}(breakpointsPanel.editedBp)
		breakpointsPanel.editedBp = nil
		breakpointsPanel.asyncLoad.clear()
		w.Close()
	}
}

func (p *stringSlicePanel) update(mw *nucular.MasterWindow, container *nucular.Window) {
	name, filterEditor, values := p.name, p.filterEditor, p.slice
	if filterEditor.Filter == nil {
		filterEditor.Filter = spacefilter
	}
	w := container.GroupBegin(name, 0)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	w.MenubarBegin()
	w.Row(20).Static(90, 0)
	w.Label("Filter:", "LC")
	filterEditor.Edit(w)
	w.MenubarEnd()

	filter := string(filterEditor.Buffer)

	w.Row(20).Dynamic(1)
	for _, value := range values {
		if strings.Index(value, filter) >= 0 {
			w.Label(value, "LC")
		}
		// TODO: contextual menu with copy (but functions need to also have a "set breakpoint" entry)
	}
}

func showVariable(w *nucular.Window, depth int, addr bool, exprMenu int, name string, v *api.Variable, ind int) {
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
	if v.Unreadable != "" {
		w.Label(fmt.Sprintf("%s = (unreadable %s)", name, v.Unreadable), "LC")
		showExprMenu(w, exprMenu)
		return
	}

	if depth > 0 && v.Addr == 0 {
		w.Label(fmt.Sprintf("%s = nil", name, v.Type), "LC")
		showExprMenu(w, exprMenu)
		return
	}

	switch v.Kind {
	case reflect.Slice:
		if w.TreePush(nucular.TreeNode, name, false) {
			showExprMenu(w, exprMenu)
			w.Scrollbar.X -= ind
			w.Label(fmt.Sprintf("len: %d cap: %d", v.Len, v.Cap), "LC")
			showArrayOrSliceContents(w, depth, addr, v, ind)
			w.Scrollbar.X += ind
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu)
		}
	case reflect.Array:
		if w.TreePush(nucular.TreeNode, name, false) {
			showExprMenu(w, exprMenu)
			w.Scrollbar.X -= ind
			w.Label(fmt.Sprintf("len: %d", v.Len), "LC")
			showArrayOrSliceContents(w, depth, addr, v, ind)
			w.Scrollbar.X += ind
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu)
		}
	case reflect.Ptr:
		if v.Type == "" || v.Children[0].Addr == 0 {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
			showExprMenu(w, exprMenu)
		} else if v.Children[0].OnlyAddr && v.Children[0].Addr != 0 {
			w.Label(fmt.Sprintf("%s = (%s)(%#x)", name, v.Type, v.Children[0].Addr), "LC")
			showExprMenu(w, exprMenu)
		} else {
			if w.TreePush(nucular.TreeNode, name, false) {
				showExprMenu(w, exprMenu)
				w.Scrollbar.X -= ind
				showVariable(w, depth+1, addr, -1, "", &v.Children[0], ind)
				w.Scrollbar.X += ind
				w.TreePop()
			} else {
				showExprMenu(w, exprMenu)
			}
		}
	case reflect.UnsafePointer:
		w.Label(fmt.Sprintf("%s = unsafe.Pointer(%#x)", name, v.Children[0].Addr), "LC")
		showExprMenu(w, exprMenu)
	case reflect.String:
		if len(v.Value) != int(v.Len) {
			w.Row(varRowHeight).Static(0, moreBtnWidth)
			w.Label(fmt.Sprintf("%s = %q", name, v.Value), "LC")
			showExprMenu(w, exprMenu)
			w.Label(fmt.Sprintf("%d more", int(v.Len)-len(v.Value)), "LC")
			//TODO: detailed view for strings
			w.Row(varRowHeight).Dynamic(1)
		} else {
			w.Label(fmt.Sprintf("%s = %q", name, v.Value), "LC")
			showExprMenu(w, exprMenu)
		}
	case reflect.Chan:
		if len(v.Children) == 0 {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
			showExprMenu(w, exprMenu)
		} else {
			if w.TreePush(nucular.TreeNode, name, false) {
				showExprMenu(w, exprMenu)
				w.Scrollbar.X -= ind
				showStructContents(w, depth, addr, v, ind)
				w.Scrollbar.X += ind
				w.TreePop()
			} else {
				showExprMenu(w, exprMenu)
			}
		}
	case reflect.Struct:
		if w.TreePush(nucular.TreeNode, name, false) {
			showExprMenu(w, exprMenu)
			w.Scrollbar.X -= ind
			if int(v.Len) != len(v.Children) && len(v.Children) == 0 {
				loadMoreStruct(v)
				w.Label("Loading...", "LC")
			} else {
				showStructContents(w, depth, addr, v, ind)
			}
			w.Scrollbar.X += ind
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu)
		}
	case reflect.Interface:
		if v.Children[0].Kind == reflect.Invalid {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
			showExprMenu(w, exprMenu)
		} else {
			if w.TreePush(nucular.TreeNode, name, false) {
				showExprMenu(w, exprMenu)
				w.Scrollbar.X -= ind
				if v.Children[0].Kind == reflect.Ptr {
					showVariable(w, depth+1, addr, -1, "data", &v.Children[0].Children[0], ind)
				} else {
					showVariable(w, depth+1, addr, -1, "data", &v.Children[0], ind)
				}
				w.Scrollbar.X += ind
				w.TreePop()
			} else {
				showExprMenu(w, exprMenu)
			}
		}
	case reflect.Map:
		if w.TreePush(nucular.TreeNode, name, false) {
			showExprMenu(w, exprMenu)
			w.Scrollbar.X -= ind
			for i := 0; i < len(v.Children); i += 2 {
				key, value := &v.Children[i], &v.Children[i+1]
				if len(key.Children) == 0 && len(key.Value) < minInlineKeyValueLen {
					var keyname string
					if key.Kind == reflect.String {
						keyname = fmt.Sprintf("[%q]", key.Value)
					} else {
						keyname = fmt.Sprintf("[%s]", key.Value)
					}
					showVariable(w, depth+1, addr, -1, keyname, value, ind)
				} else {
					showVariable(w, depth+1, addr, -1, fmt.Sprintf("[%d key]", i/2), key, ind)
					showVariable(w, depth+1, addr, -1, fmt.Sprintf("[%d value]", i/2), value, ind)
				}
			}
			if len(v.Children)/2 != int(v.Len) {
				w.Row(varRowHeight).Static(moreBtnWidth)
				if w.ButtonText(fmt.Sprintf("%d more", int(v.Len)-(len(v.Children)/2))) {
					loadMoreMap(v)
				}
				w.Row(varRowHeight).Dynamic(1)
			}
			w.Scrollbar.X += ind
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu)
		}
	case reflect.Func:
		if v.Value == "" {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
		} else {
			w.Label(fmt.Sprintf("%s = %s", name, v.Value), "LC")
		}
		showExprMenu(w, exprMenu)
	case reflect.Complex64, reflect.Complex128:
		w.Label(fmt.Sprintf("%s = (%s + %si)", name, v.Children[0].Value, v.Children[1].Value), "LC")
		showExprMenu(w, exprMenu)
	default:
		if v.Value != "" {
			w.Label(fmt.Sprintf("%s = %s", name, v.Value), "LC")
		} else {
			w.Label(fmt.Sprintf("%s = (unknown %s)", name, v.Kind), "LC")
		}
		showExprMenu(w, exprMenu)
	}
}

func showArrayOrSliceContents(w *nucular.Window, depth int, addr bool, v *api.Variable, ind int) {
	for i := range v.Children {
		showVariable(w, depth+1, addr, -1, fmt.Sprintf("[%d]", i), &v.Children[i], ind)
	}
	if len(v.Children) != int(v.Len) {
		w.Row(varRowHeight).Static(moreBtnWidth)
		if w.ButtonText(fmt.Sprintf("%d more", int(v.Len)-len(v.Children))) {
			loadMoreArrayOrSlice(v)
		}
		w.Row(varRowHeight).Dynamic(1)
	}
}

func showStructContents(w *nucular.Window, depth int, addr bool, v *api.Variable, ind int) {
	for i := range v.Children {
		showVariable(w, depth+1, addr, -1, v.Children[i].Name, &v.Children[i], ind)
	}
}

var additionalLoadMu sync.Mutex
var additionalLoadRunning bool

func loadMoreMap(v *api.Variable) {
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
				v.Children = append(v.Children, lv.Children...)
			}
			wnd.Changed()
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
		}()
	}
}

func loadMoreArrayOrSlice(v *api.Variable) {
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
				v.Children = append(v.Children, lv.Children...)
			}
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
			wnd.Changed()
		}()
	}
}

func loadMoreStruct(v *api.Variable) {
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
				*v = *lv
			}
			wnd.Changed()
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
		}()
	}
}

func updateListingPanel(mw *nucular.MasterWindow, container *nucular.Window) {
	const lineheight = 14

	listp := container.GroupBegin("listing", nucular.WindowNoHScrollbar)
	if listp == nil {
		return
	}
	defer listp.GroupEnd()

	style, _ := mw.Style()

	arroww := nucular.FontWidth(style.Font, "=>") + style.Text.Padding.X*2
	starw := nucular.FontWidth(style.Font, "*") + style.Text.Padding.X*2

	idxw := style.Text.Padding.X * 2
	if len(listingPanel.listing) > 0 {
		idxw += nucular.FontWidth(style.Font, listingPanel.listing[len(listingPanel.listing)-1].idx)
	}

	for _, line := range listingPanel.listing {
		listp.Row(lineheight).StaticScaled(starw, arroww, idxw, 0)
		if line.pc {
			rowbounds := listp.WidgetBounds()
			rowbounds.W = listp.Bounds.W
			cmds := listp.Commands()
			cmds.FillRect(rowbounds, 0, style.Selectable.PressedActive.Data.Color)
		}

		if line.breakpoint {
			listp.Label("*", "CC")
		} else {
			listp.Spacing(1)
		}

		if line.pc && listingPanel.recenterListing {
			listingPanel.recenterListing = false
			if above, below := listp.Invisible(); above || below {
				listp.Scrollbar.Y = listp.At().Y - listp.Bounds.H/2
				if listp.Scrollbar.Y < 0 {
					listp.Scrollbar.Y = 0
				}
				wnd.Changed()
			}
		}

		if line.pc && curFrame == 0 {
			listp.Label("=>", "CC")
		} else {
			listp.Spacing(1)
		}
		listp.Label(line.idx, "LC")
		listp.Label(line.text, "LC")
	}
}

func updateDisassemblyPanel(mw *nucular.MasterWindow, container *nucular.Window) {
	const lineheight = 14

	listp := container.GroupBegin("disassembly", nucular.WindowNoHScrollbar)
	if listp == nil {
		return
	}
	defer listp.GroupEnd()

	style, _ := mw.Style()

	arroww := nucular.FontWidth(style.Font, "=>") + style.Text.Padding.X*2
	starw := nucular.FontWidth(style.Font, "*") + style.Text.Padding.X*2

	var maxaddr uint64 = 0
	if len(listingPanel.text) > 0 {
		maxaddr = listingPanel.text[len(listingPanel.text)-1].Loc.PC
	}
	addrw := nucular.FontWidth(style.Font, fmt.Sprintf("%#x", maxaddr)) + style.Text.Padding.X*2

	lastfile, lastlineno := "", 0

	if len(listingPanel.text) > 0 && listingPanel.text[0].Loc.Function != nil {
		listp.Row(lineheight).Dynamic(1)
		listp.Label(fmt.Sprintf("TEXT %s(SB) %s", listingPanel.text[0].Loc.Function.Name, listingPanel.text[0].Loc.File), "LC")
	}

	for _, instr := range listingPanel.text {
		if instr.Loc.File != lastfile || instr.Loc.Line != lastlineno {
			listp.Row(lineheight).Dynamic(1)
			listp.Row(lineheight).Dynamic(1)
			text := ""
			if instr.Loc.File == listingPanel.file && instr.Loc.Line-1 < len(listingPanel.listing) {
				text = strings.TrimSpace(listingPanel.listing[instr.Loc.Line-1].text)
			}
			listp.Label(fmt.Sprintf("%s:%d: %s", instr.Loc.File, instr.Loc.Line, text), "LC")
			lastfile, lastlineno = instr.Loc.File, instr.Loc.Line
		}
		listp.Row(lineheight).StaticScaled(starw, arroww, addrw, 0)

		if instr.AtPC {
			rowbounds := listp.WidgetBounds()
			rowbounds.W = listp.Bounds.W
			cmds := listp.Commands()
			cmds.FillRect(rowbounds, 0, style.Selectable.PressedActive.Data.Color)
		}

		if instr.Breakpoint {
			listp.Label("*", "LC")
		} else {
			listp.Label(" ", "LC")
		}

		if instr.AtPC {
			if listingPanel.recenterDisassembly {
				listingPanel.recenterDisassembly = false
				if above, below := listp.Invisible(); above || below {
					listp.Scrollbar.Y = listp.At().Y - listp.Bounds.H/2
					if listp.Scrollbar.Y < 0 {
						listp.Scrollbar.Y = 0
					}
					wnd.Changed()
				}
			}
			listp.Label("=>", "LC")
		} else {
			listp.Label(" ", "LC")
		}

		listp.Label(fmt.Sprintf("%#x", instr.Loc.PC), "LC")
		listp.Label(instr.Text, "LC")
	}
}
