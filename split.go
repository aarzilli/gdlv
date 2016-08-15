package main

import (
	"fmt"
	"strings"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/label"
	ntypes "github.com/aarzilli/nucular/types"

	"golang.org/x/mobile/event/mouse"
)

type panelKind string

const (
	fullPanelKind            panelKind = "Full"
	splitHorizontalPanelKind panelKind = "Horizontal"
	splitVerticalPanelKind   panelKind = "Vertical"
	infoPanelKind            panelKind = "Info"

	splitFlags = nucular.WindowNoScrollbar
)

const (
	infoCommand     = "Command"
	infoListing     = "Listing"
	infoDisassembly = "Disassembly"
	infoGoroutines  = "Goroutines"
	infoStacktrace  = "Stacktrace"
	infoLocals      = "Locals"
	infoGlobal      = "Globals"
	infoBps         = "Breakpoints"
	infoThreads     = "Threads"
	infoRegisters   = "Registers"
	infoSources     = "Sources"
	infoFuncs       = "Functions"
	infoTypes       = "Types"
)

var infoNameToFunc = map[string]func(mw *nucular.MasterWindow, w *nucular.Window){
	infoCommand:     updateCommandPanel,
	infoListing:     updateListingPanel,
	infoDisassembly: updateDisassemblyPanel,
	infoGoroutines:  goroutinesPanel.Update,
	infoStacktrace:  stackPanel.Update,
	infoLocals:      localsPanel.Update,
	infoGlobal:      globalsPanel.Update,
	infoBps:         breakpointsPanel.Update,
	infoThreads:     threadsPanel.Update,
	infoRegisters:   regsPanel.Update,
	infoSources:     sourcesPanel.Update,
	infoFuncs:       funcsPanel.Update,
	infoTypes:       typesPanel.Update,
}
var infoModes = []string{
	infoCommand, infoListing, infoDisassembly, infoGoroutines, infoStacktrace, infoLocals, infoGlobal, infoBps, infoThreads, infoRegisters, infoSources, infoFuncs, infoTypes,
}

// TODO: ugly take it away XXXX
var infoListingIdx = 1
var infoCommandIdx = 0
var infoStacktraceIdx = 4
var infoLocalsIdx = 5

func (kind panelKind) Internal() bool {
	switch kind {
	case fullPanelKind, splitHorizontalPanelKind, splitVerticalPanelKind:
		return true
	default:
		return false
	}
}

type panel struct {
	kind     panelKind
	size     int
	infoMode int
	child    [2]*panel

	name   string
	resize bool
}

var rootPanel = &panel{kind: splitVerticalPanelKind, size: 300, name: "root",
	child: [2]*panel{
		&panel{kind: splitHorizontalPanelKind, size: 250,
			child: [2]*panel{
				&panel{kind: infoPanelKind, infoMode: infoListingIdx},
				&panel{kind: infoPanelKind, infoMode: infoCommandIdx}}},
		&panel{kind: splitHorizontalPanelKind, size: 150,
			child: [2]*panel{
				&panel{kind: infoPanelKind, infoMode: infoStacktraceIdx},
				&panel{kind: infoPanelKind, infoMode: infoLocalsIdx}}}}}

const (
	headerRow         = 20
	headerCombo       = 180
	headerSplitMenu   = 70
	verticalSpacing   = 1
	horizontalSpacing = 2
)

func (p *panel) update(mw *nucular.MasterWindow, w *nucular.Window) {
	style, scaling := mw.Style()

	switch p.kind {
	case fullPanelKind:
		p.child[0].name = p.name + "-contents"
		p.child[0].update(mw, w)

	case infoPanelKind:
		w.Row(headerRow).Static(headerSplitMenu, 0, headerCombo, 2)
		w.Menu(label.TA("Split", "CC"), 120, p.splitMenu)
		w.Spacing(1)
		w.ComboSimple(infoModes, &p.infoMode, 22)
		w.Row(0).Dynamic(1)
		infoNameToFunc[infoModes[p.infoMode]](mw, w)

	case splitHorizontalPanelKind:
		w.Row(p.size).Dynamic(1)
		n := p.name + "-top"
		flags := splitFlags
		if p.child[0].kind == infoPanelKind {
			flags |= nucular.WindowBorder
		}
		if sw := w.GroupBegin(n, flags); sw != nil {
			p.child[0].name = n
			p.child[0].update(mw, sw)
			sw.GroupEnd()
		}

		w.Row(horizontalSpacing).Dynamic(1)
		rszbounds, _ := w.Custom(ntypes.WidgetStateInactive)
		rszbounds.Y -= style.GroupWindow.Spacing.Y
		rszbounds.H += style.GroupWindow.Spacing.Y * 2

		if w.Input().Mouse.HasClickDownInRect(mouse.ButtonLeft, rszbounds, true) {
			p.resize = true
		}
		if p.resize {
			if !w.Input().Mouse.Down(mouse.ButtonLeft) {
				p.resize = false
			} else {
				p.size += int(float64(w.Input().Mouse.Delta.Y) / scaling)
			}
		}

		w.Row(0).Dynamic(1)
		n = p.name + "-bot"
		flags = splitFlags
		if p.child[1].kind == infoPanelKind {
			flags |= nucular.WindowBorder
		}
		if sw := w.GroupBegin(n, flags); sw != nil {
			p.child[1].name = n
			p.child[1].update(mw, sw)
			sw.GroupEnd()
		}

	case splitVerticalPanelKind:
		w.Row(0).Static(p.size, verticalSpacing, 0)

		n := p.name + "-left"
		flags := splitFlags
		if p.child[0].kind == infoPanelKind {
			flags |= nucular.WindowBorder
		}
		if sw := w.GroupBegin(n, flags); sw != nil {
			p.child[0].name = n
			p.child[0].update(mw, sw)
			sw.GroupEnd()
		}

		rszbounds, _ := w.Custom(ntypes.WidgetStateInactive)
		rszbounds.X -= style.NormalWindow.Spacing.X
		rszbounds.W += style.NormalWindow.Spacing.X * 2

		if w.Input().Mouse.HasClickDownInRect(mouse.ButtonLeft, rszbounds, true) {
			p.resize = true
		}
		if p.resize {
			if !w.Input().Mouse.Down(mouse.ButtonLeft) {
				p.resize = false
			} else {
				p.size += int(float64(w.Input().Mouse.Delta.X) / scaling)
			}
		}

		n = p.name + "-right"
		flags = splitFlags
		if p.child[1].kind == infoPanelKind {
			flags |= nucular.WindowBorder
		}
		if sw := w.GroupBegin(n, flags); sw != nil {
			p.child[1].name = n
			p.child[1].update(mw, sw)
			sw.GroupEnd()
		}
	}
}

