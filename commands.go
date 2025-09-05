// Copyright 2016, Gdlv Authors

package main

import (
	"bytes"
	"errors"
	"fmt"
	"go/parser"
	"go/scanner"
	"io"
	"net/rpc"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/tabwriter"
	"time"
	"unicode"

	"golang.org/x/mobile/event/key"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/prettyprint"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/font"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"
	"github.com/aarzilli/nucular/richtext"
	"github.com/aarzilli/nucular/style"
	"github.com/aarzilli/nucular/style-editor"
)

const optimizedFunctionWarning = "Warning: debugging optimized function"

type cmdfunc func(out io.Writer, args string) error

type command struct {
	aliases  []string
	group    commandGroup
	complete func()
	helpMsg  string
	cmdFn    cmdfunc
}

type commandGroup uint8

const (
	otherCmds commandGroup = iota
	breakCmds
	runCmds
	revCmds
	dataCmds
	winCmds
	scriptCmds
)

var commandGroupDescriptions = []struct {
	description string
	group       commandGroup
}{
	{"Running the program", runCmds},
	{"Reverse execution", revCmds},
	{"Manipulating breakpoints", breakCmds},
	{"Viewing program variables and memory", dataCmds},
	{"Setting up the GUI", winCmds},
	{"Starlark script commands", scriptCmds},
	{"Other commands", otherCmds},
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
	cmds []command
}

var (
	LongLoadConfig      = api.LoadConfig{true, 1, 128, 16, -1}
	LongArrayLoadConfig = api.LoadConfig{true, 1, 128, 64, -1}
	ShortLoadConfig     = api.LoadConfig{false, 0, 64, 0, 3}
)

type ByFirstAlias []command

func (a ByFirstAlias) Len() int           { return len(a) }
func (a ByFirstAlias) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByFirstAlias) Less(i, j int) bool { return a[i].aliases[0] < a[j].aliases[0] }

var cmdhistory = []string{""}
var historyShown int = 0
var historySearch bool
var historyNeedle string
var cmds *Commands

