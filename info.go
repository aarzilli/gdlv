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
}

func (l *asyncLoad) clear() {
	l.mu.Lock()
	l.loaded = false
	l.mu.Unlock()
}

func (l *asyncLoad) done() {
	l.mu.Lock()
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
	out := editorWriter{&scrollbackEditor, true}
	var err error
	goroutinesPanel.goroutines, err = client.ListGoroutines()
	if err != nil {
		fmt.Fprintf(&out, "Could not list goroutines: %v\n", err)
		return
	}
	sort.Sort(goroutinesByID(goroutinesPanel.goroutines))
	p.done()
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
	out := editorWriter{&scrollbackEditor, true}
	var err error
	stackPanel.stack, err = client.Stacktrace(curGid, stackPanel.depth, nil)
	if err != nil {
		fmt.Fprintf(&out, "Could not stacktrace: %v\n", err)
		return
	}
	p.done()
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
	out := editorWriter{&scrollbackEditor, true}
	var err error
	threadsPanel.threads, err = client.ListThreads()
	if err != nil {
		fmt.Fprintf(&out, "Could not list threads: %v\n", err)
		return
	}
	sort.Sort(threadsByID(threadsPanel.threads))
	p.done()
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
	m := map[string]int{}

	out := editorWriter{&scrollbackEditor, true}
	var err error
	localsPanel.args, err = client.ListFunctionArgs(api.EvalScope{curGid, curFrame}, LongLoadConfig)
	if err != nil {
		fmt.Fprintf(&out, "Could not list function arguments: %v\n", err)
		return
	}
	localsPanel.locals, err = client.ListLocalVariables(api.EvalScope{curGid, curFrame}, LongLoadConfig)
	if err != nil {
		fmt.Fprintf(&out, "Could not list local variables: %v\n", err)
		return
	}
	sort.Sort(variablesByName(localsPanel.args))
	sort.Sort(variablesByName(localsPanel.locals))

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
	p.done()
}

const (
	varRowHeight = 20
	moreBtnWidth = 70
)

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
	ind := int(18 * scaling)

	args, locals := localsPanel.args, localsPanel.locals

	for i := range args {
		if strings.Index(args[i].Name, filter) >= 0 {
			showVariable(w, 0, localsPanel.showAddr, args[i].Name, &args[i], ind)
		}
	}

	if len(args) > 0 {
		w.Row(varRowHeight / 2).Dynamic(1)
		w.Spacing(1)
		w.Row(varRowHeight).Dynamic(1)
	}

	for i := range locals {
		if strings.Index(locals[i].Name, filter) >= 0 {
			showVariable(w, 0, localsPanel.showAddr, locals[i].Name, &locals[i], ind)
		}
	}
}

func loadRegs(p *asyncLoad) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	regsPanel.regs, err = client.ListRegisters()
	if err != nil {
		fmt.Fprintf(&out, "Could not list registers: %v\n", err)
		return
	}
	p.done()
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
	out := editorWriter{&scrollbackEditor, true}
	var err error
	globalsPanel.globals, err = client.ListPackageVariables("", LongLoadConfig)
	if err != nil {
		fmt.Fprintf(&out, "Could not list global variabless: %v\n", err)
		return
	}
	sort.Sort(variablesByName(globalsPanel.globals))
	p.done()
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
			showVariable(w, 0, globalsPanel.showAddr, globals[i].Name, &globals[i], ind)
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
	out := editorWriter{&scrollbackEditor, true}
	var err error
	breakpointsPanel.breakpoints, err = client.ListBreakpoints()
	sort.Sort(breakpointsByID(breakpointsPanel.breakpoints))
	if err != nil {
		fmt.Fprintf(&out, "Could not list breakpoints: %v\n", err)
		return
	}
	p.done()
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

