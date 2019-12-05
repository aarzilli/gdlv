// Copyright 2016, Gdlv Authors

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"os"
	"strconv"
	"strings"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"
)

const (
	infoCommand         = "Command"
	infoListing         = "Listing"
	infoDisassembly     = "Disassembly"
	infoGoroutines      = "Goroutines"
	infoStacktrace      = "Stacktrace"
	infoLocals          = "Variables"
	infoGlobal          = "Globals"
	infoBps             = "Breakpoints"
	infoThreads         = "Threads"
	infoRegisters       = "Registers"
	infoSources         = "Sources"
	infoFuncs           = "Functions"
	infoTypes           = "Types"
	infoCheckpoints     = "Checkpoints"
	infoDeferredCalls   = "DeferredCalls"
	infoAutoCheckpoints = "AutoCheckpoints"
)

type infoPanel struct {
	update    func(w *nucular.Window)
	flags     nucular.WindowFlags
	asyncLoad *asyncLoad
}

var infoNameToPanel map[string]infoPanel

var infoModes = []string{
	infoCommand, infoListing, infoDisassembly, infoGoroutines, infoStacktrace, infoLocals, infoGlobal, infoBps, infoThreads, infoRegisters, infoSources, infoFuncs, infoTypes, infoCheckpoints, infoDeferredCalls, infoAutoCheckpoints,
}

var codeToInfoMode = map[byte]string{
	'C': infoCommand,
	'L': infoListing,
	'D': infoDisassembly,
	'G': infoGoroutines,
	'S': infoStacktrace,
	'l': infoLocals,
	'g': infoGlobal,
	'B': infoBps,
	'r': infoRegisters,
	's': infoSources,
	'f': infoFuncs,
	't': infoTypes,
	'T': infoThreads,
	'k': infoCheckpoints,
	'd': infoDeferredCalls,
	'A': infoAutoCheckpoints,
}

var infoModeToCode = map[string]byte{}

func init() {
	infoNameToPanel = make(map[string]infoPanel)

	infoNameToPanel[infoCommand] = infoPanel{updateCommandPanel, nucular.WindowNoScrollbar, nil}
	infoNameToPanel[infoListing] = infoPanel{updateListingPanel, nucular.WindowNoScrollbar, nil}
	infoNameToPanel[infoDisassembly] = infoPanel{updateDisassemblyPanel, nucular.WindowNoScrollbar, nil}
	infoNameToPanel[infoGoroutines] = infoPanel{updateGoroutines, 0, &goroutinesPanel.asyncLoad}
	infoNameToPanel[infoStacktrace] = infoPanel{updateStacktrace, 0, &stackPanel.asyncLoad}
	infoNameToPanel[infoLocals] = infoPanel{updateLocals, 0, &localsPanel.asyncLoad}
	infoNameToPanel[infoGlobal] = infoPanel{updateGlobals, 0, &globalsPanel.asyncLoad}
	infoNameToPanel[infoBps] = infoPanel{updateBreakpoints, 0, &breakpointsPanel.asyncLoad}
	infoNameToPanel[infoThreads] = infoPanel{updateThreads, 0, &threadsPanel.asyncLoad}
	infoNameToPanel[infoRegisters] = infoPanel{updateRegs, 0, &regsPanel.asyncLoad}
	infoNameToPanel[infoSources] = infoPanel{sourcesPanel.update, nucular.WindowNoScrollbar, nil}
	infoNameToPanel[infoFuncs] = infoPanel{funcsPanel.update, nucular.WindowNoScrollbar, nil}
	infoNameToPanel[infoTypes] = infoPanel{typesPanel.update, nucular.WindowNoScrollbar, nil}
	infoNameToPanel[infoCheckpoints] = infoPanel{updateCheckpoints, 0, &checkpointsPanel.asyncLoad}
	infoNameToPanel[infoDeferredCalls] = infoPanel{updateDeferredCalls, 0, &stackPanel.asyncLoad}
	infoNameToPanel[infoAutoCheckpoints] = infoPanel{updateAutoCheckpoints, 0, &autoCheckpointsPanel.asyncLoad}

	for k, v := range codeToInfoMode {
		infoModeToCode[v] = k
	}
}

