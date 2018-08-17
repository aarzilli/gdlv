// Copyright 2016, Gdlv Authors

package main

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"
	nstyle "github.com/aarzilli/nucular/style"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"

	"golang.org/x/mobile/event/mouse"
)

type asyncLoad struct {
	mu      sync.Mutex
	loaded  bool
	loading bool
	err     error
	load    func(*asyncLoad)
	code    byte
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

func (l *asyncLoad) startLoad() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.loading || l.loaded {
		return
	}

	l.loading = true
	go l.load(l)
}

func (l *asyncLoad) showRequest(container *nucular.Window) *nucular.Window {
	l.mu.Lock()
	defer l.mu.Unlock()

	container.Data = l

	if l.loading {
		container.Row(0).Dynamic(1)
		container.Label("Loading...", "LT")
		return nil
	}

	if !l.loaded {
		container.Row(0).Dynamic(1)
		if client == nil {
			container.Label("Connecting...", "LT")
			return nil
		}
		if client.Running() {
			container.Label("Running...", "LT")
			return nil
		}
		if curThread < 0 {
			container.Label("Process exited", "LT")
			return nil
		}

		l.loading = true
		go l.load(l)
		return nil
	}

	if l.err != nil {
		container.Row(0).Dynamic(1)
		container.Label(fmt.Sprintf("Error: %v", l.err), "LT")
		return nil
	}

	return container
}

const (
	currentGoroutineLocation = "Current location"
	userGoroutineLocation    = "User location"
	goStatementLocation      = "Go statement location"
	startLocation            = "Start location"

	popupFlags        = nucular.WindowTitle | nucular.WindowNoScrollbar | nucular.WindowMovable | nucular.WindowBorder
	dynamicPopupFlags = nucular.WindowDynamic | popupFlags
)

type wrappedGoroutine struct {
	api.Goroutine
	atBreakpoint bool
}

var goroutineLocations = []string{currentGoroutineLocation, userGoroutineLocation, goStatementLocation, startLocation}
var goroutinesPanel = struct {
	asyncLoad         asyncLoad
	goroutineLocation int
	goroutines        []wrappedGoroutine
	onlyStopped       bool
	id                int
}{
	goroutineLocation: 1,
	goroutines:        make([]wrappedGoroutine, 0, 10),
}

var stackPanel = struct {
	asyncLoad asyncLoad
	stack     []api.Stackframe
	depth     int
	id        int
}{
	depth: 20,
}

var threadsPanel = struct {
	asyncLoad asyncLoad
	threads   []*api.Thread
	id        int
}{}

var regsPanel = struct {
	asyncLoad asyncLoad
	regs      string
	lines     int
	allRegs   bool
	width     int
}{}

var breakpointsPanel = struct {
	asyncLoad   asyncLoad
	selected    int
	breakpoints []*api.Breakpoint
	id          int
}{}

type stringSlicePanel struct {
	name         string
	filterEditor nucular.TextEditor
	slice        []string
	selected     int
	interaction  func(p *stringSlicePanel, w *nucular.Window, clicked bool, idx int)
	id           int
}

var funcsPanel = stringSlicePanel{name: "functions", selected: -1, interaction: funcInteraction}
var typesPanel = stringSlicePanel{name: "types", selected: -1, interaction: sliceInteraction}
var sourcesPanel = stringSlicePanel{name: "sources", selected: -1, interaction: sourceInteraction}

var checkpointsPanel = struct {
	asyncLoad   asyncLoad
	selected    int
	checkpoints []api.Checkpoint
	id          int
}{}

var disassemblyPanel = struct {
	asyncLoad asyncLoad
	loc       api.Location
}{}

