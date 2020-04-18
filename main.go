// Copyright 2016, Gdlv Authors

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"image/color"
	"io/ioutil"
	"math"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"time"

	"github.com/aarzilli/gdlv/internal/assets"
	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/dlvclient/service/rpc2"
	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/font"
	"github.com/aarzilli/nucular/rect"
	"github.com/aarzilli/nucular/richtext"
	nstyle "github.com/aarzilli/nucular/style"

	"golang.org/x/mobile/event/key"
)

//go:generate go-bindata -o internal/assets/assets.go -pkg assets fontawesome-webfont.ttf droid-sans.bold.ttf codicon.ttf

const profileEnabled = false

var zeroWidth, arrowWidth, starWidth, spaceWidth int

var fontInit sync.Once
var iconFace font.Face
var codiconFace font.Face
var boldFace font.Face

var normalFontData []byte
var boldFontData []byte
var iconFontData []byte
var codiconFontData []byte

var (
	linkColor      = color.RGBA{0x00, 0x88, 0xdd, 0xff}
	linkHoverColor = color.RGBA{0x00, 0xaa, 0xff, 0xff}
)

const (
	arrowIconChar      = "\uf061"
	breakpointIconChar = "\uf28d"

	interruptIconChar = "\uEAD1"
	continueIconChar  = "\uEACF"
	cancelIconChar    = "\uEAD7"
	nextIconChar      = "\uEAD6"
	stepIconChar      = "\uEAD4"
	stepoutIconChar   = "\uEAD5"
)

func setupStyle() {
	switch conf.Theme {
	default:
		fallthrough
	case darkTheme:
		wnd.SetStyle(nstyle.FromTheme(nstyle.DarkTheme, conf.Scaling))
	case whiteTheme:
		wnd.SetStyle(nstyle.FromTheme(nstyle.WhiteTheme, conf.Scaling))
	case redTheme:
		wnd.SetStyle(nstyle.FromTable(redThemeTable, conf.Scaling))
	case boringTheme:
		style := makeBoringStyle()
		style.Scale(conf.Scaling)
		wnd.SetStyle(style)
	}

	fontInit.Do(func() {
		iconFontData, _ = assets.Asset("fontawesome-webfont.ttf")
		codiconFontData, _ = assets.Asset("codicon.ttf")

		normalFontPath := os.Getenv("GDLV_NORMAL_FONT")
		boldFontPath := os.Getenv("GDLV_BOLD_FONT")
		customFonts := false

		if normalFontPath != "" && boldFontPath != "" {
			_, normerr := os.Stat(normalFontPath)
			_, bolderr := os.Stat(boldFontPath)
			if normerr == nil && bolderr == nil {
				normalFontData, normerr = ioutil.ReadFile(normalFontPath)
				boldFontData, bolderr = ioutil.ReadFile(boldFontPath)
				if normerr == nil && bolderr == nil {
					customFonts = true
				}
			}
			if normerr != nil {
				fmt.Fprintf(os.Stderr, "Error opening GDLV_NORMAL_FONT %q: %v\n", normalFontPath, normerr)
			}
			if bolderr != nil {
				fmt.Fprintf(os.Stderr, "Error opening GDLV_BOLD_FONT %q: %v\n", boldFontPath, bolderr)
			}
		} else if normalFontPath != "" && boldFontPath == "" {
			fmt.Fprintf(os.Stderr, "GDLV_NORMAL_FONT set without GDLV_BOLD_FONT\n")
		} else if normalFontPath == "" && boldFontPath != "" {
			fmt.Fprintf(os.Stderr, "GDLV_BOLD_FONT set without GDLV_NORMAL_FONT\n")
		}

		if !customFonts {
			boldFontData, _ = assets.Asset("droid-sans.bold.ttf")
		}
	})

	style := wnd.Style()
	style.Tab.Indent = style.Tab.Padding.X + style.Tab.Spacing.X + nucular.FontHeight(style.Font) + style.GroupWindow.Spacing.X
	style.Selectable.Normal.Data.Color = style.NormalWindow.Background
	style.GroupWindow.Padding.Y = 0
	style.GroupWindow.FooterPadding.Y = 0
	style.MenuWindow.FooterPadding.Y = 0
	style.ContextualWindow.FooterPadding.Y = 0
	zeroWidth = nucular.FontWidth(style.Font, "0")
	spaceWidth = nucular.FontWidth(style.Font, " ")

	sz := int(12 * conf.Scaling)
	var err error
	iconFace, err = font.NewFace(iconFontData, sz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not parse icon font: %v\n", err)
		os.Exit(1)
	}
	codiconFace, err = font.NewFace(codiconFontData, sz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not parse codicon font: %v\n", err)
		os.Exit(1)
	}
	boldFace, err = font.NewFace(boldFontData, sz)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not parse bold font: %v\n", err)
		os.Exit(1)
	}
	if normalFontData != nil {
		style.Font, err = font.NewFace(normalFontData, sz)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not parse normal font: %v\n", err)
			os.Exit(1)
		}
	}

	arrowWidth = nucular.FontWidth(iconFace, arrowIconChar)
	starWidth = nucular.FontWidth(style.Font, breakpointIconChar)

	saveConfiguration()
}

