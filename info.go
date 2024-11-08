// Copyright 2016, Gdlv Authors

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/mobile/event/key"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/clipboard"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"
	nstyle "github.com/aarzilli/nucular/style"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/prettyprint"

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
	waitReason   string
}

var goroutineLocations = []string{currentGoroutineLocation, userGoroutineLocation, goStatementLocation, startLocation}
var goroutinesPanel = struct {
	asyncLoad         asyncLoad
	goroutineLocation int
	goroutines        []wrappedGoroutine
	groups            []api.GoroutineGroup
	tooManyGroups     bool
	onlyStopped       bool
	id                int
	limit             int
	rules             []*goroutineFilterRule
	rulesChanged      bool
}{
	goroutineLocation: 1,
	goroutines:        make([]wrappedGoroutine, 0, 10),
	limit:             100,
	rules:             []*goroutineFilterRule{newGoroutineFilterRule()},
}

var stackPanel = struct {
	asyncLoad    asyncLoad
	stack        []Stackframe
	isnew        []bool
	ancestors    []Ancestor
	depth        int
	showDeferPos bool
	id           int
	deferID      int
	mode         int
	full         bool
}{
	depth: 50,
}

type Stackframe struct {
	api.Stackframe
	shortName string
}

type Ancestor struct {
	ID    int64
	Stack []Stackframe

	Unreadable string
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
	interaction  func(p *stringSlicePanel, w *nucular.Window, clicked bool, idx int, bounds rect.Rect)
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
	autoCheckpointsPanel.asyncLoad.load = loadAutoCheckpoints
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
	lim := goroutinesPanel.limit
	if lim == 0 {
		lim = 100
	}

	filters := []api.ListGoroutinesFilter{}
	groupby := (*api.GoroutineGroupingOptions)(nil)

	rules := goroutinesPanel.rules
	goroutinesPanel.rules = goroutinesPanel.rules[:0]
	for i := range rules {
		if !rules[i].empty() {
			switch rules[i].kind {
			case goroutinesRuleKinds[1]: // With
				filters = append(filters, api.ListGoroutinesFilter{
					Kind: rules[i].goroutineField(),
					Arg:  rules[i].arg,
				})
			case goroutinesRuleKinds[2]: // Without
				filters = append(filters, api.ListGoroutinesFilter{
					Kind:    rules[i].goroutineField(),
					Negated: true,
					Arg:     rules[i].arg,
				})
			case goroutinesRuleKinds[3]: // Group by
				groupby = &api.GoroutineGroupingOptions{
					GroupBy:         rules[i].goroutineField(),
					GroupByKey:      rules[i].arg,
					MaxGroupMembers: 5,
					MaxGroups:       lim,
				}
				lim = 0
			}
			goroutinesPanel.rules = append(goroutinesPanel.rules, rules[i])
		}
	}
	if len(goroutinesPanel.rules) == 0 || goroutinesPanel.rules[len(goroutinesPanel.rules)-1].empty() {
		goroutinesPanel.rules = append(goroutinesPanel.rules, newGoroutineFilterRule())
	}

	gs, groups, _, tooManyGroups, err := client.ListGoroutinesWithFilter(0, lim, filters, groupby, nil)
	if err != nil {
		p.done(err)
		return
	}

	state, err := client.GetState()
	if err != nil {
		p.done(err)
		return
	}

	bpgoids := make([]int64, 0, 10)
	for _, th := range state.Threads {
		if th.Breakpoint != nil && th.GoroutineID > 0 {
			bpgoids = append(bpgoids, th.GoroutineID)
		}
	}

	if groupby == nil {
		sort.Sort(goroutinesByID(gs))
	}

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

		goroutinesPanel.goroutines = append(goroutinesPanel.goroutines, wrappedGoroutine{*g, atbp, goroutineFormatWaitReason(g)})
	}
	goroutinesPanel.groups = groups
	goroutinesPanel.tooManyGroups = tooManyGroups
	goroutinesPanel.rulesChanged = false

	if LogOutputNice != nil {
		logf("Goroutines:\n")
		for i := range goroutinesPanel.goroutines {
			g := goroutinesPanel.goroutines[i]
			fmt.Fprintf(LogOutputNice, "\t%d %#x in %s %s:%d\n", g.ID, g.UserCurrentLoc.PC, g.UserCurrentLoc.Function.Name(), g.UserCurrentLoc.File, g.UserCurrentLoc.Line)
		}
	}

	p.done(nil)
}

func goroutineGetDisplayLiocation(g *api.Goroutine) api.Location {
	switch goroutineLocations[goroutinesPanel.goroutineLocation] {
	default:
		fallthrough
	case currentGoroutineLocation:
		return g.CurrentLoc
	case userGoroutineLocation:
		return g.UserCurrentLoc
	case goStatementLocation:
		return g.GoStatementLoc
	case startLocation:
		return g.StartLoc
	}
}

