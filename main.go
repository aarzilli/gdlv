// Copyright 2016, Gdlv Authors

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"math"
	"os"
	"runtime/pprof"
	"strings"
	"sync"

	"github.com/aarzilli/gdlv/internal/assets"
	"github.com/aarzilli/nucular"
	nstyle "github.com/aarzilli/nucular/style"
	"github.com/derekparker/delve/service"
	"github.com/derekparker/delve/service/api"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"

	"golang.org/x/image/font"
	"golang.org/x/mobile/event/key"
)

//go:generate go-bindata -o internal/assets/assets.go -pkg assets fontawesome-webfont.ttf

const profileEnabled = false

var zeroWidth, arrowWidth, starWidth int

var iconFontInit sync.Once
var iconTtfont *truetype.Font
var iconFace font.Face

const (
	arrowIcon      = "\uf061"
	breakpointIcon = "\uf28d"

	interruptIcon = "\uf04c"
	continueIcon  = "\uf04b"
	cancelIcon    = "\uf05e"
	nextIcon      = "\uf050"
	stepIcon      = "\uf051"
	stepoutIcon   = "\uf112"

	splitIcon = "\uf0db"
)

func setupStyle() {
	theme := nstyle.DarkTheme
	if conf.WhiteTheme {
		theme = nstyle.WhiteTheme
	}
	wnd.SetStyle(nstyle.FromTheme(theme, conf.Scaling))
	style := wnd.Style()
	style.Tab.Indent = style.Tab.Padding.X + style.Tab.Spacing.X + nucular.FontHeight(style.Font)
	style.Selectable.Normal.Data.Color = style.NormalWindow.Background
	style.GroupWindow.Padding.Y = 0
	style.GroupWindow.FooterPadding.Y = 0
	style.MenuWindow.FooterPadding.Y = 0
	style.ContextualWindow.FooterPadding.Y = 0
	zeroWidth = nucular.FontWidth(style.Font, "0")

	iconFontInit.Do(func() {
		iconFontData, _ := assets.Asset("fontawesome-webfont.ttf")
		iconTtfont, _ = freetype.ParseFont(iconFontData)
	})

	sz := int(12 * conf.Scaling)
	iconFace = truetype.NewFace(iconTtfont, &truetype.Options{Size: float64(sz), Hinting: font.HintingFull, DPI: 72})

	arrowWidth = nucular.FontWidth(iconFace, arrowIcon)
	starWidth = nucular.FontWidth(style.Font, breakpointIcon)

	saveConfiguration()
}

const commandLineHeight = 28

type listline struct {
	idx    string
	lineno int
	text   string
	pc     bool
	bp     *api.Breakpoint
}

var listingPanel struct {
	file                string
	abbrevFile          string
	recenterListing     bool
	recenterDisassembly bool
	listing             []listline
	text                api.AsmInstructions
	pinnedLoc           *api.Location
	stale               bool
}

var mu sync.Mutex
var wnd nucular.MasterWindow

var running, nextInProgress bool
var client service.Client
var curThread int
var curGid int
var curFrame int

var silenced bool
var scrollbackEditor, commandLineEditor nucular.TextEditor

func prompt(thread int, gid, frame int) string {
	if thread < 0 {
		return ""
	}
	if gid < 0 {
		return fmt.Sprintf("thread %d:%d", thread, frame)
	}
	return fmt.Sprintf("goroutine %d:%d", gid, frame)
}

func guiUpdate(w *nucular.Window) {
	mu.Lock()
	defer mu.Unlock()

	var scrollbackOut = editorWriter{&scrollbackEditor, false}
	mw := w.Master()

	for _, e := range w.Input().Keyboard.Keys {
		switch {
		case (e.Modifiers == key.ModControl || e.Modifiers == key.ModControl|key.ModShift) && (e.Code == key.CodeEqualSign):
			conf.Scaling += 0.1
			setupStyle()

		case (e.Modifiers == key.ModControl || e.Modifiers == key.ModControl|key.ModShift) && (e.Code == key.CodeHyphenMinus):
			conf.Scaling -= 0.1
			setupStyle()

		case (e.Modifiers == key.ModControl) && (e.Code == key.CodeF):
			mw.SetPerf(!mw.GetPerf())

		case (e.Modifiers == 0) && (e.Code == key.CodeEscape):
			mw.ActivateEditor(&commandLineEditor)

		case (e.Modifiers == key.ModControl) && (e.Code == key.CodeDeleteForward):
			if running && client != nil {
				_, err := client.Halt()
				if err != nil {
					fmt.Fprintf(&scrollbackOut, "Request manual stop failed: %v\n", err)
				}
				err = client.CancelNext()
				if err != nil {
					fmt.Fprintf(&scrollbackOut, "Could not cancel next operation: %v\n", err)
				}
			}
		}
	}

	rootPanel.update(w)
}

