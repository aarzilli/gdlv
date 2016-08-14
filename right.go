package main

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/aarzilli/nucular"
	"github.com/derekparker/delve/service/api"
)

const (
	rightGoStack     = "Goroutines and Stack"
	rightStackLocals = "Stack and Locals"
	rigthThrLocals   = "Threads and Locals"
	rightThrRegs     = "Threads and Registers"
	rightGlobal      = "Globals"
	rightBps         = "Breakpoints"
	rightSources     = "Sources"
	rightFuncs       = "Functions"
	rightTypes       = "Types"
)

var rightcolModes = []string{rightGoStack, rightStackLocals, rigthThrLocals, rightThrRegs, rightGlobal, rightBps, rightSources, rightFuncs, rightTypes}
var rightcolMode int = 1

type rightPanel struct {
	mu      sync.Mutex
	loaded  bool
	loading bool
	name    string
	update  func(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window)
	load    func(p *rightPanel)
}

func (l *rightPanel) clear() {
	l.mu.Lock()
	l.loaded = false
	l.mu.Unlock()
}

func (l *rightPanel) done() {
	l.mu.Lock()
	l.loading = false
	l.loaded = true
	l.mu.Unlock()
	wnd.Changed()
}

var goroutinesPanel = &rightPanel{
	name:   "goroutines",
	update: updateGoroutines,
	load:   loadGoroutines,
}

var stackPanel = &rightPanel{
	name:   "stacktrace",
	update: updateStacktrace,
	load:   loadStacktrace,
}

var threadsPanel = &rightPanel{
	name:   "threads",
	update: updateThreads,
	load:   loadThreads,
}

var localsPanel = &rightPanel{
	name:   "locals",
	update: updateLocals,
	load:   loadLocals,
}

var regsPanel = &rightPanel{
	name:   "regs",
	update: updateRegs,
	load:   loadRegs,
}

var globalsPanel = &rightPanel{
	name:   "globals",
	update: updateGlobals,
	load:   loadGlobals,
}

var breakpointsPanel = &rightPanel{
	name:   "breakpoints",
	update: updateBreakpoints,
	load:   loadBreakpoints,
}

var sourcesPanel = &rightPanel{
	name:   "sources",
	update: updateSources,
	load:   loadSources,
}

var funcsPanel = &rightPanel{
	name:   "funcs",
	update: updateFuncs,
	load:   loadFuncs,
}

var typesPanel = &rightPanel{
	name:   "types",
	update: updateTypes,
	load:   loadTypes,
}

const (
	currentGoroutineLocation = "Current location"
	userGoroutineLocation    = "User location"
	goStatementLocation      = "Go statement location"
)

var goroutineLocations = []string{currentGoroutineLocation, userGoroutineLocation, goStatementLocation}
var goroutineLocation int = 1

var goroutines []*api.Goroutine
var stack []api.Stackframe
var stackDepth int = 20
var threads []*api.Thread

var localsFilterEditor = nucular.TextEditor{Filter: spacefilter}
var localsShowAddress bool = false
var args []api.Variable
var locals []api.Variable
var regs string
var globalsFilterEditor = nucular.TextEditor{Filter: spacefilter}
var globalsShowAddress bool = false
var globals []api.Variable
var breakpoints []*api.Breakpoint
var funcsFilterEditor = nucular.TextEditor{Filter: spacefilter}
var functions []string
var typesFilterEditor = nucular.TextEditor{Filter: spacefilter}
var types []string
var sourcesFilterEditor = nucular.TextEditor{Filter: spacefilter}
var sources []string

func spacefilter(ch rune) bool {
	return ch != ' ' && ch != '\t'
}

func (p *rightPanel) Update(mw *nucular.MasterWindow, container *nucular.Window) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.loading {
		container.Label("Loading...", "LT")
		return
	}

	if !p.loaded {
		if client == nil {
			container.Label("Connecting...", "LT")
			return
		}
		if running {
			container.Label("Running...", "LT")
			return
		}

		p.loading = true
		go p.load(p)
	}

	if w := container.GroupBegin(p.name, nucular.WindowBorder); w != nil {
		p.update(p, mw, w)
		w.GroupEnd()
	}
}

func loadGoroutines(p *rightPanel) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	goroutines, err = client.ListGoroutines()
	if err != nil {
		fmt.Fprintf(&out, "Could not list goroutines: %v\n", err)
		return
	}
	p.done()
}