func updateGoroutines(container *nucular.Window) {
	refresh := func() {
		go func() {
			goroutinesPanel.asyncLoad.clear()
			wnd.Changed()
		}()
	}

	if container.HelpClicked {
		showHelp(container.Master(), "Goroutines Panel Help", goroutinesPanelHelp)
	}

	w := goroutinesPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}
	style := container.Master().Style()

	goroutines := goroutinesPanel.goroutines

	w.MenubarBegin()
	w.Row(20).Static(130, 300)
	if w.PropertyInt("Limit:", 1, &goroutinesPanel.limit, 1000000000, 1, 1) {
		refresh()
	}
	if w := w.Combo(label.T(goroutineLocations[goroutinesPanel.goroutineLocation]), 500, nil); w != nil {
		w.Row(22).Dynamic(1)
		for i := range goroutineLocations {
			if w.MenuItem(label.TA(goroutineLocations[i], "LC")) {
				goroutinesPanel.goroutineLocation = i
			}
		}
		w.CheckboxText("Only stoppped at breakpoint", &goroutinesPanel.onlyStopped)
	}

	{ // rules
		for i := range goroutinesPanel.rules {
			goroutinesShowRule(w, goroutinesPanel.rules[i])
		}
		if len(goroutinesPanel.rules) == 0 || !goroutinesPanel.rules[len(goroutinesPanel.rules)-1].empty() {
			goroutinesPanel.rules = append(goroutinesPanel.rules, newGoroutineFilterRule())
			w.Master().Changed()
		}

		if goroutinesPanel.rulesChanged {
			w.Row(22).Static(130)
			if w.ButtonText("Reload") {
				refresh()
			}
		}
	}

	w.MenubarEnd()

	d := 1
	if len(goroutines) > 0 {
		d = digits(int(goroutines[len(goroutines)-1].ID))
	}

	maxthreadid := 0
	for _, g := range goroutines {
		if g.ThreadID > maxthreadid {
			maxthreadid = g.ThreadID
		}
	}

	dthread := digits(maxthreadid)

	curgroupidx := 0

	for i := range goroutines {
		if curgroupidx < len(goroutinesPanel.groups) && i == goroutinesPanel.groups[curgroupidx].Offset {
			w.Row(posRowHeight).Dynamic(1)
			w.Label(fmt.Sprintf("GROUP %s", goroutinesPanel.groups[curgroupidx].Name), "LC")
		}

		g := &goroutines[i]
		if goroutinesPanel.onlyStopped && !g.atBreakpoint {
			continue
		}

		rowHeight := posRowHeight
		if len(g.Labels) > 0 {
			rowHeight = int((float64(rowHeight) / 2) * 3)
		}

		w.Row(rowHeight).Static()
		selected := curGid == g.ID

		w.LayoutSetWidthScaled(starWidth + style.Text.Padding.X*2)
		breakpointIcon(w, g.atBreakpoint, true, "CT", style)

		w.LayoutFitWidth(goroutinesPanel.id, 1)
		w.SelectableLabel(fmt.Sprintf("%*d", d, g.ID), "LT", &selected)

		w.LayoutFitWidth(goroutinesPanel.id, 1)
		if g.ThreadID != 0 {
			w.SelectableLabel(fmt.Sprintf("%*d", dthread, g.ThreadID), "LT", &selected)
		} else {
			w.SelectableLabel(" ", "LT", &selected)
		}

		w.LayoutFitWidth(goroutinesPanel.id, 100)
		loc := formatLocation2(goroutineGetDisplayLiocation(&g.Goroutine))
		if g.waitReason != "" {
			loc += fmt.Sprintf(" [%s]", g.waitReason)
		}
		if len(g.Labels) > 0 {
			loc += fmt.Sprintf("\nLabels: %s", writeGoroutineLabels(g.Labels))
		}
		w.SelectableLabel(loc, "LT", &selected)

		if selected && curGid != g.ID && !client.Running() {
			go func(gid int64) {
				state, err := client.SwitchGoroutine(gid)
				if err != nil {
					out := editorWriter{true}
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

		if curgroupidx < len(goroutinesPanel.groups) && i >= (goroutinesPanel.groups[curgroupidx].Offset+goroutinesPanel.groups[curgroupidx].Count-1) {
			w.Row(posRowHeight).Static(50, 0)
			w.Spacing(1)
			w.Label(fmt.Sprintf("(%d goroutines)", goroutinesPanel.groups[curgroupidx].Total), "LC")
			curgroupidx++
		}
	}
}

type goroutineFilterRule struct {
	kind      string
	field     string
	arg       string
	argEditor *nucular.TextEditor
}

func newGoroutineFilterRule() *goroutineFilterRule {
	r := &goroutineFilterRule{kind: goroutinesRuleKinds[0], field: goroutinesRuleFields[0], argEditor: &nucular.TextEditor{}}
	r.argEditor.Flags = nucular.EditClipboard | nucular.EditSelectable
	return r
}

func (rule *goroutineFilterRule) empty() bool {
	return rule.kind == goroutinesRuleKinds[0] || rule.kind == goroutinesRuleKindsFilled[0]
}

func (rule *goroutineFilterRule) goroutineField() api.GoroutineField {
	for i, s := range goroutinesRuleFields {
		if s == rule.field {
			return api.GoroutineField(i + 1)
		}
	}
	return api.GoroutineFieldNone
}

var goroutinesRuleKinds = []string{"Add rule...", "With:", "Without:", "Group by:"}
var goroutinesRuleKindsFilled = []string{"Remove rule", "With:", "Without:", "Group by:"}
var goroutinesRuleFields = []string{"Current Location", "User Location", "Go Location", "Start Location", "Label", "Running", "User"}

func goroutinesShowRule(w *nucular.Window, rule *goroutineFilterRule) {
	find := func(k string, v []string) int {
		for i, s := range v {
			if s == k {
				return i
			}
		}
		return 0
	}

	combo := func(cur *string, list []string) {
		curidx := find(*cur, list)
		newidx := w.ComboSimple(list, curidx, 30)
		if curidx != newidx {
			*cur = list[newidx]
			goroutinesPanel.rulesChanged = true
		}
	}

	w.Row(20).Static(130, 300, 0)

	if rule.empty() {
		combo(&rule.kind, goroutinesRuleKinds)
	} else {
		combo(&rule.kind, goroutinesRuleKindsFilled)
	}

	if !rule.empty() {
		combo(&rule.field, goroutinesRuleFields)
		needArg := false
		if rule.kind == goroutinesRuleKinds[len(goroutinesRuleKinds)-1] { // Group by
			if rule.goroutineField() == api.GoroutineLabel {
				needArg = true
			}
		} else { // With / Without
			switch rule.goroutineField() {
			case api.GoroutineCurrentLoc, api.GoroutineUserLoc, api.GoroutineGoLoc, api.GoroutineStartLoc, api.GoroutineLabel:
				needArg = true
			case api.GoroutineRunning, api.GoroutineUser:
				needArg = false
			}
		}
		if needArg {
			rule.argEditor.Edit(w)
			if string(rule.argEditor.Buffer) != rule.arg {
				rule.arg = string(rule.argEditor.Buffer)
				goroutinesPanel.rulesChanged = true
			}
		}
	}
}

func writeGoroutineLabels(labels map[string]string) string {
	const maxNumberOfGoroutineLabels = 5
	var w bytes.Buffer
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	more := false
	if len(keys) > maxNumberOfGoroutineLabels {
		more = true
		keys = keys[:maxNumberOfGoroutineLabels]
	}
	for i, k := range keys {
		fmt.Fprintf(&w, "%q:%q", k, labels[k])
		if i != len(keys)-1 {
			fmt.Fprintf(&w, ", ")
		} else if more {
			fmt.Fprintf(&w, "... (%d more)", len(labels)-maxNumberOfGoroutineLabels)
		}
	}
	fmt.Fprintf(&w, "\n")
	return w.String()
}

const NumAncestors = 5

func stacktraceOptions() api.StacktraceOptions {
	switch stackPanel.mode {
	default:
		fallthrough
	case 0:
		return 0
	case 1:
		return api.StacktraceSimple
	case 2:
		return api.StacktraceG | api.StacktraceSimple
	}
}

func loadStacktrace(p *asyncLoad) {
	stackPanel.id++
	stackPanel.deferID++

	oldStack := stackPanel.stack

	stack, err := client.Stacktrace(curGid, stackPanel.depth, stacktraceOptions()|api.StacktraceReadDefers, nil)
	if LogOutputNice != nil {
		logf("Stack (%d %d):\n", curGid, curThread)
		for i := range stackPanel.stack {
			frame := &stackPanel.stack[i]
			fmt.Fprintf(LogOutputNice, "\t%#x %#x %s in %s:%d\n", frame.PC, frame.FrameOffset, frame.Function.Name(), frame.File, frame.Line)
		}
	}
	stackPanel.stack = decorateStacktrace(stack)

	stackPanel.ancestors = stackPanel.ancestors[:0]

	if err != nil {
		p.done(err)
		return
	}

	stackPanel.isnew = make([]bool, len(stackPanel.stack))
	for i := range stackPanel.stack {
		found := false
		for j := range oldStack {
			if stackPanel.stack[i].FrameOffset == oldStack[j].FrameOffset {
				found = true
				break
			}
		}
		stackPanel.isnew[i] = !found
		if found {
			break
		}
	}

	if len(stackPanel.stack) > 0 && len(oldStack) > 0 && !stackPanel.isnew[0] && stackPanel.stack[0].FrameOffset != oldStack[0].FrameOffset {
		stackPanel.isnew[0] = true
	}

	ancestors, _ := client.Ancestors(curGid, NumAncestors, stackPanel.depth)
	stackPanel.ancestors = make([]Ancestor, len(ancestors))
	for i := range ancestors {
		stackPanel.ancestors[i].ID = ancestors[i].ID
		stackPanel.ancestors[i].Unreadable = ancestors[i].Unreadable
		stackPanel.ancestors[i].Stack = decorateStacktrace(ancestors[i].Stack)
	}
	p.done(nil)
}

func decorateStacktrace(stack []api.Stackframe) []Stackframe {
	r := make([]Stackframe, len(stack))
	for i := range stack {
		r[i].Stackframe = stack[i]
		if stack[i].Function != nil {
			r[i].shortName = prettyprint.ShortenFunctionName(stack[i].Function.Name())
		}

	}
	return r
}

func updateStacktrace(container *nucular.Window) {
	if container.HelpClicked {
		showHelp(container.Master(), "Stacktrace Panel Help", stacktracePanelHelp)
	}
	w := stackPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	w.MenubarBegin()
	w.Row(20).Static(120, 120, 70, 200)
	configChanged := false
	if w.PropertyInt("depth:", 1, &stackPanel.depth, 200, 1, 5) {
		configChanged = true
	}
	if w.CheckboxText("Full names", &stackPanel.full) {
		configChanged = true
	}
	w.Label("Mode:", "LC")
	newmode := w.ComboSimple([]string{"Normal", "No stack switching", "From G struct"}, stackPanel.mode, 30)
	if newmode != stackPanel.mode {
		stackPanel.mode = newmode
		configChanged = true
	}
	if configChanged {
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
	d := prettyprint.Hexdigits(maxpc)

	showFrame := func(frame Stackframe, i int, isnew bool, sl func(string) bool) bool {
		w.Row(posRowHeight).Static()
		w.LayoutFitWidth(stackPanel.id, 1)
		sl(fmt.Sprintf("%*d", didx, i))
		w.LayoutFitWidth(stackPanel.id, 1)
		sl(fmt.Sprintf("%#0*x\n%+d", d, frame.PC, frame.FrameOffset))
		w.LayoutFitWidth(stackPanel.id, 100)
		var r bool
		if stackPanel.full {
			r = sl(formatLocation2(frame.Location))
		} else {
			r = sl(fmt.Sprintf("%s\nat %s:%d", frame.shortName, ShortenFilePath(frame.Location.File), frame.Location.Line))
		}
		if isnew {
			rowbounds := w.WidgetBounds()
			rowbounds.X = w.Bounds.X
			rowbounds.W = w.Bounds.W

			cmds := w.Commands()
			cmds.FillRect(rowbounds, 0, changedVariableColor())
		}
		return r
	}

	for i, frame := range stack {
		selected := curFrame == i
		prevSelected := selected
		clicked := showFrame(frame, i, stackPanel.isnew[i], func(lbl string) bool {
			return w.SelectableLabel(lbl, "LT", &selected)
		})
		if clicked && prevSelected && !selected {
			selected = true
		}
		if selected && clicked && !client.Running() {
			curFrame = i
			stackPanel.deferID++
			curDeferredCall = 0
			go refreshState(refreshToSameFrame, clearFrameSwitch, nil)
		}
	}

	if len(stack) > 0 && !stack[len(stack)-1].Bottom {
		w.Row(posRowHeight).Dynamic(1)
		w.Label("(truncated)", "LC")
	}

	for i := range stackPanel.ancestors {
		a := &stackPanel.ancestors[i]

		w.Row(posRowHeight).Static()
		w.LayoutFitWidth(stackPanel.id, 1)
		w.Spacing(1)
		w.LayoutFitWidth(stackPanel.id, 1)
		w.Spacing(1)
		w.LayoutFitWidth(stackPanel.id, 100)

		if a.Unreadable != "" {
			w.Label(fmt.Sprintf("Unreadable ancestor: %s", a.Unreadable), "LT")
			continue
		}

		w.Label(fmt.Sprintf("Created by Goroutine %d", a.ID), "LT")

		for i := range a.Stack {
			showFrame(a.Stack[i], i, false, func(lbl string) bool {
				w.Label(lbl, "LT")
				return false
			})
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
	if container.HelpClicked {
		showHelp(container.Master(), "Threads Panel Help", threadsPanelHelp)
	}
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
		loc := api.Location{thread.PC, thread.File, thread.Line, thread.Function, nil, nil}
		w.SelectableLabel(formatLocation2(loc), "LT", &selected)

		if selected && curThread != thread.ID && !client.Running() {
			go func(tid int) {
				state, err := client.SwitchThread(tid)
				if err != nil {
					out := editorWriter{true}
					fmt.Fprintf(&out, "Could not switch thread: %v\n", err)
				} else {
					go refreshState(refreshToFrameZero, clearGoroutineSwitch, state)
				}
			}(thread.ID)
		}
	}
}

func loadRegs(p *asyncLoad) {
	regs, err := client.ListThreadRegisters(0, regsPanel.allRegs)
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
	if container.HelpClicked {
		showHelp(container.Master(), "Registers Panel Help", registersPanelHelp)
	}
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
	breakpointsPanel.breakpoints, err = client.ListBreakpoints(false)
	if err == nil {
		sort.Sort(breakpointsByID(breakpointsPanel.breakpoints))
	}
	breakpointsPanel.id++
	p.done(err)
}

func updateBreakpoints(container *nucular.Window) {
	if container.HelpClicked {
		showHelp(container.Master(), "Breakpoints Panel Help", breakpointsPanelHelp)
	}
	w := breakpointsPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	style := w.Master().Style()
	savedStyle := *style

	breakpoints := make([]*api.Breakpoint, 0, len(breakpointsPanel.breakpoints))
	breakpoints = append(breakpoints, breakpointsPanel.breakpoints...)

	for _, breakpoint := range breakpoints {
		oldselectedId := breakpointsPanel.selected
		selected := breakpointsPanel.selected == breakpoint.ID
		w.Row(posRowHeight).Static()

		disableMark := ""
		if breakpoint.Disabled {
			disableMark = "[disabled] "
		} else if breakpoint.ExprString != "" {
			disableMark = "[suspended] "
		}

		if disableMark != "" {
			for _, p := range []*color.RGBA{&style.Text.Color, &style.Selectable.TextNormal, &style.Selectable.TextHover, &style.Selectable.TextPressed, &style.Selectable.TextPressed, &style.Selectable.TextNormalActive, &style.Selectable.TextHoverActive, &style.Selectable.TextPressedActive} {
				darken(p)
			}
		}

		name := breakpoint.Name
		if name == "" {
			name = breakpoint.ExprString
		}
		if name != "" {
			name += " "
		}

		w.LayoutFitWidth(breakpointsPanel.id, 100)
		if breakpoint.WatchExpr != "" {
			name = "watchpoint " + name
			if breakpoint.WatchExpr != breakpoint.Name {
				w.SelectableLabel(fmt.Sprintf("%s%s[%s] (hit count %d)\n", disableMark, name, breakpoint.WatchExpr, breakpoint.TotalHitCount), "LT", &selected)
			} else {
				w.SelectableLabel(fmt.Sprintf("%s%s (hit count %d)\n", disableMark, name, breakpoint.TotalHitCount), "LT", &selected)
			}
		} else {
			w.SelectableLabel(fmt.Sprintf("%s%s%s (hit count: %d)\nat %s:%d (%#v)", disableMark, name, breakpoint.FunctionName, breakpoint.TotalHitCount, breakpoint.File, breakpoint.Line, breakpoint.Addr), "LT", &selected)
		}

		if disableMark != "" {
			*style = savedStyle
		}

		bounds := w.LastWidgetBounds
		bounds.W = w.Bounds.W

		if !client.Running() {
			if selected {
				breakpointsPanel.selected = breakpoint.ID

				w.LayoutSetWidth(posRowHeight)
				iconFace, style.Font = style.Font, iconFace
				if w.ButtonText("\uf142") {
					bounds.W = 0
				}
				iconFace, style.Font = style.Font, iconFace
			}

			if w := w.ContextualOpen(0, image.Point{}, bounds, breakpointContextualMenu); w != nil {
				breakpointsPanel.selected = breakpoint.ID
			}

			if breakpointsPanel.selected != oldselectedId && breakpoint.File != "" {
				listingPanel.pinnedLoc = &api.Location{File: breakpoint.File, Line: breakpoint.Line, PC: breakpoint.Addr}
				go refreshState(refreshToSameFrame, clearNothing, nil)
			}
		}
	}
}

func breakpointContextualMenu(w *nucular.Window) {
	var breakpoint *api.Breakpoint
	for i := range breakpointsPanel.breakpoints {
		if breakpointsPanel.breakpoints[i].ID == breakpointsPanel.selected {
			breakpoint = breakpointsPanel.breakpoints[i]
			break
		}
	}
	w.Row(20).Dynamic(1)

	if !breakpoint.Disabled {
		if w.MenuItem(label.TA("Edit...", "LC")) {
			openBreakpointEditor(w.Master(), breakpoint)
		}
		if w.MenuItem(label.TA("Disable", "LC")) {
			go disableBreakpoint(breakpoint)
		}
	} else {
		if w.MenuItem(label.TA("Enable", "LC")) {
			go enableBreakpoint(breakpoint)
		}
	}
	if w.MenuItem(label.TA("Clear", "LC")) {
		go execClearBreakpoint(breakpointsPanel.selected)
	}
	if w.MenuItem(label.TA("Clear All", "LC")) {
		go func() {
			scrollbackOut := editorWriter{true}
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
			saveConfiguration()
			refreshState(refreshToSameFrame, clearBreakpoint, nil)
			wnd.Changed()
		}()
	}
}

func execClearBreakpoint(id int) {
	scrollbackOut := editorWriter{true}
	bp, err := client.ClearBreakpoint(id)
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not clear breakpoint %d: %v\n", id, err)
	}
	removeFrozenBreakpoint(bp)
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
	wnd.Changed()
}

func execRestartCheckpoint(id int, gid int64, where string) {
	scrollbackOut := editorWriter{true}
	err := restartCheckpointToGoroutine(id, gid)
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not restart from checkpoint c%d: %v\n", id, err)
		return
	}
	fmt.Fprintf(&scrollbackOut, "Process restarted from c%d %s\n", id, where)
	refreshState(refreshToFrameZero, clearStop, nil)
}

func restartCheckpointToGoroutine(id int, gid int64) error {
	_, err := client.RestartFrom(false, fmt.Sprintf("c%d", id), false, nil, [3]string{}, false)
	if err != nil {
		return err
	}
	if gid > 0 {
		_, err = client.SwitchGoroutine(gid)
		if err != nil {
			return fmt.Errorf("switch to goroutine %d: %v", gid, err)
		}
	}
	return nil
}

func execClearCheckpoint(id int) {
	scrollbackOut := editorWriter{true}
	err := client.ClearCheckpoint(id)
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not clear checkpoint c%d: %v\n", id, err)
	}
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
	wnd.Changed()
}

type breakpointEditor struct {
	bp            *api.Breakpoint
	printEditor   nucular.TextEditor
	condEditor    nucular.TextEditor
	hitCondEditor nucular.TextEditor
}

func openBreakpointEditor(mw nucular.MasterWindow, bp *api.Breakpoint) {
	var ed breakpointEditor
	ed.bp = bp

	ed.printEditor.Flags = nucular.EditMultiline | nucular.EditClipboard | nucular.EditSelectable
	for i := range bp.Variables {
		ed.printEditor.Buffer = append(ed.printEditor.Buffer, []rune(fmt.Sprintf("%s\n", bp.Variables[i]))...)
	}

	ed.condEditor.Flags = nucular.EditClipboard | nucular.EditSelectable | nucular.EditSigEnter
	ed.condEditor.Buffer = []rune(ed.bp.Cond)

	ed.hitCondEditor.Flags = nucular.EditClipboard | nucular.EditSelectable | nucular.EditSigEnter
	ed.hitCondEditor.Buffer = []rune(ed.bp.HitCond)

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

	committed := false

	w.Row(30).Static(100, 0)
	w.Label("Condition:", "LC")
	ev := bped.condEditor.Edit(w)
	committed = committed || (ev&nucular.EditCommitted != 0)

	w.Row(30).Static(125, 0)
	w.Label("Hit Condition:", "LC")
	ev = bped.hitCondEditor.Edit(w)
	committed = committed || (ev&nucular.EditCommitted != 0)

	cancelled := false

	for _, e := range w.Input().Keyboard.Keys {
		switch {
		case e.Code == key.CodeEscape:
			cancelled = true
		}
	}

	w.Row(20).Static(0, 80, 80)
	w.Spacing(1)
	if w.ButtonText("Cancel") || cancelled {
		go refreshState(refreshToSameFrame, clearBreakpoint, nil)
		w.Close()
	}
	if w.ButtonText("OK") || committed {
		bped.bp.Cond = string(bped.condEditor.Buffer)
		bped.bp.HitCond = string(bped.hitCondEditor.Buffer)
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
		scrollbackOut := editorWriter{true}
		fmt.Fprintf(&scrollbackOut, "Could not amend breakpoint: %v\n", err)
	} else {
		for i := range FrozenBreakpoints {
			if FrozenBreakpoints[i].Bp.ID == bped.bp.ID {
				FrozenBreakpoints[i].Bp = *bped.bp
				saveConfiguration()
				break
			}
		}
	}
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
	autoCheckpointsReloadVars()
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
	if container.HelpClicked {
		showHelp(container.Master(), "Checkpoints Panel Help", checkpointsPanelHelp)
	}
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

			style := w.Master().Style()
			w.LayoutSetWidth(posRowHeight)
			iconFace, style.Font = style.Font, iconFace
			if w.ButtonText("\uf142") {
				bounds.W = 0
			}
			iconFace, style.Font = style.Font, iconFace
		}

		if w := w.ContextualOpen(0, image.Point{}, bounds, nil); w != nil {
			checkpointsPanel.selected = checkpoint.ID
			w.Row(20).Dynamic(1)

			if w.MenuItem(label.TA("Clear checkpoint", "LC")) {
				go execClearCheckpoint(checkpointsPanel.selected)
			}

			if w.MenuItem(label.TA("Restart from checkpoint", "LC")) {
				go execRestartCheckpoint(checkpoint.ID, 0, checkpoint.Where)
			}
		}
	}
}