func currentPrompt() string {
	if running {
		return "running"
	} else if client == nil {
		if BackendServer.connectionFailed {
			return "failed"
		} else {
			return "connecting"
		}
	} else {
		if curThread < 0 {
			return "dlv>"
		} else {
			return prompt(curThread, curGid, curFrame) + ">"
		}
	}
}

func updateCommandPanel(container *nucular.Window) {
	w := container.GroupBegin("command", nucular.WindowNoScrollbar)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	style := w.Master().Style()

	w.LayoutReserveRow(commandLineHeight, 1)
	w.Row(0).Dynamic(1)
	scrollbackEditor.Edit(w)

	p := currentPrompt()

	promptwidth := nucular.FontWidth(style.Font, p) + style.Text.Padding.X*2

	w.Row(commandLineHeight).StaticScaled(promptwidth, 0)
	w.Label(p, "LC")

	if running {
		commandLineEditor.Flags |= nucular.EditReadOnly
	} else {
		commandLineEditor.Flags &= ^nucular.EditReadOnly
	}
	if commandLineEditor.Active {
		showHistory := false
		kbd := w.Input().Keyboard
		for _, k := range kbd.Keys {
			switch {
			case k.Modifiers == 0 && k.Code == key.CodeTab:
				w.Input().Keyboard.Text = ""
				completeAny()
			case k.Modifiers == 0 && k.Code == key.CodeUpArrow:
				historyShown--
				showHistory = true
			case k.Modifiers == 0 && k.Code == key.CodeDownArrow:
				historyShown++
				showHistory = true
			}
		}
		if showHistory {
			w.Input().Keyboard.Keys = w.Input().Keyboard.Keys[:0]
			switch {
			case historyShown < 0:
				historyShown = len(cmdhistory)
			case historyShown > len(cmdhistory):
				historyShown = len(cmdhistory)
			}

			if historyShown != len(cmdhistory) {
				commandLineEditor.Buffer = []rune(cmdhistory[historyShown])
				commandLineEditor.Cursor = len(commandLineEditor.Buffer)
				commandLineEditor.CursorFollow = true
			} else {
				commandLineEditor.Buffer = commandLineEditor.Buffer[:0]
				commandLineEditor.Cursor = 0
				commandLineEditor.CursorFollow = true
			}
		}
	}
	active := commandLineEditor.Edit(w)
	if active&nucular.EditCommitted != 0 {
		var scrollbackOut = editorWriter{&scrollbackEditor, false}
		cmd := string(commandLineEditor.Buffer)
		if canExecuteCmd(client, cmd) {
			if cmd == "" {
				fmt.Fprintf(&scrollbackOut, "%s %s\n", p, cmdhistory[len(cmdhistory)-1])
			} else {
				cmdhistory = append(cmdhistory, cmd)
				fmt.Fprintf(&scrollbackOut, "%s %s\n", p, cmd)
			}
			historyShown = len(cmdhistory)
			go executeCommand(cmd)
		} else {
			fmt.Fprintf(&scrollbackOut, "Only quit and restart available when not connected to delve\n")
		}
		commandLineEditor.Buffer = commandLineEditor.Buffer[:0]
		commandLineEditor.Cursor = 0
		commandLineEditor.CursorFollow = true
		commandLineEditor.Active = true
	}
}

func canExecuteCmd(client service.Client, cmd string) bool {
	if client != nil {
		return true
	}
	return cmd == "q" || cmd == "quit" || cmd == "restart"
}

func digits(n int) int {
	if n <= 0 {
		return 1
	}
	return int(math.Floor(math.Log10(float64(n)))) + 1
}

func hexdigits(n uint64) int {
	if n <= 0 {
		return 1
	}
	return int(math.Floor(math.Log10(float64(n))/math.Log10(16))) + 1
}

func expandTabs(in string) string {
	hastab := false
	for _, c := range in {
		if c == '\t' {
			hastab = true
			break
		}
	}
	if !hastab {
		return in
	}

	var buf bytes.Buffer
	count := 0
	for _, c := range in {
		switch c {
		case '\t':
			d := (((count/8)+1)*8 - count)
			for i := 0; i < d; i++ {
				buf.WriteRune(' ')
			}
			count = 0
		case '\n':
			buf.WriteRune('\n')
			count = 0
		default:
			buf.WriteRune(c)
			count++
		}
	}
	return buf.String()
}

