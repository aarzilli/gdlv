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

	selected    int
	checkpoints []autoCheckpoint

	doneBackward, doneForward bool
}{}

type autoCheckpoint struct {
	ID          int
	Where       string
	GoroutineID int
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

	{
		w.MenubarBegin()
		w.Row(20).Static(0, 200)
		if w.ButtonText("Create checkpoints") {
			go autoCheckpointsReset()
		}
		w.MenubarEnd()
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

	w.Row(0).Dynamic(1)
	if w := w.GroupBegin("auto-checkpoints-list", 0); w != nil {
		{
			w.MenubarBegin()
			w.Row(varRowHeight).Static(100, 100, 100)

			w.Label("Checkpoint ID", "LT")
			w.Label("Goroutine ID", "LT")
			w.Label("Breakpoint", "LT")

			w.MenubarEnd()
		}

		for _, check := range autoCheckpointsPanel.checkpoints {
			selected := autoCheckpointsPanel.selected == check.ID
			w.Row(varRowHeight).Static(100, 100, 100, varRowHeight)

			w.SelectableLabel(fmt.Sprintf("c%d,%s", check.ID, check.Where), "LT", &selected)
			bounds := w.LastWidgetBounds
			bounds.W = w.Bounds.W

			w.SelectableLabel(fmt.Sprintf("%d", check.GoroutineID), "LT", &selected)

			if check.Breakpoint != nil {
				w.SelectableLabel(formatBreakpointName(check.Breakpoint, false), "LT", &selected)
			} else {
				if check.Where == "acp+0" {
					w.SelectableLabel("START", "LT", &selected)
				} else {
					w.SelectableLabel("", "LT", &selected)
				}
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

			if check.Breakpoint != nil {
				// Second line, breakpoint informations
				w.Row(varRowHeight).Static(100, 0)
				w.Spacing(1)
				w.Label(fmt.Sprintf("at %s\n", formatBreakpointLocation(check.Breakpoint)), "LT")

				for _, v := range check.Variables {
					w.Row(varRowHeight).Static(100, 0)
					w.Spacing(1)
					if v.Value != "" {
						w.Label(fmt.Sprintf("%s = %s", v.Name, v.Value), "LT")
					} else {
						w.Label(fmt.Sprintf("%s = %s", v.Name, v.SinglelineString(false, false)), "LT")
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

		w.GroupEnd()
	}
}

func addcheck(cnt int, gid int, bp *api.Breakpoint, bpi *api.BreakpointInfo) {
	where := fmt.Sprintf("acp%+d", cnt)
	cp, err := client.Checkpoint(where)
	if err != nil {
		panic(fmt.Errorf(" (%d): %v\n", cnt, err))
	}

	vars := []*Variable{}

	if bpi != nil {
		for i := range bpi.Variables {
			v := &bpi.Variables[i]
			vars = append(vars, wrapApiVariable(v, v.Name, v.Name, true, 0))
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
			if state.Err != nil {
				panic(fmt.Errorf(" (continuing in backward motion): %v", state.Err))
			}
			if state.Exited {
				return true, true
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

func addcheckpointsloop(dir int, cntstart int) {
	ticker := time.NewTicker(1 * time.Second)

	for cnt := cntstart; cnt < cntstart+10; {
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

	defer func() {
		ierr := recover()
		if ierr != nil {
			fmt.Fprintf(&out, "Error creating automatic checkpoints %v", ierr)
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

	addcheckpointsloop(+1, 1)

	_, err := client.RestartFrom(false, fmt.Sprintf("c%d", startCheckpoint), false, nil, [3]string{}, false)
	if err != nil {
		panic(fmt.Errorf(" (resetting before backward motion): %v", err))
	}

	addcheckpointsloop(-1, 1)

	_, err = client.RestartFrom(false, fmt.Sprintf("c%d", startCheckpoint), false, nil, [3]string{}, false)
	if err != nil {
		panic(fmt.Errorf(" (resetting before stopping): %v", err))
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

	bps, err := client.ListBreakpoints()
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
			check.Variables[i] = wrapApiVariable(v, v.Name, v.Name, true, 0)
		}
	}
}

func autoCheckpointsLoadMore(dir int) {
	var out = editorWriter{true}

	autoCheckpointsPanel.mu.Lock()
	autoCheckpointsPanel.loading = true
	autoCheckpointsPanel.mu.Unlock()
	wnd.Changed()

	defer func() {
		ierr := recover()
		if ierr != nil {
			fmt.Fprintf(&out, "Error loading more checkpoints %v", ierr)
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

	if dir < 0 {
		check = autoCheckpointsPanel.checkpoints[0]
	} else {
		check = autoCheckpointsPanel.checkpoints[len(autoCheckpointsPanel.checkpoints)-1]
	}

	_, err := client.RestartFrom(false, fmt.Sprintf("c%d", check.ID), false, nil, [3]string{}, false)
	if err != nil {
		panic(fmt.Errorf(" (moving to end): %v", err))
	}

	addcheckpointsloop(dir, check.Counter()*dir+1)

	sort.Slice(autoCheckpointsPanel.checkpoints, func(i, j int) bool {
		return autoCheckpointsPanel.checkpoints[i].Counter() < autoCheckpointsPanel.checkpoints[j].Counter()
	})
}