const commandLineHeight = 28

type listline struct {
	idx          string
	lineno       int
	text         string
	textWithTabs string
	pc           bool
	bp           *api.Breakpoint
	bpenabled    bool
}

var listingPanel struct {
	file                string
	abbrevFile          string
	recenterListing     bool
	recenterDisassembly bool
	listing             []listline
	text                api.AsmInstructions
	framePC             uint64
	pinnedLoc           *api.Location
	stale               bool
	optimized           bool
	id                  int

	stepIntoInfo   stepIntoInfo
	stepIntoFilled bool
}

var wnd nucular.MasterWindow

var nextInProgress bool
var client *rpc2.RPCClient
var curThread int
var curGid int
var curFrame int
var curDeferredCall int
var curPC uint64
var lastModExe time.Time
var scriptRunning bool
var starlarkMode chan string
var starlarkPrompt string

var commandLineEditor nucular.TextEditor

var delayFrame bool
var frameCount int

func guiUpdate(w *nucular.Window) {
	df := delayFrame
	delayFrame = false

	if df {
		time.Sleep(50 * time.Millisecond)
	}

	mw := w.Master()

	for _, e := range wnd.Input().Keyboard.Keys {
		switch {
		case (e.Modifiers&key.ModControl != 0) && e.Code == key.CodeEqualSign:
			// mitigation for shiny bug on macOS (see https://github.com/aarzilli/gdlv/issues/39)
			fallthrough
		case (e.Modifiers&key.ModControl != 0) && e.Rune == '+':
			conf.Scaling += 0.1
			setupStyle()

		case (e.Modifiers&key.ModControl != 0) && e.Code == key.CodeHyphenMinus:
			// mitigation for shiny bug on macOS (see https://github.com/aarzilli/gdlv/issues/39)
			fallthrough
		case (e.Modifiers&key.ModControl != 0) && e.Rune == '-':
			conf.Scaling -= 0.1
			setupStyle()

		case (e.Modifiers == key.ModControl) && (e.Code == key.CodeF):
			mw.SetPerf(!mw.GetPerf())

		case (e.Modifiers == 0) && (e.Code == key.CodeEscape):
			mw.ActivateEditor(&commandLineEditor)

		case (e.Modifiers == 0) && (e.Code == key.CodeF5):
			if !client.Running() && client != nil {
				doCommand("continue")
			}

		case (e.Modifiers == 0) && (e.Code == key.CodeF10):
			fallthrough
		case (e.Modifiers == key.ModAlt) && (e.Code == key.CodeRightArrow):
			if !client.Running() && client != nil {
				doCommand("next")
			}

		case (e.Modifiers == 0) && (e.Code == key.CodeF11):
			fallthrough
		case (e.Modifiers == key.ModAlt) && (e.Code == key.CodeDownArrow):
			if !client.Running() && client != nil {
				doCommand("step")
			}

		case (e.Modifiers == key.ModShift) && (e.Code == key.CodeF11):
			fallthrough
		case (e.Modifiers == key.ModAlt) && (e.Code == key.CodeUpArrow):
			if !client.Running() && client != nil {
				doCommand("stepout")
			}

		case (e.Modifiers == key.ModShift) && (e.Code == key.CodeF5):
			fallthrough
		case (e.Modifiers == key.ModControl) && (e.Code == key.CodeDeleteForward):
			if client != nil {
				doCommand("interrupt")
			}

		case (e.Modifiers == key.ModAlt) && (e.Code == key.Code1):
			openWindow(infoListing)

		case (e.Modifiers == key.ModAlt) && (e.Code == key.Code2):
			openWindow(infoLocals)

		case (e.Modifiers == key.ModAlt) && (e.Code == key.Code3):
			openWindow(infoGlobal)

		case (e.Modifiers == key.ModAlt) && (e.Code == key.Code4):
			openWindow(infoRegisters)

		case (e.Modifiers == key.ModAlt) && (e.Code == key.Code5):
			openWindow(infoBps)

		case (e.Modifiers == key.ModAlt) && (e.Code == key.Code6):
			openWindow(infoStacktrace)

		case (e.Modifiers == key.ModAlt) && (e.Code == key.Code7):
			openWindow(infoDisassembly)

		case (e.Modifiers == key.ModAlt) && (e.Code == key.Code8):
			openWindow(infoGoroutines)

		case (e.Modifiers == key.ModAlt) && (e.Code == key.Code9):
			openWindow(infoThreads)
		}
	}

	descale := func(x int) int {
		return int(float64(x) / conf.Scaling)
	}

	frameCount++
	if frameCount%200 == 0 {
		changed := false
		wnd.Walk(func(title string, data interface{}, docked bool, size int, rect rect.Rect) {
			if docked {
				return
			}
			title = cleanWindowTitle(title)
			rect.X = descale(rect.X)
			rect.Y = descale(rect.Y)
			rect.H = descale(rect.H)
			rect.W = descale(rect.W)
			if rect != conf.SavedBounds[title] {
				conf.SavedBounds[title] = rect
				changed = true
			}
		})
		if changed {
			saveConfiguration()
		}
	}
}