type clearKind uint16

const (
	clearFrameSwitch clearKind = iota
	clearGoroutineSwitch
	clearStop
	clearBreakpoint
	clearNothing
)

type refreshToFrame uint16

const (
	refreshToFrameZero refreshToFrame = iota
	refreshToSameFrame
	refreshToUserFrame
)

func refreshState(toframe refreshToFrame, clearKind clearKind, state *api.DebuggerState) {
	defer wnd.Changed()

	var scrollbackOut = editorWriter{&scrollbackEditor, false}

	failstate := func(pos string, err error) {
		curThread = -1
		curGid = -1
		curFrame = 0
		fmt.Fprintf(&scrollbackOut, "Error refreshing state %s: %v\n", pos, err)
	}

	if state == nil {
		var err error
		state, err = client.GetState()
		if err != nil {
			mu.Lock()
			failstate("GetState()", err)
			mu.Unlock()
			return
		}
	}

	nextInProgress = state.NextInProgress

	mu.Lock()
	defer mu.Unlock()
	listingPanel.listing = listingPanel.listing[:0]
	listingPanel.text = nil
	listingPanel.recenterListing, listingPanel.recenterDisassembly = true, true
	if state.CurrentThread != nil {
		curThread = state.CurrentThread.ID
	} else {
		curThread = -1
		curFrame = 0
	}
	if state.SelectedGoroutine != nil && state.SelectedGoroutine.ID > 0 {
		curGid = state.SelectedGoroutine.ID
	} else {
		curGid = -1
	}

	switch clearKind {
	case clearNothing:
		// nothing to clear
	case clearBreakpoint:
		breakpointsPanel.asyncLoad.clear()
	case clearFrameSwitch:
		localsPanel.asyncLoad.clear()
		listingPanel.pinnedLoc = nil
	case clearGoroutineSwitch:
		stackPanel.asyncLoad.clear()
		localsPanel.asyncLoad.clear()
		regsPanel.asyncLoad.clear()
		listingPanel.pinnedLoc = nil
	case clearStop:
		localsPanel.asyncLoad.clear()
		regsPanel.asyncLoad.clear()
		goroutinesPanel.asyncLoad.clear()
		stackPanel.asyncLoad.clear()
		threadsPanel.asyncLoad.clear()
		globalsPanel.asyncLoad.clear()
		breakpointsPanel.asyncLoad.clear()
		listingPanel.pinnedLoc = nil
	}

	loc := listingPanel.pinnedLoc

	if loc == nil {
	findCurrentLocation:
		switch toframe {
		case refreshToFrameZero:
			curFrame = 0
			if state.SelectedGoroutine != nil {
				if state.CurrentThread != nil && state.SelectedGoroutine.ThreadID == state.CurrentThread.ID {
					loc = &api.Location{File: state.CurrentThread.File, Line: state.CurrentThread.Line, PC: state.CurrentThread.PC}
				} else {
					loc = &state.SelectedGoroutine.CurrentLoc
				}
			} else if state.CurrentThread != nil {
				loc = &api.Location{File: state.CurrentThread.File, Line: state.CurrentThread.Line, PC: state.CurrentThread.PC}
			}

		case refreshToSameFrame:
			frames, err := client.Stacktrace(curGid, curFrame+1, nil)
			if err != nil {
				failstate("Stacktrace()", err)
				return
			}
			if curFrame >= len(frames) {
				curFrame = 0
			}
			if curFrame < len(frames) {
				loc = &frames[curFrame].Location
			}

		case refreshToUserFrame:
			const runtimeprefix = "runtime."
			curFrame = 0
			frames, err := client.Stacktrace(curGid, 20, nil)
			if err != nil {
				failstate("Stacktrace()", err)
				return
			}
			if len(frames) == 0 {
				toframe = refreshToFrameZero
				goto findCurrentLocation
			}
			for i := range frames {
				if frames[i].Function == nil {
					continue
				}
				name := frames[i].Function.Name
				if !strings.HasPrefix(name, runtimeprefix) {
					curFrame = i
					break
				}
				if len(name) > len(runtimeprefix) {
					ch := name[len(runtimeprefix)]
					if ch >= 'A' && ch <= 'Z' {
						curFrame = i
						break
					}
				}
			}
			loc = &frames[curFrame].Location
		}
	}

	if loc != nil {
		flavour := api.IntelFlavour
		if conf.DisassemblyFlavour == 1 {
			flavour = api.GNUFlavour
		}
		if loc.PC != 0 {
			text, err := client.DisassemblePC(api.EvalScope{curGid, curFrame}, loc.PC, flavour)
			if err != nil {
				failstate("DisassemblePC()", err)
				return
			}

			listingPanel.text = text
		} else {
			listingPanel.text = nil
		}

		breakpoints, err := client.ListBreakpoints()
		if err != nil {
			failstate("ListBreakpoints()", err)
			return
		}
		listingPanel.file = loc.File
		listingPanel.abbrevFile = abbrevFileName(loc.File)
		bpmap := map[int]*api.Breakpoint{}
		for _, bp := range breakpoints {
			if bp.File == loc.File {
				bpmap[bp.Line] = bp
			}
		}

		fh, err := os.Open(loc.File)
		if err != nil {
			failstate("Open()", err)
			return
		}
		defer fh.Close()

		fi, _ := fh.Stat()
		lastModExe := client.LastModified()
		listingPanel.stale = fi.ModTime().After(lastModExe)

		buf := bufio.NewScanner(fh)
		lineno := 0
		for buf.Scan() {
			lineno++
			breakpoint := bpmap[lineno]
			listingPanel.listing = append(listingPanel.listing, listline{"", lineno, expandTabs(buf.Text()), lineno == loc.Line && listingPanel.pinnedLoc == nil, breakpoint})
		}

		if err := buf.Err(); err != nil {
			failstate("(reading file)", err)
			return
		}

		d := digits(len(listingPanel.listing))
		if d < 3 {
			d = 3
		}
		for i := range listingPanel.listing {
			listingPanel.listing[i].idx = fmt.Sprintf("%*d", d, i+1)
		}
	}
}

