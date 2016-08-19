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

	"github.com/derekparker/delve/service"
	"github.com/derekparker/delve/service/api"
)

type cmdfunc func(c service.Client, out io.Writer, args string) error

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
	client  service.Client
}

var (
	LongLoadConfig  = api.LoadConfig{true, 1, 64, 64, -1}
	ShortLoadConfig = api.LoadConfig{false, 0, 64, 0, 3}
)

type ByFirstAlias []command

func (a ByFirstAlias) Len() int           { return len(a) }
func (a ByFirstAlias) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByFirstAlias) Less(i, j int) bool { return a[i].aliases[0] < a[j].aliases[0] }

var lastCmd string
var cmds *Commands

func DebugCommands(client service.Client) *Commands {
	c := &Commands{client: client}

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
		{aliases: []string{"restart", "r"}, cmdFn: restart, helpMsg: "Restart process."},
		{aliases: []string{"continue", "c"}, cmdFn: cont, helpMsg: "Run until breakpoint or program termination."},
		{aliases: []string{"step", "s"}, cmdFn: step, helpMsg: "Single step through program."},
		{aliases: []string{"step-instruction", "si"}, cmdFn: stepInstruction, helpMsg: "Single step a single cpu instruction."},
		{aliases: []string{"next", "n"}, cmdFn: next, helpMsg: "Step over to next source line."},
		{aliases: []string{"stepout"}, cmdFn: stepout, helpMsg: "Step out of the current function."},
		{aliases: []string{"print", "p"}, complete: completeVariable, cmdFn: printVar, helpMsg: `Evaluate an expression.

	[goroutine <n>] [frame <m>] print <expression>

See $GOPATH/src/github.com/derekparker/delve/Documentation/cli/expr.md for a description of supported expressions.`},
		{aliases: []string{"set"}, cmdFn: setVar, complete: completeVariable, helpMsg: `Changes the value of a variable.

	[goroutine <n>] [frame <m>] set <variable> = <value>

See $GOPATH/src/github.com/derekparker/delve/Documentation/cli/expr.md for a description of supported expressions. Only numerical variables and pointers can be changed.`},
		{aliases: []string{"layout"}, cmdFn: layoutCommand, helpMsg: `Manages window layout.
	
	layout <name>

Loads the specified layout.

	layout save <name> <descr>
	
Saves the current layout.

	layout list
	
Lists saved layouts.`},
		{aliases: []string{"theme"}, cmdFn: themeCommand, helpMsg: `Changes theme`},
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

func noCmdAvailable(client service.Client, out io.Writer, args string) error {
	return noCmdError
}

func nullCommand(client service.Client, out io.Writer, args string) error {
	return nil
}

func (c *Commands) help(client service.Client, out io.Writer, args string) error {
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

func setBreakpoint(client service.Client, out io.Writer, tracepoint bool, argstr string) error {
	defer breakpointsPanel.asyncLoad.clear()
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
	locs, err := client.FindLocation(api.EvalScope{GoroutineID: -1, Frame: 0}, locspec)
	if err != nil {
		if requestedBp.Name == "" {
			return err
		}
		requestedBp.Name = ""
		locspec = argstr
		var err2 error
		locs, err2 = client.FindLocation(api.EvalScope{GoroutineID: -1, Frame: 0}, locspec)
		if err2 != nil {
			return err
		}
	}
	for _, loc := range locs {
		requestedBp.Addr = loc.PC

		bp, err := client.CreateBreakpoint(requestedBp)
		if err != nil {
			return err
		}

		fmt.Fprintf(out, "%s set at %s\n", formatBreakpointName(bp, true), formatBreakpointLocation(bp))
	}
	return nil
}

func breakpoint(client service.Client, out io.Writer, args string) error {
	return setBreakpoint(client, out, false, args)
}

func tracepoint(client service.Client, out io.Writer, args string) error {
	return setBreakpoint(client, out, true, args)
}

func restart(client service.Client, out io.Writer, args string) error {
	if err := client.Restart(); err != nil {
		return err
	}
	fmt.Fprintln(out, "Process restarted with PID", client.ProcessPid())
	return nil
}

func cont(client service.Client, out io.Writer, args string) error {
	stateChan := client.Continue()
	var state *api.DebuggerState
	for state = range stateChan {
		if state.Err != nil {
			return state.Err
		}
		printcontext(out, state)
	}
	refreshState(false, clearStop, state)
	return nil
}

func continueUntilCompleteNext(client service.Client, out io.Writer, state *api.DebuggerState, op string) error {
	if !state.NextInProgress {
		refreshState(false, clearStop, state)
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
		if !state.NextInProgress {
			refreshState(false, clearStop, state)
			return nil
		}
		fmt.Fprintf(out, "    breakpoint hit during %s, continuing...\n", op)
	}
}

func step(client service.Client, out io.Writer, args string) error {
	state, err := client.Step()
	if err != nil {
		return err
	}
	printcontext(out, state)
	return continueUntilCompleteNext(client, out, state, "step")
}

func stepInstruction(client service.Client, out io.Writer, args string) error {
	state, err := client.StepInstruction()
	if err != nil {
		return err
	}
	printcontext(out, state)
	refreshState(false, clearStop, state)
	return nil
}

func next(client service.Client, out io.Writer, args string) error {
	state, err := client.Next()
	if err != nil {
		return err
	}
	printcontext(out, state)
	return continueUntilCompleteNext(client, out, state, "next")
}

func stepout(client service.Client, out io.Writer, args string) error {
	state, err := client.StepOut()
	if err != nil {
		return err
	}
	printcontext(out, state)
	return continueUntilCompleteNext(client, out, state, "stepout")
}

func printVar(client service.Client, out io.Writer, args string) error {
	if len(args) == 0 {
		return fmt.Errorf("not enough arguments")
	}
	val, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, args, LongLoadConfig)
	if err != nil {
		return err
	}

	fmt.Fprintln(out, val.MultilineString(""))
	return nil
}

func setVar(client service.Client, out io.Writer, args string) error {
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

func exitCommand(client service.Client, out io.Writer, args string) error {
	return ExitRequestError{}
}

func layoutCommand(client service.Client, out io.Writer, args string) error {
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

func themeCommand(client service.Client, out io.Writer, args string) error {
	switch args {
	case "dark":
		conf.WhiteTheme = false
		setupStyle()
		return nil
	case "white":
		conf.WhiteTheme = true
		setupStyle()
		return nil
	default:
		return fmt.Errorf("available themes: 'dark', 'white'")
	}
}

func scrollCommand(client service.Client, out io.Writer, args string) error {
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
			//TODO: ask if we should kill
			client.Detach(true)
			wnd.Close()
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
	return c.Find(cmdstr)(client, out, args)
}
