package main

import (
	"fmt"
	"image"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/label"
)

var autoCheckpointsPanel = struct {
	asyncLoad asyncLoad
	mu        sync.Mutex
	loading   bool
	id        int

	selected    int
	checkpoints []autoCheckpoint

	forwardLimit, backwardLimit int
	timeoutEdit                 nucular.TextEditor

	doneBackward, doneForward bool
}{
	forwardLimit:  10,
	backwardLimit: 10,
	timeoutEdit:   nucular.TextEditor{Buffer: []rune("1s")},
}

type autoCheckpoint struct {
	ID          int
	Where       string
	GoroutineID int64
	Breakpoint  *api.Breakpoint
	Variables   []*Variable
}

func loadAutoCheckpoints(p *asyncLoad) {
	if !client.Recorded() {
		p.done(fmt.Errorf("Error: not a recording"))
		return
	}
	p.done(nil)
}

func updateAutoCheckpoints(container *nucular.Window) {
	if container.HelpClicked {
		showHelp(container.Master(), "Auto-checkpoints Panel Help", autoCheckpointsPanelHelp)
	}
	w := autoCheckpointsPanel.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	autoCheckpointsPanel.mu.Lock()
	defer autoCheckpointsPanel.mu.Unlock()

	if autoCheckpointsPanel.loading {
		w.Row(0).Dynamic(1)
		container.Label("Loading...", "LT")
		return
	}

	w.Row(20).Static(180, 180)

	w.PropertyInt("Backward:", 0, &autoCheckpointsPanel.backwardLimit, 200, 1, 1)
	w.PropertyInt("Forward:", 0, &autoCheckpointsPanel.forwardLimit, 200, 1, 1)

	w.Row(20).Static(80, 80, 100, 100)
	w.Label("Timeout: ", "RT")
	autoCheckpointsPanel.timeoutEdit.Edit(w)
	w.Spacing(1)
	if w.ButtonText("Create") {
		go autoCheckpointsReset()
	}

	if !autoCheckpointsPanel.doneBackward && len(autoCheckpointsPanel.checkpoints) > 0 {
		w.Row(varRowHeight).Static(100)
		if w.ButtonText("More...") {
			go autoCheckpointsLoadMore(-1)
		}
	}

	style := w.Master().Style()
	savedGroupWindowStyle := style.GroupWindow
	style.GroupWindow = style.NormalWindow
	defer func() {
		style.GroupWindow = savedGroupWindowStyle
	}()

	const (
		checkpointIDMinWidht   = 100
		goroutineIDMinWidth    = 40
		breakpointNameMinWidth = 40
		positionMinWidth       = 100
	)

	w.Row(varRowHeight).Static()

	w.LayoutFitWidth(autoCheckpointsPanel.id, checkpointIDMinWidht)
	w.Label("checkid", "LT")
	w.LayoutFitWidth(autoCheckpointsPanel.id, goroutineIDMinWidth)
	w.Label("goid", "LT")
	w.LayoutFitWidth(autoCheckpointsPanel.id, breakpointNameMinWidth)
	w.Label("bpid", "LT")
	w.LayoutFitWidth(autoCheckpointsPanel.id, positionMinWidth)
	w.Spacing(1)

	for _, check := range autoCheckpointsPanel.checkpoints {
		selected := autoCheckpointsPanel.selected == check.ID
		w.Row(varRowHeight).Static()

		w.LayoutFitWidth(autoCheckpointsPanel.id, checkpointIDMinWidht)
		w.SelectableLabel(fmt.Sprintf("c%d,%s", check.ID, check.Where), "LT", &selected)
		bounds := w.LastWidgetBounds
		bounds.W = w.Bounds.W

		w.LayoutFitWidth(autoCheckpointsPanel.id, goroutineIDMinWidth)
		w.SelectableLabel(fmt.Sprintf("%d", check.GoroutineID), "LT", &selected)

		w.LayoutFitWidth(autoCheckpointsPanel.id, breakpointNameMinWidth)
		if check.Breakpoint != nil {
			w.SelectableLabel(formatBreakpointName2(check.Breakpoint), "LT", &selected)
		} else {
			if check.Where == "acp+0" {
				w.SelectableLabel("START", "LT", &selected)
			} else {
				w.SelectableLabel("", "LT", &selected)
			}
		}

		w.LayoutFitWidth(autoCheckpointsPanel.id, positionMinWidth)
		if check.Breakpoint != nil {
			w.SelectableLabel(fmt.Sprintf("at %s\n", formatBreakpointLocation(check.Breakpoint, true)), "LT", &selected)

			for _, v := range check.Variables {
				w.Row(varRowHeight).Static()

				w.LayoutFitWidth(autoCheckpointsPanel.id, checkpointIDMinWidht)
				w.Spacing(1)
				w.LayoutFitWidth(autoCheckpointsPanel.id, goroutineIDMinWidth)
				w.Spacing(1)
				w.LayoutFitWidth(autoCheckpointsPanel.id, breakpointNameMinWidth)
				w.Spacing(1)

				w.LayoutFitWidth(autoCheckpointsPanel.id, positionMinWidth)
				if v.Value != "" {
					w.Label(fmt.Sprintf("%s = %s", v.Name, v.Value), "LT")
				} else {
					w.Label(fmt.Sprintf("%s = %s", v.Name, v.SinglelineString(false, false)), "LT")
				}
			}
		} else {
			w.Spacing(1)
		}

		if !client.Running() {
			if selected {
				autoCheckpointsPanel.selected = check.ID
			}

			if w := w.ContextualOpen(0, image.Point{}, bounds, nil); w != nil {
				autoCheckpointsPanel.selected = check.ID

				w.Row(20).Dynamic(1)
				if w.MenuItem(label.TA("Restart from checkpoint", "LC")) {
					go execRestartCheckpoint(check.ID, check.GoroutineID, check.Where)
				}
			}
		}
	}

	if !autoCheckpointsPanel.doneForward && len(autoCheckpointsPanel.checkpoints) > 0 {
		w.Row(varRowHeight).Static(100)
		if w.ButtonText("More...") {
			go autoCheckpointsLoadMore(+1)
		}
	}
}

