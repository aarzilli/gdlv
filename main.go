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

const commandLineHeight = 20

type listingPanel struct {
	mode     int
	showcur  bool
	path     string
	recenter bool
	listing  []listline
	text     api.AsmInstructions
}

type listline struct {
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

var rightcolModes = []string{"Goroutines and Stack", "Stack and Locals", "Threads and Locals", "Breakpoints", "Sources", "Functions", "Types"}
var rightcolMode int = 1

var scrollbackEditor, commandLineEditor nucular.TextEditor
var out = editorWriter{&scrollbackEditor}

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

	w.LayoutRowStatic(0, 0, 1, rightColWidth)

	// LEFT COLUMN

	if leftcol := w.GroupBegin("left-column", nucular.WindowNoScrollbar); leftcol != nil {

		leftcol.LayoutRowStatic(20, 200, 0)
		modes := []string{"Listing", "Disassembly"}
		if !lp.showcur {
			modes = []string{"Listing"}
		}

		item_height := int(18 * scaling)
		item_padding := style.Combo.ButtonPadding.Y
		window_padding := style.ComboWindow.Padding.Y
		max_height := (len(modes)+1)*item_height + item_padding*3 + window_padding*2
		leftcol.Combo(label.T(modes[lp.mode]), max_height, func(mw *nucular.MasterWindow, w *nucular.Window) {
			w.LayoutRowDynamicScaled(item_height, 1)
			for i := range modes {
				if w.MenuItem(label.TA(modes[i], "LC")) {
					lp.mode = i
					go refreshState(true)
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

		leftcol.LayoutRowDynamic(0, 1)

		if listp := leftcol.GroupBegin("list-panel", nucular.WindowNoHScrollbar|nucular.WindowBorder); listp != nil {
			lp.show(mw, listp)
			listp.GroupEnd()
		}

		leftcol.LayoutRowDynamic(1, 1)
		leftcol.Spacing(1)
		// TODO: make this a resize handle

		leftcol.LayoutRowDynamic(scrollbackHeight, 1)
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

		leftcol.LayoutRowStaticScaled(int(commandLineHeight*scaling), promptwidth, 0)
		leftcol.Label(p, "LC")

		if client == nil || running {
			w.Label(" ", "LC")
		} else {
			active := commandLineEditor.Edit(leftcol)
			if active&nucular.EditCommitted != 0 {
				cmd := string(commandLineEditor.Buffer)
				fmt.Fprintf(&out, "%s %s\n", p, cmd)
				//TODO: execute commands
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
		rightcol.LayoutRowStatic(20, 180, 0)
		rightcol.ComboSimple(rightcolModes, &rightcolMode, 22)
		rightcol.Spacing(1)
		rightcol.LayoutRowDynamic(20, 1)
		rightcol.Label("Not implemented", "LC")
		rightcol.GroupEnd()
	}
}

func (lp *listingPanel) show(mw *nucular.MasterWindow, listp *nucular.Window) {
	const lineheight = 14
	style, scaling := mw.Style()

	arroww := nucular.FontWidth(style.Font, "=>") + style.Text.Padding.X*2
	starw := nucular.FontWidth(style.Font, "*") + style.Text.Padding.X*2

	switch lp.mode {
	case 0:
		zerow := nucular.FontWidth(style.Font, "0") + style.Text.Padding.X*2

		d := digits(len(lp.listing))
		if d < 3 {
			d = 3
		}

		listp.LayoutRowStaticScaled(int(lineheight*scaling), starw, arroww, zerow*(d+1), 0)

		for i, line := range lp.listing {
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
			listp.Label(fmt.Sprintf("%*d", d, i+1), "LC")
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
			listp.LayoutRowDynamicScaled(int(lineheight*scaling), 1)
			listp.Label(fmt.Sprintf("TEXT %s(SB) %s", lp.text[0].Loc.Function.Name, lp.text[0].Loc.File), "LC")
		}

		for _, instr := range lp.text {
			if instr.Loc.File != lastfile || instr.Loc.Line != lastlineno {
				listp.LayoutRowDynamicScaled(int(lineheight*scaling), 1)
				listp.Label(fmt.Sprintf("%s:%d:", instr.Loc.File, instr.Loc.Line), "LC")
				lastfile, lastlineno = instr.Loc.File, instr.Loc.Line
			}
			listp.LayoutRowStaticScaled(int(lineheight*scaling), starw, arroww, addrw, 0)
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
	const prefix = "API server listening at: "
	if !strings.HasPrefix(listenstr, prefix) {
		fmt.Fprintf(&out, "Could not parse connection string: %q\n", listenstr)
		return
	}

	addr := listenstr[len(prefix):]
	func() {
		mu.Lock()
		defer mu.Unlock()

		client = rpc2.NewClient(addr)
		if client == nil {
			fmt.Fprintf(&out, "Could not connect\n")
		}
	}()

	refreshState(false)
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

func refreshState(keepframe bool) {
	defer wnd.Changed()

	failstate := func(pos string, err error) {
		curThread = -1
		curGid = -1
		curFrame = 0
		fmt.Fprintf(&out, "Error refreshing state %s: %v\n", pos, err)

	}

	state, err := client.GetState()
	if err != nil {
		mu.Lock()
		failstate("GetState()", err)
		mu.Unlock()
		return
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
				lp.listing = append(lp.listing, listline{expandTabs(buf.Text()), lineno == state.CurrentThread.Line, breakpoint})
			}

			if err := buf.Err(); err != nil {
				failstate("(reading file)", err)
				return
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
	commandLineEditor.Flags = nucular.EditSelectable | nucular.EditSigEnter

	args := []string{"--headless"}
	args = append(args, os.Args[1:]...)

	cmd := exec.Command("dlv", args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	err := cmd.Start()

	if err != nil {
		mu.Lock()
		fmt.Fprintf(&out, "Could not start delve: %v\n", err)
		mu.Unlock()
	} else {
		go func() {
			first := true
			scan := bufio.NewScanner(stdout)
			for scan.Scan() {
				if first {
					connectTo(scan.Text())
					first = false
				} else {
					fmt.Fprintln(&out, scan.Text())
				}
			}
			if err := scan.Err(); err != nil {
				mu.Lock()
				fmt.Fprintf(&out, "Error reading stdout: %v\n", err)
				mu.Unlock()
			}
		}()

		go func() {
			_, err := io.Copy(&out, stderr)
			if err != nil {
				mu.Lock()
				fmt.Fprintf(&out, "Error reading stderr: %v\n", err)
				mu.Unlock()
			}
		}()
	}

	wnd.Main()
}
