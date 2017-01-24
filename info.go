// Copyright 2016, Gdlv Authors

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"os/exec"
	"path/filepath"
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

func (l *asyncLoad) showRequest(container *nucular.Window, flags nucular.WindowFlags, name string, load func(*asyncLoad)) *nucular.Window {
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

	if w := container.GroupBegin(name, flags); w != nil {
		return w
	}
	return nil
}

const (
	currentGoroutineLocation = "Current location"
	userGoroutineLocation    = "User location"
	goStatementLocation      = "Go statement location"
	popupFlags               = nucular.WindowTitle | nucular.WindowNoScrollbar | nucular.WindowMovable | nucular.WindowBorder
	dynamicPopupFlags        = nucular.WindowDynamic | popupFlags
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

	expressions []string
	selected    int
	ed          nucular.TextEditor
	v           []*api.Variable
}{
	filterEditor: nucular.TextEditor{Filter: spacefilter},
	selected:     -1,
	ed:           nucular.TextEditor{Flags: nucular.EditSelectable | nucular.EditSigEnter | nucular.EditClipboard},
}

var regsPanel = struct {
	asyncLoad asyncLoad
	regs      string
	lines     int
	allRegs   bool
	width     int
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
}{}

type stringSlicePanel struct {
	name         string
	filterEditor nucular.TextEditor
	slice        []string
	selected     int
	interaction  func(p *stringSlicePanel, w *nucular.Window, clicked bool, idx int)
}

var funcsPanel = stringSlicePanel{name: "functions", selected: -1, interaction: funcInteraction}
var typesPanel = stringSlicePanel{name: "types", selected: -1, interaction: sliceInteraction}
var sourcesPanel = stringSlicePanel{name: "sources", selected: -1, interaction: sourceInteraction}

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