func currentPrompt() string {
	if client.Running() {
		return "running"
	} else if client == nil {
		switch {
		case BackendServer.connectionFailed:
			return "failed"
		case !BackendServer.buildok:
			return "build failed"
		default:
			return "connecting"
		}
	} else {
		pmpt := ">"
		if starlarkMode != nil {
			pmpt = starlarkPrompt
		}
		if curThread < 0 {
			return fmt.Sprintf("dlv%s", pmpt)
		} else if curGid < 0 {
			return fmt.Sprintf("thread %d:%d%s", curThread, curFrame, pmpt)
		} else if curDeferredCall > 0 {
			return fmt.Sprintf("deferred call %d:%d:%d%s", curGid, curFrame, curDeferredCall, pmpt)
		} else {
			return fmt.Sprintf("goroutine %d:%d%s", curGid, curFrame, pmpt)
		}
	}
}

func updateCommandPanel(w *nucular.Window) {
	style := w.Master().Style()

	w.Row(headerRow).Static()
	w.LayoutReserveRow(commandLineHeight, 1)
	commandToolbar(w)

	w.Row(0).Dynamic(1)
	if c := scrollbackEditor.Widget(w, scrollbackClear); c != nil {
		scrollbackClear = false
		c.Align(richtext.AlignLeftDumb)
		if len(scrollbackPreInitWrite) > 0 {
			c.Text(string(scrollbackPreInitWrite))
		}
		c.End()
		scrollbackEditor.Sel.S = int32(len(scrollbackPreInitWrite))
		scrollbackEditor.Sel.E = scrollbackEditor.Sel.S
		scrollbackEditor.FollowCursor()
		scrollbackMu.Lock()
		scrollbackInitialized = true
		scrollbackMu.Unlock()
	}

	p := currentPrompt()
	p2 := p

	if historySearch {
		p2 += " (searching)"
	}

	promptwidth := nucular.FontWidth(style.Font, p2) + style.Text.Padding.X*2

	w.Row(commandLineHeight).StaticScaled(promptwidth, 0)
	w.Label(p2, "LC")

	if client.Running() {
		//commandLineEditor.Flags |= nucular.EditReadOnly
		if !commandLineEditor.Active {
			w.Master().ActivateEditor(&commandLineEditor)
		}
	} else {
		commandLineEditor.Flags &= ^nucular.EditReadOnly
	}
	if commandLineEditor.Active {
		showHistory := false
		kbd := &w.Input().Keyboard
		for _, k := range kbd.Keys {
			switch {
			case k.Modifiers == 0 && k.Code == key.CodeTab:
				historySearch = false
				w.Input().Keyboard.Text = ""
				completeAny()
			case k.Modifiers == 0 && k.Code == key.CodeUpArrow:
				historySearch = false
				historyShown--
				showHistory = true
			case k.Modifiers == 0 && k.Code == key.CodeDownArrow:
				historySearch = false
				historyShown++
				showHistory = true
			case k.Modifiers == key.ModControl && k.Code == key.CodeR:
				historySearch = true
				historyShown = -1
				historyNeedle = ""
				showHistory = true
			case k.Modifiers == 0 && k.Code == key.CodeEscape:
				historySearch = false
				historyShown = -1
				showHistory = true
			case k.Modifiers == 0 && k.Code == key.CodeDeleteBackspace && historySearch:
				historyNeedle = historyNeedle[:len(historyNeedle)]
			}
		}
		if historySearch && kbd.Text != "" && kbd.Text != "\n" {
			historyNeedle = historyNeedle + kbd.Text
			kbd.Text = ""
			searchHistory()
			showHistory = true
		}
		if showHistory {
			w.Input().Keyboard.Keys = w.Input().Keyboard.Keys[:0]
			if historyShown < 0 || historyShown > len(cmdhistory) {
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
		historySearch = false
		var scrollbackOut = editorWriter{false}
		cmd := string(commandLineEditor.Buffer)
		if scriptRunning {
			fmt.Fprintf(&scrollbackOut, "a script is running\n")
		} else if starlarkMode != nil {
			cmdhistory = append(cmdhistory, cmd)
			fmt.Fprintf(&scrollbackOut, "%s %s\n", p, cmd)
			starlarkMode <- cmd
		} else if canExecuteCmd(cmd) && !client.Running() {
			if cmd == "" {
				if len(cmdhistory) > 0 {
					fmt.Fprintf(&scrollbackOut, "%s %s\n", p, cmdhistory[len(cmdhistory)-1])
					cmd = cmdhistory[len(cmdhistory)-1]
				} else {
					cmd = "help"
				}
			} else {
				cmdhistory = append(cmdhistory, cmd)
				fmt.Fprintf(&scrollbackOut, "%s %s\n", p, cmd)
			}
			historyShown = len(cmdhistory)
			go executeCommand(cmd)
		} else if client.Running() && client != nil && BackendServer.stdinChan != nil && curThread >= 0 {
			select {
			case BackendServer.stdinChan <- cmd + "\n":
			default:
			}
		} else {
			fmt.Fprintf(&scrollbackOut, "Only quit and restart available when not connected to delve\n")
		}
		commandLineEditor.Buffer = commandLineEditor.Buffer[:0]
		commandLineEditor.Cursor = 0
		commandLineEditor.CursorFollow = true
		commandLineEditor.Active = true
	}
}

func searchHistory() {
	if historyShown < 0 || historyShown >= len(cmdhistory) {
		historyShown = len(cmdhistory) - 1
	}
	for historyShown >= 0 {
		if strings.Index(cmdhistory[historyShown], historyNeedle) >= 0 {
			return
		}
		historyShown--
	}
	historyShown = -1
}

func canExecuteCmd(cmd string) bool {
	if client != nil {
		return true
	}
	return cmd == "q" || cmd == "quit" || cmd == "r" || cmd == "restart"
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

func expandTabsEx(in string, colno int) (string, int) {
	hastab := false
	for _, c := range in {
		if c == '\t' {
			hastab = true
			break
		}
	}
	if !hastab {
		return in, colno
	}

	var buf bytes.Buffer
	count := 0
	colidx := -1
	for i, c := range in {
		switch c {
		case '\t':
			d := (((count/8)+1)*8 - count)
			for i := 0; i < d; i++ {
				colno--
				buf.WriteRune(' ')
			}
			count = 0
		case '\n':
			colno--
			buf.WriteRune('\n')
			count = 0
		default:
			colno--
			buf.WriteRune(c)
			count++
		}
		if colno <= 0 && colidx < 0 {
			colidx = i
		}
	}
	return buf.String(), colidx
}

func expandTabs(in string) string {
	r, _ := expandTabsEx(in, 0)
	return r
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

	var scrollbackOut = editorWriter{false}

	failstate := func(pos string, err error) {
		fmt.Fprintf(&scrollbackOut, "Error refreshing state %s: %v\n", pos, err)
	}

	if state == nil {
		var err error
		state, err = client.GetState()
		if err != nil {
			wnd.Lock()
			curThread = -1
			curGid = -1
			curFrame = 0
			curDeferredCall = 0
			if !strings.Contains(err.Error(), " has exited with status ") {
				failstate("GetState()", err)
			}

			listingPanel.id++
			if clearKind != clearBreakpoint {
				loadListing(listingPanel.pinnedLoc, failstate)
			}

			wnd.Unlock()
			return
		}
	}

	wnd.Lock()
	defer wnd.Unlock()

	nextInProgress = state.NextInProgress

	delayFrame = true

	if state.CurrentThread != nil {
		curThread = state.CurrentThread.ID
	} else {
		curThread = -1
		curFrame = 0
		curDeferredCall = 0
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
		checkpointsPanel.asyncLoad.clear()
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
		checkpointsPanel.asyncLoad.clear()
		listingPanel.pinnedLoc = nil
		silenced = false

		bpcount := 0
		for _, th := range state.Threads {
			if th.Breakpoint != nil {
				bpcount++
			}
		}

		if bpcount > 1 {
			fmt.Fprintf(&scrollbackOut, "Simultaneously stopped on %d goroutines!\n", bpcount)
		}
	}

	loc := listingPanel.pinnedLoc

	if loc == nil {
	findCurrentLocation:
		switch toframe {
		case refreshToFrameZero:
			curFrame = 0
			curDeferredCall = 0
			loc = currentLocation(state)

		case refreshToSameFrame:
			frames, err := client.Stacktrace(curGid, curFrame+1, api.StacktraceReadDefers, nil)
			if err != nil {
				curFrame = 0
				curDeferredCall = 0
				failstate("Stacktrace()", err)
				return
			}
			if curFrame >= len(frames) {
				curFrame = 0
				curDeferredCall = 0
			}
			if curFrame < len(frames) {
				if curDeferredCall-1 >= len(frames[curFrame].Defers) {
					curDeferredCall = 0
				}
				if curDeferredCall <= 0 {
					loc = &frames[curFrame].Location
				} else if curDeferredCall-1 < len(frames[curFrame].Defers) {
					if stackPanel.showDeferPos {
						loc = &frames[curFrame].Defers[curDeferredCall-1].DeferLoc
					} else {
						loc = &frames[curFrame].Defers[curDeferredCall-1].DeferredLoc
					}
				}
			}

		case refreshToUserFrame:
			const runtimeprefix = "runtime."
			curFrame = 0
			curDeferredCall = 0
			frames, err := client.Stacktrace(curGid, 20, 0, nil)
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
				name := frames[i].Function.Name()
				if !strings.HasPrefix(name, runtimeprefix) {
					curFrame = i
					curDeferredCall = 0
					break
				}
				if len(name) > len(runtimeprefix) {
					ch := name[len(runtimeprefix)]
					if ch >= 'A' && ch <= 'Z' {
						curFrame = i
						curDeferredCall = 0
						break
					}
				}
			}
			loc = &frames[curFrame].Location
		}
	}

	if loc == nil {
		curPC = 0
		return
	}

	curPC = loc.PC

	listingPanel.id++

	listingPanel.text = nil
	disassemblyPanel.asyncLoad.clear()
	disassemblyPanel.loc = *loc

	if clearKind != clearBreakpoint {
		loadListing(loc, failstate)
	}

	applyBreakpoints(failstate)

	wnd.Walk(func(title string, data interface{}, docked bool, splitSize int, rect rect.Rect) {
		if asyncLoad, ok := data.(*asyncLoad); ok && asyncLoad != nil {
			if title == "Details" && clearKind != clearNothing && clearKind != clearBreakpoint {
				asyncLoad.clear()
			}
			asyncLoad.startLoad()
		}
	})

}

func loadDisassembly(p *asyncLoad) {
	listingPanel.text = nil
	listingPanel.recenterDisassembly = true

	loc := disassemblyPanel.loc

	flavour := api.IntelFlavour
	if conf.DisassemblyFlavour == 1 {
		flavour = api.GNUFlavour
	}
	if loc.PC != 0 {
		text, err := client.DisassemblePC(currentEvalScope(), loc.PC, flavour)
		if err != nil {
			p.done(err)
			return
		}

		listingPanel.text = text
		listingPanel.framePC = loc.PC
	} else {
		listingPanel.text = nil
		listingPanel.framePC = 0
	}
	p.done(nil)
}

func loadListing(loc *api.Location, failstate func(string, error)) {
	listingPanel.listing = listingPanel.listing[:0]
	listingPanel.recenterListing = true

	listingPanel.stepIntoInfo.Filename = ""
	listingPanel.stepIntoInfo.Lineno = -1
	listingPanel.stepIntoInfo.Colno = -1
	listingPanel.stepIntoInfo.Valid = false

	if loc == nil {
		listingPanel.file = ""
		listingPanel.abbrevFile = ""
		return
	}

	listingPanel.file = loc.File
	listingPanel.abbrevFile = abbrevFileName(loc.File)

	if loc.File == "<autogenerated>" {
		return
	}

	fh, err := os.Open(conf.substitutePath(loc.File))
	if err != nil {
		failstate("Open()", err)
		return
	}
	defer fh.Close()

	fi, _ := fh.Stat()
	listingPanel.stale = fi.ModTime().After(lastModExe)

	listingPanel.optimized = false
	if loc.Function != nil && loc.Function.Optimized {
		listingPanel.optimized = true
	}

	buf := bufio.NewScanner(fh)
	lineno := 0
	for buf.Scan() {
		lineno++
		atpc := lineno == loc.Line && listingPanel.pinnedLoc == nil
		linetext := expandTabs(buf.Text())
		listingPanel.listing = append(listingPanel.listing, listline{"", lineno, linetext, buf.Text(), atpc, nil, false})
	}

	const maxFontCacheSize = 500000
	sz := 4*len(listingPanel.listing) + len(listingPanel.listing)/2
	if sz > maxFontCacheSize {
		sz = maxFontCacheSize
	}
	nucular.ChangeFontWidthCache(sz)

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

func applyBreakpoints(failstate func(string, error)) {
	breakpoints, err := client.ListBreakpoints()
	if err != nil {
		failstate("ListBreakpoints()", err)
		return
	}

	bpmap := map[int]anyBreakpoint{}
	for _, bp := range breakpoints {
		if bp.File == listingPanel.file {
			bpmap[bp.Line] = anyBreakpoint{bp, true}
		}
	}

	for _, fbp := range DisabledBreakpoints {
		if fbp.Bp.File == listingPanel.file {
			bpmap[fbp.Bp.Line] = anyBreakpoint{&fbp.Bp, false}
		}
	}

	for i := range listingPanel.listing {
		b := bpmap[listingPanel.listing[i].lineno]
		listingPanel.listing[i].bp = b.Breakpoint
		listingPanel.listing[i].bpenabled = b.enabled
	}
}

func currentLocation(state *api.DebuggerState) *api.Location {
	if state.SelectedGoroutine != nil {
		if state.CurrentThread != nil && state.SelectedGoroutine.ThreadID == state.CurrentThread.ID {
			return &api.Location{File: state.CurrentThread.File, Line: state.CurrentThread.Line, PC: state.CurrentThread.PC, Function: state.CurrentThread.Function}
		} else {
			return &state.SelectedGoroutine.CurrentLoc
		}
	} else if state.CurrentThread != nil {
		return &api.Location{File: state.CurrentThread.File, Line: state.CurrentThread.Line, PC: state.CurrentThread.PC, Function: state.CurrentThread.Function}
	}

	return nil
}

func currentEvalScope() api.EvalScope {
	return api.EvalScope{curGid, curFrame, curDeferredCall}
}

func usage(err string) {
	if err != "" {
		if err[len(err)-1] != '\n' {
			err += "\n"
		}
		fmt.Fprintf(os.Stderr, err)
	}
	fmt.Fprintf(os.Stderr, `Usage:
	gdlv [options] connect <address>
	gdlv [options] debug <program's arguments...>
	gdlv [options] run <program file> <program's arguments...>
	gdlv [options] exec <executable> <program's arguments...>
	gdlv [options] test <testflags...>
	gdlv [options] attach <pid> [path to executable]
	gdlv [options] core <executable> <core file>
	gdlv [options] replay <trace directory>
	
All commands except "core" and "replay" can be prefixed with the name of a backend, for example:

	gdlv rr:run <program file> <program's arguments...>
	
Executes "gdlv run" using mozilla rr has a backend.

Options must appear before the command and include:

	-d <dir>	builds inside the specified directory instead of the current directory (for debug and test)
	-tags <taglist>	list of tags to pass to 'go build'
`)
	os.Exit(1)
}

func parseOptions(args []string) commandLineOptions {
	var opts commandLineOptions

	i := 1

optionsLoop:
	for i < len(args) {
		switch args[i] {
		case "-d":
			i++
			if i >= len(args) {
				usage("wrong number of arguments after -d")
			}
			opts.buildDir = args[i]
			i++
		case "-tags":
			i++
			if i >= len(args) {
				usage("wrong number of arguments after -tags")
			}
			opts.tags = args[i]
			i++
		default:
			break optionsLoop
		}
	}

	if i >= len(args) {
		usage("wrong number of arguments, expected a command")
	}

	opts.cmd = args[i]
	opts.cmdArgs = args[i+1:]

	opts.defaultBackend = true
	const defaultBackend = "--backend=default"
	opts.backend = defaultBackend
	if colon := strings.Index(opts.cmd, ":"); colon >= 0 {
		if opts.cmd[:colon] == "rr" {
			opts.defaultBackend = false
		}
		opts.backend = "--backend=" + opts.cmd[:colon]
		opts.cmd = opts.cmd[colon+1:]
	}

	return opts
}

type commandLineOptions struct {
	cmd            string
	cmdArgs        []string
	backend        string
	defaultBackend bool
	buildDir       string
	tags           string
}

func main() {
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" {
		fmt.Fprintf(os.Stderr, "DISPLAY not set\n")
		os.Exit(1)
	}

	loadConfiguration()

	if profileEnabled {
		if f, err := os.Create("cpu.pprof"); err == nil {
			if err := pprof.StartCPUProfile(f); err == nil {
				defer pprof.StopCPUProfile()
			}
		}
	}

	checkCompatibility()

	setupLog()
	if LogOutputNice != nil {
		defer LogOutputNice.Close()
	}
	if LogOutputRpc != nil {
		defer LogOutputRpc.Close()
	}

	logf("Command line: %s", strings.Join(os.Args, " "))

	BackendServer = parseArguments()

	if BackendServer.debugid != "" && conf.FrozenBreakpoints != nil && conf.DisabledBreakpoints != nil {
		FrozenBreakpoints = append(FrozenBreakpoints[:0], conf.FrozenBreakpoints[BackendServer.debugid]...)
		DisabledBreakpoints = append(DisabledBreakpoints[:0], conf.DisabledBreakpoints[BackendServer.debugid]...)
	}

	loadPanelDescrToplevel(conf.Layouts["default"].Layout)

	curThread = -1
	curGid = -1

	commandLineEditor.Flags = nucular.EditSelectable | nucular.EditSigEnter | nucular.EditClipboard
	commandLineEditor.Active = true

	var scrollbackOut = editorWriter{true}

	fmt.Fprintf(&scrollbackOut, `gdlv  Copyright (C) 2016-2020 Gdlv Authors
This program comes with ABSOLUTELY NO WARRANTY;
This is free software, and you are welcome to redistribute it
under certain conditions; see COPYING for details.

To change font size use Ctrl-plus/Ctrl-minus or the 'config zoom' command.


`)

	cmds = DebugCommands()

	executeInit()

	go BackendServer.Start()

	wnd.Main()

	BackendServer.Close()
}