const (
	headerRow       = 20
	headerCombo     = 110
	controlBtnWidth = 30
)

func loadPanelDescrToplevel(in string) {
	var width, height int
	if len(in) > 3 {
		if in[0] == '$' {
			if dollar := strings.Index(in[1:], "$"); dollar >= 0 {
				szstr := in[1 : 1+dollar]
				in = in[2+dollar:]
				if comma := strings.Index(szstr, ","); comma >= 0 {
					width, _ = strconv.Atoi(szstr[:comma])
					height, _ = strconv.Atoi(szstr[comma+1:])
				}
			}
		}
	}
	if width <= 0 || height <= 0 {
		width = 640
		height = 480
	}

	if wnd == nil {
		wnd = nucular.NewMasterWindowSize(nucular.WindowNoScrollbar, "Gdlv", image.Point{width, height}, guiUpdate)
		setupStyle()
	}

	in = loadPanelDescr(in, wnd.ResetWindows())

	loadFloatingDescr(in)

	return
}

func loadPanelDescr(in string, curDockSplit *nucular.DockSplit) (rest string) {
	switch in[0] {
	case '0':
		rest = loadPanelDescr(in[1:], curDockSplit)
		return rest
	case '_', '|':
		horiz := true
		if in[0] == '|' {
			horiz = false
		}
		var i int
		for i = 1; i < len(in); i++ {
			if in[i] < '0' || in[i] > '9' {
				break
			}
		}
		size, _ := strconv.Atoi(in[1:i])

		left, right := curDockSplit.Split(horiz, size)

		rest = in[i:]
		rest = loadPanelDescr(rest, left)
		rest = loadPanelDescr(rest, right)
		return rest
	default:
		m := codeToInfoMode[in[0]]
		p := infoNameToPanel[m]
		curDockSplit.Open(m, p.Flags(m), rect.Rect{0, 0, 500, 300}, true, p.update)
		rest = in[1:]
		return rest
	}
}

func loadFloatingDescr(rest string) {
	for len(rest) > 0 {
		if rest[0] != ',' {
			fmt.Fprintf(os.Stderr, "deserialization error: %q\n", rest)
			return
		}

		rest = rest[1:]

		var dim [4]int

		for i := 0; i < len(dim); i++ {
			for j := 0; j < len(rest); j++ {
				if rest[j] < '0' || rest[j] > '9' {
					var err error
					dim[i], err = strconv.Atoi(rest[:j])
					if err != nil {
						fmt.Fprintf(os.Stderr, "deserialization error (%v): %q\n", err, rest)
					}
					if i == len(dim)-1 {
						rest = rest[j:]
					} else {
						rest = rest[j+1:]
					}
					break
				}
			}
		}

		m := codeToInfoMode[rest[0]]
		p := infoNameToPanel[m]
		rest = rest[1:]
		wnd.PopupOpen(m, p.Flags(m), rect.Rect{dim[0], dim[1], dim[2], dim[3]}, true, p.update)
	}
}

func cleanWindowTitle(title string) string {
	if idx := strings.Index(title, " "); idx >= 0 {
		title = title[:idx]
	}
	return title
}

func (p *infoPanel) Flags(m string) nucular.WindowFlags {
	flags := nucular.WindowDefaultFlags | nucular.WindowNonmodal | p.flags
	if m == infoCommand {
		flags = flags &^ nucular.WindowClosable
	}
	return flags
}

func serializeLayout() string {
	var out bytes.Buffer
	cnt := 0
	descale := func(x int) int {
		return int(float64(x) / conf.Scaling)
	}
	wnd.Walk(func(title string, data interface{}, docked bool, size int, rect rect.Rect) {
		title = cleanWindowTitle(title)
		c := infoModeToCode[title]
		if c == 0 {
			c = '?'
		}
		if cnt == 0 {
			fmt.Fprintf(&out, "$%d,%d$", descale(rect.W), descale(rect.H))
		} else if docked {
			if size != 0 {
				if size < 0 {
					fmt.Fprintf(&out, "|%d", -size)
				} else {
					fmt.Fprintf(&out, "_%d", size)
				}
			} else {
				if cnt == 1 {
					fmt.Fprintf(&out, "0")
				}
				fmt.Fprintf(&out, "%c", c)
			}
		} else {
			fmt.Fprintf(&out, ",%d,%d,%d,%d%c", descale(rect.X), descale(rect.Y), descale(rect.W), descale(rect.H), c)
		}
		cnt++
	})
	return out.String()
}