func (p *panel) splitMenu(mw *nucular.MasterWindow, w *nucular.Window) {
	w.Row(20).Dynamic(1)
	if w.MenuItem(label.TA("Horizontal", "LC")) {
		//TODO: implement
	}
	if w.MenuItem(label.TA("Vertical", "LC")) {
		//TODO: implement
	}
	if w.MenuItem(label.TA("Close", "LC")) {
		//TODO: implement
	}
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
		commandLineEditor.Active = true
	}
}

func updateListingPanel(mw *nucular.MasterWindow, container *nucular.Window) {
	const lineheight = 14

	listp := container.GroupBegin("listing", 0)
	if listp == nil {
		return
	}
	defer listp.GroupEnd()

	style, _ := mw.Style()

	arroww := nucular.FontWidth(style.Font, "=>") + style.Text.Padding.X*2
	starw := nucular.FontWidth(style.Font, "*") + style.Text.Padding.X*2

	idxw := style.Text.Padding.X * 2
	if len(lp.listing) > 0 {
		idxw += nucular.FontWidth(style.Font, lp.listing[len(lp.listing)-1].idx)
	}

	for _, line := range lp.listing {
		listp.Row(lineheight).StaticScaled(starw, arroww, idxw, 0)
		if line.pc {
			rowbounds := listp.WidgetBounds()
			rowbounds.W = listp.Bounds.W
			cmds := listp.Commands()
			cmds.FillRect(rowbounds, 0, style.Selectable.PressedActive.Data.Color)
		}

		if line.breakpoint {
			listp.Label("*", "CC")
		} else {
			listp.Spacing(1)
		}

		if line.pc && lp.recenterListing {
			lp.recenterListing = false
			if above, below := listp.Invisible(); above || below {
				listp.Scrollbar.Y = listp.At().Y - listp.Bounds.H/2
				if listp.Scrollbar.Y < 0 {
					listp.Scrollbar.Y = 0
				}
			}
		}

		if line.pc && curFrame == 0 {
			listp.Label("=>", "CC")
		} else {
			listp.Spacing(1)
		}
		listp.Label(line.idx, "LC")
		listp.Label(line.text, "LC")
	}
}

func updateDisassemblyPanel(mw *nucular.MasterWindow, container *nucular.Window) {
	const lineheight = 14

	listp := container.GroupBegin("disassembly", 0)
	if listp == nil {
		return
	}
	defer listp.GroupEnd()

	style, _ := mw.Style()

	arroww := nucular.FontWidth(style.Font, "=>") + style.Text.Padding.X*2
	starw := nucular.FontWidth(style.Font, "*") + style.Text.Padding.X*2

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
			listp.Row(lineheight).Dynamic(1)
			text := ""
			if instr.Loc.File == lp.file && instr.Loc.Line-1 < len(lp.listing) {
				text = strings.TrimSpace(lp.listing[instr.Loc.Line-1].text)
			}
			listp.Label(fmt.Sprintf("%s:%d: %s", instr.Loc.File, instr.Loc.Line, text), "LC")
			lastfile, lastlineno = instr.Loc.File, instr.Loc.Line
		}
		listp.Row(lineheight).StaticScaled(starw, arroww, addrw, 0)

		if instr.AtPC {
			rowbounds := listp.WidgetBounds()
			rowbounds.W = listp.Bounds.W
			cmds := listp.Commands()
			cmds.FillRect(rowbounds, 0, style.Selectable.PressedActive.Data.Color)
		}

		if instr.Breakpoint {
			listp.Label("*", "LC")
		} else {
			listp.Label(" ", "LC")
		}

		if instr.AtPC {
			if lp.recenterDisassembly {
				lp.recenterDisassembly = false
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