func addcheck(cnt int, gid int64, bp *api.Breakpoint, bpi *api.BreakpointInfo) {
	where := fmt.Sprintf("acp%+d", cnt)
	cp, err := client.Checkpoint(where)
	if err != nil {
		panic(fmt.Errorf(" (%d): %v\n", cnt, err))
	}

	vars := []*Variable{}

	if bpi != nil {
		for i := range bpi.Variables {
			v := &bpi.Variables[i]
			vars = append(vars, wrapApiVariable("", v, v.Name, v.Name, true, nil, 0))
		}
	}

	autoCheckpointsPanel.checkpoints = append(autoCheckpointsPanel.checkpoints, autoCheckpoint{
		ID:          cp,
		Where:       where,
		GoroutineID: gid,
		Breakpoint:  bp,
		Variables:   vars,
	})
}

func addcheckpoints(pcnt *int, statech <-chan *api.DebuggerState, dir int, ticker *time.Ticker) (done, finished bool) {
	outOfTime := false
	ok := false
	for {
		select {
		case state, statechOpen := <-statech:
			if !statechOpen {
				if outOfTime {
					return true, false
				}
				if !ok {
					return true, true
				}
				return false, false
			}
			if state.Exited {
				return true, true
			}
			if state.Err != nil {
				if dir < 0 {
					panic(fmt.Errorf(" (continuing in backward motion): %v", state.Err))
				} else {
					panic(fmt.Errorf(" (continuing in forward motion): %v", state.Err))
				}
			}
			for _, th := range state.Threads {
				if th.Breakpoint != nil {
					addcheck(dir**pcnt, th.GoroutineID, th.Breakpoint, th.BreakpointInfo)
					ok = true
					*pcnt++
				}
			}
		case <-ticker.C:
			outOfTime = true
			client.Halt()
		}
	}
}

