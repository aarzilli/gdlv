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

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/label"
	nstyle "github.com/aarzilli/nucular/style"

	"github.com/derekparker/delve/service"
	"github.com/derekparker/delve/service/api"
	"github.com/derekparker/delve/service/rpc2"

	"golang.org/x/mobile/event/key"
)

func fixStyle(style *nstyle.Style) {
	style.Selectable.Normal.Data.Color = style.NormalWindow.Background
	style.NormalWindow.Padding.Y = 0
	style.GroupWindow.Padding.Y = 0
	style.GroupWindow.FooterPadding.Y = 0
	style.MenuWindow.FooterPadding.Y = 0
	style.ContextualWindow.FooterPadding.Y = 0
}

var rightColWidth int = 200
var scrollbackHeight int = 200

const commandLineHeight = 28

type listingPanel struct {
	mode     int
	showcur  bool
	path     string
	recenter bool
	listing  []listline
	text     api.AsmInstructions
}

type listline struct {
	idx        string
	text       string
	pc         bool
	breakpoint bool
}

var mu sync.Mutex
var wnd *nucular.MasterWindow
var lp listingPanel
var running bool
var client service.Client

var curThread int
var curGid int
var curFrame int

var rightcolModes = []string{"Goroutines and Stack", "Stack and Locals", "Threads and Locals", "Threads and Registers", "Globals", "Breakpoints", "Sources", "Functions", "Types"}
var rightcolMode int = 1

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
			mw.SetStyle(nstyle.FromTheme(nstyle.DarkTheme), nil, conf.Scaling)
			style, _ := mw.Style()
			fixStyle(style)
			saveConfiguration()

		case (e.Modifiers == key.ModControl || e.Modifiers == key.ModControl|key.ModShift) && (e.Rune == '-'):
			conf.Scaling -= 0.1
			mw.SetStyle(nstyle.FromTheme(nstyle.DarkTheme), nil, conf.Scaling)
			style, _ := mw.Style()
			fixStyle(style)
			saveConfiguration()

		case (e.Modifiers == key.ModControl) && (e.Code == key.CodeW):
			go mw.Close()
		}
	}

	style, scaling := mw.Style()
	_, _ = style, scaling

	w.Row(0).Static(0, 1, rightColWidth)

	// LEFT COLUMN

	if leftcol := w.GroupBegin("left-column", nucular.WindowNoScrollbar); leftcol != nil {

		leftcol.Row(25).Static(200, 0)
		modes := []string{"Listing", "Disassembly"}
		if !lp.showcur {
			modes = []string{"Listing"}
		}

		item_height := int(25 * scaling)
		item_padding := style.Combo.ButtonPadding.Y
		window_padding := style.ComboWindow.Padding.Y
		max_height := (len(modes)+1)*item_height + item_padding*3 + window_padding*2
		leftcol.Combo(label.T(modes[lp.mode]), max_height, func(mw *nucular.MasterWindow, w *nucular.Window) {
			w.RowScaled(item_height).Dynamic(1)
			for i := range modes {
				if w.MenuItem(label.TA(modes[i], "LC")) {
					lp.mode = i
					go refreshState(true, nil)
				}
			}
		})

		if !lp.showcur {
			leftcol.Label(lp.path, "LC")
		} else {
			leftcol.Label(prompt(curThread, curGid, curFrame), "LC")
		}

		leftcol.LayoutReserveRow(1, 1)
		leftcol.LayoutReserveRow(scrollbackHeight, 1)
		leftcol.LayoutReserveRowScaled(int(commandLineHeight*scaling), 1)

		leftcol.Row(0).Dynamic(1)

		if listp := leftcol.GroupBegin("list-panel", nucular.WindowNoHScrollbar|nucular.WindowBorder); listp != nil {
			lp.show(mw, listp)
			listp.GroupEnd()
		}

		leftcol.Row(1).Dynamic(1)
		leftcol.Spacing(1)
		// TODO: make this a resize handle

		leftcol.Row(scrollbackHeight).Dynamic(1)
		scrollbackEditor.Edit(leftcol)

		var p string
		if curThread < 0 {
			if running {
				p = "running"
			} else if client == nil {
				p = "connecting"
			} else {
				p = "dlv>"
			}
		} else {
			p = prompt(curThread, curGid, curFrame) + ">"
		}
		promptwidth := nucular.FontWidth(style.Font, p) + style.Text.Padding.X*2

		leftcol.Row(commandLineHeight).StaticScaled(promptwidth, 0)
		leftcol.Label(p, "LC")

		if client == nil || running {
			w.Label(" ", "LC")
		} else {
			active := commandLineEditor.Edit(leftcol)
			if active&nucular.EditCommitted != 0 {
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
				commandLineEditor.Active = true
			}
		}

		leftcol.GroupEnd()
	}

	// SPACING
	w.Spacing(1)
	// TODO: make this a resize handle

	// RIGHT COLUMN

	if rightcol := w.GroupBegin("right-column", nucular.WindowNoScrollbar|nucular.WindowBorder); rightcol != nil {
		//TODO: not implemented
		rightcol.Row(25).Static(180, 0)
		rightcol.ComboSimple(rightcolModes, &rightcolMode, 22)
		rightcol.Spacing(1)
		rightcol.Row(25).Dynamic(1)
		rightcol.Label("Not implemented", "LC")
		rightcol.GroupEnd()
	}
}

