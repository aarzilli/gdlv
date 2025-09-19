package main

import (
	"bufio"
	"fmt"
	"io"
	"os"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
)

type frozenBreakpoint struct {
	Bp             api.Breakpoint
	LineInFunction int
	LineContents   string
}

var FrozenBreakpoints []frozenBreakpoint

// Saves position information for bp in FrozenBreakpoints
func freezeBreakpoint(out io.Writer, bp *api.Breakpoint) {
	if bp == nil || bp.ID < 0 || bp.FunctionName == "" || bp.File == "" {
		return
	}
	var fbp frozenBreakpoint
	fbp.Bp = *bp

	locs, _, err := client.FindLocation(api.EvalScope{-1, 0, 0}, fbp.Bp.FunctionName, true, nil)
	if err != nil || len(locs) != 1 || locs[0].Function == nil || locs[0].Function.Name() != fbp.Bp.FunctionName {
		fmt.Fprintf(out, "Function not found while recording breakpoint\n")
		return
	}
	functionLoc := locs[0]

	if functionLoc.File != bp.File {
		fmt.Fprintf(out, "File mismatch while recording breakpoint\n")
		return
	}

	fbp.LineInFunction = bp.Line - functionLoc.Line

	if fbp.LineInFunction > 0 {
		fh, err := os.Open(bp.File)
		if err != nil {
			fmt.Fprintf(out, "Could not open source file while recording breakpoint\n")
			return
		}
		defer fh.Close()

		// check for executable staleness
		fi, _ := fh.Stat()
		lastModExe := client.LastModified()
		if fi.ModTime().After(lastModExe) {
			// executable is stale
			fmt.Fprintf(out, "Breakpoint set on stale executable\n")
			return
		}

		buf := bufio.NewScanner(fh)
		lineno := 0
		for buf.Scan() {
			lineno++
			if bp.Line == lineno {
				fbp.LineContents = buf.Text()
				break
			}
		}
	}

	FrozenBreakpoints = append(FrozenBreakpoints, fbp)
	saveConfiguration()
}

func removeFrozenBreakpoint(bp *api.Breakpoint) {
	if bp == nil {
		return
	}
	for i := range FrozenBreakpoints {
		if FrozenBreakpoints[i].Bp.ID == bp.ID {
			copy(FrozenBreakpoints[i:], FrozenBreakpoints[i+1:])
			FrozenBreakpoints = FrozenBreakpoints[:len(FrozenBreakpoints)-1]
			break
		}
	}
	saveConfiguration()
}

// Collect breakpoint configuration of all frozen breakpoints
func updateFrozenBreakpoints() {
	for i := range FrozenBreakpoints {
		bp, err := client.GetBreakpoint(FrozenBreakpoints[i].Bp.ID)
		if err == nil {
			FrozenBreakpoints[i].Bp = *bp
		}
	}
}

// Clears all breakpoints in FrozenBreakpoints
func clearFrozenBreakpoints() {
	for _, fbp := range FrozenBreakpoints {
		client.ClearBreakpoint(fbp.Bp.ID)
	}
}

func restoreFrozenBreakpoints(out io.Writer) {
	// Restore frozen breakpoints
	for i := range FrozenBreakpoints {
		FrozenBreakpoints[i].Restore(out)
	}

	// Re-freeze breakpoints
	FrozenBreakpoints = FrozenBreakpoints[:0]
	bps, err := client.ListBreakpoints(false)
	if err != nil {
		return
	}
	for _, bp := range bps {
		if bp.ID >= 0 {
			freezeBreakpoint(out, bp)
		}
	}
}

func (fbp *frozenBreakpoint) Restore(out io.Writer) {
	if fbp.Bp.FunctionName == "" || fbp.Bp.File == "" {
		return
	}

	if fbp.LineInFunction == 0 {
		fbp.Bp.Addr = 0
		fbp.Bp.File = ""
		fbp.Bp.Line = -1
		_, err := client.CreateBreakpoint(&fbp.Bp)
		if err != nil {
			fmt.Fprintf(out, "Could not restore breakpoint at function %s: %v\n", fbp.Bp.FunctionName, err)
		}
		return
	}

	locs, _, err := client.FindLocation(api.EvalScope{-1, 0, 0}, fbp.Bp.FunctionName, true, nil)
	if err != nil || len(locs) != 1 || locs[0].Function == nil || locs[0].Function.Name() != fbp.Bp.FunctionName {
		fmt.Fprintf(out, "Could not restore breakpoint %d, function not found\n", fbp.Bp.ID)
		return
	}
	functionLoc := locs[0]

	// Find line closest to startOfFunction + LineInFunction that matches LineContents
	// If not found just set it to startOfFunction + LineInFunction

	fh, err := os.Open(functionLoc.File)
	if err != nil {
		return
	}
	defer fh.Close()

	dist := func(lineno int) int {
		dist := lineno - (functionLoc.Line + fbp.LineInFunction)
		if dist < 0 {
			return -dist
		}
		return dist
	}

	bestMatch := -1

	buf := bufio.NewScanner(fh)
	lineno := 0
	for buf.Scan() {
		lineno++
		if buf.Text() == fbp.LineContents {
			if bestMatch < 0 || dist(lineno) < dist(bestMatch) {
				bestMatch = lineno
			}
		}
	}

	if bestMatch < 0 {
		bestMatch = functionLoc.Line + fbp.LineInFunction
	}

	fbp.Bp.Addr = 0
	fbp.Bp.FunctionName = ""
	fbp.Bp.File = functionLoc.File
	fbp.Bp.Line = bestMatch

	fbp.Set(out, &functionLoc)
}

func (fbp *frozenBreakpoint) Set(out io.Writer, functionLoc *api.Location) {
	bp, err := client.CreateBreakpointWithExpr(&fbp.Bp, fmt.Sprintf("%s:%d", fbp.Bp.File, fbp.Bp.Line), nil, true)
	if err != nil {
		fmt.Fprintf(out, "Could not restore breakpoint at %s:%d: %v\n", fbp.Bp.File, fbp.Bp.Line, err)
		return
	}

	savedDisabled := fbp.Bp.Disabled
	fbp.Bp = *bp
	fbp.Bp.Disabled = savedDisabled

	if functionLoc != nil {
		if bp.FunctionName != functionLoc.Function.Name() {
			client.ClearBreakpoint(bp.ID)
			fmt.Fprintf(out, "Could not restore breakpoint %d (function name mismatch)\n", fbp.Bp.ID)
		}
	}

	if fbp.Bp.Disabled {
		client.AmendBreakpoint(&fbp.Bp)
	}
}

func disableBreakpoint(bp *api.Breakpoint) {
	for i := range FrozenBreakpoints {
		if bp == nil || FrozenBreakpoints[i].Bp.ID == bp.ID {
			FrozenBreakpoints[i].Bp.Disabled = true
			client.AmendBreakpoint(&FrozenBreakpoints[i].Bp)
			if bp != nil {
				break
			}
		}
	}
	saveConfiguration()
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
	wnd.Changed()
}

func enableBreakpoint(bp *api.Breakpoint) {
	for i := range FrozenBreakpoints {
		if bp == nil || FrozenBreakpoints[i].Bp.ID == bp.ID {
			FrozenBreakpoints[i].Bp.Disabled = false
			client.AmendBreakpoint(&FrozenBreakpoints[i].Bp)
			if bp != nil {
				break
			}
		}
	}
	saveConfiguration()
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
	wnd.Changed()
}