func DebugCommands() *Commands {
	c := &Commands{}

	c.cmds = []command{
		{aliases: []string{"help", "h"}, cmdFn: c.help, helpMsg: `Prints the help message.

	help [command]
	
Type "help" followed by the name of a command for more information about it.`},
		{aliases: []string{"break", "b"}, group: breakCmds, cmdFn: breakpoint, complete: completeLocation, helpMsg: `Sets a breakpoint.

	break [name] <linespec>
	break

See $GOPATH/src/github.com/go-delve/delve/Documentation/cli/locspec.md for the syntax of linespec. To set breakpoints you can also right click on a source line and click "Set breakpoint". Breakpoint properties can be changed by right clicking on a breakpoint (either in the source panel or the breakpoints panel) and selecting "Edit breakpoint".

Without arguments displays all currently set breakponts.`},
		{aliases: []string{"clear"}, group: breakCmds, cmdFn: clear, helpMsg: `Deletes breakpoint.
		
			clear <breakpoint name or id>`},
		{aliases: []string{"restart", "r"}, group: runCmds, cmdFn: restart, helpMsg: `Restart process.

For live processes any argument passed to restart will be used as argument for the program. 
If no arguments are specified the program will be restarted with the same arguments as the last time it was started.

Use:

	restart --
	
To clear the arguments passed to the program.

If the target is recorded:

	restart [<checkpoint>]

Restarts the recording at the specified (optional) breakpoint.

	restart -r

Re-records the program, any arguments specified after '-r' are passed to the target program. Pass '--' to clear the arguments passed to the program.

All forms of restart that support specifying a list of arguments to pass to the target program (i.e. 'restart' on a live process or 'restart -r' on a recording) also support specifying a list of redirects for the target process:

	<input.txt	redirects the standard input of the target process from input.txt
	>output.txt	redirects the standard output of the target process to output.txt
	2>error.txt	redirects the standard error of the target process to error.txt
`},
		{aliases: []string{"continue", "c"}, group: runCmds, cmdFn: cont, helpMsg: "Run until breakpoint or program termination."},
		{aliases: []string{"rewind", "rw"}, group: revCmds, cmdFn: rewind, helpMsg: "Run backwards until breakpoint or program termination."},
		{aliases: []string{"rev"}, group: revCmds, cmdFn: c.reverse, helpMsg: `Executes program backwards.
		
		rev next
		rev step
		rev stepout
		rev step-instruction
`},

		{aliases: []string{"checkpoint", "check"}, cmdFn: checkpoint, helpMsg: `Creates a checkpoint at the current position.
	
	checkpoint [where]`},
		{aliases: []string{"step", "s"}, group: runCmds, cmdFn: step, helpMsg: `Single step through program.
		
		step [-list|-first|-last|name]
		
Specify a name to step into one specific function call. Use the -list option for all the function calls on the current line. To step into a specific function call you can also right click on a function call (on the current line) and select "Step into".

Option -first will step into the first function call of the line, -last will step into the last call of the line. When called without arguments step will use -first as default, but this can be changed using config.`},
		{aliases: []string{"step-instruction", "si"}, group: runCmds, cmdFn: stepInstruction, helpMsg: "Single step a single cpu instruction."},
		{aliases: []string{"next-instruction", "ni"}, group: runCmds,
			cmdFn: nextInstruction, helpMsg: "Like step-instruction but does not step into CALL instructions."},
		{aliases: []string{"next", "n"}, group: runCmds, cmdFn: next, helpMsg: "Step over to next source line."},
		{aliases: []string{"stepout", "o"}, group: runCmds, cmdFn: stepout, helpMsg: "Step out of the current function."},
		{aliases: []string{"cancelnext"}, group: runCmds, cmdFn: cancelnext, helpMsg: "Cancels the next operation currently in progress."},
		{aliases: []string{"interrupt"}, group: runCmds, cmdFn: interrupt, helpMsg: "interrupts execution."},
		{aliases: []string{"print", "p"}, group: dataCmds, complete: completeVariable, cmdFn: printVar, helpMsg: `Evaluate an expression.

	print [@<scope-expr>] [format-expr] <expression>
	print [@<scope-expr>] [format-expr] $ <starlar-expression>

See $GOPATH/src/github.com/go-delve/delve/Documentation/cli/expr.md for a description of supported expressions.
Type 'help scope-expr' for a description of <scope-expr>.
Type 'help format-expr' for a description of <format-expr>.`},
		{aliases: []string{"list", "ls"}, complete: completeLocation, cmdFn: listCommand, helpMsg: `Show source code.
		
			list <linespec>
		
		See $GOPATH/src/github.com/go-delve/delve/Documentation/cli/expr.md for a description of supported expressions.`},
		{aliases: []string{"set"}, group: dataCmds, cmdFn: setVar, complete: completeVariable, helpMsg: `Changes the value of a variable.

	set <variable> = <value>

See $GOPATH/src/github.com/go-delve/delve/Documentation/cli/expr.md for a description of supported expressions. Only numerical variables and pointers can be changed.`},
		{aliases: []string{"display", "disp", "dp"}, group: dataCmds, complete: completeVariable, cmdFn: displayVar, helpMsg: `Adds one expression to the Variables panel.
	
	display [@<scope-expr>] [format-expr] <expression>
	display [@<scope-expr>] [format-expr] $ <starlark-expression>

See $GOPATH/src/github.com/go-delve/delve/Documentation/cli/expr.md for a description of supported expressions.
Starlark expressions must terminate within 500ms.
Type 'help scope-expr' for a description of <scope-expr>.
Type 'help format-expr' for a description of <format-expr>.`},
		{aliases: []string{"details", "det", "dt"}, group: dataCmds, complete: completeVariable, cmdFn: detailsVar, helpMsg: `Opens details window for the specified expression.
	
	details <expr>
`},
		{aliases: []string{"layout"}, group: winCmds, cmdFn: layoutCommand, helpMsg: `Manages window layout.
	
	layout <name>

Loads the specified layout.

	layout save <name> <descr>
	
Saves the current layout. Overwrite "default" to change the default layout.

	layout list
	
Lists saved layouts.`},
		{aliases: []string{"config"}, cmdFn: configCommand, helpMsg: `Configuration

	config
	config alias <command> <alias>
	config zoom <factor>
	
Without arguments opens the configuration window.
With the 'alias' subcommand sets up a command alias.
With the 'zoom' subcommand changes the display scaling factor (makes fonts larger or smaller).
`},
		{aliases: []string{"scroll"}, group: winCmds, cmdFn: scrollCommand, helpMsg: `Controls scrollback behavior.
	
	scroll clear		Clears scrollback
	scroll silence		Silences output from inferior
	scroll noise		Re-enables output from inferior.
`},
		{aliases: []string{"exit", "quit", "q"}, cmdFn: exitCommand, helpMsg: "Exit the debugger."},

		{aliases: []string{"window", "win"}, complete: completeWindow, cmdFn: windowCommand, helpMsg: `Opens a window.
	
	window <kind>
	
Kind is one of listing, diassembly, goroutines, stacktrace, variables, globals, breakpoints, threads, registers, sources, functions, types and checkpoints.

Shortcuts:
	Alt-1	Listing window
	Alt-2	Variables window
	Alt-3	Globals window
	Alt-4	Registers window
	Alt-5	Breakpoints window
	Alt-6	Stacktrace window
	Alt-7	Disassembly window
	Alt-8	Goroutines window
	Alt-9	Threads Window
`},
		{aliases: []string{"source"}, cmdFn: sourceCommand, complete: completeFilesystem, helpMsg: `Executes a starlark script
	
	source <path>

If path is a single '-' character an interactive starlark interpreter will start instead. Type 'exit' to exit.
See documentation in doc/starlark.md.`},

		{aliases: []string{"stack"}, cmdFn: stackCommand, helpMsg: `Prints stacktrace
			
			stack [depth]
		
Prints the current stack trace. If depth is omitted it defaults to 5, all other settings are copied from the stacktrace panel.`},

		{aliases: []string{"goroutines"}, cmdFn: goroutinesCommand, helpMsg: `Prints the list of currently running goroutines.

All parameters are copied from the goroutines panel.`},

		{aliases: []string{"dump"}, cmdFn: dump, helpMsg: `Creates a core dump from the current process state

	dump <output file>

The core dump is always written in ELF, even on systems (windows, macOS) where this is not customary. For environments other than linux/amd64 threads and registers are dumped in a format that only Delve can read back.`},
		{aliases: []string{"watch"}, group: breakCmds, cmdFn: watchpoint, helpMsg: `Set watchpoint.
	
	watch [-r|-w|-rw] <expr>
	
	-r	stops when the memory location is read
	-w	stops when the memory location is written
	-rw	stops when the memory location is read or written

The memory location is specified with the same expression language used by 'print', for example:

	watch v

will watch the address of variable 'v'.

See also: "help print".`},

		{aliases: []string{"call"}, group: runCmds, cmdFn: call, helpMsg: `Resumes process, injecting a function call (EXPERIMENTAL!!!)
	
	call [-unsafe] <function call expression>
	
Current limitations:
- only pointers to stack-allocated objects can be passed as argument.
- only some automatic type conversions are supported.
- functions can only be called on running goroutines that are not
  executing the runtime.
- the current goroutine needs to have at least 256 bytes of free space on
  the stack.
- functions can only be called when the goroutine is stopped at a safe
  point.
- calling a function will resume execution of all goroutines.
- only supported on linux's native backend.
`},

		{aliases: []string{"target"}, cmdFn: target, helpMsg: `Manages child process debugging.

	target follow-exec [-on [regex]] [-off]

Enables or disables follow exec mode. When follow exec mode Delve will automatically attach to new child processes executed by the target process. An optional regular expression can be passed to 'target follow-exec', only child processes with a command line matching the regular expression will be followed.

	target list

List currently attached processes.

	target switch [pid]

Switches to the specified process.
`},

		{aliases: []string{"libraries"}, cmdFn: libraries, helpMsg: `List loaded dynamic libraries`},
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
	if args == "scope-expr" {
		fmt.Fprint(out, `A scope expression can be used as the first argument of the 'print' and 'display' commands to describe the scope in which an expression should be evaluated. For example in:
		
print @g2f8 a+1

The text "@g2f8" is a scope expression describing that the expression "a+1" should be executed in the 8th frame (f8) of goroutine 2 (g2).

A scope expression always starts with an '@' character and should contain either a goroutine specifier, a frame specifier or both.

A goroutine specifier is a positive integer following the character 'g'. The integer can be specified in decimal or in hexadecimal, following a '0x' prefix.

There are three kinds of frame specifiers:

1. The character 'f' followed by a positive integer specifies the frame number in which the expression should be evaluated. 'f0' specifies the topmost stack frame, 'f1' specifies the caller frame, etc.

2. The character 'f' followed by a negative integer specifies the frame offset for the frame in which the expression should be evaluated. Gdlv will look in the topmost 100 frames for a frame with the same offset as the one specified.

3. The character 'f' followed by a regular expression delimited by the character '/'. This specifies that the expression should be evaluated in the first frame that's executing a function whose name matches the regular expression.
`)
		return nil
	}

	if args == "format-expr" {
		fmt.Fprint(out, `A format expression can be used to change the way print and display format their output. The syntax is similar to the format directives of printf.
		
print %02x
	
	Changes the format of integer numbers to hexadecimal, padding with 0s for two characters

print %0.2f
	
	Changes the format of floating point numbers to %0.2f.

print %200s x

	Changes the number of characters retrieved for strings to 200.

print %#s x

	Formats strings like 'hexdump -C'.

print %100a x
	
	Changes the number of elements loaded for slices, arrays and maps to 100.

print %5v x
	
	Changes the recursion depth for structs and pointers to 5.

print %5t x

	If x is a slice or an array it will display its contents in a table of 5 columns. The expression %5.2t can be used to create a 5 columns by 2 lines table.

print %+0.2g%o%1000s x

	Multiple formatting directives can be used simultaneously, in this example integer numbers will be print in octal, floating point numbers will be formatted with %+0.2g and strings will be loaded up to 1000 characters.
`)
		return nil
	}

	if args != "" {
		for _, cmd := range c.cmds {
			for _, alias := range cmd.aliases {
				if alias == args {
					fmt.Fprintln(out, cmd.helpMsg)
					return nil
				}
			}
		}
		switch args {
		case "fonts":
			fmt.Fprintf(out, `By default gdlv uses a built-in version of Droid Sans Mono.

If you don't like the font or if it doesn't cover a script that you need you
can change the font by setting the environment variables GDLV_NORMAL_FONT
and GDLV_BOLD_FONT to the path of two ttf files.
`)
			return nil
		}
		return noCmdError
	}

	fmt.Fprintln(out, "The following commands are available:")

	for _, cgd := range commandGroupDescriptions {
		fmt.Fprintf(out, "\n%s:\n", cgd.description)
		w := new(tabwriter.Writer)
		w.Init(out, 0, 8, 0, ' ', 0)
		for _, cmd := range c.cmds {
			if cmd.group != cgd.group {
				continue
			}
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
	}
	fmt.Fprintln(out, "Type help followed by a command for full documentation.")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Keybindings:")
	{
		w := new(tabwriter.Writer)
		w.Init(out, 0, 8, 0, ' ', 0)
		fmt.Fprintf(w, "    %s +/-/0 \t Zoom in/out/reset\n", zoomMetaKeyStr)
		fmt.Fprintln(w, "    Escape \t Focus command line")
		fmt.Fprintln(w, "    Shift-F5, Ctrl-delete \t Request manual stop")
		fmt.Fprintln(w, "    F5, Alt-enter \t Continue")
		fmt.Fprintln(w, "    F10, Alt-right \t Next")
		fmt.Fprintln(w, "    F11, Alt-down \t Step")
		fmt.Fprintln(w, "    Shift-F11, Alt-up \t Step Out")
		fmt.Fprintln(w, "    Shift-enter \t Add new expression to the variables window")
		if err := w.Flush(); err != nil {
			return err
		}
	}
	fmt.Fprintln(out)
	fmt.Fprintln(out, "Keybindings for the variables window:")
	{
		w := new(tabwriter.Writer)
		w.Init(out, 0, 8, 0, ' ', 0)
		fmt.Fprintln(w, "    Shift-up \t move to previous expression")
		fmt.Fprintln(w, "    Shift-down \t move to next expression")
		fmt.Fprintln(w, "    Shift-delete \t removes current expression")
		fmt.Fprintln(w, "    Ctrl-o \t expands current expression")
		if err := w.Flush(); err != nil {
			return err
		}
	}

	fmt.Fprintln(out, "\nFor help about changing fonts type \"help fonts\".")

	return nil
}

func setBreakpoint(out io.Writer, tracepoint bool, argstr string) error {
	if argstr == "" {
		listBreakpoints()
		return nil
	}

	if curThread < 0 {
		cmd := "B"
		if tracepoint {
			cmd = "T"
		}
		ScheduledBreakpoints = append(ScheduledBreakpoints, fmt.Sprintf("%s%s", cmd, argstr))
		fmt.Fprintf(out, "Breakpoint will be set on restart\n")
		return nil
	}

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
	locs, substSpec, findLocErr := client.FindLocation(currentEvalScope(), locspec, true, nil)
	if findLocErr != nil && requestedBp.Name != "" {
		requestedBp.Name = ""
		locspec = argstr
		var err2 error
		var substSpec2 string
		locs, substSpec2, err2 = client.FindLocation(currentEvalScope(), locspec, true, nil)
		if err2 == nil {
			substSpec = substSpec2
			findLocErr = nil
		}
	}
	if findLocErr != nil && shouldAskToSuspendBreakpoint() {
		wnd.PopupOpen("Set suspended breakpoint?", dynamicPopupFlags, rect.Rect{100, 100, 550, 400}, true, func(w *nucular.Window) {
			w.Row(30).Static(0)
			w.Label("Could not find breakpoint location, set suspended breakpoint?", "LC")
			yes, no := yesno(w)
			switch {
			case yes:
				bp, err := client.CreateBreakpointWithExpr(requestedBp, locspec, nil, true)
				out := &editorWriter{false}
				if err != nil {
					fmt.Fprintf(out, "Could not set breakpoint: %v", err)
				} else {
					fmt.Fprintf(out, "%s set at %s\n", formatBreakpointName(bp, true), formatBreakpointLocation(bp, false))
					go refreshState(refreshToSameFrame, clearBreakpoint, nil)
				}
				w.Close()
			case no:
				w.Close()
			}
		})
	}
	if findLocErr != nil {
		return findLocErr
	}
	if substSpec != "" {
		locspec = substSpec
	}
	for _, loc := range locs {
		requestedBp.Addr = loc.PC
		requestedBp.Addrs = loc.PCs
		requestedBp.AddrPid = loc.PCPids
		setBreakpointEx(out, requestedBp, locspec)
	}
	return nil
}

func shouldAskToSuspendBreakpoint() bool {
	fns, _ := client.ListFunctions(`^plugin\.Open$`, 0)
	_, err := client.GetState()
	return len(fns) > 0 || isErrProcessExited(err) || client.FollowExecEnabled()
}

func isErrProcessExited(err error) bool {
	rpcError, ok := err.(rpc.ServerError)
	return ok && strings.Contains(rpcError.Error(), "has exited with status")
}

func setBreakpointEx(out io.Writer, requestedBp *api.Breakpoint, locspec string) {
	if curThread < 0 {
		switch {
		default:
			fallthrough
		case requestedBp.Addr != 0:
			fmt.Fprintf(out, "error: process exited\n")
			return
		case requestedBp.FunctionName != "":
			ScheduledBreakpoints = append(ScheduledBreakpoints, fmt.Sprintf("B%s", requestedBp.FunctionName))
		case requestedBp.File != "":
			ScheduledBreakpoints = append(ScheduledBreakpoints, fmt.Sprintf("T%s:%d", requestedBp.File, requestedBp.Line))
		}
		fmt.Fprintf(out, "Breakpoint will be set on restart\n")
		return
	}
	bp, err := client.CreateBreakpointWithExpr(requestedBp, locspec, nil, true)
	if err != nil {
		fmt.Fprintf(out, "Could not create breakpoint: %v\n", err)
	}

	fmt.Fprintf(out, "%s set at %s\n", formatBreakpointName(bp, true), formatBreakpointLocation(bp, false))
	if len(bp.Addrs) > 1 {
		fmt.Fprintf(out, "\tother addresses:")
		for _, addr := range bp.Addrs {
			fmt.Fprintf(out, " %#x", addr)
		}
	}
	freezeBreakpoint(out, bp)
}

func listBreakpoints() {
	wnd.Lock()
	defer wnd.Unlock()
	style := wnd.Style()
	c := scrollbackEditor.Append(true)
	defer c.End()
	bps, err := client.ListBreakpoints(false)
	if err != nil {
		c.Text(fmt.Sprintf("Command failed: %v\n", err))
		return
	}
	for _, bp := range bps {
		c.Text(fmt.Sprintf("%s at %#x for ", formatBreakpointName(bp, true), bp.Addr))
		if bp.FunctionName != "" {
			c.Text(fmt.Sprintf("%s()\n        ", bp.FunctionName))
		} else {
			c.Text("\n        ")
		}
		writeLinkToLocation(c, style, bp.File, bp.Line, bp.Addr)
		c.Text(fmt.Sprintf(" (%d)\n", bp.TotalHitCount))
		if bp.Cond != "" {
			c.Text(fmt.Sprintf("\tcond %s\n", bp.Cond))
		}
	}
}

func breakpoint(out io.Writer, args string) error {
	return setBreakpoint(out, false, args)
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
	fmt.Fprintf(out, "%s cleared at %s\n", formatBreakpointName(bp, true), formatBreakpointLocation(bp, false))
	return nil
}

func restart(out io.Writer, args string) error {
	resetArgs := false
	var newArgs []string
	rerecord := false
	args = strings.TrimSpace(args)
	restartCheckpoint := ""
	if args != "" {
		argv := splitQuotedFields(args, '\'')
		if len(argv) > 0 {
			if client != nil && client.Recorded() && argv[0] == "-r" {
				restartCheckpoint = ""
				rerecord = true
				argv = argv[1:]
			}
		}
		if len(argv) > 0 {
			if argv[0] == "--" {
				argv = argv[1:]
			}
			resetArgs = true
			newArgs = argv
		}
	}

	if client != nil && client.Recorded() && !rerecord {
		restartCheckpoint = args
		newArgs = nil
		resetArgs = false
	}

	if rerecord && !delveFeatures.hasRerecord {
		return fmt.Errorf("rerecord unsupported by this version of delve")
	}

	if len(BackendServer.buildcmd) > 0 && (BackendServer.buildcmd[0] == "test") {
		newArgs = addTestPrefix(newArgs)
	}

	if client == nil {
		go pseudoCommandWrap(func(w io.Writer) error {
			return doRebuild(w, resetArgs, newArgs)
		})
		return nil
	}

	if BackendServer.StaleExecutable() && (!client.Recorded() || rerecord) {
		wnd.PopupOpen("Recompile?", dynamicPopupFlags, rect.Rect{100, 100, 550, 400}, true, func(w *nucular.Window) {
			w.Row(30).Static(0)
			w.Label("Executable is stale. Rebuild?", "LC")
			yes, no := yesno(w)

			switch {
			case yes:
				go pseudoCommandWrap(func(w io.Writer) error {
					return doRebuild(w, resetArgs, newArgs)
				})
				w.Close()
			case no:
				go pseudoCommandWrap(func(w io.Writer) error {
					return doRestart(w, restartCheckpoint, resetArgs, newArgs, rerecord)
				})
				w.Close()
			}
		})
		return nil
	}

	return doRestart(out, restartCheckpoint, resetArgs, newArgs, rerecord)
}

func yesno(w *nucular.Window) (yes, no bool) {
	for _, e := range w.Input().Keyboard.Keys {
		switch {
		case e.Code == key.CodeEscape:
			no = true
		case e.Code == key.CodeReturnEnter:
			yes = true
		}
	}
	w.Row(30).Static(0, 100, 100, 0)
	w.Spacing(1)
	if w.ButtonText("Yes") {
		yes = true
	}
	if w.ButtonText("No") {
		no = true
	}
	w.Spacing(1)
	return yes, no
}

func splitQuotedFields(in string, quote rune) []string {
	type stateEnum int
	const (
		inSpace stateEnum = iota
		inField
		inQuote
		inQuoteEscaped
	)
	state := inSpace
	r := []string{}
	var buf bytes.Buffer

	for _, ch := range in {
		switch state {
		case inSpace:
			if ch == quote {
				state = inQuote
			} else if !unicode.IsSpace(ch) {
				buf.WriteRune(ch)
				state = inField
			}

		case inField:
			if ch == quote {
				state = inQuote
			} else if unicode.IsSpace(ch) {
				r = append(r, buf.String())
				buf.Reset()
			} else {
				buf.WriteRune(ch)
			}

		case inQuote:
			if ch == quote {
				state = inField
			} else if ch == '\\' {
				state = inQuoteEscaped
			} else {
				buf.WriteRune(ch)
			}

		case inQuoteEscaped:
			buf.WriteRune(ch)
			state = inQuote
		}
	}

	if buf.Len() != 0 {
		r = append(r, buf.String())
	}

	return r
}

func pseudoCommandWrap(cmd func(io.Writer) error) {
	wnd.Changed()
	defer wnd.Changed()

	out := editorWriter{true}
	err := cmd(&out)
	if err != nil {
		fmt.Fprintf(&out, "Error executing command: %v\n", err)
	}
}

func doRestart(out io.Writer, restartCheckpoint string, resetArgs bool, argsAndRedirects []string, rerecord bool) error {
	args, redirects, err := parseRedirects(argsAndRedirects)
	if err != nil {
		return err
	}
	shouldFinishRestart := client == nil || !client.Recorded() || rerecord
	_, err = client.RestartFrom(rerecord, restartCheckpoint, resetArgs, args, redirects, false)
	if err != nil {
		return err
	}
	if shouldFinishRestart {
		finishRestart(out, true)
	}
	firstStop = true
	refreshState(refreshToFrameZero, clearStop, nil)
	return nil
}

func doRebuild(out io.Writer, resetArgs bool, argsAndRedirects []string) error {
	args, redirects, err := parseRedirects(argsAndRedirects)
	if err != nil {
		return err
	}
	rerecord := client != nil && client.Recorded()

	dorestart := BackendServer.serverProcess != nil
	BackendServer.Rebuild()
	if !dorestart || !BackendServer.buildok {
		return nil
	}

	updateFrozenBreakpoints()
	clearFrozenBreakpoints()

	discarded, err := client.RestartFrom(rerecord, "", resetArgs, args, redirects, false)
	if err != nil {
		fmt.Fprintf(out, "error on restart\n")
		return err
	}
	fmt.Fprintln(out, "Process restarted with PID", client.ProcessPid())
	for i := range discarded {
		fmt.Fprintf(out, "Discarded %s at %s: %v\n", formatBreakpointName(discarded[i].Breakpoint, false), formatBreakpointLocation(discarded[i].Breakpoint, false), discarded[i].Reason)
	}

	restoreFrozenBreakpoints(out)

	finishRestart(out, true)

	refreshState(refreshToFrameZero, clearStop, nil)
	return nil
}

func parseRedirects(w []string) (args []string, redirects [3]string, err error) {
	for len(w) > 0 {
		var found bool
		var err error
		w, found, err = parseOneRedirect(w, &redirects)
		if err != nil {
			return nil, [3]string{}, err
		}
		if !found {
			break
		}
	}
	return w, redirects, nil
}

func parseOneRedirect(w []string, redirs *[3]string) ([]string, bool, error) {
	prefixes := []string{"<", ">", "2>"}
	names := []string{"stdin", "stdout", "stderr"}
	if len(w) >= 2 {
		for _, prefix := range prefixes {
			if w[len(w)-2] == prefix {
				w[len(w)-2] += w[len(w)-1]
				w = w[:len(w)-1]
				break
			}
		}
	}
	for i, prefix := range prefixes {
		if strings.HasPrefix(w[len(w)-1], prefix) {
			if redirs[i] != "" {
				return nil, false, fmt.Errorf("redirect error: %s redirected twice", names[i])
			}
			redirs[i] = w[len(w)-1][len(prefix):]
			return w[:len(w)-1], true, nil
		}
	}
	return w, false, nil
}

func cont(out io.Writer, args string) error {
	stateChan := client.Continue()
	var state *api.DebuggerState
	for state = range stateChan {
		if state.Err != nil {
			refreshState(refreshToFrameZero, clearStop, state)
			return state.Err
		}
		printcontext(out, state)
	}
	refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func call(out io.Writer, args string) error {
	const unsafePrefix = "-unsafe "
	unsafe := false
	if strings.HasPrefix(args, unsafePrefix) {
		unsafe = true
		args = args[len(unsafePrefix):]
	}
	state, err := client.Call(curGid, args, unsafe)
	if err != nil {
		return err
	}
	printcontext(out, state)
	refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func rewind(out io.Writer, args string) error {
	stateChan := client.Rewind()
	var state *api.DebuggerState
	for state = range stateChan {
		if state.Err != nil {
			refreshState(refreshToFrameZero, clearStop, state)
			return state.Err
		}
		printcontext(out, state)
	}
	refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func (c *Commands) reverse(out io.Writer, args string) error {
	v := strings.SplitN(args, " ", 2)
	if len(v) < 1 {
		return fmt.Errorf("rev must be followed by next, step, step-instruction or stepout")
	}

	cmd := c.findCommand(v[0])
	if cmd == nil {
		return fmt.Errorf("unknown command %q", v[0])
	}

	args = ""
	if len(v) > 1 {
		args = v[1]
	}

	const revprefix = "-rev "

	switch cmd.aliases[0] {
	case "next":
		return next(out, revprefix+args)
	case "step":
		return step(out, revprefix+args)
	case "stepout":
		return stepout(out, revprefix+args)
	case "step-instruction":
		return stepInstruction(out, revprefix+args)
	default:
		return fmt.Errorf("rev must be followed by next, step, step-instruction or stepout")
	}

}

type continueAction uint8

const (
	continueActionIgnoreThis continueAction = iota
	continueActionIgnoreAll
	continueActionStopAndCancel
	continueActionStopWithoutCancel
)

func continueUntilCompleteNext(out io.Writer, state *api.DebuggerState, op string, bp *api.Breakpoint) error {
	ignoreAll := false
	if !state.NextInProgress {
		goto continueCompleted
	}
continueLoop:
	for {
		stateChan := client.DirectionCongruentContinue()
		for state = range stateChan {
			if state.Err != nil {
				break continueLoop
			}
			printcontext(out, state)
		}
		if bp != nil {
			for _, th := range state.Threads {
				if th.Breakpoint != nil && th.Breakpoint.ID == bp.ID {
					break continueLoop
				}
			}
		}
		if !state.NextInProgress || conf.StopOnNextBreakpoint {
			break continueLoop
		}

		if ignoreAll {
			fmt.Fprintf(out, "    breakpoint hit during %s, continuing...\n", op)
			continue
		}

		answerChan := make(chan continueAction)
		wnd.PopupOpen("Configuration", dynamicPopupFlags, rect.Rect{100, 100, 600, 700}, true, func(w *nucular.Window) {
			w.Row(20).Dynamic(1)
			w.Label(fmt.Sprintf("Another goroutine hit a breakpoint before '%s' finished.", op), "LC")
			w.Label(fmt.Sprintf("You can either chose to ignore other breakpoints and finish '%s' or to stop here.", op), "LC")
			w.Row(80).Dynamic(1)
			w.LabelWrap(fmt.Sprintf("If you chose to stop here you can either cancel '%s' or suspend it; if you chose  to suspend it you won't be able to 'step', 'next' or 'stepout' until you either     cancel it or complete it.", op))

			w.Row(30).Dynamic(1)
			if w.ButtonText(fmt.Sprintf("continue '%s', ignore this breakpoint", op)) {
				answerChan <- continueActionIgnoreThis
				w.Close()
			}
			if w.ButtonText(fmt.Sprintf("continue '%s', ignore any other breakpoints", op)) {
				answerChan <- continueActionIgnoreAll
				w.Close()
			}
			if w.ButtonText(fmt.Sprintf("stop here, cancel '%s'", op)) {
				answerChan <- continueActionStopAndCancel
				w.Close()
			}
			if w.ButtonText(fmt.Sprintf("stop here, do not cancel '%s'", op)) {
				answerChan <- continueActionStopWithoutCancel
				w.Close()
			}
		})
		switch <-answerChan {
		case continueActionIgnoreThis:
			// nothing to do
		case continueActionIgnoreAll:
			ignoreAll = true
		case continueActionStopAndCancel:
			client.CancelNext()
			break continueLoop
		case continueActionStopWithoutCancel:
			break continueLoop
		}

		fmt.Fprintf(out, "    breakpoint hit during %s, continuing...\n", op)
	}

continueCompleted:
	refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func processRevArg(args string, normal, reverse func() (*api.DebuggerState, error)) (string, func() (*api.DebuggerState, error), bool) {
	const revprefix = "-rev "
	if strings.HasPrefix(args, revprefix) {
		return strings.TrimSpace(args[len(revprefix):]), reverse, true
	}
	return args, normal, false
}

func step(out io.Writer, args string) error {
	getsics := func() ([]stepIntoCall, uint64, error) {
		state, err := client.GetState()
		if err != nil {
			return nil, 0, err
		}
		if curGid < 0 {
			return nil, 0, errors.New("no selected goroutine")
		}
		loc := currentLocation(state)
		if loc == nil {
			return nil, 0, errors.New("could not find current location")
		}
		return stepIntoList(*loc), state.CurrentThread.PC, nil
	}

	args, stepfn, isrev := processRevArg(args, client.Step, client.ReverseStep)

	if isrev && args != "" && args != "-first" {
		return fmt.Errorf("can not reverse step with arguments")
	}

	if args == "" {
		args = conf.DefaultStepBehaviour
	}

	ignoreFrameChange = true

	switch args {
	case "", "-first":
		return stepIntoFirst(out, stepfn)

	case "-last":
		sics, _, _ := getsics()
		if len(sics) > 0 {
			return stepInto(out, sics[len(sics)-1])
		} else {
			return stepIntoFirst(out, client.Step)
		}

	case "-list":
		sics, pc, err := getsics()
		if err != nil {
			return err
		}
		for _, sic := range sics {
			if sic.Inst.Loc.PC >= pc {
				fmt.Fprintf(out, "%s\t%s\n", sic.Name, sic.ExprString())
			}
		}
	default:
		sics, _, err := getsics()
		if err != nil {
			return err
		}
		for _, sic := range sics {
			if sic.Name == args {
				return stepInto(out, sic)
			}
		}
		return fmt.Errorf("could not find call %s", args)
	}
	return nil
}

func stepIntoFirst(out io.Writer, stepfn func() (*api.DebuggerState, error)) error {
	state, err := stepfn()
	if err != nil {
		return err
	}
	printcontext(out, state)
	return continueUntilCompleteNext(out, state, "step", nil)
}

func stepInto(out io.Writer, sic stepIntoCall) error {
	stack, err := client.Stacktrace(curGid, 1, 0, nil)
	if err != nil {
		return err
	}
	if len(stack) < 1 {
		return errors.New("could not stacktrace")
	}
	cond := fmt.Sprintf("(runtime.curg.goid == %d) && (runtime.frameoff == %d)", curGid, stack[0].FrameOffset)
	bp, err := client.CreateBreakpoint(&api.Breakpoint{Addr: sic.Inst.Loc.PC, Cond: cond})
	if err != nil {
		return err
	}

	// we use next here instead of continue so that if for any reason the
	// breakpoint can not be reached (for example a panic or a branch) we will
	// not run the program to completion
	state, err := client.Next()
	if err != nil {
		client.ClearBreakpoint(bp.ID)
		return err
	}
	printcontext(out, state)
	err = continueUntilCompleteNext(out, state, "step", nil)
	client.ClearBreakpoint(bp.ID)
	if err != nil {
		return err
	}
	bpfound := false
	for _, th := range state.Threads {
		if th.Breakpoint != nil && th.Breakpoint.ID == bp.ID {
			bpfound = true
			break
		}
	}
	if bpfound {
		return stepIntoFirst(out, client.Step)
	}
	return nil
}

func stepInstruction(out io.Writer, args string) error {
	args, stepfn, _ := processRevArg(args, func() (*api.DebuggerState, error) { return client.StepInstruction(false) }, func() (*api.DebuggerState, error) { return client.ReverseStepInstruction(false) })
	state, err := stepfn()
	if err != nil {
		return err
	}
	printcontext(out, state)
	refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func next(out io.Writer, args string) error {
	args, stepfn, _ := processRevArg(args, client.Next, client.ReverseNext)
	state, err := stepfn()
	if err != nil {
		return err
	}
	printcontext(out, state)
	return continueUntilCompleteNext(out, state, "next", nil)
}

func nextInstruction(out io.Writer, args string) error {
	args, stepfn, _ := processRevArg(args, func() (*api.DebuggerState, error) { return client.StepInstruction(true) }, func() (*api.DebuggerState, error) { return client.ReverseStepInstruction(true) })
	state, err := stepfn()
	if err != nil {
		return err
	}
	printcontext(out, state)
	refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func stepout(out io.Writer, args string) error {
	args, stepfn, _ := processRevArg(args, client.StepOut, client.ReverseStepOut)
	state, err := stepfn()
	if err != nil {
		return err
	}
	ignoreFrameChange = true
	printcontext(out, state)
	return continueUntilCompleteNext(out, state, "stepout", nil)
}

func cancelnext(out io.Writer, args string) error {
	return client.CancelNext()
}

func interrupt(out io.Writer, args string) error {
	if args == "eof" {
		close(BackendServer.stdinChan)
		return nil
	}
	StarlarkEnv.Cancel()
	state, err := client.GetStateNonBlocking()
	if err == nil && state.Recording {
		return client.StopRecording()
	}
	_, err = client.Halt()
	if err != nil {
		return err
	}
	//refreshState(refreshToFrameZero, clearStop, state)
	return nil
}

func printVar(out io.Writer, args string) error {
	if len(args) == 0 {
		return fmt.Errorf("not enough arguments")
	}
	val, se := evalScopedExpr(args, getVariableLoadConfig(), false)
	if val.Unreadable != "" {
		return errors.New(val.Unreadable)
	}
	valstr := val.MultilineString("", &se.Fmt)
	nlcount := 0
	for _, ch := range valstr {
		if ch == '\n' {
			nlcount++
		}
	}
	if nlcount > 20 && val.Kind != reflect.String {
		fmt.Fprintln(out, "Expression added to variables panel")
		addExpression(args, false)
	} else {
		fmt.Fprintln(out, valstr)
	}

	if val.Kind == reflect.Chan {
		chanGoroutines(val)
	}

	return nil
}

func displayVar(out io.Writer, args string) error {
	addExpression(args, false)
	return nil
}

func detailsVar(out io.Writer, args string) error {
	newDetailViewer(wnd, args)
	return nil
}

func listCommand(out io.Writer, args string) error {
	locs, _, err := client.FindLocation(currentEvalScope(), args, false, nil)
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
	return client.SetVariable(currentEvalScope(), lexpr, rexpr)
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

func checkpoint(out io.Writer, args string) error {
	if args == "" {
		state, err := client.GetState()
		if err != nil {
			return err
		}
		var loc api.Location = api.Location{PC: state.CurrentThread.PC, File: state.CurrentThread.File, Line: state.CurrentThread.Line, Function: state.CurrentThread.Function}
		if state.SelectedGoroutine != nil {
			loc = state.SelectedGoroutine.CurrentLoc
		}
		args = fmt.Sprintf("%s() %s:%d (%#x)", loc.Function.Name(), loc.File, loc.Line, loc.PC)
	}

	cpid, err := client.Checkpoint(args)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "Checkpoint c%d created.\n", cpid)
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
	return nil
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

		conf.Layouts[name] = LayoutDescr{Description: description, Layout: serializeLayout()}
		saveConfiguration()
	default:
		ld, ok := conf.Layouts[argv[0]]
		if !ok {
			return fmt.Errorf("unknown layout %q", argv[0])
		}
		loadPanelDescrToplevel(ld.Layout)
		wnd.Changed()
	}
	return nil
}

func configCommand(out io.Writer, args string) error {
	const (
		aliasPrefix = "alias "
		zoomPrefix  = "zoom "
	)
	switch {
	case strings.HasPrefix(args, aliasPrefix):
		return configureSetAlias(strings.TrimSpace(args[len(aliasPrefix):]))
	case strings.HasPrefix(args, zoomPrefix):
		s, err := strconv.ParseFloat(strings.TrimSpace(args[len(zoomPrefix):]), 64)
		if err != nil {
			return err
		}
		conf.Scaling = s
		setupStyle()
		return nil
	}
	cw := newConfigWindow()
	wnd.PopupOpen("Configuration", dynamicPopupFlags, rect.Rect{100, 100, 600, 700}, true, cw.Update)
	return nil
}

func configureSetAlias(rest string) error {
	argv := splitQuotedFields(rest, '"')
	switch len(argv) {
	case 1: // delete alias rule
		for i := range cmds.cmds {
			cmd := &cmds.cmds[i]
			for i := range cmd.aliases {
				if cmd.aliases[i] == argv[0] {
					cmd.aliases = append(cmd.aliases[:i], cmd.aliases[i+1:]...)
					return nil
				}
			}
		}
		return fmt.Errorf("could not find command %q", argv[0])
	case 2: // add alias rule
		alias, cmd := argv[1], argv[0]
		for i := range cmds.cmds {
			for _, ka := range cmds.cmds[i].aliases {
				if ka == cmd {
					cmds.cmds[i].aliases = append(cmds.cmds[i].aliases, alias)
					return nil
				}
			}
		}
		return fmt.Errorf("could not find command %q", cmd)
	}
	return fmt.Errorf("wrong number of arguments")
}

type configWindow struct {
	selectedSubstitutionRule int
	from                     nucular.TextEditor
	to                       nucular.TextEditor
}

func newConfigWindow() *configWindow {
	return &configWindow{
		selectedSubstitutionRule: -1,
		from:                     nucular.TextEditor{Flags: nucular.EditSelectable | nucular.EditClipboard},
		to:                       nucular.TextEditor{Flags: nucular.EditSelectable | nucular.EditClipboard},
	}
}

func (cw *configWindow) Update(w *nucular.Window) {
	const col1 = 160
	w.Row(20).Static(col1, 200)
	w.Label("Theme:", "LC")
	if conf.Theme == "" {
		conf.Theme = darkTheme
	}
	if w := w.Combo(label.TA(conf.Theme, "LC"), 500, nil); w != nil {
		w.Row(20).Dynamic(1)
		for _, theme := range themes {
			if w.MenuItem(label.TA(theme, "LC")) {
				conf.Theme = theme
				setupStyle()
			}
		}
	}

	w.Row(20).Static(col1, 0)
	w.Label("Display zoom:", "LC")
	if w.PropertyFloat("Zoom", 0.2, &conf.Scaling, 4.0, 0.1, 0.1, 2) {
		setupStyle()
	}

	w.Row(20).Static(col1, 150)
	w.Label("Startup function:", "LC")
	stringCombo(w, []string{"main.main", "runtime.main"}, &conf.StartupFunc)

	w.Label("Disassembly Flavor:", "LC")
	disassfl := []string{"Intel", "GNU"}
	conf.DisassemblyFlavour = w.ComboSimple(disassfl, conf.DisassemblyFlavour, 20)

	w.Row(20).Dynamic(1)
	w.Label("When a breakpoint is hit during next/step/stepout gdlv should:", "LC")
	w.Row(20).Static(col1, 200)
	w.Spacing(1)
	breakb := []string{"Ask what to do", "Stop"}
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

	w.Row(20).Static()
	w.LayoutFitWidth(0, 100)
	w.Label("Default step behavior:", "LC")
	w.LayoutSetWidth(200)
	stringCombo(w, []string{"-first", "-last"}, &conf.DefaultStepBehaviour)

	if conf.MaxArrayValues == 0 {
		conf.MaxArrayValues = LongLoadConfig.MaxArrayValues
	}
	if conf.MaxStringLen == 0 {
		conf.MaxStringLen = LongLoadConfig.MaxStringLen
	}

	w.Row(30).Static(0)

	w.Row(30).Static(200, 200)
	w.Label("Load configuration:", "LC")
	w.PropertyInt("Max array load:", 1, &conf.MaxArrayValues, 4096, 1, 1)
	w.Row(30).Static(200, 200)
	w.Spacing(1)
	w.PropertyInt("Max string load:", 1, &conf.MaxStringLen, 4096, 1, 1)

	w.Row(30).Static(0)
	if w.TreePush(nucular.TreeTab, "Path substitutions:", false) {
		w.Row(240).Static(0, 200)
		if w := w.GroupBegin("path-substitution-list", nucular.WindowNoHScrollbar); w != nil {
			w.Row(30).Static(0)
			if len(conf.SubstitutePath) == 0 {
				w.Label("(no substitution rules)", "LC")
			}
			for i, r := range conf.SubstitutePath {
				s := cw.selectedSubstitutionRule == i
				w.SelectableLabel(fmt.Sprintf("%s -> %s", r.From, r.To), "LC", &s)
				if s {
					cw.selectedSubstitutionRule = i
				}
			}
			w.GroupEnd()
		}
		if w := w.GroupBegin("path-substitution-controls", nucular.WindowNoScrollbar); w != nil {
			w.Row(30).Static(0)
			if w.ButtonText("Remove") && cw.selectedSubstitutionRule >= 0 && cw.selectedSubstitutionRule < len(conf.SubstitutePath) {
				copy(conf.SubstitutePath[cw.selectedSubstitutionRule:], conf.SubstitutePath[cw.selectedSubstitutionRule+1:])
				conf.SubstitutePath = conf.SubstitutePath[:len(conf.SubstitutePath)-1]
				cw.selectedSubstitutionRule = -1
				saveConfiguration()
			}
			if w.ButtonText("Guess configuration") {
				rules, err := client.GuessSubstitutePath()
				if err != nil {
					fmt.Fprintf(&editorWriter{true}, "Could not guess configuration: %v\n", err)
				} else {
					conf.SubstitutePath = conf.SubstitutePath[:0]
					for _, rule := range rules {
						conf.SubstitutePath = append(conf.SubstitutePath, SubstitutePathRule{From: rule[0], To: rule[1]})
					}
					saveConfiguration()
				}
			}
			w.GroupEnd()
		}
		w.Row(30).Static(0)
		w.Label("New rule:", "LC")
		w.Row(30).Static(50, 150, 50, 150, 80)
		w.Label("From:", "LC")
		cw.from.Edit(w)
		w.Label("To:", "LC")
		cw.to.Edit(w)
		if w.ButtonText("Add") {
			conf.SubstitutePath = append(conf.SubstitutePath, SubstitutePathRule{From: string(cw.from.Buffer), To: string(cw.to.Buffer)})
			cw.from.Buffer = cw.from.Buffer[:0]
			cw.to.Buffer = cw.to.Buffer[:0]
			saveConfiguration()
		}

		w.TreePop()
	}

	w.Row(20).Static(0, 100)
	w.Spacing(1)
	if w.ButtonText("OK") {
		saveConfiguration()
		w.Close()
	}
}

func stringCombo(w *nucular.Window, values []string, value *string) {
	i0 := 0
	for i := range values {
		if values[i] == *value {
			i0 = i
			break
		}
	}
	i := w.ComboSimple(values, i0, 20)
	if i >= 0 {
		*value = values[i]
	}

}

func scrollCommand(out io.Writer, args string) error {
	switch args {
	case "clear":
		wnd.Lock()
		scrollbackClear = true
		wnd.Unlock()
	case "silence":
		wnd.Lock()
		silenced = true
		wnd.Unlock()
		fmt.Fprintf(out, "Inferior output silenced\n")
	case "noise":
		wnd.Lock()
		silenced = false
		wnd.Unlock()
		fmt.Fprintf(out, "Inferior output enabled\n")
	default:
		wnd.Lock()
		s := silenced
		wnd.Unlock()
		if s {
			fmt.Fprintf(out, "Inferior output is silenced\n")
		} else {
			fmt.Fprintf(out, "Inferior output is not silenced\n")
		}
	}
	return nil
}

func windowCommand(out io.Writer, args string) error {
	args = strings.ToLower(strings.TrimSpace(args))
	if args == "styled" {
		styled.EditStyle(wnd, nucular.WindowNonmodal|nucular.WindowClosable, func(out string) {
			fh, err := os.Create("boring-style.go")
			if err != nil {
				fmt.Fprintf(os.Stderr, "error creating boring-style.go: %v", err)
				return
			}
			defer fh.Close()
			fmt.Fprintf(fh, `package main
			
import (
	"image/color"
	"image"

	lbl "github.com/aarzilli/nucular/label"
	nstyle "github.com/aarzilli/nucular/style"
)

func makeBoringStyle() *nstyle.Style {
	style := &nstyle.Style{}

%s
	
	return style
}
`, out)
		})
		return nil
	}
	foundw := ""
	for _, w := range infoModes {
		if strings.ToLower(w) == args {
			openWindow(w)
			return nil
		}
		if strings.HasPrefix(strings.ToLower(w), args) {
			if foundw != "" {
				return fmt.Errorf("unknown window kind %q", args)
			}
			foundw = w
		}
	}
	if foundw != "" {
		openWindow(foundw)
		return nil
	}
	return fmt.Errorf("unknown window kind %q", args)
}

func sourceCommand(out io.Writer, args string) error {
	defer refreshState(refreshToFrameZero, clearStop, nil)

	scriptRunning = true
	wnd.Changed()
	defer func() {
		scriptRunning = false
		wnd.Changed()
	}()

	if len(args) == 0 {
		return fmt.Errorf("wrong number of arguments: source <filename>")
	}

	if args == "-" {
		starlarkMode = make(chan string)
		promptChan := make(chan string)
		go func() {
			for pmpt := range promptChan {
				wnd.Lock()
				starlarkPrompt = pmpt
				wnd.Unlock()
				wnd.Changed()
			}
			wnd.Lock()
			starlarkMode = nil
			wnd.Unlock()
			wnd.Changed()
		}()
		go StarlarkEnv.REPL(out, starlarkMode, promptChan)
		return nil
	}

	v, err := StarlarkEnv.Execute(out, expandTilde(args), nil, "main", nil, nil)
	if err != nil {
		return err
	}
	if v != nil {
		fmt.Fprintf(out, "%v\n", v.String())
	}
	return nil
}

func stackCommand(out io.Writer, args string) error {
	depth, err := strconv.Atoi(args)
	if err != nil {
		depth = 5
	}
	frames, err := client.Stacktrace(curGid, depth, stacktraceOptions(), nil)
	if err != nil {
		return err
	}
	printStack(nil, frames, "")
	ancestors, err := client.Ancestors(curGid, NumAncestors, depth)
	if err != nil {
		return err
	}
	for _, ancestor := range ancestors {
		fmt.Fprintf(out, "Created by Goroutine %d:\n", ancestor.ID)
		if ancestor.Unreadable != "" {
			fmt.Fprintf(out, "\t%s\n", ancestor.Unreadable)
			continue
		}
		printStack(nil, ancestor.Stack, "        ")
	}
	return nil
}

func goroutinesCommand(out io.Writer, args string) error {
	wnd.Lock()
	defer wnd.Unlock()
	c := scrollbackEditor.Append(true)
	defer c.End()

	lim := goroutinesPanel.limit
	if lim == 0 {
		lim = 100
	}
	gs, _, err := client.ListGoroutines(0, lim)
	if err != nil {
		return err
	}

	printGoroutines(c, gs)

	return nil
}

func printGoroutines(c *richtext.Ctor, gs []*api.Goroutine) {
	style := wnd.Style()
	for _, g := range gs {
		if g.ID == curGid {
			c.Text("* ")
		} else {
			c.Text("  ")
		}
		locType := ""
		switch goroutineLocations[goroutinesPanel.goroutineLocation] {
		default:
			fallthrough
		case currentGoroutineLocation:
			locType = "Current"
		case userGoroutineLocation:
			locType = "User"
		case goStatementLocation:
			locType = "Go"
		case startLocation:
			locType = "Start"
		}
		loc := goroutineGetDisplayLiocation(g)
		gid := g.ID
		writeLink(c, style, fmt.Sprintf("Goroutine %d", g.ID), func() {
			state, err := client.SwitchGoroutine(gid)
			if err != nil {
				fmt.Fprintf(&editorWriter{true}, "Could not switch goroutine: %v\n", err)
			} else {
				go refreshState(refreshToUserFrame, clearGoroutineSwitch, state)
			}
		})
		c.Text(fmt.Sprintf(" - %s: ", locType))
		writeLinkToLocation(c, style, loc.File, loc.Line, loc.PC)
		c.Text(fmt.Sprintf(" %s (%#x)", loc.Function.Name(), loc.PC))

		if g.ThreadID != 0 {
			c.Text(fmt.Sprintf(" (thread %d)", g.ThreadID))
		}

		if wr := goroutineFormatWaitReason(g); wr != "" {
			c.Text(fmt.Sprintf(" [%s]", wr))
		}

		c.Text("\n")
	}
}

func goroutineFormatWaitReason(g *api.Goroutine) string {
	if !((g.Status == api.GoroutineWaiting || g.Status == api.GoroutineSyscall) && g.WaitReason != 0) {
		return ""
	}

	var wr string
	if g.WaitReason > 0 && g.WaitReason < int64(len(waitReasonStrings)) {
		wr = waitReasonStrings[g.WaitReason]
	} else {
		wr = fmt.Sprintf("unknown wait reason %d", g.WaitReason)
	}
	if g.WaitSince > 0 {
		return fmt.Sprintf("%s %s", wr, time.Since(time.Unix(0, g.WaitSince)).String())
	}

	return wr
}

var waitReasonStrings = [...]string{
	"",
	"GC assist marking",
	"IO wait",
	"chan receive (nil chan)",
	"chan send (nil chan)",
	"dumping heap",
	"garbage collection",
	"garbage collection scan",
	"panicwait",
	"select",
	"select (no cases)",
	"GC assist wait",
	"GC sweep wait",
	"GC scavenge wait",
	"chan receive",
	"chan send",
	"finalizer wait",
	"force gc (idle)",
	"semacquire",
	"sleep",
	"sync.Cond.Wait",
	"timer goroutine (idle)",
	"trace reader (blocked)",
	"wait for GC cycle",
	"GC worker (idle)",
	"preempted",
	"debug call",
}

func dump(out io.Writer, args string) error {
	var mu sync.Mutex
	dumpState, err := client.CoreDumpStart(args)
	if err != nil {
		return err
	}

	wnd.PopupOpen("Dumping", dynamicPopupFlags, rect.Rect{100, 100, 400, 700}, true, func(w *nucular.Window) {
		mu.Lock()
		defer mu.Unlock()
		w.Row(20).Dynamic(1)
		if dumpState.ThreadsDone != dumpState.ThreadsTotal {
			w.Label("Dumping threads...", "LT")
			n := int(dumpState.ThreadsDone)
			w.Progress(&n, int(dumpState.ThreadsTotal), false)
		} else {
			w.Label("Dumping memory...", "LT")
			n := int(dumpState.MemDone)
			w.Progress(&n, int(dumpState.MemTotal), false)
		}
		w.Row(20).Static(0, 100)
		w.Spacing(1)
		if w.ButtonText("Cancel Dump") {
			client.CoreDumpCancel()
			w.Close()
		}
		if !dumpState.Dumping {
			w.Close()
		}
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			ds := client.CoreDumpWait(100)
			mu.Lock()
			dumpState = ds
			mu.Unlock()
			if !dumpState.Dumping {
				return
			}
		}
	}()

	<-done

	if dumpState.Err != "" {
		fmt.Fprintf(out, "error dumping: %s\n", dumpState.Err)
	} else if !dumpState.AllDone {
		fmt.Fprintf(out, "canceled\n")
	} else if dumpState.MemDone != dumpState.MemTotal {
		fmt.Fprintf(out, "core dump could not be completed\n")
	} else {
		fmt.Fprintf(out, "done\n")
	}

	return nil
}

func watchpoint(out io.Writer, args string) error {
	v := strings.SplitN(args, " ", 2)
	if len(v) != 2 {
		return errors.New("wrong number of arguments: watch [-r|-w|-rw] <expr>")
	}
	var wtype api.WatchType
	switch v[0] {
	case "-r":
		wtype = api.WatchRead
	case "-w":
		wtype = api.WatchWrite
	case "-rw":
		wtype = api.WatchRead | api.WatchWrite
	default:
		return fmt.Errorf("wrong argument %q to watch", v[0])
	}
	bp, err := client.CreateWatchpoint(currentEvalScope(), v[1], wtype)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "%s set at %s\n", formatBreakpointName(bp, true), formatBreakpointLocation(bp, false))
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
	return nil
}

func split2PartsBySpace(s string) []string {
	v := strings.SplitN(s, " ", 2)
	for i := range v {
		v[i] = strings.TrimSpace(v[i])
	}
	return v
}

func target(out io.Writer, args string) error {
	argv := split2PartsBySpace(args)
	switch argv[0] {
	case "list":
		tgts, err := client.ListTargets()
		if err != nil {
			return err
		}
		w := new(tabwriter.Writer)
		w.Init(out, 4, 4, 2, ' ', 0)
		for _, tgt := range tgts {
			selected := ""
			if tgt.Pid == curPid {
				selected = "*"
			}
			fmt.Fprintf(w, "%s\t%d\t%s\n", selected, tgt.Pid, tgt.CmdLine)
		}
		w.Flush()
		return nil
	case "follow-exec":
		if len(argv) == 1 {
			if client.FollowExecEnabled() {
				fmt.Fprintf(out, "Follow exec is enabled.\n")
			} else {
				fmt.Fprintf(out, "Follow exec is disabled.\n")
			}
			return nil
		}
		argv = split2PartsBySpace(argv[1])
		switch argv[0] {
		case "-on":
			var regex string
			if len(argv) == 2 {
				regex = argv[1]
			}
			client.FollowExec(true, regex)
		case "-off":
			if len(argv) > 1 {
				return errors.New("too many arguments")
			}
			client.FollowExec(false, "")
		default:
			return fmt.Errorf("unknown argument %q to 'target follow-exec'", argv[0])
		}
		return nil
	case "switch":
		tgts, err := client.ListTargets()
		if err != nil {
			return err
		}
		pid, err := strconv.Atoi(argv[1])
		if err != nil {
			return err
		}
		found := false
		for _, tgt := range tgts {
			if tgt.Pid == pid {
				found = true
				client.SwitchThread(tgt.CurrentThread.ID)
			}
		}
		if !found {
			return fmt.Errorf("could not find target %d", pid)
		}
		return nil
	case "":
		return errors.New("not enough arguments for 'target'")
	default:
		return fmt.Errorf("unknown command 'target %s'", argv[0])
	}
}

func libraries(out io.Writer, args string) error {
	libs, err := client.ListDynamicLibraries()
	if err != nil {
		return err
	}
	d := digits(len(libs))
	for i := range libs {
		fmt.Fprintf(out, "%"+strconv.Itoa(d)+"d. %#x %s\n", i, libs[i].Address, libs[i].Path)
		if libs[i].LoadError != "" {
			fmt.Fprintf(out, "    Load error: %s", libs[i].LoadError)
		}
	}
	return nil
}

func formatBreakpointName(bp *api.Breakpoint, upcase bool) string {
	thing := "breakpoint"
	if bp.Tracepoint {
		thing = "tracepoint"
	}
	if bp.WatchExpr != "" {
		thing = "watchpoint"
	}
	if upcase {
		thing = strings.Title(thing)
	}
	id := bp.Name
	if id == "" {
		id = strconv.Itoa(bp.ID)
	}
	if bp.WatchExpr != "" && bp.WatchExpr != bp.Name {
		return fmt.Sprintf("%s %s on [%s]", thing, id, bp.WatchExpr)
	}
	return fmt.Sprintf("%s %s", thing, id)
}

func formatBreakpointName2(bp *api.Breakpoint) string {
	if bp.Name != "" {
		return bp.Name
	}
	return strconv.Itoa(bp.ID)
}

func formatBreakpointLocation(bp *api.Breakpoint, shortenFnName bool) string {
	p := ShortenFilePath(bp.File)
	if bp.FunctionName != "" {
		fnname := bp.FunctionName
		if shortenFnName {
			fnname = prettyprint.ShortenType(fnname)
		}
		return fmt.Sprintf("%#v for %s() %s:%d", bp.Addr, fnname, p, bp.Line)
	}
	return fmt.Sprintf("%#v for %s:%d", bp.Addr, p, bp.Line)
}

func printcontext(out io.Writer, state *api.DebuggerState) error {
	if LogOutputNice != nil {
		logf("Threads:\n")
		for i := range state.Threads {
			fmt.Fprintf(LogOutputNice, "\tcurrent:%v id:%d gid:%d breakpoint:%#v\n", state.Threads[i].ID == state.CurrentThread.ID, state.Threads[i].ID, state.Threads[i].GoroutineID, state.Threads[i].Breakpoint)
		}
	}

	if !onNewline {
		out.Write([]byte{'\n'})
	}

	for i := range state.Threads {
		if (state.CurrentThread != nil) && (state.Threads[i].ID == state.CurrentThread.ID) {
			continue
		}
		if state.Threads[i].Breakpoint != nil {
			printcontextThread(state.Threads[i])
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

	printcontextThread(state.CurrentThread)

	return nil
}

func printReturnValues(c *richtext.Ctor, th *api.Thread) {
	if len(th.ReturnValues) == 0 {
		return
	}
	c.Text("Values returned:\n")
	for _, v := range th.ReturnValues {
		c.Text(fmt.Sprintf("\t%s: %s\n", v.Name, wrapApiVariableSimple(&v).MultilineString("\t", nil)))
	}
	c.Text("\n")
}

func printcontextThread(th *api.Thread) {
	wnd.Lock()
	defer wnd.Unlock()
	style := wnd.Style()
	c := scrollbackEditor.Append(true)
	defer c.End()

	fn := th.Function

	if th.Breakpoint == nil {
		c.Text(fmt.Sprintf("> %s() ", fn.Name()))
		writeLinkToLocation(c, style, th.File, th.Line, th.PC)
		c.Text(fmt.Sprintf(" (PC %#x)\n", th.PC))
		printReturnValues(c, th)
		return
	}

	args := ""
	if th.BreakpointInfo != nil && th.Breakpoint.LoadArgs != nil && *th.Breakpoint.LoadArgs == ShortLoadConfig {
		var arg []string
		for _, ar := range th.BreakpointInfo.Arguments {
			arg = append(arg, wrapApiVariableSimple(&ar).SinglelineString(true, true))
		}
		args = strings.Join(arg, ", ")
	}

	bpname := ""
	if th.Breakpoint.Name != "" {
		bpname = fmt.Sprintf("[%s] ", th.Breakpoint.Name)
	}

	c.Text(fmt.Sprintf("> %s%s(%s) ", bpname, fn.Name(), args))
	writeLinkToLocation(c, style, th.File, th.Line, th.PC)
	if hitCount, ok := th.Breakpoint.HitCount[strconv.FormatInt(th.GoroutineID, 10)]; ok {
		c.Text(fmt.Sprintf(" (hits goroutine(%d):%d total:%d) (PC: %#v)\n",
			th.GoroutineID,
			hitCount,
			th.Breakpoint.TotalHitCount,
			th.PC))
	} else {
		c.Text(fmt.Sprintf(" (hits total:%d) (PC: %#v)\n", th.Breakpoint.TotalHitCount, th.PC))
	}
	if th.Function != nil && th.Function.Optimized {
		c.Text(optimizedFunctionWarning)
		c.Text("\n")
	}

	printReturnValues(c, th)

	if th.BreakpointInfo != nil {
		bp := th.Breakpoint
		bpi := th.BreakpointInfo

		if bpi.Goroutine != nil {
			writeGoroutineLong(os.Stdout, bpi.Goroutine, "\t")
		}

		for _, v := range bpi.Variables {
			c.Text(fmt.Sprintf("    %s: %s\n", v.Name, wrapApiVariableSimple(&v).MultilineString("        ", nil)))
		}

		for _, v := range bpi.Locals {
			if *bp.LoadLocals == LongLoadConfig {
				c.Text(fmt.Sprintf("    %s: %s\n", v.Name, wrapApiVariableSimple(&v).MultilineString("        ", nil)))
			} else {
				c.Text(fmt.Sprintf("    %s: %s\n", v.Name, wrapApiVariableSimple(&v).SinglelineString(true, true)))
			}
		}

		if bp.LoadArgs != nil && *bp.LoadArgs == LongLoadConfig {
			for _, v := range bpi.Arguments {
				c.Text(fmt.Sprintf("    %s: %s\n", v.Name, wrapApiVariableSimple(&v).MultilineString("        ", nil)))
			}
		}

		if bpi.Stacktrace != nil {
			c.Text("    Stack:\n")
			printStack(nil, bpi.Stacktrace, "        ")
		}
	}
}

func formatLocation(loc api.Location) string {
	return fmt.Sprintf("%s at %s:%d (%#v)", loc.Function.Name(), ShortenFilePath(loc.File), loc.Line, loc.PC)
}

func writeGoroutineLong(w io.Writer, g *api.Goroutine, prefix string) {
	fmt.Fprintf(w, "%sGoroutine %d:\n%s\tRuntime: %s\n%s\tUser: %s\n%s\tGo: %s\n",
		prefix, g.ID,
		prefix, formatLocation(g.CurrentLoc),
		prefix, formatLocation(g.UserCurrentLoc),
		prefix, formatLocation(g.GoStatementLoc))
}

func writeLink(c *richtext.Ctor, style *style.Style, text string, fn func()) {
	c.SetStyle(richtext.TextStyle{Face: style.Font, Color: linkColor, Flags: richtext.Underline})
	c.Link(text, linkHoverColor, fn)
	c.SetStyle(richtext.TextStyle{Face: style.Font, Cursor: font.TextCursor})
}

func writeLinkToLocation(c *richtext.Ctor, style *style.Style, file string, line int, pc uint64) {
	writeLink(c, style, fmt.Sprintf("%s:%d", ShortenFilePath(file), line), func() {
		listingPanel.pinnedLoc = &api.Location{File: file, Line: line, PC: pc}
		go refreshState(refreshToSameFrame, clearNothing, nil)
	})
}

func printStack(c *richtext.Ctor, stack []api.Stackframe, ind string) {
	if c == nil {
		wnd.Lock()
		defer wnd.Unlock()
		c = scrollbackEditor.Append(true)
		defer c.End()
	}
	if len(stack) == 0 {
		return
	}
	d := digits(len(stack) - 1)
	fmtstr := "%s%" + strconv.Itoa(d) + "d  0x%016x in %s\n%sat "
	s := ind + strings.Repeat(" ", d+2+len(ind))

	style := wnd.Style()

	for i := range stack {
		c.Text(fmt.Sprintf(fmtstr, ind, i, stack[i].PC, stack[i].Function.Name(), s))
		writeLinkToLocation(c, style, stack[i].File, stack[i].Line, stack[i].PC)
		c.Text("\n")

		for j := range stack[i].Arguments {
			c.Text(fmt.Sprintf("%s    %s = %s\n", s, stack[i].Arguments[j].Name, wrapApiVariableSimple(&stack[i].Arguments[j]).SinglelineString(true, true)))
		}
		for j := range stack[i].Locals {
			c.Text(fmt.Sprintf("%s    %s = %s\n", s, stack[i].Locals[j].Name, wrapApiVariableSimple(&stack[i].Locals[j]).SinglelineString(true, true)))
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
	wnd.Changed()
	defer wnd.Changed()

	logf("Command: %s", cmdstr)

	out := editorWriter{true}
	cmdstr, args := parseCommand(cmdstr)
	if err := cmds.Call(cmdstr, args, &out); err != nil {
		if _, ok := err.(ExitRequestError); ok {
			handleExitRequest()
			return
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

// handleExitRequest prompts what to do about a multiclient server (if the
// server is multiclient), what to do about the target process (if we
// attached to it) and then exits.
func handleExitRequest() {
	if client != nil && curThread >= 0 && client.IsMulticlient() {
		wnd.PopupOpen("Quit Action", dynamicPopupFlags, rect.Rect{100, 100, 500, 700}, true, func(w *nucular.Window) {
			w.Row(20).Dynamic(1)
			if w.ButtonText("Disconnect from headless instance, resume target process") {
				client.Disconnect(true)
				go func() {
					BackendServer.Close()
					wnd.Close()
				}()
			}
			if w.ButtonText("Disconnect from headless instance, leave target suspended") {
				client.Disconnect(false)
				go func() {
					BackendServer.Close()
					wnd.Close()
				}()
			}
			if w.ButtonText("Kill headless instance") {
				handleExitRequest2()
				w.Close()
			}
		})
		return
	}

	handleExitRequest2()
}

// handleExitRequest2, part two of handleExitRequest. Prompts about what to do about the target process (if we attached to it) and then exits.
func handleExitRequest2() {
	if client == nil || curThread < 0 || !client.AttachedToExistingProcess() {
		if client != nil {
			client.Detach(true)
		}
		BackendServer.Close()
		wnd.Close()
		return
	}

	wnd.PopupOpen("Confirm Quit", dynamicPopupFlags, rect.Rect{100, 100, 400, 700}, true, func(w *nucular.Window) {
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
			go func() {
				BackendServer.Close()
				wnd.Close()
			}()
		}
		w.Spacing(1)
	})
}

func parseCommand(cmdstr string) (string, string) {
	vals := strings.SplitN(strings.TrimSpace(cmdstr), " ", 2)
	if len(vals) == 1 {
		return vals[0], ""
	}
	return vals[0], strings.TrimSpace(vals[1])
}

func (c *Commands) findCommand(cmdstr string) *command {
	for i := range c.cmds {
		v := &c.cmds[i]
		if v.match(cmdstr) {
			return v
		}
	}
	return nil
}

// Find will look up the command function for the given command input.
// If it cannot find the command it will default to noCmdAvailable().
// If the command is an empty string it will replay the last command.
func (c *Commands) Find(cmdstr string) cmdfunc {
	if v := c.findCommand(cmdstr); v != nil {
		return v.cmdFn
	}

	return func(out io.Writer, argstr string) error {
		return fmt.Errorf("command %q not available", cmdstr)
	}
}

func (c *Commands) Call(cmdstr, args string, out io.Writer) error {
	return c.Find(cmdstr)(out, args)
}

func doCommand(cmd string) {
	var scrollbackOut = editorWriter{false}
	fmt.Fprintf(&scrollbackOut, "%s %s\n", currentPrompt(), cmd)
	go executeCommand(cmd)
}

func continueToLine(file string, lineno int) {
	out := editorWriter{true}
	bp, err := client.CreateBreakpoint(&api.Breakpoint{File: file, Line: lineno})
	if err != nil {
		fmt.Fprintf(&out, "Could not continue to specified line, could not create breakpoint: %v\n", err)
		return
	}
	state, err := client.StepOut()
	if err != nil {
		fmt.Fprintf(&out, "Could not continue to specified line, could not step out: %v\n", err)
		return
	}
	printcontext(&out, state)
	err = continueUntilCompleteNext(&out, state, "continue-to-line", bp)
	client.ClearBreakpoint(bp.ID)
	client.CancelNext()
	refreshState(refreshToSameFrame, clearBreakpoint, nil)
	if err != nil {
		fmt.Fprintf(&out, "Could not continue to specified line, could not step out: %v\n", err)
		return
	}
}

func getVariableLoadConfig() api.LoadConfig {
	cfg := LongLoadConfig
	if conf.MaxArrayValues > 0 {
		cfg.MaxArrayValues = conf.MaxArrayValues
	}
	if conf.MaxStringLen > 0 {
		cfg.MaxStringLen = conf.MaxStringLen
	}
	return cfg
}

func chanGoroutines(v *Variable) {
	scope := currentEvalScope()
	gs, _, _, _, err := client.ListGoroutinesWithFilter(0, 100, []api.ListGoroutinesFilter{{Kind: api.GoroutineWaitingOnChannel, Arg: fmt.Sprintf("*(*%q)(%#x)", v.Type, v.Addr)}}, nil, &scope)
	if err != nil {
		fmt.Fprintf(&editorWriter{true}, "error getting list of goroutines for channel: %v", err)
		return
	}
	c := scrollbackEditor.Append(true)
	defer c.End()

	if v.Expression != "" && len(v.Expression) < 30 {
		c.Text(fmt.Sprintf("Goroutines waiting on channel %s:\n", v.Expression))
	} else {
		c.Text("Goroutines waiting on channel:\n")
	}
	printGoroutines(c, gs)
}
