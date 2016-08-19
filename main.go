package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/aarzilli/nucular"
	nstyle "github.com/aarzilli/nucular/style"

	"github.com/derekparker/delve/service"
	"github.com/derekparker/delve/service/api"
	"github.com/derekparker/delve/service/rpc2"

	"golang.org/x/mobile/event/key"
)

func setupStyle() {
	theme := nstyle.DarkTheme
	if conf.WhiteTheme {
		theme = nstyle.WhiteTheme
	}
	wnd.SetStyle(nstyle.FromTheme(theme), nil, conf.Scaling)
	style, _ := wnd.Style()
	style.Selectable.Normal.Data.Color = style.NormalWindow.Background
	style.GroupWindow.Padding.Y = 0
	style.GroupWindow.FooterPadding.Y = 0
	style.MenuWindow.FooterPadding.Y = 0
	style.ContextualWindow.FooterPadding.Y = 0
	saveConfiguration()
}

const commandLineHeight = 28

type listline struct {
	idx        string
	text       string
	pc         bool
	breakpoint bool
}

var listingPanel struct {
	file                string
	path                string
	recenterListing     bool
	recenterDisassembly bool
	listing             []listline
	text                api.AsmInstructions
}

var mu sync.Mutex
var wnd *nucular.MasterWindow

var running bool
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
		return fmt.Sprintf("thread %d frame %d", thread, frame)
	}
	return fmt.Sprintf("goroutine %d frame %d", gid, frame)
}

func guiUpdate(mw *nucular.MasterWindow, w *nucular.Window) {
	mu.Lock()
	defer mu.Unlock()

	var scrollbackOut = editorWriter{&scrollbackEditor, false}

	for _, e := range w.Input().Keyboard.Keys {
		switch {
		case (e.Modifiers == key.ModControl || e.Modifiers == key.ModControl|key.ModShift) && (e.Rune == '+') || (e.Rune == '='):
			conf.Scaling += 0.1
			setupStyle()

		case (e.Modifiers == key.ModControl || e.Modifiers == key.ModControl|key.ModShift) && (e.Rune == '-'):
			conf.Scaling -= 0.1
			setupStyle()

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

	rootPanel.update(mw, w)
}

func updateCommandPanel(mw *nucular.MasterWindow, container *nucular.Window) {
	style, _ := mw.Style()

	w := container.GroupBegin("command", nucular.WindowNoScrollbar)
	if w == nil {
		return
	}
	defer w.GroupEnd()

	w.LayoutReserveRow(commandLineHeight, 1)
	w.Row(0).Dynamic(1)
	scrollbackEditor.Edit(w)

	var p string
	if running {
		p = "running"
	} else if client == nil {
		p = "connecting"
	} else {
		if curThread < 0 {
			p = "dlv>"
		} else {
			p = prompt(curThread, curGid, curFrame) + ">"
		}
	}

	promptwidth := nucular.FontWidth(style.Font, p) + style.Text.Padding.X*2

	w.Row(commandLineHeight).StaticScaled(promptwidth, 0)
	w.Label(p, "LC")

	if client == nil || running {
		commandLineEditor.Flags |= nucular.EditReadOnly
	} else {
		commandLineEditor.Flags &= ^nucular.EditReadOnly
	}
	for _, k := range w.Input().Keyboard.Keys {
		if k.Modifiers == 0 && k.Code == key.CodeTab {
			completeAny()
		}
	}
	active := commandLineEditor.Edit(w)
	if active&nucular.EditCommitted != 0 {
		var scrollbackOut = editorWriter{&scrollbackEditor, false}

		cmd := string(commandLineEditor.Buffer)
		if cmd == "" {
			fmt.Fprintf(&scrollbackOut, "%s %s\n", p, lastCmd)
		} else {
			lastCmd = cmd
			fmt.Fprintf(&scrollbackOut, "%s %s\n", p, cmd)
		}
		go executeCommand(cmd)
		commandLineEditor.Buffer = commandLineEditor.Buffer[:0]
		commandLineEditor.Cursor = 0
		commandLineEditor.CursorFollow = true
		commandLineEditor.Active = true
	}
}

func connectTo(listenstr string) {
	var scrollbackOut = editorWriter{&scrollbackEditor, false}

	const prefix = "API server listening at: "
	if !strings.HasPrefix(listenstr, prefix) {
		mu.Lock()
		fmt.Fprintf(&scrollbackOut, "Could not parse connection string: %q\n", listenstr)
		mu.Unlock()
		return
	}

	addr := listenstr[len(prefix):]
	func() {
		mu.Lock()
		defer mu.Unlock()

		client = rpc2.NewClient(addr)
		if client == nil {
			fmt.Fprintf(&scrollbackOut, "Could not connect\n")
		}

		cmds = DebugCommands(client)
	}()

	mu.Lock()
	running = true
	fmt.Fprintf(&scrollbackOut, "Loading program info...")

	var err error
	funcsPanel.slice, err = client.ListFunctions("")
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not list functions: %v\n", err)
	}

	sourcesPanel.slice, err = client.ListSources("")
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not list sources: %v\n", err)
	}

	typesPanel.slice, err = client.ListTypes("")
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not list types: %v\n", err)
	}

	completeLocationSetup()

	fmt.Fprintf(&scrollbackOut, "done\n")
	running = false
	mu.Unlock()

	refreshState(false, clearStop, nil)
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
		if c == '\t' {
			d := (((count/8)+1)*8 - count)
			for i := 0; i < d; i++ {
				buf.WriteRune(' ')
			}
		} else {
			buf.WriteRune(c)
			count++
		}
	}
	return buf.String()
}