func init() {
	goroutinesPanel.asyncLoad.load = loadGoroutines
	stackPanel.asyncLoad.load = loadStacktrace
	threadsPanel.asyncLoad.load = loadThreads
	regsPanel.asyncLoad.load = loadRegs
	breakpointsPanel.asyncLoad.load = loadBreakpoints
	checkpointsPanel.asyncLoad.load = loadCheckpoints
	globalsPanel.asyncLoad.load = loadGlobals
	localsPanel.asyncLoad.load = loadLocals
	disassemblyPanel.asyncLoad.load = loadDisassembly
}

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
	gs, err := client.ListGoroutines()
	if err != nil {
		p.done(err)
		return
	}
	state, err := client.GetState()
	if err != nil {
		p.done(err)
		return
	}

	bpgoids := make([]int, 0, 10)
	for _, th := range state.Threads {
		if th.Breakpoint != nil && th.GoroutineID > 0 {
			bpgoids = append(bpgoids, th.GoroutineID)
		}
	}

	sort.Sort(goroutinesByID(gs))

	goroutinesPanel.goroutines = goroutinesPanel.goroutines[:0]
	goroutinesPanel.id++

	for _, g := range gs {
		atbp := false
		for _, bpgoid := range bpgoids {
			if bpgoid == g.ID {
				atbp = true
				break
			}
		}

		goroutinesPanel.goroutines = append(goroutinesPanel.goroutines, wrappedGoroutine{*g, atbp})
	}

	p.done(nil)
}

