// Copyright 2016, Gdlv Authors

package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"math"
	"os"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aarzilli/gdlv/internal/assets"
	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/dlvclient/service/rpc2"
	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/rect"
	nstyle "github.com/aarzilli/nucular/style"
	"github.com/golang/freetype"
	"github.com/golang/freetype/truetype"

	"golang.org/x/image/font"
	"golang.org/x/mobile/event/key"
)

//go:generate go-bindata -o internal/assets/assets.go -pkg assets fontawesome-webfont.ttf droid-sans.bold.ttf

const profileEnabled = false

var zeroWidth, arrowWidth, starWidth, spaceWidth int

var fontInit sync.Once
var iconTtfont *truetype.Font
var iconFace font.Face
var boldTtfont, normalTtfont *truetype.Font
var boldFace font.Face

const (
	arrowIconChar      = "\uf061"
	breakpointIconChar = "\uf28d"

	interruptIconChar = "\uf04c"
	continueIconChar  = "\uf04b"
	cancelIconChar    = "\uf05e"
	nextIconChar      = "\uf050"
	stepIconChar      = "\uf051"
	stepoutIconChar   = "\uf112"

	splitIconChar = "\uf0db"
)

func setupStyle() {
	switch conf.Theme {
	default:
		fallthrough
	case boringTheme:
		style := makeBoringStyle()
		style.Scale(conf.Scaling)
		wnd.SetStyle(style)
	case darkTheme:
		wnd.SetStyle(nstyle.FromTheme(nstyle.DarkTheme, conf.Scaling))
	case whiteTheme:
		wnd.SetStyle(nstyle.FromTheme(nstyle.WhiteTheme, conf.Scaling))
	case redTheme:
		wnd.SetStyle(nstyle.FromTable(redThemeTable, conf.Scaling))
	}

	fontInit.Do(func() {
		iconFontData, _ := assets.Asset("fontawesome-webfont.ttf")
		iconTtfont, _ = freetype.ParseFont(iconFontData)

		normalFontPath := os.Getenv("GDLV_NORMAL_FONT")
		boldFontPath := os.Getenv("GDLV_BOLD_FONT")
		customFonts := false

		if normalFontPath != "" && boldFontPath != "" {
			_, normerr := os.Stat(normalFontPath)
			_, bolderr := os.Stat(boldFontPath)
			if normerr == nil && bolderr == nil {
				normalFontData, normerr := ioutil.ReadFile(normalFontPath)
				boldFontData, bolderr := ioutil.ReadFile(boldFontPath)
				if normerr == nil && bolderr == nil {
					normalTtfont, normerr = freetype.ParseFont(normalFontData)
					boldTtfont, bolderr = freetype.ParseFont(boldFontData)
					if normerr == nil && bolderr == nil {
						customFonts = true
					}
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
			boldFontData, _ := assets.Asset("droid-sans.bold.ttf")
			boldTtfont, _ = freetype.ParseFont(boldFontData)
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
	iconFace = truetype.NewFace(iconTtfont, &truetype.Options{Size: float64(sz), Hinting: font.HintingFull, DPI: 72})
	boldFace = truetype.NewFace(boldTtfont, &truetype.Options{Size: float64(sz), Hinting: font.HintingFull, DPI: 72})
	if normalTtfont != nil {
		style.Font = truetype.NewFace(normalTtfont, &truetype.Options{Size: float64(sz), Hinting: font.HintingFull, DPI: 72})
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

var silenced bool
var scrollbackEditor, commandLineEditor nucular.TextEditor

var delayFrame bool
var frameCount int

var LogOutput io.WriteCloser

func guiUpdate(w *nucular.Window) {
	df := delayFrame
	delayFrame = false

	if df {
		time.Sleep(50 * time.Millisecond)
	}

	var scrollbackOut = editorWriter{&scrollbackEditor, false}
	mw := w.Master()

	for _, e := range wnd.Input().Keyboard.Keys {
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
			if client.Running() && client != nil {
				_, err := client.Halt()
				if err != nil {
					fmt.Fprintf(&scrollbackOut, "Request manual stop failed: %v\n", err)
				}
				err = client.CancelNext()
				if err != nil {
					fmt.Fprintf(&scrollbackOut, "Could not cancel next operation: %v\n", err)
				}
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
		if curThread < 0 {
			return "dlv>"
		} else if curGid < 0 {
			return fmt.Sprintf("thread %d:%d>", curThread, curFrame)
		} else if curDeferredCall > 0 {
			return fmt.Sprintf("deferred call %d:%d:%d>", curGid, curFrame, curDeferredCall)
		} else {
			return fmt.Sprintf("goroutine %d:%d>", curGid, curFrame)
		}
	}
}

func updateCommandPanel(w *nucular.Window) {
	style := w.Master().Style()

	w.Row(headerRow).Static()
	w.LayoutReserveRow(commandLineHeight, 1)
	commandToolbar(w)

	w.Row(0).Dynamic(1)
	scrollbackEditor.Edit(w)

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
		var scrollbackOut = editorWriter{&scrollbackEditor, false}
		cmd := string(commandLineEditor.Buffer)
		if canExecuteCmd(cmd) && !client.Running() {
			if cmd == "" {
				fmt.Fprintf(&scrollbackOut, "%s %s\n", p, cmdhistory[len(cmdhistory)-1])
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

	var scrollbackOut = editorWriter{&scrollbackEditor, false}

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
			frames, err := client.Stacktrace(curGid, curFrame+1, true, nil)
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
			frames, err := client.Stacktrace(curGid, 20, false, nil)
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
		wnd.Lock()
		defer wnd.Unlock()
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
	fmt.Fprintf(os.Stderr, `Usage:
	gdlv connect <address>
	gdlv debug <program's arguments...>
	gdlv run <program file> <program's arguments...>
	gdlv exec <executable> <program's arguments...>
	gdlv test <testflags...>
	gdlv attach <pid> [path to executable]
	gdlv core <executable> <core file>
	gdlv replay <trace directory>
	
All commands except "core" and "replay" can be prefixed with the name of a backend, for example:

	gdlv rr:run <program file> <program's arguments...>
	
Executes "gdlv run" using mozilla rr has a backend.
`)
	os.Exit(1)
}

func replacepid(in string) string {
	return strings.Replace(in, "%p", strconv.Itoa(os.Getpid()), -1)
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

	if os.Getenv("GDLVLOG") != "" {
		logfile := replacepid(os.Getenv("GDLVLOG"))
		fh, err := os.Create(logfile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error opening log %v\n", err)
		} else {
			LogOutput = fh
			defer func() {
				LogOutput.Close()
				os.Remove(logfile)
			}()
		}
	}

	BackendServer = parseArguments()

	if BackendServer.debugid != "" && conf.FrozenBreakpoints != nil && conf.DisabledBreakpoints != nil {
		FrozenBreakpoints = append(FrozenBreakpoints[:0], conf.FrozenBreakpoints[BackendServer.debugid]...)
		DisabledBreakpoints = append(DisabledBreakpoints[:0], conf.DisabledBreakpoints[BackendServer.debugid]...)
	}

	loadPanelDescrToplevel(conf.Layouts["default"].Layout)

	curThread = -1
	curGid = -1

	scrollbackEditor.Flags = nucular.EditSelectable | nucular.EditReadOnly | nucular.EditMultiline | nucular.EditClipboard
	commandLineEditor.Flags = nucular.EditSelectable | nucular.EditSigEnter | nucular.EditClipboard
	commandLineEditor.Active = true

	var scrollbackOut = editorWriter{&scrollbackEditor, true}

	fmt.Fprintf(&scrollbackOut, `gdlv  Copyright (C) 2016-2017 Gdlv Authors
This program comes with ABSOLUTELY NO WARRANTY;
This is free software, and you are welcome to redistribute it
under certain conditions; see COPYING for details.
`)

	cmds = DebugCommands()

	go BackendServer.Start()

	wnd.Main()

	BackendServer.Close()
}
