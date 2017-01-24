// Copyright 2016, Gdlv Authors

package main

import (
	"errors"
	"fmt"
	"go/parser"
	"go/scanner"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/derekparker/delve/service/api"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"
)

type cmdfunc func(out io.Writer, args string) error

type command struct {
	aliases  []string
	complete func()
	helpMsg  string
	cmdFn    cmdfunc
}

// Returns true if the command string matches one of the aliases for this command
func (c command) match(cmdstr string) bool {
	for _, v := range c.aliases {
		if v == cmdstr {
			return true
		}
	}
	return false
}

type Commands struct {
	cmds    []command
	lastCmd cmdfunc
}

var (
	LongLoadConfig  = api.LoadConfig{true, 1, 64, 64, -1}
	ShortLoadConfig = api.LoadConfig{false, 0, 64, 0, 3}
)

type ByFirstAlias []command

func (a ByFirstAlias) Len() int           { return len(a) }
func (a ByFirstAlias) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByFirstAlias) Less(i, j int) bool { return a[i].aliases[0] < a[j].aliases[0] }

var cmdhistory = []string{""}
var historyShown int = 0
var cmds *Commands

func DebugCommands() *Commands {
	c := &Commands{}

	c.cmds = []command{
		{aliases: []string{"help", "h"}, cmdFn: c.help, helpMsg: `Prints the help message.

	help [command]
	
Type "help" followed by the name of a command for more information about it.`},
		{aliases: []string{"break", "b"}, cmdFn: breakpoint, complete: completeLocation, helpMsg: `Sets a breakpoint.

	break [name] <linespec>

See $GOPATH/src/github.com/derekparker/delve/Documentation/cli/locspec.md for the syntax of linespec.

See also: "help on", "help cond" and "help clear"`},
		{aliases: []string{"trace", "t"}, cmdFn: tracepoint, complete: completeLocation, helpMsg: `Set tracepoint.

	trace [name] <linespec>
	
A tracepoint is a breakpoint that does not stop the execution of the program, instead when the tracepoint is hit a notification is displayed. See $GOPATH/src/github.com/derekparker/delve/Documentation/cli/locspec.md for the syntax of linespec.

See also: "help on", "help cond" and "help clear"`},
		{aliases: []string{"clear"}, cmdFn: clear, helpMsg: `Deletes breakpoint.
		
			clear <breakpoint name or id>`},
		{aliases: []string{"restart", "r"}, cmdFn: restart, helpMsg: "Restart process."},
		{aliases: []string{"continue", "c"}, cmdFn: cont, helpMsg: "Run until breakpoint or program termination."},
		{aliases: []string{"step", "s"}, cmdFn: step, helpMsg: "Single step through program."},
		{aliases: []string{"step-instruction", "si"}, cmdFn: stepInstruction, helpMsg: "Single step a single cpu instruction."},
		{aliases: []string{"next", "n"}, cmdFn: next, helpMsg: "Step over to next source line."},
		{aliases: []string{"stepout"}, cmdFn: stepout, helpMsg: "Step out of the current function."},
		{aliases: []string{"cancelnext"}, cmdFn: cancelnext, helpMsg: "Cancels the next operation currently in progress."},
		{aliases: []string{"interrupt"}, cmdFn: interrupt, helpMsg: "interrupts execution."},
		{aliases: []string{"print", "p"}, complete: completeVariable, cmdFn: printVar, helpMsg: `Evaluate an expression.

	print <expression>

See $GOPATH/src/github.com/derekparker/delve/Documentation/cli/expr.md for a description of supported expressions.`},
		{aliases: []string{"list", "ls"}, complete: completeLocation, cmdFn: listCommand, helpMsg: `Show source code.
		
			list <linespec>
		
		See $GOPATH/src/github.com/derekparker/delve/Documentation/cli/expr.md for a description of supported expressions.`},
		{aliases: []string{"set"}, cmdFn: setVar, complete: completeVariable, helpMsg: `Changes the value of a variable.

	set <variable> = <value>

See $GOPATH/src/github.com/derekparker/delve/Documentation/cli/expr.md for a description of supported expressions. Only numerical variables and pointers can be changed.`},
		{aliases: []string{"layout"}, cmdFn: layoutCommand, helpMsg: `Manages window layout.
	
	layout <name>

Loads the specified layout.

	layout save <name> <descr>
	
Saves the current layout.

	layout list
	
Lists saved layouts.`},
		{aliases: []string{"config"}, cmdFn: configCommand, helpMsg: `Configuration`},
		{aliases: []string{"scroll"}, cmdFn: scrollCommand, helpMsg: `Controls scrollback behavior.
	
	scroll clear		Clears scrollback
	scroll silence		Silences output from inferior
	scroll noise		Re-enables output from inferior.
`},
		{aliases: []string{"exit", "quit", "q"}, cmdFn: exitCommand, helpMsg: "Exit the debugger."},
	}

	sort.Sort(ByFirstAlias(c.cmds))
	return c
}