func (lp *listingPanel) show(mw *nucular.MasterWindow, listp *nucular.Window) {
	const lineheight = 14
	style, _ := mw.Style()

	arroww := nucular.FontWidth(style.Font, "=>") + style.Text.Padding.X*2
	starw := nucular.FontWidth(style.Font, "*") + style.Text.Padding.X*2

	switch lp.mode {
	case 0:
		idxw := style.Text.Padding.X * 2
		if len(lp.listing) > 0 {
			idxw += nucular.FontWidth(style.Font, lp.listing[len(lp.listing)-1].idx)
		}

		listp.Row(lineheight).StaticScaled(starw, arroww, idxw, 0)

		for _, line := range lp.listing {
			if line.breakpoint {
				listp.Label("*", "CC")
			} else {
				listp.Spacing(1)
			}

			if line.pc {
				if lp.recenter {
					lp.recenter = false
					if above, below := listp.Invisible(); above || below {
						listp.Scrollbar.Y = listp.At().Y - listp.Bounds.H/2
						if listp.Scrollbar.Y < 0 {
							listp.Scrollbar.Y = 0
						}
					}
				}
				listp.Label("=>", "CC")
			} else {
				listp.Spacing(1)
			}
			listp.Label(line.idx, "LC")
			listp.Label(line.text, "LC")
		}

	case 1:
		var maxaddr uint64 = 0
		if len(lp.text) > 0 {
			maxaddr = lp.text[len(lp.text)-1].Loc.PC
		}
		addrw := nucular.FontWidth(style.Font, fmt.Sprintf("%#x", maxaddr)) + style.Text.Padding.X*2

		lastfile, lastlineno := "", 0

		if len(lp.text) > 0 && lp.text[0].Loc.Function != nil {
			listp.Row(lineheight).Dynamic(1)
			listp.Label(fmt.Sprintf("TEXT %s(SB) %s", lp.text[0].Loc.Function.Name, lp.text[0].Loc.File), "LC")
		}

		for _, instr := range lp.text {
			if instr.Loc.File != lastfile || instr.Loc.Line != lastlineno {
				listp.Row(lineheight).Dynamic(1)
				listp.Label(fmt.Sprintf("%s:%d:", instr.Loc.File, instr.Loc.Line), "LC")
				lastfile, lastlineno = instr.Loc.File, instr.Loc.Line
			}
			listp.Row(lineheight).StaticScaled(starw, arroww, addrw, 0)
			if instr.Breakpoint {
				listp.Label("*", "LC")
			} else {
				listp.Label(" ", "LC")
			}

			if instr.AtPC {
				if lp.recenter {
					lp.recenter = false
					if above, below := listp.Invisible(); above || below {
						listp.Scrollbar.Y = listp.At().Y - listp.Bounds.H/2
						if listp.Scrollbar.Y < 0 {
							listp.Scrollbar.Y = 0
						}
					}
				}
				listp.Label("=>", "LC")
			} else {
				listp.Label(" ", "LC")
			}

			listp.Label(fmt.Sprintf("%#x", instr.Loc.PC), "LC")
			listp.Label(instr.Text, "LC")
		}
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

	refreshState(false, nil)
}

func digits(n int) int {
	if n <= 0 {
		return 1
	}
	return int(math.Floor(math.Log10(float64(n)))) + 1
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

func refreshState(keepframe bool, state *api.DebuggerState) {
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
	lp.listing = lp.listing[:0]
	lp.text = nil
	lp.recenter = true
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
		if state.CurrentThread != nil {
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

	if loc != nil {
		switch lp.mode {
		case 0:
			breakpoints, err := client.ListBreakpoints()
			if err != nil {
				failstate("ListBreakpoints()", err)
				return
			}
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
				lp.listing = append(lp.listing, listline{"", expandTabs(buf.Text()), lineno == state.CurrentThread.Line, breakpoint})
			}

			if err := buf.Err(); err != nil {
				failstate("(reading file)", err)
				return
			}

			d := digits(len(lp.listing))
			if d < 3 {
				d = 3
			}
			for i := range lp.listing {
				lp.listing[i].idx = fmt.Sprintf("%*d", d, i)
			}

		case 1:
			text, err := client.DisassemblePC(api.EvalScope{curGid, curFrame}, loc.PC, api.IntelFlavour)
			if err != nil {
				failstate("DisassemblePC()", err)
				return
			}

			lp.text = text
		}
	}
}

type editorWriter struct {
	ed   *nucular.TextEditor
	lock bool
}

func (w *editorWriter) Write(b []byte) (int, error) {
	if w.lock {
		mu.Lock()
		defer mu.Unlock()
	}
	atend := w.ed.Cursor == len(w.ed.Buffer) || w.ed.Cursor == len(w.ed.Buffer)-1
	w.ed.Buffer = append(w.ed.Buffer, []rune(expandTabs(string(b)))...)
	if atend {
		w.ed.Cursor = len(w.ed.Buffer)
		if b[len(b)-1] == '\n' {
			w.ed.Cursor--
		}
		w.ed.CursorFollow = true
		w.ed.Redraw = true
	}
	return len(b), nil
}

func main() {
	loadConfiguration()

	wnd = nucular.NewMasterWindow(guiUpdate, nucular.WindowNoScrollbar)
	wnd.SetStyle(nstyle.FromTheme(nstyle.DarkTheme), nil, conf.Scaling)
	style, _ := wnd.Style()
	fixStyle(style)

	lp.showcur = true
	curThread = -1
	curGid = -1

	scrollbackEditor.Flags = nucular.EditSelectable | nucular.EditReadOnly | nucular.EditMultiline
	commandLineEditor.Flags = nucular.EditSelectable | nucular.EditSigEnter | nucular.EditClipboard

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
			first := true
			scan := bufio.NewScanner(stdout)
			for scan.Scan() {
				if first {
					connectTo(scan.Text())
					first = false
				} else {
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