func addcheckpointsloop(dir int, cntstart, limit int) {
	ticker := time.NewTicker(autoCheckpointsTimeout())

	for cnt := cntstart; cnt < cntstart+limit; {
		var statech <-chan *api.DebuggerState
		if dir < 0 {
			statech = client.Rewind()
		} else {
			statech = client.Continue()
		}
		done, finished := addcheckpoints(&cnt, statech, dir, ticker)
		if done {
			if finished {
				if dir < 0 {
					autoCheckpointsPanel.doneBackward = true
				} else {
					autoCheckpointsPanel.doneForward = true
				}
			}
			break
		}
	}
	ticker.Stop()
}

const autoCheckpointWherePrefix = "acp"

func autoCheckpointsReset() {
	var out = editorWriter{true}

	autoCheckpointsPanel.mu.Lock()
	autoCheckpointsPanel.loading = true
	autoCheckpointsPanel.mu.Unlock()
	wnd.Changed()

	autoCheckpointsPanel.id++

	defer func() {
		ierr := recover()
		if ierr != nil {
			fmt.Fprintf(&out, "Error creating automatic checkpoints:%v\n", ierr)
		}
		autoCheckpointsPanel.mu.Lock()
		autoCheckpointsPanel.loading = false
		autoCheckpointsPanel.mu.Unlock()
		refreshState(refreshToSameFrame, clearBreakpoint, nil)
	}()

	for _, check := range autoCheckpointsPanel.checkpoints {
		err := client.ClearCheckpoint(check.ID)
		if err != nil {
			fmt.Fprintf(&out, "Could not clear checkpoint %d: %v\n", check.ID, err)
			return
		}
	}

	autoCheckpointsPanel.checkpoints = autoCheckpointsPanel.checkpoints[:0]
	autoCheckpointsPanel.doneBackward = false
	autoCheckpointsPanel.doneForward = false

	addcheck(0, curGid, nil, nil)
	startCheckpoint := autoCheckpointsPanel.checkpoints[0].ID

	if autoCheckpointsPanel.forwardLimit > 0 {
		addcheckpointsloop(+1, 1, autoCheckpointsPanel.forwardLimit)
		_, err := client.RestartFrom(false, fmt.Sprintf("c%d", startCheckpoint), false, nil, [3]string{}, false)
		if err != nil {
			panic(fmt.Errorf(" (resetting before backward motion): %v", err))
		}
	}

	if autoCheckpointsPanel.backwardLimit > 0 {
		addcheckpointsloop(-1, 1, autoCheckpointsPanel.backwardLimit)
		_, err := client.RestartFrom(false, fmt.Sprintf("c%d", startCheckpoint), false, nil, [3]string{}, false)
		if err != nil {
			panic(fmt.Errorf(" (resetting before stopping): %v", err))
		}
	}

	sort.Slice(autoCheckpointsPanel.checkpoints, func(i, j int) bool {
		return autoCheckpointsPanel.checkpoints[i].Counter() < autoCheckpointsPanel.checkpoints[j].Counter()
	})
}

func (a *autoCheckpoint) Counter() int {
	if !strings.HasPrefix(a.Where, autoCheckpointWherePrefix) {
		return 0
	}
	n, _ := strconv.Atoi(a.Where[len(autoCheckpointWherePrefix):])
	return n
}