func updateGoroutines(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	style, _ := mw.Style()

	w.MenubarBegin()
	w.Row(20).Static(180)
	w.ComboSimple(goroutineLocations, &goroutineLocation, 22)
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
		w.Label(fmt.Sprintf("%*d", d, g.ID), "LC")
		if g.ThreadID != 0 {
			w.Label(fmt.Sprintf("%*d", dthread, g.ThreadID), "LC")
		} else {
			w.Spacing(1)
		}
		switch goroutineLocations[goroutineLocation] {
		case currentGoroutineLocation:
			w.Label(formatLocation(g.CurrentLoc), "LC")
		case userGoroutineLocation:
			w.Label(formatLocation(g.UserCurrentLoc), "LC")
		case goStatementLocation:
			w.Label(formatLocation(g.GoStatementLoc), "LC")
		}
		//TODO: switch goroutine on click
	}
}

func loadStacktrace(p *rightPanel) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	stack, err = client.Stacktrace(curGid, stackDepth, nil)
	if err != nil {
		fmt.Fprintf(&out, "Could not stacktrace: %v\n", err)
		return
	}
	p.done()
}

func updateStacktrace(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	style, _ := mw.Style()

	w.MenubarBegin()
	w.Row(20).Static(120)
	if w.PropertyInt("depth:", 1, &stackDepth, 200, 1, 5) {
		go func() {
			p.clear()
			wnd.Changed()
		}()
	}
	w.MenubarEnd()

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
		w.Label(fmt.Sprintf("%*d", didx, i), "LT")
		w.Label(fmt.Sprintf("%#0*x", d, frame.PC), "LT")
		name := "(nil)"
		if frame.Function != nil {
			name = frame.Function.Name
		}
		w.Label(fmt.Sprintf("%s\nat %s:%d", name, ShortenFilePath(frame.File), frame.Line), "LT")
		// TODO: switch stack frame on click
	}
}

func loadThreads(p *rightPanel) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	threads, err = client.ListThreads()
	if err != nil {
		fmt.Fprintf(&out, "Could not list threads: %v\n", err)
		return
	}
	p.done()
}

func updateThreads(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	style, _ := mw.Style()

	pad := style.Selectable.Padding.X * 2
	d := 1
	if len(threads) > 0 {
		d = digits(threads[len(threads)-1].ID)
	}
	w.Row(20).StaticScaled(nucular.FontWidth(style.Font, "0")*d+pad, 0)

	for _, thread := range threads {
		w.Label(fmt.Sprintf("%*d", d, thread.ID), "LC")
		loc := api.Location{thread.PC, thread.File, thread.Line, thread.Function}
		w.Label(formatLocation(loc), "LC")
		//TODO: switch thread on click
	}
	w.GroupEnd()
}

func loadLocals(p *rightPanel) {
	m := map[string]int{}

	out := editorWriter{&scrollbackEditor, true}
	var err error
	args, err = client.ListFunctionArgs(api.EvalScope{curGid, curFrame}, LongLoadConfig)
	if err != nil {
		fmt.Fprintf(&out, "Could not list function arguments: %v\n", err)
		return
	}
	locals, err = client.ListLocalVariables(api.EvalScope{curGid, curFrame}, LongLoadConfig)
	if err != nil {
		fmt.Fprintf(&out, "Could not list local variables: %v\n", err)
		return
	}

	changename := func(v *api.Variable) {
		if n, ok := m[v.Name]; ok {
			n++
			m[v.Name] = n
			v.Name = fmt.Sprintf("%s(%d)", v.Name, n)
		} else {
			m[v.Name] = 0
		}
	}

	for i := range args {
		changename(&args[i])
	}
	for i := range locals {
		changename(&locals[i])
	}
	p.done()
}

func updateLocals(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	w.MenubarBegin()
	w.Row(20).Static(90, 0, 100)
	w.Label("Filter:", "LC")
	localsFilterEditor.Edit(w)
	filter := string(localsFilterEditor.Buffer)
	w.CheckboxText("Address", &localsShowAddress)
	w.MenubarEnd()
	w.Row(20).Dynamic(1)

	_, scaling := mw.Style()
	ind := int(18 * scaling)

	for i := range args {
		if strings.Index(args[i].Name, filter) >= 0 {
			showVariable(w, 0, localsShowAddress, args[i].Name, &args[i], ind)
		}
	}

	if len(args) > 0 {
		w.Row(10).Dynamic(1)
		w.Spacing(1)
		w.Row(20).Dynamic(1)
	}

	for i := range locals {
		if strings.Index(locals[i].Name, filter) >= 0 {
			showVariable(w, 0, localsShowAddress, locals[i].Name, &locals[i], ind)
		}
	}
}