func listingToolbar(sw *nucular.Window) {
	sw.Row(headerRow).Static()

	showfilename := true

	if listingPanel.pinnedLoc != nil {
		sw.LayoutSetWidth(200)
		if sw.ButtonText("Back to current frame") {
			listingPanel.pinnedLoc = nil
			go refreshState(refreshToSameFrame, clearNothing, nil)
		}
		showfilename = false
	}

	if listingPanel.stale {
		sw.LayoutSetWidth(400)
		sw.LabelColored("Warning: listing may not match stale executable", "LC", color.RGBA{0xff, 0x00, 0x00, 0xff})
		showfilename = false
	}

	if listingPanel.optimized {
		sw.LayoutFitWidth(listingPanel.id, 100)
		sw.LabelColored(optimizedFunctionWarning, "LC", color.RGBA{0xff, 0x00, 0x00, 0xff})
	}

	if showfilename {
		sw.LayoutSetWidthScaled(4096)
		sw.Label(listingPanel.abbrevFile, "LC")
	}

}

func commandToolbar(sw *nucular.Window) {
	hovering := ""
	style := sw.Master().Style()
	iconbtn := func(icon string, tooltip string) bool {
		iconFace, style.Font = style.Font, iconFace
		r := sw.ButtonText(icon)
		if sw.Input().Mouse.HoveringRect(sw.LastWidgetBounds) {
			hovering = tooltip
		}
		iconFace, style.Font = style.Font, iconFace
		return r
	}
	cmdbtn := func(icon, cmd string) {
		if iconbtn(icon, cmd) {
			doCommand(cmd)
		}
	}
	switch {
	case client == nil:

	case scriptRunning:
		sw.LayoutSetWidth(100)
		if sw.ButtonText("stop script") {
			StarlarkEnv.Cancel()
		}

	case client.Running():
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(interruptIconChar, "interrupt")
		if sw.ButtonText("EOF") {
			doCommand("interrupt eof")
		}
		if sw.Input().Mouse.HoveringRect(sw.LastWidgetBounds) {
			hovering = "EOF"
		}

	case nextInProgress:
		sw.LayoutSetWidth(controlBtnWidth)
		if iconbtn(continueIconChar, "continue next") {
			doCommand("continue")
		}
		sw.LayoutSetWidth(controlBtnWidth)
		if iconbtn(cancelIconChar, "cancel next") {
			doCommand("cancelnext")
		}

	default:
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(continueIconChar, "continue")
		sw.LayoutSetWidth(controlBtnWidth / 2)
		sw.Spacing(1)
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(nextIconChar, "next")
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(stepIconChar, "step")
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(stepoutIconChar, "stepout")
	}

	sw.LayoutSetWidth(100)
	sw.Label(hovering, "LC")

	sw.LayoutResetStatic(0, headerCombo, 2)
	sw.Spacing(1)
	if w := sw.Combo(label.TA("NEW WINDOW", "CC"), 800, nil); w != nil {
		w.Row(20).Dynamic(1)
		for _, m := range infoModes {
			if m == "Command" {
				continue
			}
			if w.MenuItem(label.TA(m, "LC")) {
				openWindow(m)
			}
		}
	}
	sw.Spacing(1)
}

func openWindow(m string) {
	found := false
	wnd.Walk(func(title string, data interface{}, docked bool, size int, rect rect.Rect) {
		title = cleanWindowTitle(title)
		if title == m {
			// raise?
			found = true
			return
		}
	})
	if found {
		return
	}
	bounds, ok := conf.SavedBounds[m]
	if !ok {
		bounds = rect.Rect{0, 0, 500, 300}
	}
	p := infoNameToPanel[m]
	wnd.PopupOpen(m, p.Flags(m), bounds, true, p.update)
}