type editorWriter struct {
	ed   *nucular.TextEditor
	lock bool
}

const (
	scrollbackHighMark = 8 * 1024
	scrollbackLowMark  = 4 * 1024
)

func (w *editorWriter) Write(b []byte) (int, error) {
	if w.lock {
		mu.Lock()
		defer mu.Unlock()
		defer wnd.Changed()
	}
	w.ed.Buffer = append(w.ed.Buffer, []rune(expandTabs(string(b)))...)
	if len(w.ed.Buffer) > scrollbackHighMark {
		copy(w.ed.Buffer, w.ed.Buffer[scrollbackLowMark:])
		w.ed.Buffer = w.ed.Buffer[:len(w.ed.Buffer)-scrollbackLowMark]
		w.ed.Cursor = len(w.ed.Buffer) - 256
	}
	oldcursor := w.ed.Cursor
	for w.ed.Cursor = len(w.ed.Buffer) - 2; w.ed.Cursor > oldcursor; w.ed.Cursor-- {
		if w.ed.Buffer[w.ed.Cursor] == '\n' {
			break
		}
	}
	if w.ed.Cursor > 0 {
		w.ed.Cursor++
	}
	w.ed.CursorFollow = true
	w.ed.Redraw = true
	return len(b), nil
}

func usage() {
	fmt.Fprintf(os.Stderr, "usage:\n\tgdlv connect <address>\n\tgdlv debug <program's arguments...>\n\tgdlv run <program file> <program's arguments...>\n\tgdlv exec <executable> <program's arguments...>\n\tgdlv test <testflags...>\n\tgdlv attach <pid>\n")
	os.Exit(1)
}

func main() {
	loadConfiguration()

	if profileEnabled {
		if f, err := os.Create("cpu.pprof"); err == nil {
			if err := pprof.StartCPUProfile(f); err == nil {
				defer pprof.StopCPUProfile()
			}
		}
	}

	BackendServer = parseArguments()

	wnd = nucular.NewMasterWindow(guiUpdate, nucular.WindowNoScrollbar)
	setupStyle()

	rootPanel, _ = parsePanelDescr(conf.Layouts["default"].Layout, nil)

	curThread = -1
	curGid = -1

	scrollbackEditor.Flags = nucular.EditSelectable | nucular.EditReadOnly | nucular.EditMultiline | nucular.EditClipboard
	commandLineEditor.Flags = nucular.EditSelectable | nucular.EditSigEnter | nucular.EditClipboard
	commandLineEditor.Active = true

	var scrollbackOut = editorWriter{&scrollbackEditor, true}

	fmt.Fprintf(&scrollbackOut, `gdlv  Copyright (C) 2016 Gdlv Authors
This program comes with ABSOLUTELY NO WARRANTY;
This is free software, and you are welcome to redistribute it
under certain conditions; see COPYING for details.
`)

	cmds = DebugCommands()

	go BackendServer.Start()

	wnd.Main()

	BackendServer.Close()
}