func autoCheckpointsReloadVars() {
	var out = editorWriter{true}

	autoCheckpointsPanel.mu.Lock()
	autoCheckpointsPanel.loading = true
	autoCheckpointsPanel.mu.Unlock()
	wnd.Changed()

	autoCheckpointsPanel.id++

	defer func() {
		ierr := recover()
		if ierr != nil {
			fmt.Fprintf(&out, "Error reloading checkpoints %v", ierr)
		}
		autoCheckpointsPanel.mu.Lock()
		autoCheckpointsPanel.loading = false
		autoCheckpointsPanel.mu.Unlock()
		refreshState(refreshToSameFrame, clearBreakpoint, nil)
	}()

	bps, err := client.ListBreakpoints(false)
	if err != nil {
		panic(err)
	}

	findBreakpoint := func(id int) *api.Breakpoint {
		for _, bp := range bps {
			if bp.ID == id {
				return bp
			}
		}
		return nil
	}

	for i := range autoCheckpointsPanel.checkpoints {
		check := &autoCheckpointsPanel.checkpoints[i]
		err := restartCheckpointToGoroutine(check.ID, check.GoroutineID)
		if err != nil {
			panic(err)
		}
		if check.Breakpoint == nil {
			continue
		}
		check.Breakpoint = findBreakpoint(check.Breakpoint.ID)
		if check.Breakpoint == nil || len(check.Breakpoint.Variables) <= 0 {
			check.Variables = nil
			continue
		}
		check.Variables = make([]*Variable, len(check.Breakpoint.Variables))
		for i := range check.Breakpoint.Variables {
			v, err := client.EvalVariable(api.EvalScope{GoroutineID: -1}, check.Breakpoint.Variables[i], getVariableLoadConfig())
			if err != nil {
				panic(err)
			}
			check.Variables[i] = wrapApiVariable("", v, v.Name, v.Name, true, nil, 0)
		}
	}
}

func autoCheckpointsLoadMore(dir int) {
	var out = editorWriter{true}

	autoCheckpointsPanel.mu.Lock()
	autoCheckpointsPanel.loading = true
	autoCheckpointsPanel.mu.Unlock()
	wnd.Changed()

	autoCheckpointsPanel.id++

	defer func() {
		ierr := recover()
		if ierr != nil {
			fmt.Fprintf(&out, "Error loading more checkpoints:%v\n", ierr)
		}
		autoCheckpointsPanel.mu.Lock()
		autoCheckpointsPanel.loading = false
		autoCheckpointsPanel.mu.Unlock()
		refreshState(refreshToSameFrame, clearBreakpoint, nil)
	}()

	if len(autoCheckpointsPanel.checkpoints) <= 0 {
		return
	}

	var check autoCheckpoint
	var limit int

	if dir < 0 {
		limit = autoCheckpointsPanel.backwardLimit
		check = autoCheckpointsPanel.checkpoints[0]
	} else {
		limit = autoCheckpointsPanel.forwardLimit
		check = autoCheckpointsPanel.checkpoints[len(autoCheckpointsPanel.checkpoints)-1]
	}

	_, err := client.RestartFrom(false, fmt.Sprintf("c%d", check.ID), false, nil, [3]string{}, false)
	if err != nil {
		panic(fmt.Errorf(" (moving to end): %v", err))
	}

	if limit <= 0 {
		limit = 10
	}

	addcheckpointsloop(dir, check.Counter()*dir+1, limit)

	sort.Slice(autoCheckpointsPanel.checkpoints, func(i, j int) bool {
		return autoCheckpointsPanel.checkpoints[i].Counter() < autoCheckpointsPanel.checkpoints[j].Counter()
	})
}

var timeoutSuffixes = []string{"s", "sec", "m", "min", "h"}

func autoCheckpointsTimeout() time.Duration {
	timeoutstr := string(autoCheckpointsPanel.timeoutEdit.Buffer)

	convert := func(t string, kind byte) time.Duration {
		n, _ := strconv.Atoi(t)
		if n <= 0 {
			autoCheckpointsPanel.timeoutEdit.Buffer = []rune("1s")
			wnd.Changed()
			return 1 * time.Second
		}

		switch kind {
		case 's':
			return time.Duration(n) * time.Second
		case 'm':
			return time.Duration(n) * 60 * time.Second
		case 'h':
			return time.Duration(n) * 60 * 60 * time.Second
		}

		return 1 * time.Second
	}

	for _, suffix := range timeoutSuffixes {
		if strings.HasSuffix(timeoutstr, suffix) {
			t := timeoutstr[:len(timeoutstr)-len(suffix)]
			return convert(t, suffix[0])
		}
	}

	return convert(timeoutstr, 's')
}