func updateDeferredCalls(container *nucular.Window) {
	if container.HelpClicked {
		showHelp(container.Master(), "Deferred Calls Panel Help", deferredCallsPanelHelp)
	}
	w := stackPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	w.MenubarBegin()
	w.Row(20).Static(200)
	combosel := 0
	if stackPanel.showDeferPos {
		combosel = 1
	}
	combosel = w.ComboSimple([]string{"Deferred function location", "Deferred call location"}, combosel, 20)
	stackPanel.showDeferPos = (combosel != 0)
	w.MenubarEnd()

	stack := stackPanel.stack

	if curFrame >= len(stack) {
		return
	}

	maxpc := uint64(0)
	for _, deferredCall := range stack[curFrame].Defers {
		pc := deferredCall.DeferredLoc.PC
		if stackPanel.showDeferPos {
			pc = deferredCall.DeferLoc.PC
		}
		if pc > maxpc {
			maxpc = pc
		}
	}

	didx := digits(len(stack[curFrame].Defers))
	d := prettyprint.Hexdigits(maxpc)

	for i, deferredCall := range stack[curFrame].Defers {
		loc := deferredCall.DeferredLoc
		if stackPanel.showDeferPos {
			loc = deferredCall.DeferLoc
		}

		selected := curDeferredCall-1 == i
		w.Row(posRowHeight).Static()
		w.LayoutFitWidth(stackPanel.deferID, 1)
		w.SelectableLabel(fmt.Sprintf("%*d", didx, i+1), "LT", &selected)
		w.LayoutFitWidth(stackPanel.deferID, 1)
		w.SelectableLabel(fmt.Sprintf("%#0*x", d, loc.PC), "LT", &selected)
		w.LayoutFitWidth(stackPanel.deferID, 1)
		var locstr string
		if deferredCall.Unreadable == "" {
			locstr = formatLocation2(loc)
		} else {
			locstr = deferredCall.Unreadable
		}
		clicked := w.SelectableLabel(locstr, "LT", &selected)
		if selected && clicked && !client.Running() {
			curDeferredCall = i + 1
			go refreshState(refreshToSameFrame, clearFrameSwitch, nil)
		}
	}
}