func updateGoroutines(container *nucular.Window) {
	w := goroutinesPanel.asyncLoad.showRequest(container, nucular.WindowNoHScrollbar, "goroutines", loadGoroutines)
	if w == nil {
		return
	}
	defer w.GroupEnd()
	style := container.Master().Style()

	goroutines := goroutinesPanel.goroutines

	w.MenubarBegin()
	w.Row(20).Static(180)
	goroutinesPanel.goroutineLocation = w.ComboSimple(goroutineLocations, goroutinesPanel.goroutineLocation, 22)
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

	w.Row(40).StaticScaled(zerow*d+pad, zerow*dthread+pad, 0)
	for _, g := range goroutines {
		selected := curGid == g.ID
		w.SelectableLabel(fmt.Sprintf("%*d", d, g.ID), "LT", &selected)
		if g.ThreadID != 0 {
			w.SelectableLabel(fmt.Sprintf("%*d", dthread, g.ThreadID), "LT", &selected)
		} else {
			w.SelectableLabel(" ", "LT", &selected)
		}
		switch goroutineLocations[goroutinesPanel.goroutineLocation] {
		case currentGoroutineLocation:
			w.SelectableLabel(formatLocation2(g.CurrentLoc), "LT", &selected)
		case userGoroutineLocation:
			w.SelectableLabel(formatLocation2(g.UserCurrentLoc), "LT", &selected)
		case goStatementLocation:
			w.SelectableLabel(formatLocation2(g.GoStatementLoc), "LT", &selected)
		}
		if selected && curGid != g.ID && !running {
			go func(gid int) {
				state, err := client.SwitchGoroutine(gid)
				if err != nil {
					out := editorWriter{&scrollbackEditor, true}
					fmt.Fprintf(&out, "Could not switch goroutine: %v\n", err)
				} else {
					refreshto := refreshToFrameZero
					if goroutineLocations[goroutinesPanel.goroutineLocation] == userGoroutineLocation {
						refreshto = refreshToUserFrame
					}
					go refreshState(refreshto, clearGoroutineSwitch, state)
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

func updateStacktrace(container *nucular.Window) {
	w := stackPanel.asyncLoad.showRequest(container, nucular.WindowNoHScrollbar, "stack", loadStacktrace)
	if w == nil {
		return
	}
	defer w.GroupEnd()
	style := container.Master().Style()

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
		w.SelectableLabel(formatLocation2(frame.Location), "LT", &selected)
		if selected && curFrame != i && !running {
			curFrame = i
			go refreshState(refreshToSameFrame, clearFrameSwitch, nil)
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

func updateThreads(container *nucular.Window) {
	w := threadsPanel.asyncLoad.showRequest(container, nucular.WindowNoHScrollbar, "threads", loadThreads)
	if w == nil {
		return
	}
	defer w.GroupEnd()
	style := w.Master().Style()

	threads := threadsPanel.threads

	pad := style.Selectable.Padding.X * 2
	d := 1
	if len(threads) > 0 {
		d = digits(threads[len(threads)-1].ID)
	}
	w.Row(40).StaticScaled(zeroWidth*d+pad, 0)

	for _, thread := range threads {
		selected := curThread == thread.ID
		w.SelectableLabel(fmt.Sprintf("%*d", d, thread.ID), "LT", &selected)
		loc := api.Location{thread.PC, thread.File, thread.Line, thread.Function}
		w.SelectableLabel(formatLocation2(loc), "LT", &selected)
		if selected && curThread != thread.ID && !running {
			go func(tid int) {
				state, err := client.SwitchThread(tid)
				if err != nil {
					out := editorWriter{&scrollbackEditor, true}
					fmt.Fprintf(&out, "Could not switch thread: %v\n", err)
				} else {
					go refreshState(refreshToFrameZero, clearGoroutineSwitch, state)
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
	for i := range localsPanel.expressions {
		loadOneExpr(i)
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
			showVariable(w, 0, localsPanel.showAddr, -1, args[i].Name, &args[i])
		}
	}

	if len(args) > 0 {
		w.Row(varRowHeight / 2).Dynamic(1)
		w.Spacing(1)
	}

	for i := range locals {
		if strings.Index(locals[i].Name, filter) >= 0 {
			showVariable(w, 0, localsPanel.showAddr, -1, locals[i].Name, &locals[i])
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
	var err error
	localsPanel.v[i], err = client.EvalVariable(api.EvalScope{curGid, curFrame}, localsPanel.expressions[i], LongLoadConfig)
	if err != nil {
		localsPanel.v[i] = &api.Variable{Name: localsPanel.expressions[i], Unreadable: err.Error()}
	}
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

func showExprMenu(w *nucular.Window, exprMenuIdx int, v *api.Variable) {
	if running {
		return
	}
	if exprMenuIdx >= 0 && exprMenuIdx < len(localsPanel.expressions) {
		if w := w.ContextualOpen(0, image.Point{}, w.LastWidgetBounds, nil); w != nil {
			w.Row(20).Dynamic(1)

			if fn := detailsAvailable(localsPanel.v[exprMenuIdx]); fn != nil {
				if w.MenuItem(label.TA("Details", "LC")) {
					fn(w.Master(), localsPanel.v[exprMenuIdx])
				}
			}
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
	} else if fn := detailsAvailable(v); fn != nil {
		if w := w.ContextualOpen(0, image.Point{}, w.LastWidgetBounds, nil); w != nil {
			w.Row(20).Dynamic(1)
			if w.MenuItem(label.TA("Details", "LC")) {
				fn(w.Master(), v)
			}
		}
	}
}

func loadRegs(p *asyncLoad) {
	regs, err := client.ListRegisters(0, regsPanel.allRegs)
	regsPanel.regs = expandTabs(regs.String())
	regsPanel.lines = 1
	lineStart := 0
	maxline := 0
	for i := range regsPanel.regs {
		if regsPanel.regs[i] == '\n' {
			if lw := i - lineStart; lw > maxline {
				maxline = lw
			}
			lineStart = i + 1
			regsPanel.lines++
		}
	}
	regsPanel.width = zeroWidth * maxline
	p.done(err)
}

func updateRegs(container *nucular.Window) {
	w := regsPanel.asyncLoad.showRequest(container, 0, "registers", loadRegs)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	w.MenubarBegin()
	w.Row(varRowHeight).Static(100)
	if w.CheckboxText("Show All", &regsPanel.allRegs) {
		loadRegs(&regsPanel.asyncLoad)
	}
	w.MenubarEnd()

	w.Row(20 * regsPanel.lines).Static(regsPanel.width)
	w.Label(regsPanel.regs, "LT")
}

func loadGlobals(p *asyncLoad) {
	var err error
	globalsPanel.globals, err = client.ListPackageVariables("", LongLoadConfig)
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
			showVariable(w, 0, globalsPanel.showAddr, -1, globals[i].Name, &globals[i])
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

func updateBreakpoints(container *nucular.Window) {
	w := breakpointsPanel.asyncLoad.showRequest(container, nucular.WindowNoHScrollbar, "breakpoints", loadBreakpoints)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	style := w.Master().Style()

	breakpoints := breakpointsPanel.breakpoints

	pad := style.Selectable.Padding.X * 2
	d := 1
	if len(breakpoints) > 0 {
		d = digits(breakpoints[len(breakpoints)-1].ID)
	}
	if d < 3 {
		d = 3
	}

	w.Row(40).StaticScaled(zeroWidth*d+pad, 0)
	for _, breakpoint := range breakpoints {
		oldselectedId := breakpointsPanel.selected
		selected := breakpointsPanel.selected == breakpoint.ID
		w.SelectableLabel(fmt.Sprintf("%*d", d, breakpoint.ID), "LT", &selected)
		bounds := w.LastWidgetBounds
		bounds.W = w.Bounds.W
		w.SelectableLabel(fmt.Sprintf("%s in %s\nat %s:%d (%#v)", breakpoint.Name, breakpoint.FunctionName, breakpoint.File, breakpoint.Line, breakpoint.Addr), "LT", &selected)
		if !running {
			if selected {
				breakpointsPanel.selected = breakpoint.ID
			}

			if w := w.ContextualOpen(0, image.Point{}, bounds, nil); w != nil {
				breakpointsPanel.selected = breakpoint.ID
				w.Row(20).Dynamic(1)
				if w.MenuItem(label.TA("Edit...", "LC")) {
					if bp := breakpointsPanelSelectedBreakpoint(); bp != nil {
						openBreakpointEditor(w.Master(), bp)
					}
				}
				if w.MenuItem(label.TA("Clear", "LC")) {
					go execClearBreakpoint(breakpointsPanel.selected)
				}
				if w.MenuItem(label.TA("Clear All", "LC")) {
					go func() {
						scrollbackOut := editorWriter{&scrollbackEditor, true}
						for _, bp := range breakpointsPanel.breakpoints {
							if bp.ID < 0 {
								continue
							}
							_, err := client.ClearBreakpoint(bp.ID)
							if err != nil {
								fmt.Fprintf(&scrollbackOut, "Could not clear breakpoint %d: %v\n", bp.ID, err)
							}
						}
						FrozenBreakpoints = nil
						refreshState(refreshToSameFrame, clearBreakpoint, nil)
						wnd.Changed()
					}()
				}
			}

			if breakpointsPanel.selected != oldselectedId {
				if bp := breakpointsPanelSelectedBreakpoint(); bp != nil {
					listingPanel.pinnedLoc = &api.Location{File: bp.File, Line: bp.Line, PC: bp.Addr}
					go refreshState(refreshToSameFrame, clearNothing, nil)
				}
			}
		}
	}
}

func breakpointsPanelSelectedBreakpoint() *api.Breakpoint {
	for _, bp := range breakpointsPanel.breakpoints {
		if bp.ID == breakpointsPanel.selected {
			return bp
		}
	}
	return nil
}

func execClearBreakpoint(id int) {
	scrollbackOut := editorWriter{&scrollbackEditor, true}
	bp, err := client.ClearBreakpoint(id)
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not clear breakpoint %d: %v\n", id, err)
	}
	removeFrozenBreakpoint(bp)
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
	wnd.Changed()
}

type breakpointEditor struct {
	bp          *api.Breakpoint
	printEditor nucular.TextEditor
	condEditor  nucular.TextEditor
}

func openBreakpointEditor(mw nucular.MasterWindow, bp *api.Breakpoint) {
	var ed breakpointEditor
	ed.bp = bp

	ed.printEditor.Flags = nucular.EditMultiline | nucular.EditClipboard | nucular.EditSelectable
	for i := range bp.Variables {
		ed.printEditor.Buffer = append(ed.printEditor.Buffer, []rune(fmt.Sprintf("%s\n", bp.Variables[i]))...)
	}

	ed.condEditor.Flags = nucular.EditClipboard | nucular.EditSelectable
	ed.condEditor.Buffer = []rune(ed.bp.Cond)

	mw.PopupOpen(fmt.Sprintf("Editing breakpoint %d", breakpointsPanel.selected), dynamicPopupFlags, rect.Rect{100, 100, 400, 700}, true, ed.update)
}

func (bped *breakpointEditor) update(w *nucular.Window) {
	w.Row(20).Dynamic(2)
	if w.OptionText("breakpoint", !bped.bp.Tracepoint) {
		bped.bp.Tracepoint = false
	}
	if w.OptionText("tracepoint", bped.bp.Tracepoint) {
		bped.bp.Tracepoint = true
	}

	w.Row(20).Static(100, 100, 150)
	arguments := bped.bp.LoadArgs != nil
	w.CheckboxText("Arguments", &arguments)
	locals := bped.bp.LoadLocals != nil
	w.CheckboxText("Locals", &locals)
	w.PropertyInt("Stacktrace", 0, &bped.bp.Stacktrace, 200, 1, 10)

	verboseArguments, verboseLocals := false, false
	w.Row(20).Static(20, 100, 100)
	w.Spacing(1)
	if arguments {
		verboseArguments = bped.bp.LoadArgs != nil && *bped.bp.LoadArgs == LongLoadConfig
		w.CheckboxText("-v", &verboseArguments)
	} else {
		w.Spacing(1)
	}
	if locals {
		verboseLocals = bped.bp.LoadLocals != nil && *bped.bp.LoadLocals == LongLoadConfig
		w.CheckboxText("-v", &verboseLocals)
	} else {
		w.Spacing(1)
	}

	if arguments {
		if verboseArguments {
			bped.bp.LoadArgs = &LongLoadConfig
		} else {
			bped.bp.LoadArgs = &ShortLoadConfig
		}
	} else {
		bped.bp.LoadArgs = nil
	}

	if locals {
		if verboseLocals {
			bped.bp.LoadLocals = &LongLoadConfig
		} else {
			bped.bp.LoadLocals = &ShortLoadConfig
		}
	} else {
		bped.bp.LoadLocals = nil
	}

	w.Row(20).Dynamic(1)
	w.Label("Print:", "LC")
	w.Row(100).Dynamic(1)
	bped.printEditor.Edit(w)

	w.Row(20).Static(70, 0)
	w.Label("Condition:", "LC")
	bped.condEditor.Edit(w)

	w.Row(20).Static(0, 80, 80)
	w.Spacing(1)
	if w.ButtonText("Cancel") {
		refreshState(refreshToSameFrame, clearBreakpoint, nil)
		w.Close()
	}
	if w.ButtonText("OK") {
		bped.bp.Cond = string(bped.condEditor.Buffer)
		bped.bp.Variables = bped.bp.Variables[:0]
		for _, p := range strings.Split(string(bped.printEditor.Buffer), "\n") {
			if p == "" {
				continue
			}
			bped.bp.Variables = append(bped.bp.Variables, p)
		}
		go bped.amendBreakpoint()
		w.Close()
	}
}

func (bped *breakpointEditor) amendBreakpoint() {
	err := client.AmendBreakpoint(bped.bp)
	if err != nil {
		scrollbackOut := editorWriter{&scrollbackEditor, true}
		fmt.Fprintf(&scrollbackOut, "Could not amend breakpoint: %v\n", err)
	}
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
}

func (p *stringSlicePanel) update(container *nucular.Window) {
	if p.filterEditor.Filter == nil {
		p.filterEditor.Filter = spacefilter
	}
	w := container.GroupBegin(p.name, 0)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	w.MenubarBegin()
	w.Row(20).Static(90, 0)
	w.Label("Filter:", "LC")
	p.filterEditor.Edit(w)
	w.MenubarEnd()

	filter := string(p.filterEditor.Buffer)

	w.Row(20).Dynamic(1)
	for i, value := range p.slice {
		if strings.Index(value, filter) < 0 {
			continue
		}
		selected := i == p.selected
		clicked := w.SelectableLabel(value, "LC", &selected)
		if selected {
			p.selected = i
		}
		if p.interaction != nil {
			p.interaction(p, w, clicked, i)
		}
	}
}

func funcInteraction(p *stringSlicePanel, w *nucular.Window, clicked bool, idx int) {
	if clicked {
		locs, err := client.FindLocation(api.EvalScope{curGid, curFrame}, p.slice[p.selected])
		if err == nil && len(locs) == 1 {
			listingPanel.pinnedLoc = &locs[0]
			go refreshState(refreshToSameFrame, clearNothing, nil)
		}
	}
	if w := w.ContextualOpen(0, image.Point{}, w.LastWidgetBounds, nil); w != nil {
		w.Row(20).Dynamic(1)
		if w.MenuItem(label.TA("Set breakpoint", "LC")) {
			go functionListSetBreakpoint(p.slice[idx])
		}
		if w.MenuItem(label.TA("Copy to clipboard", "LC")) {
			clipboard.Set(p.slice[idx])
		}
	}
}

func functionListSetBreakpoint(name string) {
	setBreakpointEx(&editorWriter{&scrollbackEditor, true}, &api.Breakpoint{FunctionName: name, Line: -1})
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
}

func sourceInteraction(p *stringSlicePanel, w *nucular.Window, clicked bool, idx int) {
	if clicked {
		listingPanel.pinnedLoc = &api.Location{File: p.slice[p.selected], Line: 1}
		go refreshState(refreshToSameFrame, clearNothing, nil)
	}
	if w := w.ContextualOpen(0, image.Point{}, w.LastWidgetBounds, nil); w != nil {
		w.Row(20).Dynamic(1)
		if w.MenuItem(label.TA("Copy to clipboard", "LC")) {
			clipboard.Set(p.slice[idx])
		}
	}
}

func sliceInteraction(p *stringSlicePanel, w *nucular.Window, clicked bool, idx int) {
	if w := w.ContextualOpen(0, image.Point{}, w.LastWidgetBounds, nil); w != nil {
		w.Row(20).Dynamic(1)
		if w.MenuItem(label.TA("Copy to clipboard", "LC")) {
			clipboard.Set(p.slice[idx])
		}
	}
}

func showVariable(w *nucular.Window, depth int, addr bool, exprMenu int, name string, v *api.Variable) {
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
	w.Row(varRowHeight).StaticScaled(84 * zeroWidth)
	if v.Unreadable != "" {
		w.Label(fmt.Sprintf("%s = (unreadable %s)", name, v.Unreadable), "LC")
		showExprMenu(w, exprMenu, v)
		return
	}

	if depth > 0 && v.Addr == 0 {
		w.Label(fmt.Sprintf("%s = nil", name, v.Type), "LC")
		showExprMenu(w, exprMenu, v)
		return
	}

	switch v.Kind {
	case reflect.Slice:
		if !w.TreeIsOpen(varname) {
			name += " = " + v.SinglelineString()
		}
		if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
			showExprMenu(w, exprMenu, v)
			w.Label(fmt.Sprintf("len: %d cap: %d", v.Len, v.Cap), "LC")
			showArrayOrSliceContents(w, depth, addr, v)
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu, v)
		}
	case reflect.Array:
		if !w.TreeIsOpen(varname) {
			name += " = " + v.SinglelineString()
		}
		if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
			showExprMenu(w, exprMenu, v)
			w.Label(fmt.Sprintf("len: %d", v.Len), "LC")
			showArrayOrSliceContents(w, depth, addr, v)
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu, v)
		}
	case reflect.Ptr:
		if v.Type == "" || v.Children[0].Addr == 0 {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
			showExprMenu(w, exprMenu, v)
		} else if v.Children[0].OnlyAddr && v.Children[0].Addr != 0 {
			w.Label(fmt.Sprintf("%s = (%s)(%#x)", name, v.Type, v.Children[0].Addr), "LC")
			showExprMenu(w, exprMenu, v)
		} else {
			if !w.TreeIsOpen(varname) {
				name += " = " + v.SinglelineString()
			}
			if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
				showExprMenu(w, exprMenu, v)
				showVariable(w, depth+1, addr, -1, "", &v.Children[0])
				w.TreePop()
			} else {
				showExprMenu(w, exprMenu, v)
			}
		}
	case reflect.UnsafePointer:
		w.Label(fmt.Sprintf("%s = unsafe.Pointer(%#x)", name, v.Children[0].Addr), "LC")
		showExprMenu(w, exprMenu, v)
	case reflect.String:
		if v.Len == int64(len(v.Value)) {
			w.Label(fmt.Sprintf("%s = %q", name, v.Value), "LC")
		} else {
			w.Label(fmt.Sprintf("%s = %q...", name, v.Value), "LC")
		}
		showExprMenu(w, exprMenu, v)
	case reflect.Chan:
		if len(v.Children) == 0 {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
			showExprMenu(w, exprMenu, v)
		} else {
			if !w.TreeIsOpen(varname) {
				name += " = " + v.SinglelineString()
			}
			if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
				showExprMenu(w, exprMenu, v)
				showStructContents(w, depth, addr, v)
				w.TreePop()
			} else {
				showExprMenu(w, exprMenu, v)
			}
		}
	case reflect.Struct:
		if !w.TreeIsOpen(varname) {
			name += " = " + v.SinglelineString()
		}
		if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
			showExprMenu(w, exprMenu, v)
			if int(v.Len) != len(v.Children) && len(v.Children) == 0 {
				loadMoreStruct(v)
				w.Label("Loading...", "LC")
			} else {
				showStructContents(w, depth, addr, v)
			}
			w.TreePop()
		} else {
			showExprMenu(w, exprMenu, v)
		}
	case reflect.Interface:
		if v.Children[0].Kind == reflect.Invalid {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
			showExprMenu(w, exprMenu, v)
		} else {
			if !w.TreeIsOpen(varname) {
				name += " = " + v.SinglelineString()
			}
			if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
				showExprMenu(w, exprMenu, v)
				if v.Children[0].Kind == reflect.Ptr {
					showVariable(w, depth+1, addr, -1, "data", &v.Children[0].Children[0])
				} else {
					showVariable(w, depth+1, addr, -1, "data", &v.Children[0])
				}
				w.TreePop()
			} else {
				showExprMenu(w, exprMenu, v)
			}
		}
	case reflect.Map:
		if !w.TreeIsOpen(varname) {
			name += " = " + v.SinglelineString()
		}
		if w.TreePushNamed(nucular.TreeNode, varname, name, false) {
			showExprMenu(w, exprMenu, v)
			for i := 0; i < len(v.Children); i += 2 {
				key, value := &v.Children[i], &v.Children[i+1]
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
			showExprMenu(w, exprMenu, v)
		}
	case reflect.Func:
		if v.Value == "" {
			w.Label(fmt.Sprintf("%s = nil", name), "LC")
		} else {
			w.Label(fmt.Sprintf("%s = %s", name, v.Value), "LC")
		}
		showExprMenu(w, exprMenu, v)
	case reflect.Complex64, reflect.Complex128:
		w.Label(fmt.Sprintf("%s = (%s + %si)", name, v.Children[0].Value, v.Children[1].Value), "LC")
		showExprMenu(w, exprMenu, v)
	default:
		if v.Value != "" {
			if (v.Kind == reflect.Int || v.Kind == reflect.Uint) && ((v.Type == "uint8") || (v.Type == "int32")) && strings.Index(v.Value, " ") < 0 {
				n, _ := strconv.Atoi(v.Value)
				v.Value = fmt.Sprintf("%s %q", v.Value, n)
			}

			w.Label(fmt.Sprintf("%s = %s", name, v.Value), "LC")
		} else {
			w.Label(fmt.Sprintf("%s = (unknown %s)", name, v.Kind), "LC")
		}
		showExprMenu(w, exprMenu, v)
	}
}

func showArrayOrSliceContents(w *nucular.Window, depth int, addr bool, v *api.Variable) {
	for i := range v.Children {
		showVariable(w, depth+1, addr, -1, fmt.Sprintf("[%d]", i), &v.Children[i])
	}
	if len(v.Children) != int(v.Len) {
		w.Row(varRowHeight).Static(moreBtnWidth)
		if w.ButtonText(fmt.Sprintf("%d more", int(v.Len)-len(v.Children))) {
			loadMoreArrayOrSlice(v)
		}
	}
}

func showStructContents(w *nucular.Window, depth int, addr bool, v *api.Variable) {
	for i := range v.Children {
		showVariable(w, depth+1, addr, -1, v.Children[i].Name, &v.Children[i])
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

func abbrevFileName(path string) string {
	gopath := os.Getenv("GOPATH")
	if gopath == "" {
		gopath = filepath.Join(os.Getenv("HOME"), "go")
	}
	if len(gopath) > 0 && gopath[len(gopath)-1] != '/' {
		gopath = gopath + "/"
	}
	goroot := os.Getenv("GOROOT")
	if goroot == "" {
		goexe, err := exec.LookPath("go")
		if err == nil {
			goroot = filepath.Dir(filepath.Dir(goexe))
		}
	}
	if len(goroot) > 0 && goroot[len(goroot)-1] != '/' {
		goroot = goroot + "/"
	}

	if gopath != "" && strings.HasPrefix(path, gopath) {
		return "$GOPATH/" + path[len(gopath):]
	}
	if goroot != "" && strings.HasPrefix(path, goroot) {
		return "$GOROOT/" + path[len(goroot):]
	}
	return path
}

func updateListingPanel(container *nucular.Window) {
	const lineheight = 14

	listp := container.GroupBegin("listing", nucular.WindowNoHScrollbar)
	if listp == nil {
		return
	}
	defer listp.GroupEnd()

	style := container.Master().Style()

	arroww := arrowWidth + style.Text.Padding.X*2
	starw := starWidth + style.Text.Padding.X*2

	idxw := style.Text.Padding.X * 2
	if len(listingPanel.listing) > 0 {
		idxw += nucular.FontWidth(style.Font, listingPanel.listing[len(listingPanel.listing)-1].idx)
	}

	scrollbary := listp.Scrollbar.Y

	for _, line := range listingPanel.listing {
		listp.Row(lineheight).StaticScaled(starw, arroww, idxw, 0)

		rowbounds := listp.WidgetBounds()
		rowbounds.W = listp.Bounds.W

		centerline := line.pc || (listingPanel.pinnedLoc != nil && line.lineno == listingPanel.pinnedLoc.Line)

		if centerline {
			cmds := listp.Commands()
			cmds.FillRect(rowbounds, 0, style.Selectable.PressedActive.Data.Color)
		}

		if line.bp != nil {
			iconFace, style.Font = style.Font, iconFace
			listp.LabelColored(breakpointIcon, "CC", color.RGBA{0xff, 0x00, 0x00, 0xff})
			iconFace, style.Font = style.Font, iconFace
		} else {
			listp.Spacing(1)
		}

		if centerline && listingPanel.recenterListing {
			listingPanel.recenterListing = false
			if above, below := listp.Invisible(); above || below {
				scrollbary = listp.At().Y - listp.Bounds.H/2
				if scrollbary < 0 {
					scrollbary = 0
				}
			}
		}

		if line.pc && curFrame == 0 {
			iconFace, style.Font = style.Font, iconFace
			listp.LabelColored(arrowIcon, "CC", color.RGBA{0xff, 0xff, 0x00, 0xff})
			iconFace, style.Font = style.Font, iconFace
		} else {
			listp.Spacing(1)
		}
		listp.Label(line.idx, "LC")
		listp.Label(line.text, "LC")

		if !running {
			if w := listp.ContextualOpen(0, image.Point{}, rowbounds, nil); w != nil {
				w.Row(20).Dynamic(1)
				if line.bp != nil {
					if w.MenuItem(label.TA("Edit breakpoint", "LC")) {
						openBreakpointEditor(w.Master(), line.bp)
					}
					if w.MenuItem(label.TA("Clear breakpoint", "LC")) {
						go execClearBreakpoint(line.bp.ID)
					}
				} else {
					if w.MenuItem(label.TA("Set breakpoint", "LC")) {
						go listingSetBreakpoint(listingPanel.file, line.lineno)
					}
				}
			}
		}
	}

	if scrollbary != listp.Scrollbar.Y {
		listp.Scrollbar.Y = scrollbary
		wnd.Changed()
	}
}

func listingSetBreakpoint(file string, line int) {
	setBreakpointEx(&editorWriter{&scrollbackEditor, true}, &api.Breakpoint{File: file, Line: line})
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
}

func updateDisassemblyPanel(container *nucular.Window) {
	const lineheight = 14

	listp := container.GroupBegin("disassembly", nucular.WindowNoHScrollbar)
	if listp == nil {
		return
	}
	defer listp.GroupEnd()

	style := container.Master().Style()

	arroww := arrowWidth + style.Text.Padding.X*2
	starw := starWidth + style.Text.Padding.X*2

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

	scrollbary := listp.Scrollbar.Y

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
			iconFace, style.Font = style.Font, iconFace
			listp.LabelColored(breakpointIcon, "CC", color.RGBA{0xff, 0x00, 0x00, 0xff})
			iconFace, style.Font = style.Font, iconFace
		} else {
			listp.Label(" ", "LC")
		}

		if instr.AtPC {
			if listingPanel.recenterDisassembly {
				listingPanel.recenterDisassembly = false
				if above, below := listp.Invisible(); above || below {
					scrollbary = listp.At().Y - listp.Bounds.H/2
					if scrollbary < 0 {
						scrollbary = 0
					}
				}
			}
			iconFace, style.Font = style.Font, iconFace
			listp.LabelColored(arrowIcon, "CC", color.RGBA{0xff, 0xff, 0x00, 0xff})
			iconFace, style.Font = style.Font, iconFace
		} else {
			listp.Label(" ", "LC")
		}

		listp.Label(fmt.Sprintf("%#x", instr.Loc.PC), "LC")
		listp.Label(instr.Text, "LC")
	}

	if scrollbary != listp.Scrollbar.Y {
		listp.Scrollbar.Y = scrollbary
		wnd.Changed()
	}
}

type openDetailsWindowFn func(nucular.MasterWindow, *api.Variable)

func detailsAvailable(v *api.Variable) openDetailsWindowFn {
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
	v          *api.Variable
	mode       stringViewerMode
	numberMode numberMode
	ed         nucular.TextEditor
	mu         sync.Mutex
}

func newStringViewer(mw nucular.MasterWindow, v *api.Variable) {
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
			n, _ := strconv.Atoi(sv.v.Children[i].Value)
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
			n, _ := strconv.Atoi(sv.v.Children[i].Value)
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
			expr := fmt.Sprintf("(*(*%q)(%#x))[%d:]", sv.v.RealType, sv.v.Addr, len(sv.v.Value))
			lv, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, expr, LongLoadConfig)
			if err != nil {
				out := editorWriter{&scrollbackEditor, true}
				fmt.Fprintf(&out, "Error loading string contents %s: %v\n", expr, err)
			} else {
				switch sv.v.Kind {
				case reflect.String:
					sv.v.Value += lv.Value
				case reflect.Array, reflect.Slice:
					sv.v.Children = append(sv.v.Children, lv.Children...)
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
	v          *api.Variable
	displayLen int
	mode       numberMode
	ed         nucular.TextEditor
	mu         sync.Mutex
}

func newIntArrayViewer(mw nucular.MasterWindow, v *api.Variable) {
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
		array[i], _ = strconv.ParseInt(av.v.Children[i].Value, 10, 64)
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
	v    *api.Variable
	mode numberMode
	ed   nucular.TextEditor
}

func newIntViewer(mw nucular.MasterWindow, v *api.Variable) {
	iv := &intViewer{v: v}
	iv.mode = decMode
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
		w.Close()
	}
}

func (iv *intViewer) setupView() {
	n, _ := strconv.Atoi(iv.v.Value)
	switch iv.mode {
	case decMode:
		iv.ed.Buffer = []rune(fmt.Sprintf("%d", n))
	case hexMode:
		iv.ed.Buffer = []rune(fmt.Sprintf("%x", n))
	case octMode:
		iv.ed.Buffer = []rune(fmt.Sprintf("%o", n))
	}
}

func formatLocation2(loc api.Location) string {
	name := "(nil)"
	if loc.Function != nil {
		name = loc.Function.Name
	}
	return fmt.Sprintf("%s\nat %s:%d", name, ShortenFilePath(loc.File), loc.Line)
}
