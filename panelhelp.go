package main

import (
	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/rect"
	"github.com/aarzilli/nucular/richtext"
)

func showHelp(mw nucular.MasterWindow, title, help string) {
	ed := richtext.New(richtext.Selectable | richtext.AutoWrap | richtext.Clipboard)
	mw.PopupOpen(title, popupFlags|nucular.WindowDynamic|nucular.WindowClosable, rect.Rect{100, 100, 550, 1000}, true, func(w *nucular.Window) {
		if c := ed.Rows(w, false); c != nil {
			c.Align(richtext.AlignLeft)
			c.Text(help)
			c.Text("\n\n")
			c.End()
		}
		w.Row(30).Static(0, 100, 0)
		w.Spacing(1)
		if w.ButtonText("Close") {
			w.Close()
		}
	})
}

var listingPanelHelp = `Displays current listing:
- first column: a red sign if a breakpoint is set on that line, the sign is
  dimmed for disabled breakpoints.
- second column: a yellow arrow for the current line of the topmost frame of
  the current goroutine.
- third coulumn: line number
- fourth column: line of source code.

Right click to set or edit breakpoints.`

var disassemblyPanelHelp = `Displays current disassembly. A yellow arrow marks the current instruction.
Dimmed lines are unreachable from the current line.`

var goroutinesPanelHelp = `Shows the list of all goroutines. The "Limit" field specifies the maximum
number of goroutines to show.

Columns from left to right are:

- a red sign for goroutines stopped on a breakpoint
- goroutine ID
- thread ID for goroutines currently running on a thread
- goroutine location (function, file and line)

The goroutine location is determined by the dropdown menu:

- Current location: topmost frame
- User location: topmost frame that isn't inside the runtime.
- Go statement location: location of the go statement that started the
  goroutine
- Start location: location of the first instruction executed by the
  goroutine.

Clicking on a goroutine switches to it.`

var stacktracePanelHelp = `Shows current stack trace.
Columns from left to right:

- frame number
- PC address (first line) and frame offset from the bottom of the stack
  (second line)
- frame location (function, file and line)
`

var threadsPanelHelp = `List of threads.
Columns from left to right:

- thread ID
- thread location (function, file and line)`

var localsPanelHelp = `Shows local variables and display expressions.
Add a new expression to evaluate using the 'display' command, see:
'help display' for more informations.
Expressions and local variables are refreshed after every stop.`

var autoCheckpointsPanelHelp = `Automatic checkpoints`
var registersPanelHelp = `Shows registers of the current thread.`
var breakpointsPanelHelp = `Shows current breakpoints. Click on a breakpoint to see the line of source
code where it is set. Right click for more options.`
var checkpointsPanelHelp = `Checkpoints`
var deferredCallsPanelHelp = `Deferred calls`
var globalsPanelHelp = `Shows all global variables. Note that keeping this window open can slow down
debugging.`
