package main

import (
	"fmt"
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
var args []api.Variable
var locals []api.Variable
var regs string
var globals []api.Variable
var breakpoints []*api.Breakpoint
var functions []string
var types []string
var sources []string

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
		// TODO: multiline text drawing
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
	p.done()
}

func updateLocals(p *rightPanel, mw *nucular.MasterWindow, w *nucular.Window) {
	w.Row(20).Dynamic(1)
	w.Label("Not implemented", "LT")
	//TODO: implement
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
	//TODO: display registers
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
	//TODO: display breakpoints
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

func updateFuncs(p *rightPanel, mw *nucular.MasterWindow, container *nucular.Window) {
	//TODO: display functions
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

func updateSources(p *rightPanel, mw *nucular.MasterWindow, container *nucular.Window) {
	//TODO: display sources
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

func updateTypes(p *rightPanel, mw *nucular.MasterWindow, container *nucular.Window) {
	//TODO: display types
}