func updateGoroutines(container *nucular.Window) {
	w := goroutinesPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}
	style := container.Master().Style()

	goroutines := goroutinesPanel.goroutines

	w.MenubarBegin()
	w.Row(20).Static(180, 240)
	goroutinesPanel.goroutineLocation = w.ComboSimple(goroutineLocations, goroutinesPanel.goroutineLocation, 22)
	w.CheckboxText("Only stopped at breakpoint", &goroutinesPanel.onlyStopped)
	w.MenubarEnd()

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

	for _, g := range goroutines {
		w.Row(posRowHeight).Static()
		if goroutinesPanel.onlyStopped && !g.atBreakpoint {
			continue
		}
		selected := curGid == g.ID

		w.LayoutSetWidthScaled(starWidth + style.Text.Padding.X*2)
		breakpointIcon(w, g.atBreakpoint, "CT", style)

		w.LayoutFitWidth(goroutinesPanel.id, 1)
		w.SelectableLabel(fmt.Sprintf("%*d", d, g.ID), "LT", &selected)

		w.LayoutFitWidth(goroutinesPanel.id, 1)
		if g.ThreadID != 0 {
			w.SelectableLabel(fmt.Sprintf("%*d", dthread, g.ThreadID), "LT", &selected)
		} else {
			w.SelectableLabel(" ", "LT", &selected)
		}

		w.LayoutFitWidth(goroutinesPanel.id, 100)
		switch goroutineLocations[goroutinesPanel.goroutineLocation] {
		case currentGoroutineLocation:
			w.SelectableLabel(formatLocation2(g.CurrentLoc), "LT", &selected)
		case userGoroutineLocation:
			w.SelectableLabel(formatLocation2(g.UserCurrentLoc), "LT", &selected)
		case goStatementLocation:
			w.SelectableLabel(formatLocation2(g.GoStatementLoc), "LT", &selected)
		case startLocation:
			w.SelectableLabel(formatLocation2(g.StartLoc), "LT", &selected)
		}

		if selected && curGid != g.ID && !client.Running() {
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
	stackPanel.id++
	p.done(err)
}

func updateStacktrace(container *nucular.Window) {
	w := stackPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

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

	didx := digits(len(stack))
	d := hexdigits(maxpc)

	for i, frame := range stack {
		w.Row(posRowHeight).Static()
		selected := curFrame == i
		w.LayoutFitWidth(stackPanel.id, 1)
		w.SelectableLabel(fmt.Sprintf("%*d", didx, i), "LT", &selected)
		w.LayoutFitWidth(stackPanel.id, 1)
		w.SelectableLabel(fmt.Sprintf("%#0*x\n%+d", d, frame.PC, frame.FrameOffset), "LT", &selected)
		w.LayoutFitWidth(stackPanel.id, 100)
		w.SelectableLabel(formatLocation2(frame.Location), "LT", &selected)
		if selected && curFrame != i && !client.Running() {
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
	threadsPanel.id++
	p.done(err)
}

func updateThreads(container *nucular.Window) {
	w := threadsPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	threads := threadsPanel.threads

	d := 1
	if len(threads) > 0 {
		d = digits(threads[len(threads)-1].ID)
	}

	for _, thread := range threads {
		selected := curThread == thread.ID
		w.Row(posRowHeight).Static()

		w.LayoutFitWidth(threadsPanel.id, 1)
		w.SelectableLabel(fmt.Sprintf("%*d", d, thread.ID), "LT", &selected)

		w.LayoutFitWidth(threadsPanel.id, 1)
		loc := api.Location{thread.PC, thread.File, thread.Line, thread.Function}
		w.SelectableLabel(formatLocation2(loc), "LT", &selected)

		if selected && curThread != thread.ID && !client.Running() {
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
	w := regsPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	w.MenubarBegin()
	w.Row(varRowHeight).Static(100)
	if w.CheckboxText("Show All", &regsPanel.allRegs) {
		loadRegs(&regsPanel.asyncLoad)
	}
	w.MenubarEnd()

	w.Row(20 * regsPanel.lines).Static(regsPanel.width)
	w.Label(regsPanel.regs, "LT")
}

type breakpointsByID []*api.Breakpoint

func (bps breakpointsByID) Len() int           { return len(bps) }
func (bps breakpointsByID) Swap(i, j int)      { bps[i], bps[j] = bps[j], bps[i] }
func (bps breakpointsByID) Less(i, j int) bool { return bps[i].ID < bps[j].ID }

func loadBreakpoints(p *asyncLoad) {
	var err error
	breakpointsPanel.breakpoints, err = client.ListBreakpoints()
	if err == nil {
		sort.Sort(breakpointsByID(breakpointsPanel.breakpoints))
	}
	breakpointsPanel.id++
	p.done(err)
}

func updateBreakpoints(container *nucular.Window) {
	w := breakpointsPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	breakpoints := breakpointsPanel.breakpoints

	d := 1
	if len(breakpoints) > 0 {
		d = digits(breakpoints[len(breakpoints)-1].ID)
	}
	if d < 3 {
		d = 3
	}

	for _, breakpoint := range breakpoints {
		oldselectedId := breakpointsPanel.selected
		selected := breakpointsPanel.selected == breakpoint.ID
		w.Row(posRowHeight).Static()

		w.LayoutFitWidth(breakpointsPanel.id, 10)
		w.SelectableLabel(fmt.Sprintf("%*d", d, breakpoint.ID), "LT", &selected)
		bounds := w.LastWidgetBounds
		bounds.W = w.Bounds.W

		w.LayoutFitWidth(breakpointsPanel.id, 100)
		w.SelectableLabel(fmt.Sprintf("%s in %s (hit count: %d)\nat %s:%d (%#v)", breakpoint.Name, breakpoint.FunctionName, breakpoint.TotalHitCount, breakpoint.File, breakpoint.Line, breakpoint.Addr), "LT", &selected)

		if !client.Running() {
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

func execRestartCheckpoint(id int) {
	scrollbackOut := editorWriter{&scrollbackEditor, true}
	_, err := client.RestartFrom(fmt.Sprintf("c%d", id), false, nil)
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not restart from checkpoint c%d: %v\n", id, err)
		return
	}
	fmt.Fprintf(&scrollbackOut, "Process restarted from c%d\n", id)
	refreshState(refreshToFrameZero, clearStop, nil)
}

func execClearCheckpoint(id int) {
	scrollbackOut := editorWriter{&scrollbackEditor, true}
	err := client.ClearCheckpoint(id)
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not clear checkpoint c%d: %v\n", id, err)
	}
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

	w.Row(30).Static(100, 0)
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

type checkpointsByID []api.Checkpoint

func (cps checkpointsByID) Len() int           { return len(cps) }
func (cps checkpointsByID) Less(i, j int) bool { return cps[i].ID < cps[i].ID }
func (cps checkpointsByID) Swap(i, j int)      { cps[i], cps[j] = cps[j], cps[i] }

func loadCheckpoints(p *asyncLoad) {
	var err error
	checkpointsPanel.checkpoints, err = client.ListCheckpoints()
	if err == nil {
		sort.Sort(checkpointsByID(checkpointsPanel.checkpoints))
	}
	checkpointsPanel.id++
	p.done(err)
}

func updateCheckpoints(container *nucular.Window) {
	w := checkpointsPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	checkpoints := checkpointsPanel.checkpoints

	for _, checkpoint := range checkpoints {
		selected := checkpointsPanel.selected == checkpoint.ID
		w.Row(posRowHeight).Static()

		w.LayoutFitWidth(checkpointsPanel.id, 10)
		w.SelectableLabel(fmt.Sprintf("c%d", checkpoint.ID), "LT", &selected)
		bounds := w.LastWidgetBounds
		bounds.W = w.Bounds.W

		w.LayoutFitWidth(checkpointsPanel.id, 10)
		w.SelectableLabel(checkpoint.When, "LT", &selected)

		w.LayoutFitWidth(checkpointsPanel.id, 10)
		w.SelectableLabel(checkpoint.Where, "LT", &selected)

		if client.Running() {
			continue
		}

		if selected {
			checkpointsPanel.selected = checkpoint.ID
		}

		if w := w.ContextualOpen(0, image.Point{}, bounds, nil); w != nil {
			checkpointsPanel.selected = checkpoint.ID
			w.Row(20).Dynamic(1)

			if w.MenuItem(label.TA("Clear checkpoint", "LC")) {
				go execClearCheckpoint(checkpointsPanel.selected)
			}

			if w.MenuItem(label.TA("Restart from checkpoint", "LC")) {
				go execRestartCheckpoint(checkpointsPanel.selected)
			}
		}
	}
}

func (p *stringSlicePanel) update(container *nucular.Window) {
	if p.filterEditor.Filter == nil {
		p.filterEditor.Filter = spacefilter
	}
	container.Row(0).Dynamic(1)
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

	for i, value := range p.slice {
		if strings.Index(value, filter) < 0 {
			continue
		}
		w.Row(20).Static()
		selected := i == p.selected
		w.LayoutFitWidth(p.id, 100)
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

func breakpointIcon(w *nucular.Window, atbp bool, align label.Align, style *nstyle.Style) {
	if atbp {
		iconFace, style.Font = style.Font, iconFace
		w.LabelColored(breakpointIconChar, align, color.RGBA{0xff, 0x00, 0x00, 0xff})
		iconFace, style.Font = style.Font, iconFace
	} else {
		w.Spacing(1)
	}
}

func updateListingPanel(container *nucular.Window) {
	if len(listingPanel.listing) == 0 {
		updateDisassemblyPanel(container)
		return
	}

	container.Data = nil

	listingToolbar(container)

	const lineheight = 14

	container.Row(0).Dynamic(1)
	gl, listp := nucular.GroupListStart(container, len(listingPanel.listing), "listing", 0)
	if listp == nil {
		return
	}

	style := container.Master().Style()

	arroww := arrowWidth + style.Text.Padding.X*2
	starw := starWidth + style.Text.Padding.X*2

	if !listingPanel.recenterListing {
		gl.SkipToVisible(lineheight)
	}

	for gl.Next() {
		listp.Row(lineheight).Static()
		line := listingPanel.listing[gl.Index()]
		centerline := line.pc || (listingPanel.pinnedLoc != nil && line.lineno == listingPanel.pinnedLoc.Line)

		if centerline {
			rowbounds := listp.WidgetBounds()
			rowbounds.X = listp.Bounds.X
			rowbounds.W = listp.Bounds.W

			cmds := listp.Commands()
			cmds.FillRect(rowbounds, 0, style.Selectable.PressedActive.Data.Color)

			if listingPanel.recenterListing {
				gl.Center()
				listingPanel.recenterListing = false
			}
		}

		listp.LayoutSetWidth(starw)
		breakpointIcon(listp, line.bp != nil, "CC", style)
		bpbounds := listp.LastWidgetBounds

		isCurrentLine := line.pc && curFrame == 0 && !client.Running() && curThread >= 0

		listp.LayoutSetWidth(arroww)
		if isCurrentLine {
			iconFace, style.Font = style.Font, iconFace
			listp.LabelColored(arrowIconChar, "CC", color.RGBA{0xff, 0xff, 0x00, 0xff})
			iconFace, style.Font = style.Font, iconFace
		} else {
			listp.Spacing(1)
		}

		listp.LayoutFitWidth(listingPanel.id, 1)
		listp.Label(line.idx, "LC")
		listp.LayoutFitWidth(listingPanel.id, 100)
		listp.Label(line.text, "LC")
		textbounds := listp.LastWidgetBounds

		if centerline && listingPanel.recenterListing {
			listingPanel.recenterListing = false
			gl.Center()
		}

		if !client.Running() {
			ctxtbounds := bpbounds
			ctxtbounds.W = (textbounds.X + textbounds.W) - ctxtbounds.X

			if listp.Input().Mouse.Clicked(mouse.ButtonMiddle, ctxtbounds) {
				if line.bp != nil {
					go execClearBreakpoint(line.bp.ID)
				} else {
					go listingSetBreakpoint(listingPanel.file, line.lineno)
				}
			}

			if listp.Input().Mouse.Clicked(mouse.ButtonRight, ctxtbounds) {
				m := listp.Input().Mouse.Buttons[mouse.ButtonRight]
				colno := (m.ClickedPos.X - textbounds.X) / zeroWidth
				_, colno = expandTabsEx(line.textWithTabs, colno)
				colno++
				listingPanel.stepIntoInfo.Config(listingPanel.file, line.lineno, colno)
			}

			if w := listp.ContextualOpen(0, image.Point{}, ctxtbounds, nil); w != nil {
				if !listingPanel.stepIntoFilled {
					listingPanel.stepIntoFilled = true
				}
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
				if isCurrentLine {
					if listingPanel.stepIntoInfo.Valid {
						if w.MenuItem(label.TA(listingPanel.stepIntoInfo.Msg, "LC")) {
							go stepInto(&editorWriter{&scrollbackEditor, true}, listingPanel.stepIntoInfo.Call)
						}
					}
				} else {
					if w.MenuItem(label.TA("Continue to this line", "LC")) {
						go continueToLine(listingPanel.file, line.lineno)
					}
				}
			}
		}
	}
}

func listingSetBreakpoint(file string, line int) {
	setBreakpointEx(&editorWriter{&scrollbackEditor, true}, &api.Breakpoint{File: file, Line: line})
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
}

func updateDisassemblyPanel(container *nucular.Window) {
	container = disassemblyPanel.asyncLoad.showRequest(container)
	if container == nil {
		return
	}

	const lineheight = 14

	container.Row(0).Dynamic(1)

	gl, listp := nucular.GroupListStart(container, len(listingPanel.text), "disassembly", 0)
	if listp == nil {
		return
	}

	style := container.Master().Style()

	arroww := arrowWidth + style.Text.Padding.X*2
	starw := starWidth + style.Text.Padding.X*2

	lastfile, lastlineno := "", 0

	if len(listingPanel.text) > 0 && listingPanel.text[0].Loc.Function != nil {
		listp.Row(lineheight).Dynamic(1)
		listp.Label(fmt.Sprintf("TEXT %s(SB) %s", listingPanel.text[0].Loc.Function.Name(), listingPanel.text[0].Loc.File), "LC")
	}

	for gl.Next() {
		instr := listingPanel.text[gl.Index()]
		if instr.Loc.File != lastfile || instr.Loc.Line != lastlineno {
			listp.Row(lineheight).Static()
			listp.Row(lineheight).Static()
			text := ""
			if instr.Loc.File == listingPanel.file && instr.Loc.Line-1 < len(listingPanel.listing) && instr.Loc.Line-1 > 0 {
				text = strings.TrimSpace(listingPanel.listing[instr.Loc.Line-1].text)
			}
			listp.LayoutFitWidth(listingPanel.id, 1)
			listp.Label(fmt.Sprintf("%s:%d: %s", instr.Loc.File, instr.Loc.Line, text), "LC")
			lastfile, lastlineno = instr.Loc.File, instr.Loc.Line
		}
		listp.Row(lineheight).Static()

		listp.LayoutSetWidthScaled(starw)

		centerline := instr.AtPC || instr.Loc.PC == listingPanel.framePC

		if centerline {
			rowbounds := listp.WidgetBounds()
			rowbounds.X = listp.Bounds.X
			rowbounds.W = listp.Bounds.W
			cmds := listp.Commands()
			cmds.FillRect(rowbounds, 0, style.Selectable.PressedActive.Data.Color)
		}

		breakpointIcon(listp, instr.Breakpoint, "CC", style)

		listp.LayoutSetWidth(arroww)

		if instr.AtPC {
			iconFace, style.Font = style.Font, iconFace
			listp.LabelColored(arrowIconChar, "CC", color.RGBA{0xff, 0xff, 0x00, 0xff})
			iconFace, style.Font = style.Font, iconFace
		} else {
			listp.Label(" ", "LC")
		}

		listp.LayoutFitWidth(listingPanel.id, 10)
		listp.Label(fmt.Sprintf("%x", instr.Loc.PC), "LC")
		listp.LayoutFitWidth(listingPanel.id, 100)
		listp.Label(instr.Text, "LC")

		if listingPanel.recenterDisassembly && centerline {
			listingPanel.recenterDisassembly = false
			gl.Center()
		}
	}
}