var noCmdError = errors.New("command not available")

func noCmdAvailable(out io.Writer, args string) error {
	return noCmdError
}

func nullCommand(out io.Writer, args string) error {
	return nil
}

func (c *Commands) help(out io.Writer, args string) error {
	if args != "" {
		for _, cmd := range c.cmds {
			for _, alias := range cmd.aliases {
				if alias == args {
					fmt.Fprintln(out, cmd.helpMsg)
					return nil
				}
			}
		}
		return noCmdError
	}

	fmt.Fprintln(out, "The following commands are available:")
	w := new(tabwriter.Writer)
	w.Init(out, 0, 8, 0, ' ', 0)
	for _, cmd := range c.cmds {
		h := cmd.helpMsg
		if idx := strings.Index(h, "\n"); idx >= 0 {
			h = h[:idx]
		}
		if len(cmd.aliases) > 1 {
			fmt.Fprintf(w, "    %s (alias: %s) \t %s\n", cmd.aliases[0], strings.Join(cmd.aliases[1:], " | "), h)
		} else {
			fmt.Fprintf(w, "    %s \t %s\n", cmd.aliases[0], h)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(out, "Type help followed by a command for full documentation.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Keybindings:")
	fmt.Fprintln(w, "    Ctrl +/- \t Zoom in/out")
	fmt.Fprintln(w, "    Escape \t Focus command line")
	fmt.Fprintln(w, "    Ctrl delete \t Request manual stop")
	if err := w.Flush(); err != nil {
		return err
	}
	return nil
}

func setBreakpoint(out io.Writer, tracepoint bool, argstr string) error {
	defer refreshState(refreshToSameFrame, clearBreakpoint, nil)
	args := strings.SplitN(argstr, " ", 2)

	requestedBp := &api.Breakpoint{}
	locspec := ""
	switch len(args) {
	case 1:
		locspec = argstr
	case 2:
		if api.ValidBreakpointName(args[0]) == nil {
			requestedBp.Name = args[0]
			locspec = args[1]
		} else {
			locspec = argstr
		}
	default:
		return fmt.Errorf("address required")
	}

	requestedBp.Tracepoint = tracepoint
	locs, err := client.FindLocation(api.EvalScope{curGid, curFrame}, locspec)
	if err != nil {
		if requestedBp.Name == "" {
			return err
		}
		requestedBp.Name = ""
		locspec = argstr
		var err2 error
		locs, err2 = client.FindLocation(api.EvalScope{curGid, curFrame}, locspec)
		if err2 != nil {
			return err
		}
	}
	for _, loc := range locs {
		requestedBp.Addr = loc.PC
		setBreakpointEx(out, requestedBp)
	}
	return nil
}

func setBreakpointEx(out io.Writer, requestedBp *api.Breakpoint) {
	bp, err := client.CreateBreakpoint(requestedBp)
	if err != nil {
		fmt.Fprintf(out, "Could not create breakpoint: %v\n", err)
	}

	fmt.Fprintf(out, "%s set at %s\n", formatBreakpointName(bp, true), formatBreakpointLocation(bp))
	freezeBreakpoint(out, bp)
}

func breakpoint(out io.Writer, args string) error {
	return setBreakpoint(out, false, args)
}

func tracepoint(out io.Writer, args string) error {
	return setBreakpoint(out, true, args)
}

func clear(out io.Writer, args string) error {
	if len(args) == 0 {
		return fmt.Errorf("not enough arguments")
	}
	id, err := strconv.Atoi(args)
	var bp *api.Breakpoint
	if err == nil {
		bp, err = client.ClearBreakpoint(id)
	} else {
		bp, err = client.ClearBreakpointByName(args)
	}
	removeFrozenBreakpoint(bp)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s cleared at %s\n", formatBreakpointName(bp, true), formatBreakpointLocation(bp))
	return nil
}

func restart(out io.Writer, args string) error {
	dorestart := BackendServer.serverProcess != nil
	BackendServer.Rebuild()
	if !dorestart || !BackendServer.buildok {
		return nil
	}

	updateFrozenBreakpoints()
	clearFrozenBreakpoints()

	discarded, err := client.Restart()
	if err != nil {
		fmt.Fprintf(out, "error on restart\n")
		return err
	}
	fmt.Fprintln(out, "Process restarted with PID", client.ProcessPid())
	for i := range discarded {
		fmt.Fprintf(out, "Discarded %s at %s: %v\n", formatBreakpointName(discarded[i].Breakpoint, false), formatBreakpointLocation(discarded[i].Breakpoint), discarded[i].Reason)
	}

	restoreFrozenBreakpoints(out)

	continueToRuntimeMain()
	refreshState(refreshToFrameZero, clearStop, nil)
	return nil
}

func cont(out io.Writer, args string) error {
	stateChan := client.Continue()
	var state *api.DebuggerState
	for state = range stateChan {
		if state.Err != nil {
			return state.Err
		}
		printcontext(out, state)
	}
	refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func continueUntilCompleteNext(out io.Writer, state *api.DebuggerState, op string) error {
	if !state.NextInProgress {
		refreshState(refreshToFrameZero, clearStop, state)
		return nil
	}
	for {
		stateChan := client.Continue()
		var state *api.DebuggerState
		for state = range stateChan {
			if state.Err != nil {
				return state.Err
			}
			printcontext(out, state)
		}
		if !state.NextInProgress || conf.StopOnNextBreakpoint {
			refreshState(refreshToFrameZero, clearStop, state)
			return nil
		}
		fmt.Fprintf(out, "    breakpoint hit during %s, continuing...\n", op)
	}
}

func step(out io.Writer, args string) error {
	state, err := client.Step()
	if err != nil {
		return err
	}
	printcontext(out, state)
	return continueUntilCompleteNext(out, state, "step")
}

func stepInstruction(out io.Writer, args string) error {
	state, err := client.StepInstruction()
	if err != nil {
		return err
	}
	printcontext(out, state)
	refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func next(out io.Writer, args string) error {
	state, err := client.Next()
	if err != nil {
		return err
	}
	printcontext(out, state)
	return continueUntilCompleteNext(out, state, "next")
}

func stepout(out io.Writer, args string) error {
	state, err := client.StepOut()
	if err != nil {
		return err
	}
	printcontext(out, state)
	return continueUntilCompleteNext(out, state, "stepout")
}

func cancelnext(out io.Writer, args string) error {
	return client.CancelNext()
}

func interrupt(out io.Writer, args string) error {
	state, err := client.Halt()
	if err != nil {
		return err
	}
	refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func printVar(out io.Writer, args string) error {
	if len(args) == 0 {
		return fmt.Errorf("not enough arguments")
	}
	val, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, args, LongLoadConfig)
	if err != nil {
		return err
	}
	valstr := val.MultilineString("")
	nlcount := 0
	for _, ch := range valstr {
		if ch == '\n' {
			nlcount++
		}
	}
	if nlcount > 20 {
		fmt.Fprintln(out, "Expression added to variables panel")
		addExpression(args)
	} else {
		fmt.Fprintln(out, valstr)
	}
	return nil
}

func listCommand(out io.Writer, args string) error {
	locs, err := client.FindLocation(api.EvalScope{curGid, curFrame}, args)
	if err != nil {
		return err
	}
	switch len(locs) {
	case 1:
		// ok
	case 0:
		return errors.New("no location found")
	default:
		return errors.New("can not list multiple locations")
	}

	listingPanel.pinnedLoc = &locs[0]
	refreshState(refreshToSameFrame, clearNothing, nil)

	return nil
}

func setVar(out io.Writer, args string) error {
	// HACK: in go '=' is not an operator, we detect the error and try to recover from it by splitting the input string
	_, err := parser.ParseExpr(args)
	if err == nil {
		return fmt.Errorf("syntax error '=' not found")
	}

	el, ok := err.(scanner.ErrorList)
	if !ok || el[0].Msg != "expected '==', found '='" {
		return err
	}

	lexpr := args[:el[0].Pos.Offset]
	rexpr := args[el[0].Pos.Offset+1:]
	return client.SetVariable(api.EvalScope{curGid, curFrame}, lexpr, rexpr)
}

// ExitRequestError is returned when the user
// exits Delve.
type ExitRequestError struct{}

func (ere ExitRequestError) Error() string {
	return ""
}

func exitCommand(out io.Writer, args string) error {
	return ExitRequestError{}
}

func layoutCommand(out io.Writer, args string) error {
	argv := strings.SplitN(args, " ", 3)
	if len(argv) < 0 {
		return fmt.Errorf("not enough arguments")
	}
	switch argv[0] {
	case "list":
		w := new(tabwriter.Writer)
		w.Init(out, 0, 8, 0, ' ', 0)
		for name, ld := range conf.Layouts {
			fmt.Fprintf(w, "%s \t %s\n", name, ld.Description)
		}
		if err := w.Flush(); err != nil {
			return err
		}
	case "save":
		if len(argv) < 2 {
			return fmt.Errorf("not enough arguments")
		}
		name := argv[1]
		description := ""
		if len(argv) > 2 {
			description = argv[2]
		}

		s, err := rootPanel.String()
		if err != nil {
			return err
		}
		conf.Layouts[name] = LayoutDescr{Description: description, Layout: s}
		saveConfiguration()
	default:
		ld, ok := conf.Layouts[argv[0]]
		if !ok {
			return fmt.Errorf("unknown layout %q", argv[0])
		}
		newRoot, _ := parsePanelDescr(ld.Layout, nil)
		mu.Lock()
		rootPanel = newRoot
		wnd.Changed()
		mu.Unlock()
	}
	return nil
}

func configCommand(out io.Writer, args string) error {
	wnd.PopupOpen("Configuration", dynamicPopupFlags, rect.Rect{100, 100, 600, 700}, true, configWindow)
	return nil
}

func configWindow(w *nucular.Window) {
	const col1 = 160
	w.Row(20).Static(col1, 150)
	w.Label("Theme:", "LC")
	themeLbl := "Dark theme"
	if conf.WhiteTheme {
		themeLbl = "White theme"
	}
	if w := w.Combo(label.TA(themeLbl, "LC"), 100, nil); w != nil {
		w.Row(20).Dynamic(1)
		if w.MenuItem(label.TA("Dark theme", "LC")) {
			conf.WhiteTheme = false
			setupStyle()
		}
		if w.MenuItem(label.TA("White theme", "LC")) {
			conf.WhiteTheme = true
			setupStyle()
		}
	}

	w.Row(20).Static(col1, 150)
	w.Label("Disassembly Flavor:", "LC")
	disassfl := []string{"Intel", "GNU"}
	conf.DisassemblyFlavour = w.ComboSimple(disassfl, conf.DisassemblyFlavour, 20)

	w.Row(20).Dynamic(1)
	w.Label("When a breakpoint is hit during next/step/stepout gdlv should:", "LC")
	w.Row(20).Static(col1, 200)
	w.Spacing(1)
	breakb := []string{"Automatically continue", "Stop"}
	breakbLbl := breakb[0]
	if conf.StopOnNextBreakpoint {
		breakbLbl = breakb[1]
	}
	if w := w.Combo(label.TA(breakbLbl, "LC"), 100, nil); w != nil {
		w.Row(20).Dynamic(1)
		if w.MenuItem(label.TA(breakb[0], "LC")) {
			conf.StopOnNextBreakpoint = false
		}
		if w.MenuItem(label.TA(breakb[1], "LC")) {
			conf.StopOnNextBreakpoint = true
		}
	}

	w.Row(20).Static(0, 100)
	w.Spacing(1)
	if w.ButtonText("OK") {
		w.Close()
	}
}

func scrollCommand(out io.Writer, args string) error {
	switch args {
	case "clear":
		mu.Lock()
		scrollbackEditor.Buffer = scrollbackEditor.Buffer[:0]
		scrollbackEditor.Cursor = 0
		scrollbackEditor.CursorFollow = true
		mu.Unlock()
	case "silence":
		mu.Lock()
		silenced = true
		mu.Unlock()
		fmt.Fprintf(out, "Inferior output silenced\n")
	case "noise":
		mu.Lock()
		silenced = false
		mu.Unlock()
		fmt.Fprintf(out, "Inferior output enabled\n")
	default:
		mu.Lock()
		s := silenced
		mu.Unlock()
		if s {
			fmt.Fprintf(out, "Inferior output is silenced\n")
		} else {
			fmt.Fprintf(out, "Inferior output is not silenced\n")
		}
	}
	return nil
}

func formatBreakpointName(bp *api.Breakpoint, upcase bool) string {
	thing := "breakpoint"
	if bp.Tracepoint {
		thing = "tracepoint"
	}
	if upcase {
		thing = strings.Title(thing)
	}
	id := bp.Name
	if id == "" {
		id = strconv.Itoa(bp.ID)
	}
	return fmt.Sprintf("%s %s", thing, id)
}

func formatBreakpointLocation(bp *api.Breakpoint) string {
	p := ShortenFilePath(bp.File)
	if bp.FunctionName != "" {
		return fmt.Sprintf("%#v for %s() %s:%d", bp.Addr, bp.FunctionName, p, bp.Line)
	}
	return fmt.Sprintf("%#v for %s:%d", bp.Addr, p, bp.Line)
}

func printcontext(out io.Writer, state *api.DebuggerState) error {
	for i := range state.Threads {
		if (state.CurrentThread != nil) && (state.Threads[i].ID == state.CurrentThread.ID) {
			continue
		}
		if state.Threads[i].Breakpoint != nil {
			printcontextThread(out, state.Threads[i])
		}
	}

	if state.CurrentThread == nil {
		fmt.Fprintln(out, "No current thread available")
		return nil
	}
	if len(state.CurrentThread.File) == 0 {
		fmt.Fprintf(out, "Stopped at: 0x%x\n", state.CurrentThread.PC)
		return nil
	}

	printcontextThread(out, state.CurrentThread)

	return nil
}

func printcontextThread(out io.Writer, th *api.Thread) {
	fn := th.Function

	if th.Breakpoint == nil {
		fmt.Fprintf(out, "> %s() %s:%d (PC: %#v)\n", fn.Name, ShortenFilePath(th.File), th.Line, th.PC)
		return
	}

	args := ""
	if th.BreakpointInfo != nil && th.Breakpoint.LoadArgs != nil && *th.Breakpoint.LoadArgs == ShortLoadConfig {
		var arg []string
		for _, ar := range th.BreakpointInfo.Arguments {
			arg = append(arg, ar.SinglelineString())
		}
		args = strings.Join(arg, ", ")
	}

	bpname := ""
	if th.Breakpoint.Name != "" {
		bpname = fmt.Sprintf("[%s] ", th.Breakpoint.Name)
	}

	if hitCount, ok := th.Breakpoint.HitCount[strconv.Itoa(th.GoroutineID)]; ok {
		fmt.Fprintf(out, "> %s%s(%s) %s:%d (hits goroutine(%d):%d total:%d) (PC: %#v)\n",
			bpname,
			fn.Name,
			args,
			ShortenFilePath(th.File),
			th.Line,
			th.GoroutineID,
			hitCount,
			th.Breakpoint.TotalHitCount,
			th.PC)
	} else {
		fmt.Fprintf(out, "> %s%s(%s) %s:%d (hits total:%d) (PC: %#v)\n",
			bpname,
			fn.Name,
			args,
			ShortenFilePath(th.File),
			th.Line,
			th.Breakpoint.TotalHitCount,
			th.PC)
	}

	if th.BreakpointInfo != nil {
		bp := th.Breakpoint
		bpi := th.BreakpointInfo

		if bpi.Goroutine != nil {
			writeGoroutineLong(os.Stdout, bpi.Goroutine, "\t")
		}

		for _, v := range bpi.Variables {
			fmt.Fprintf(out, "    %s: %s\n", v.Name, v.MultilineString("\t"))
		}

		for _, v := range bpi.Locals {
			if *bp.LoadLocals == LongLoadConfig {
				fmt.Fprintf(out, "    %s: %s\n", v.Name, v.MultilineString("\t"))
			} else {
				fmt.Fprintf(out, "    %s: %s\n", v.Name, v.SinglelineString())
			}
		}

		if bp.LoadArgs != nil && *bp.LoadArgs == LongLoadConfig {
			for _, v := range bpi.Arguments {
				fmt.Fprintf(out, "    %s: %s\n", v.Name, v.MultilineString("\t"))
			}
		}

		if bpi.Stacktrace != nil {
			fmt.Fprintf(out, "    Stack:\n")
			printStack(out, bpi.Stacktrace, "        ")
		}
	}
}

func formatLocation(loc api.Location) string {
	fname := ""
	if loc.Function != nil {
		fname = loc.Function.Name
	}
	return fmt.Sprintf("%s at %s:%d (%#v)", fname, ShortenFilePath(loc.File), loc.Line, loc.PC)
}

func writeGoroutineLong(w io.Writer, g *api.Goroutine, prefix string) {
	fmt.Fprintf(w, "%sGoroutine %d:\n%s\tRuntime: %s\n%s\tUser: %s\n%s\tGo: %s\n",
		prefix, g.ID,
		prefix, formatLocation(g.CurrentLoc),
		prefix, formatLocation(g.UserCurrentLoc),
		prefix, formatLocation(g.GoStatementLoc))
}

func printStack(out io.Writer, stack []api.Stackframe, ind string) {
	if len(stack) == 0 {
		return
	}
	d := digits(len(stack) - 1)
	fmtstr := "%s%" + strconv.Itoa(d) + "d  0x%016x in %s\n"
	s := ind + strings.Repeat(" ", d+2+len(ind))

	for i := range stack {
		name := "(nil)"
		if stack[i].Function != nil {
			name = stack[i].Function.Name
		}
		fmt.Fprintf(out, fmtstr, ind, i, stack[i].PC, name)
		fmt.Fprintf(out, "%sat %s:%d\n", s, ShortenFilePath(stack[i].File), stack[i].Line)

		for j := range stack[i].Arguments {
			fmt.Fprintf(out, "%s    %s = %s\n", s, stack[i].Arguments[j].Name, stack[i].Arguments[j].SinglelineString())
		}
		for j := range stack[i].Locals {
			fmt.Fprintf(out, "%s    %s = %s\n", s, stack[i].Locals[j].Name, stack[i].Locals[j].SinglelineString())
		}
	}
}

// ShortenFilePath take a full file path and attempts to shorten
// it by replacing the current directory to './'.
func ShortenFilePath(fullPath string) string {
	workingDir, _ := os.Getwd()
	return strings.Replace(fullPath, workingDir, ".", 1)
}

func executeCommand(cmdstr string) {
	mu.Lock()
	running = true
	wnd.Changed()
	mu.Unlock()
	defer func() {
		mu.Lock()
		running = false
		wnd.Changed()
		mu.Unlock()
	}()

	out := editorWriter{&scrollbackEditor, true}
	cmdstr, args := parseCommand(cmdstr)
	if err := cmds.Call(cmdstr, args, &out); err != nil {
		if _, ok := err.(ExitRequestError); ok {
			if client.AttachedToExistingProcess() {
				wnd.PopupOpen("Confirm Quit", dynamicPopupFlags, rect.Rect{100, 100, 400, 700}, true, confirmQuit)
			} else {
				client.Detach(true)
				wnd.Close()
			}
		}
		// The type information gets lost in serialization / de-serialization,
		// so we do a string compare on the error message to see if the process
		// has exited, or if the command actually failed.
		if strings.Contains(err.Error(), "exited") {
			fmt.Fprintln(&out, err.Error())
		} else {
			fmt.Fprintf(&out, "Command failed: %s\n", err)
		}
	}
}

func confirmQuit(w *nucular.Window) {
	w.Row(20).Dynamic(1)
	w.Label("Would you like to kill the process?", "LT")
	w.Row(20).Static(0, 80, 80, 0)
	w.Spacing(1)
	exit := false
	kill := false
	if w.ButtonText("Yes") {
		exit = true
		kill = true
	}
	if w.ButtonText("No") {
		exit = true
		kill = false
	}
	if exit {
		client.Detach(kill)
		go wnd.Close()
	}
	w.Spacing(1)
}

func parseCommand(cmdstr string) (string, string) {
	vals := strings.SplitN(cmdstr, " ", 2)
	if len(vals) == 1 {
		return vals[0], ""
	}
	return vals[0], strings.TrimSpace(vals[1])
}

// Find will look up the command function for the given command input.
// If it cannot find the command it will default to noCmdAvailable().
// If the command is an empty string it will replay the last command.
func (c *Commands) Find(cmdstr string) cmdfunc {
	// If <enter> use last command, if there was one.
	if cmdstr == "" {
		if c.lastCmd != nil {
			return c.lastCmd
		}
		return nullCommand
	}

	for _, v := range c.cmds {
		if v.match(cmdstr) {
			c.lastCmd = v.cmdFn
			return v.cmdFn
		}
	}

	return noCmdAvailable
}

func (c *Commands) Call(cmdstr, args string, out io.Writer) error {
	return c.Find(cmdstr)(out, args)
}