func showVariable(w *nucular.Window, depth int, addr bool, name string, v *api.Variable, ind int) {
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
		return
	}

	if depth > 0 && v.Addr == 0 {
		w.Label(fmt.Sprintf("%s = nil", name, v.Type), "LC")
		return
	}

	switch v.Kind {
	case reflect.Slice:
		if w.TreePush(nucular.TreeNode, name, false) {
			w.Scrollbar.X -= ind
			w.Label(fmt.Sprintf("len: %d cap: %d", v.Len, v.Cap), "LC")
			showArrayOrSliceContents(w, depth, addr, v, ind)
			w.Scrollbar.X += ind
			w.TreePop()
		}
	case reflect.Array:
		if w.TreePush(nucular.TreeNode, name, false) {
			w.Scrollbar.X -= ind
			w.Label(fmt.Sprintf("len: %d", v.Len), "LC")
			showArrayOrSliceContents(w, depth, addr, v, ind)
			w.Scrollbar.X += ind
			w.TreePop()
		}
	case reflect.Ptr:
		if v.Type == "" || v.Children[0].Addr == 0 {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
		} else if v.Children[0].OnlyAddr && v.Children[0].Addr != 0 {
			w.Label(fmt.Sprintf("%s = (%s)(%#x)", name, v.Type, v.Children[0].Addr), "LC")
		} else {
			if w.TreePush(nucular.TreeNode, name, false) {
				w.Scrollbar.X -= ind
				showVariable(w, depth+1, addr, "", &v.Children[0], ind)
				w.Scrollbar.X += ind
				w.TreePop()
			}
		}
	case reflect.UnsafePointer:
		w.Label(fmt.Sprintf("%s = unsafe.Pointer(%#x)", name, v.Children[0].Addr), "LC")
	case reflect.String:
		if len(v.Value) != int(v.Len) {
			w.Row(varRowHeight).Static(0, moreBtnWidth)
			w.Label(fmt.Sprintf("%s = %q", name, v.Value), "LC")
			w.Label(fmt.Sprintf("%d more", int(v.Len)-len(v.Value)), "LC")
			//TODO: detailed view for strings
			w.Row(varRowHeight).Dynamic(1)
		} else {
			w.Label(fmt.Sprintf("%s = %q", name, v.Value), "LC")
		}
	case reflect.Chan:
		if len(v.Children) == 0 {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
		} else {
			if w.TreePush(nucular.TreeNode, name, false) {
				w.Scrollbar.X -= ind
				showStructContents(w, depth, addr, v, ind)
				w.Scrollbar.X += ind
				w.TreePop()
			}
		}
	case reflect.Struct:
		if w.TreePush(nucular.TreeNode, name, false) {
			w.Scrollbar.X -= ind
			if int(v.Len) != len(v.Children) && len(v.Children) == 0 {
				loadMoreStruct(v)
				w.Label("Loading...", "LC")
			} else {
				showStructContents(w, depth, addr, v, ind)
			}
			w.Scrollbar.X += ind
			w.TreePop()
		}
	case reflect.Interface:
		if v.Children[0].Kind == reflect.Invalid {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
		} else {
			if w.TreePush(nucular.TreeNode, name, false) {
				w.Scrollbar.X -= ind
				if v.Children[0].Kind == reflect.Ptr {
					showVariable(w, depth+1, addr, "data", &v.Children[0].Children[0], ind)
				} else {
					showVariable(w, depth+1, addr, "data", &v.Children[0], ind)
				}
				w.Scrollbar.X += ind
				w.TreePop()
			}
		}
	case reflect.Map:
		if w.TreePush(nucular.TreeNode, name, false) {
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
					showVariable(w, depth+1, addr, keyname, value, ind)
				} else {
					showVariable(w, depth+1, addr, fmt.Sprintf("[%d key]", i/2), key, ind)
					showVariable(w, depth+1, addr, fmt.Sprintf("[%d value]", i/2), value, ind)
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
		}
	case reflect.Func:
		if v.Value == "" {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
		} else {
			w.Label(fmt.Sprintf("%s = %s", name, v.Value), "LC")
		}
	case reflect.Complex64, reflect.Complex128:
		w.Label(fmt.Sprintf("%s = (%s + %si)", name, v.Children[0].Value, v.Children[1].Value), "LC")
	default:
		if v.Value != "" {
			w.Label(fmt.Sprintf("%s = %s", name, v.Value), "LC")
		} else {
			w.Label(fmt.Sprintf("%s = (unknown %s)", name, v.Kind), "LC")
		}
	}
}

func showArrayOrSliceContents(w *nucular.Window, depth int, addr bool, v *api.Variable, ind int) {
	for i := range v.Children {
		showVariable(w, depth+1, addr, fmt.Sprintf("[%d]", i), &v.Children[i], ind)
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
		showVariable(w, depth+1, addr, v.Children[i].Name, &v.Children[i], ind)
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