type clearKind int

const (
	clearFrameSwitch clearKind = iota
	clearGoroutineSwitch
	clearStop
)

func refreshState(keepframe bool, clearKind clearKind, state *api.DebuggerState) {
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
		curFrame = 0
	}
	var loc *api.Location
	if !keepframe {
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
	} else {
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
	}

	switch clearKind {
	case clearFrameSwitch:
		localsPanel.asyncLoad.clear()
	case clearGoroutineSwitch:
		stackPanel.asyncLoad.clear()
		localsPanel.asyncLoad.clear()
		regsPanel.asyncLoad.clear()
	case clearStop:
		localsPanel.asyncLoad.clear()
		regsPanel.asyncLoad.clear()
		goroutinesPanel.asyncLoad.clear()
		stackPanel.asyncLoad.clear()
		threadsPanel.asyncLoad.clear()
		globalsPanel.asyncLoad.clear()
		breakpointsPanel.asyncLoad.clear()
	}

	if loc != nil {
		text, err := client.DisassemblePC(api.EvalScope{curGid, curFrame}, loc.PC, api.IntelFlavour)
		if err != nil {
			failstate("DisassemblePC()", err)
			return
		}

		listingPanel.text = text

		breakpoints, err := client.ListBreakpoints()
		if err != nil {
			failstate("ListBreakpoints()", err)
			return
		}
		listingPanel.file = loc.File
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

		buf := bufio.NewScanner(fh)
		lineno := 0
		for buf.Scan() {
			lineno++
			_, breakpoint := bpmap[lineno]
			listingPanel.listing = append(listingPanel.listing, listline{"", expandTabs(buf.Text()), lineno == loc.Line, breakpoint})
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

func main() {
	loadConfiguration()

	wnd = nucular.NewMasterWindow(guiUpdate, nucular.WindowNoScrollbar)
	setupStyle()

	rootPanel, _ = parsePanelDescr(conf.Layouts["default"].Layout, nil)

	curThread = -1
	curGid = -1

	scrollbackEditor.Flags = nucular.EditSelectable | nucular.EditReadOnly | nucular.EditMultiline
	commandLineEditor.Flags = nucular.EditSelectable | nucular.EditSigEnter | nucular.EditClipboard
	commandLineEditor.Active = true

	args := []string{"--headless"}
	args = append(args, os.Args[1:]...)

	cmd := exec.Command("dlv", args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	err := cmd.Start()

	var scrollbackOut = editorWriter{&scrollbackEditor, true}

	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not start delve: %v\n", err)
	} else {
		go func() {
			bucket := 0
			t0 := time.Now()
			first := true
			scan := bufio.NewScanner(stdout)
			for scan.Scan() {
				if first {
					connectTo(scan.Text())
					first = false
				} else {
					mu.Lock()
					if silenced {
						mu.Unlock()
						continue
					}
					mu.Unlock()
					now := time.Now()
					if now.Sub(t0) > 500*time.Millisecond {
						t0 = now
						bucket = 0
					}
					bucket += len(scan.Text())
					if bucket > scrollbackLowMark {
						mu.Lock()
						silenced = true
						mu.Unlock()
						fmt.Fprintf(&scrollbackOut, "too much output in 500ms (%d), output silenced\n", bucket)
						bucket = 0
						continue
					}
					fmt.Fprintln(&scrollbackOut, scan.Text())
				}
			}
			if err := scan.Err(); err != nil {
				fmt.Fprintf(&scrollbackOut, "Error reading stdout: %v\n", err)
			}
		}()

		go func() {
			_, err := io.Copy(&scrollbackOut, stderr)
			if err != nil {
				fmt.Fprintf(&scrollbackOut, "Error reading stderr: %v\n", err)
			}
		}()
	}

	wnd.Main()
}