func (p *stringSlicePanel) update(container *nucular.Window) {
	if p.filterEditor.Filter == nil {
		p.filterEditor.Filter = spacefilter
	}
	if p.filterEditor.Flags == 0 {
		p.filterEditor.Flags = nucular.EditClipboard | nucular.EditSelectable
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
		bounds := w.LastWidgetBounds
		if selected {
			p.selected = i

			style := w.Master().Style()
			w.LayoutSetWidth(posRowHeight)
			iconFace, style.Font = style.Font, iconFace
			if w.ButtonText("\uf142") {
				bounds.W = 0
			}
			iconFace, style.Font = style.Font, iconFace
		}
		if p.interaction != nil {
			p.interaction(p, w, clicked, i, bounds)
		}
	}
}

func funcInteraction(p *stringSlicePanel, w *nucular.Window, clicked bool, idx int, bounds rect.Rect) {
	if clicked {
		locs, _, err := client.FindLocation(currentEvalScope(), p.slice[p.selected], true, nil)
		if err == nil && len(locs) == 1 {
			listingPanel.pinnedLoc = &locs[0]
			go refreshState(refreshToSameFrame, clearNothing, nil)
		}
	}
	if w := w.ContextualOpen(0, image.Point{}, bounds, nil); w != nil {
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
	setBreakpointEx(&editorWriter{true}, &api.Breakpoint{FunctionName: name, Line: -1}, "")
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
}

func sourceInteraction(p *stringSlicePanel, w *nucular.Window, clicked bool, idx int, bounds rect.Rect) {
	if clicked {
		listingPanel.pinnedLoc = &api.Location{File: p.slice[p.selected], Line: 1}
		go refreshState(refreshToSameFrame, clearNothing, nil)
	}
	if w := w.ContextualOpen(0, image.Point{}, bounds, nil); w != nil {
		w.Row(20).Dynamic(1)
		if w.MenuItem(label.TA("Copy to clipboard", "LC")) {
			clipboard.Set(p.slice[idx])
		}
	}
}

func sliceInteraction(p *stringSlicePanel, w *nucular.Window, clicked bool, idx int, bounds rect.Rect) {
	if w := w.ContextualOpen(0, image.Point{}, bounds, nil); w != nil {
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

func breakpointIcon(w *nucular.Window, atbp, enabledbp bool, align label.Align, style *nstyle.Style) {
	if atbp {
		iconFace, style.Font = style.Font, iconFace
		c := color.RGBA{0xff, 0x00, 0x00, 0xff}
		if !enabledbp {
			c = color.RGBA{0x80, 0x00, 0x00, 0x80}
		}
		w.LabelColored(breakpointIconChar, align, c)
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

	if container.HelpClicked {
		showHelp(container.Master(), "Listing Panel Help", listingPanelHelp)
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

		centerlineBounds := listp.WidgetBounds()
		centerlineBounds.X = listp.Bounds.X
		centerlineBounds.W = listp.Bounds.W

		if centerline {
			cmds := listp.Commands()
			cmds.FillRect(centerlineBounds, 0, style.Selectable.PressedActive.Data.Color)

			if listingPanel.recenterListing {
				gl.Center()
				listingPanel.recenterListing = false
			}
		}

		listp.LayoutSetWidth(starw)
		breakpointIcon(listp, line.bp != nil, line.bpenabled, "CC", style)
		bpbounds := listp.LastWidgetBounds

		isCurrentLine := line.pc && curFrame == 0 && curDeferredCall == 0 && !client.Running() && curThread >= 0

		listp.LayoutSetWidth(arroww)
		if isCurrentLine {
			iconFace, style.Font = style.Font, iconFace
			listp.LabelColored(arrowIconChar, "CC", color.RGBA{0xff, 0xff, 0x00, 0xff})
			iconFace, style.Font = style.Font, iconFace
		} else {
			listp.Spacing(1)
		}

		textColor := style.Text.Color
		if centerline {
			textColor = style.Selectable.TextPressedActive
		}

		listp.LayoutFitWidth(listingPanel.id, 1)
		listp.LabelColored(line.idx, "LC", textColor)
		listp.LayoutFitWidth(listingPanel.id, 100)
		listp.LabelColored(line.text, "LC", textColor)
		textbounds := listp.LastWidgetBounds

		if centerline && listingPanel.recenterListing {
			listingPanel.recenterListing = false
			gl.Center()
		}

		if centerline {
			showCenterlineChanges(centerlineBounds, listp)
		}

		// Contextual Menu
		if !client.Running() {
			ctxtbounds := bpbounds
			ctxtbounds.W = (textbounds.X + textbounds.W) - ctxtbounds.X

			if listp.Input().Mouse.Clicked(mouse.ButtonMiddle, ctxtbounds) {
				if line.bp != nil {
					if line.bpenabled {
						go disableBreakpoint(line.bp)
					} else {
						go enableBreakpoint(line.bp)
					}
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
					if line.bpenabled {
						if w.MenuItem(label.TA("Disable breakpoint", "LC")) {
							go disableBreakpoint(line.bp)
						}
					} else {
						if w.MenuItem(label.TA("Enable breakpoint", "LC")) {
							go enableBreakpoint(line.bp)
						}
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
							go stepInto(&editorWriter{true}, listingPanel.stepIntoInfo.Call)
						}
					}
				} else {
					if w.MenuItem(label.TA("Continue to this line", "LC")) {
						go continueToLine(listingPanel.file, line.lineno)
					}
				}
			}
		}

		// Breakpoint Info
		if line.bp != nil && (line.bp.Cond != "" || line.bp.HitCond != "" || len(line.bp.Variables) > 0) {
			listp.Row(lineheight).Static(0)
			bpcolor := style.Text.Color
			darken(&bpcolor)
			var bpinfo bytes.Buffer
			fmt.Fprintf(&bpinfo, "// ")
			if line.bp.Cond != "" {
				fmt.Fprintf(&bpinfo, "when %s ", line.bp.Cond)
				if line.bp.HitCond != "" {
					fmt.Fprintf(&bpinfo, " AND hitcount %s ", line.bp.HitCond)
				}
			} else if line.bp.HitCond != "" {
				fmt.Fprintf(&bpinfo, "when hitcount %s ", line.bp.HitCond)
			}
			if len(line.bp.Variables) > 0 {
				fmt.Fprintf(&bpinfo, "print %s", strings.Join(line.bp.Variables, "; "))
			}
			listp.LabelColored(bpinfo.String(), "LC", bpcolor)
		}
	}
}

func listingSetBreakpoint(file string, line int) {
	setBreakpointEx(&editorWriter{true}, &api.Breakpoint{File: file, Line: line}, fmt.Sprintf("%s:%d", file, line))
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
}

func showCenterlineChanges(centerlineBounds rect.Rect, w *nucular.Window) {
	msg := ""
	if curGid < 0 {
		if oldThread != curThread {
			msg = "THREAD CHANGED"
		}
	}
	if oldFrameOffset != curFrameOffset {
		msg = "FRAME CHANGED"
	}
	if oldGid != curGid {
		msg = "GOROUTINE CHANGED"
	}
	if msg == "" {
		return
	}

	style := w.Master().Style()

	width := nucular.FontWidth(style.Font, msg)

	bounds := centerlineBounds
	bounds.X = bounds.X + bounds.W - width - 6*style.NormalWindow.ScrollbarSize.X
	bounds.W = width + 6*style.NormalWindow.ScrollbarSize.X

	bg := color.RGBA{0xff, 0xff, 0x00, 0xff}
	fg := color.RGBA{0x00, 0x00, 0x00, 0xff}

	cmds := w.Commands()
	cmds.FillRect(bounds, 0, style.Selectable.PressedActive.Data.Color)

	bounds.X += 2 * style.NormalWindow.ScrollbarSize.X
	bounds.W -= 4 * style.NormalWindow.ScrollbarSize.X
	cmds.FillRect(bounds, 0, bg)

	bounds.X += style.NormalWindow.ScrollbarSize.X - style.Text.Padding.X
	cmds.DrawText(bounds, msg, boldFace, fg)
}

func updateDisassemblyPanel(container *nucular.Window) {
	container = disassemblyPanel.asyncLoad.showRequest(container)
	if container == nil {
		return
	}

	if container.HelpClicked {
		showHelp(container.Master(), "Disassembly Panel Help", disassemblyPanelHelp)
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

	oldDisassHoverIdx := listingPanel.disassHoverIdx
	listingPanel.disassHoverIdx = -1

	reachableColor := style.Text.Color
	unreachableColor := style.Text.Color
	darken(&unreachableColor)

	for gl.Next() {
		instr := listingPanel.text[gl.Index()]

		if !instr.reach {
			style.Text.Color = unreachableColor
		} else {
			style.Text.Color = reachableColor
		}

		if instr.Loc.File != lastfile || instr.Loc.Line != lastlineno {
			if instr.Loc.File != listingPanel.file || strings.ToLower(filepath.Ext(listingPanel.file)) != ".s" {
				listp.Row(lineheight).Static()
				listp.Row(lineheight).Static()
				text := ""
				if instr.Loc.File == listingPanel.file && instr.Loc.Line-1 < len(listingPanel.listing) && instr.Loc.Line-1 > 0 {
					text = strings.TrimSpace(listingPanel.listing[instr.Loc.Line-1].text)
				}
				listp.LayoutFitWidth(listingPanel.id, 1)
				listp.Label(fmt.Sprintf("%s:%d: %s", instr.Loc.File, instr.Loc.Line, text), "LC")
			}
			lastfile, lastlineno = instr.Loc.File, instr.Loc.Line
		}
		listp.Row(lineheight).Static()

		listp.LayoutSetWidthScaled(starw)

		centerline := instr.AtPC || instr.Loc.PC == listingPanel.framePC

		centerlineBounds := listp.WidgetBounds()
		centerlineBounds.X = listp.Bounds.X
		centerlineBounds.W = listp.Bounds.W

		if centerline {
			cmds := listp.Commands()
			cmds.FillRect(centerlineBounds, 0, style.Selectable.PressedActive.Data.Color)
		}

		if gl.Index() == listingPanel.disassHoverIdx || gl.Index() == listingPanel.disassHoverClickIdx {
			if listingPanel.centerOnDisassHover {
				listingPanel.centerOnDisassHover = false
				gl.Center()
			}
			rowbounds := listp.WidgetBounds()
			rowbounds.X = listp.Bounds.X
			rowbounds.W = listp.Bounds.W
			cmds := listp.Commands()
			c := style.Selectable.PressedActive.Data.Color
			darken(&c)
			cmds.FillRect(rowbounds, 0, c)
		}

		breakpointIcon(listp, instr.Breakpoint, true, "CC", style)

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
		listp.Label(instr.op, "LC")
		listp.LayoutFitWidth(listingPanel.id, 100)
		listp.Label(instr.args, "LC")

		if centerline {
			showCenterlineChanges(centerlineBounds, listp)
		}

		if listp.Input().Mouse.HoveringRect(listp.LastWidgetBounds) {
			if instr.dstidx >= 0 {
				if listp.Input().Mouse.IsClickInRect(mouse.ButtonLeft, listp.LastWidgetBounds) {
					listingPanel.disassHoverClickIdx = instr.dstidx
					listingPanel.centerOnDisassHover = true
				}
				if listingPanel.disassHoverIdx != instr.dstidx {
					listingPanel.disassHoverIdx = instr.dstidx
				}
			}
		}

		if listingPanel.recenterDisassembly && centerline {
			listingPanel.recenterDisassembly = false
			gl.Center()
		}
	}

	style.Text.Color = reachableColor

	if oldDisassHoverIdx != listingPanel.disassHoverIdx {
		listp.Master().Changed()
	}
}

type wrappedInstruction struct {
	api.AsmInstruction

	op     string
	args   string
	reach  bool
	dstidx int
}

var asmprefixes = map[string]bool{
	"cs":       true,
	"ds":       true,
	"es":       true,
	"fs":       true,
	"gs":       true,
	"ss":       true,
	"lock":     true,
	"rep":      true,
	"repn":     true,
	"repne":    true,
	"addr16":   true,
	"data16":   true,
	"addr32":   true,
	"data32":   true,
	"bnd":      true,
	"xacquire": true,
	"xrelease": true,
	"pt":       true,
	"pn":       true,
}

func wrapInstructions(text api.AsmInstructions, curpc uint64) []wrappedInstruction {
	r := make([]wrappedInstruction, len(text))
	for i := range text {
		r[i].AsmInstruction = text[i]

		cur := 0
		for {
			n := strings.Index(text[i].Text[cur:], " ")
			if n < 0 {
				cur = len(text[i].Text)
				break
			}
			op := strings.ToLower(strings.TrimSpace(text[i].Text[cur : cur+n]))
			cur += n + 1
			if !asmprefixes[op] && !strings.HasPrefix(op, "rex.") {
				break
			}
		}

		r[i].op = strings.TrimSpace(text[i].Text[:cur])
		r[i].args = text[i].Text[cur:]
	}

	marked := make(map[uint64]bool, len(r))
	fringe := make(map[uint64]bool, len(r))

	fringe[curpc] = true

	fringepop := func() uint64 {
		for pc := range fringe {
			delete(fringe, pc)
			return pc
		}
		return 0
	}

	fringepush := func(pc uint64) {
		if marked[pc] {
			return
		}
		fringe[pc] = true
	}

	findinstr := func(pc uint64) int {
		for i := range r {
			if r[i].Loc.PC == pc {
				return i
			}
		}
		return -1
	}

	for len(fringe) > 0 {
		pc := fringepop()

		for i := findinstr(pc); i >= 0 && i < len(r); i++ {
			marked[r[i].Loc.PC] = true
			r[i].reach = true
			dstpc, err := strconv.ParseUint(r[i].args, 0, 64)
			if err == nil {
				fringepush(dstpc)
			}
			if strings.ToLower(r[i].op) == "jmp" {
				break
			}
		}
	}

	for i := range r {
		dstpc, err := strconv.ParseUint(r[i].args, 0, 64)
		if err != nil {
			r[i].dstidx = -1
			continue
		}
		r[i].dstidx = findinstr(dstpc)
	}

	return r
}