func loadRegs(p *rightPanel) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	regs, err = client.ListRegisters()
	if err != nil {
		fmt.Fprintf(&out, "Could not list registers: %v\n", err)
		return
	}
	p.done()
}

func updateRegs(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	lines := 1
	for i := range regs {
		if regs[i] == '\n' {
			lines++
		}
	}
	w.Row(20 * lines).Dynamic(1)
	w.Label(regs, "LT")
}

func loadGlobals(p *rightPanel) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	globals, err = client.ListPackageVariables("", LongLoadConfig)
	if err != nil {
		fmt.Fprintf(&out, "Could not list global variabless: %v\n", err)
		return
	}
	p.done()
}

func updateGlobals(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	//TODO: display globals (must be grouped by package)
	w.MenubarBegin()
	w.Row(20).Static(90, 0, 100)
	w.Label("Filter:", "LC")
	globalsFilterEditor.Edit(w)
	filter := string(globalsFilterEditor.Buffer)
	w.CheckboxText("Address", &globalsShowAddress)
	w.MenubarEnd()
	w.Row(20).Dynamic(1)

	_, scaling := mw.Style()
	ind := int(18 * scaling)

	for i := range globals {
		if strings.Index(globals[i].Name, filter) >= 0 {
			showVariable(w, 0, globalsShowAddress, globals[i].Name, &globals[i], ind)
		}
	}
}

func loadBreakpoints(p *rightPanel) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	breakpoints, err = client.ListBreakpoints()
	if err != nil {
		fmt.Fprintf(&out, "Could not list breakpoints: %v\n", err)
		return
	}
	p.done()
}

func updateBreakpoints(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	style, _ := mw.Style()

	pad := style.Selectable.Padding.X * 2
	d := 1
	if len(breakpoints) > 0 {
		d = digits(breakpoints[len(breakpoints)-1].ID)
	}

	w.Row(40).StaticScaled(nucular.FontWidth(style.Font, "0")*d+pad, 0)
	for _, breakpoint := range breakpoints {
		w.Label(fmt.Sprintf("%*d", d, breakpoint.ID), "LT")
		w.Label(fmt.Sprintf("%s in %s\nat %s:%d (%#v)", breakpoint.Name, breakpoint.FunctionName, breakpoint.File, breakpoint.Line, breakpoint.Addr), "LT")
		//TODO: menu on right click
	}
}

func loadFuncs(p *rightPanel) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	functions, err = client.ListFunctions("")
	if err != nil {
		fmt.Fprintf(&out, "Could not list functions: %v\n", err)
		return
	}
	p.done()
}

func updateFuncs(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	updateStringSlice(mw, w, &funcsFilterEditor, functions)
}

func loadSources(p *rightPanel) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	sources, err = client.ListSources("")
	if err != nil {
		fmt.Fprintf(&out, "Could not list sources: %v\n", err)
		return
	}
	p.done()
}

func updateSources(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	updateStringSlice(mw, w, &sourcesFilterEditor, sources)
}

func loadTypes(p *rightPanel) {
	out := editorWriter{&scrollbackEditor, true}
	var err error
	types, err = client.ListTypes("")
	if err != nil {
		fmt.Fprintf(&out, "Could not list types: %v\n", err)
		return
	}
	p.done()
}

func updateTypes(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	updateStringSlice(mw, w, &typesFilterEditor, types)
}

func updateStringSlice(mw *nucular.MasterWindow, w *nucular.Window, filterEditor *nucular.TextEditor, values []string) {
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
			w.Label(fmt.Sprintf("%s = %q...+%d more", name, v.Value, int(v.Len)-len(v.Value)), "LC")
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
				//TODO: load
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
				if w.ButtonText(fmt.Sprintf("%d more", int(v.Len)-(len(v.Children)/2))) {
					//TODO: load more
				}
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
}

func showStructContents(w *nucular.Window, depth int, addr bool, v *api.Variable, ind int) {
	for i := range v.Children {
		showVariable(w, depth+1, addr, v.Children[i].Name, &v.Children[i], ind)
	}
}
