// Copyright 2016, Gdlv Authors

package main

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"math/rand"
	"strconv"
	"strings"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"
)

type panelKind string

const (
	fullPanelKind            panelKind = "Full"
	splitHorizontalPanelKind panelKind = "Horizontal"
	splitVerticalPanelKind   panelKind = "Vertical"
	infoPanelKind            panelKind = "Info"
)

const (
	infoCommand     = "Command"
	infoListing     = "Listing"
	infoDisassembly = "Disassembly"
	infoGoroutines  = "Goroutines"
	infoStacktrace  = "Stacktrace"
	infoLocals      = "Variables"
	infoGlobal      = "Globals"
	infoBps         = "Breakpoints"
	infoThreads     = "Threads"
	infoRegisters   = "Registers"
	infoSources     = "Sources"
	infoFuncs       = "Functions"
	infoTypes       = "Types"
	infoCheckpoints = "Checkpoints"
)

type infoPanel struct {
	update    func(w *nucular.Window)
	asyncLoad *asyncLoad
}

var infoNameToPanel map[string]infoPanel

var infoModes = []string{
	infoCommand, infoListing, infoDisassembly, infoGoroutines, infoStacktrace, infoLocals, infoGlobal, infoBps, infoThreads, infoRegisters, infoSources, infoFuncs, infoTypes, infoCheckpoints,
}

var codeToInfoMode = map[byte]string{
	'C': infoCommand,
	'L': infoListing,
	'D': infoDisassembly,
	'G': infoGoroutines,
	'S': infoStacktrace,
	'l': infoLocals,
	'g': infoGlobal,
	'B': infoBps,
	'r': infoRegisters,
	's': infoSources,
	'f': infoFuncs,
	't': infoTypes,
	'T': infoThreads,
	'k': infoCheckpoints,
}

var infoModeToCode = map[string]byte{}

func init() {
	infoNameToPanel = make(map[string]infoPanel)

	infoNameToPanel[infoCommand] = infoPanel{updateCommandPanel, nil}
	infoNameToPanel[infoListing] = infoPanel{updateListingPanel, nil}
	infoNameToPanel[infoDisassembly] = infoPanel{updateDisassemblyPanel, nil}
	infoNameToPanel[infoGoroutines] = infoPanel{updateGoroutines, &goroutinesPanel.asyncLoad}
	infoNameToPanel[infoStacktrace] = infoPanel{updateStacktrace, &stackPanel.asyncLoad}
	infoNameToPanel[infoLocals] = infoPanel{updateLocals, &localsPanel.asyncLoad}
	infoNameToPanel[infoGlobal] = infoPanel{updateGlobals, &globalsPanel.asyncLoad}
	infoNameToPanel[infoBps] = infoPanel{updateBreakpoints, &breakpointsPanel.asyncLoad}
	infoNameToPanel[infoThreads] = infoPanel{updateThreads, &threadsPanel.asyncLoad}
	infoNameToPanel[infoRegisters] = infoPanel{updateRegs, &regsPanel.asyncLoad}
	infoNameToPanel[infoSources] = infoPanel{sourcesPanel.update, nil}
	infoNameToPanel[infoFuncs] = infoPanel{funcsPanel.update, nil}
	infoNameToPanel[infoTypes] = infoPanel{typesPanel.update, nil}
	infoNameToPanel[infoCheckpoints] = infoPanel{updateCheckpoints, &checkpointsPanel.asyncLoad}

	for k, v := range codeToInfoMode {
		infoModeToCode[v] = k
	}
}

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
	split    nucular.ScalableSplit
	infoMode int
	child    [2]*panel
	parent   *panel

	name string
}

var rootPanel *panel

const (
	headerRow         = 20
	headerCombo       = 110
	controlBtnWidth   = 30
	headerSplitMenu   = 30
	verticalSpacing   = 8
	horizontalSpacing = 8
	splitMinHeight    = 20
	splitMinWidth     = 20
	splitFlags        = nucular.WindowNoScrollbar | nucular.WindowBorder
)

func parsePanelDescrToplevel(in string) (p *panel, height int, width int) {
	if len(in) > 3 {
		if in[0] == '$' {
			if dollar := strings.Index(in[1:], "$"); dollar >= 0 {
				szstr := in[1 : 1+dollar]
				in = in[2+dollar:]
				if comma := strings.Index(szstr, ","); comma >= 0 {
					width, _ = strconv.Atoi(szstr[:comma])
					height, _ = strconv.Atoi(szstr[comma+1:])
				}
			}
		}
	}
	if width <= 0 || height <= 0 {
		width = 640
		height = 480
	}
	p, _ = parsePanelDescr(in, nil)
	return
}

func parsePanelDescr(in string, parent *panel) (p *panel, rest string) {
	switch in[0] {
	case '0':
		p = &panel{kind: fullPanelKind, name: randomname(), parent: parent}
		p.child[0], rest = parsePanelDescr(in[1:], p)
		return p, rest
	case '_', '|':
		kind := splitHorizontalPanelKind
		minSize := splitMinHeight
		spacing := horizontalSpacing
		if in[0] == '|' {
			kind = splitVerticalPanelKind
			minSize = splitMinWidth
			spacing = verticalSpacing
		}
		var i int
		for i = 1; i < len(in); i++ {
			if in[i] < '0' || in[i] > '9' {
				break
			}
		}
		size, _ := strconv.Atoi(in[1:i])
		p = &panel{kind: kind, name: randomname(), parent: parent}
		p.split.Size = size
		p.split.MinSize = minSize
		p.split.Spacing = spacing
		rest = in[i:]
		p.child[0], rest = parsePanelDescr(rest, p)
		p.child[1], rest = parsePanelDescr(rest, p)
		return p, rest
	default:
		p = &panel{kind: infoPanelKind, name: randomname(), infoMode: infoModeIdx(codeToInfoMode[in[0]]), parent: parent}
		rest = in[1:]
		return p, rest
	}
}

func (p *panel) String() (s string, err error) {
	defer func() {
		if ierr := recover(); ierr != nil {
			err = ierr.(error)
		}
	}()
	var out bytes.Buffer
	p.serialize(&out)
	return out.String(), err
}

func (p *panel) serialize(out io.Writer) {
	switch p.kind {
	case fullPanelKind:
		out.Write([]byte{'0'})
		p.child[0].serialize(out)
	case splitHorizontalPanelKind:
		fmt.Fprintf(out, "_%d", p.split.Size)
		p.child[0].serialize(out)
		p.child[1].serialize(out)
	case splitVerticalPanelKind:
		fmt.Fprintf(out, "|%d", p.split.Size)
		p.child[0].serialize(out)
		p.child[1].serialize(out)
	case infoPanelKind:
		code, ok := infoModeToCode[infoModes[p.infoMode]]
		if !ok {
			panic(fmt.Errorf("could not convert info mode %s to a code", infoModes[p.infoMode]))
		}
		out.Write([]byte{code})
	}
}

func infoModeIdx(n string) int {
	for i := range infoModes {
		if infoModes[i] == n {
			return i
		}
	}
	return -1
}

func randomname() string {
	var alphabet = []byte{'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o', 'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 'A', 'B', 'C', 'D', 'E', 'F', 'G', 'H', 'I', 'J', 'K', 'L', 'M', 'N', 'O', 'P', 'Q', 'R', 'S', 'T', 'U', 'V', 'W', 'X', 'Y', 'Z'}
	out := make([]byte, 8)
	for i := range out {
		out[i] = alphabet[rand.Intn(len(alphabet))]
	}
	return string(out)
}

func (p *panel) update(w *nucular.Window) {
	area := w.Row(0).SpaceBegin(0)
	p.updateIntl(w, area)
}

func (p *panel) updateIntl(w *nucular.Window, bounds rect.Rect) {
	switch p.kind {
	case fullPanelKind:
		p.child[0].updateIntl(w, bounds)

	case infoPanelKind:
		w.LayoutSpacePushScaled(bounds)
		if sw := w.GroupBegin(p.name, splitFlags); sw != nil {
			switch infoModes[p.infoMode] {
			case infoCommand:
				p.commandToolbar(sw)
			case infoListing, infoDisassembly:
				p.listingToolbar(sw)
			default:
				p.normalToolbar(sw)
			}
			sw.Row(0).Dynamic(1)
			if p.infoMode >= 0 {
				infoNameToPanel[infoModes[p.infoMode]].update(sw)
			}
			sw.GroupEnd()
		}

	case splitHorizontalPanelKind:
		bounds0, bounds1 := p.split.Horizontal(w, bounds)

		if bounds0.H > 0 {
			p.child[0].updateIntl(w, bounds0)
		}

		if bounds1.H > 0 {
			p.child[1].updateIntl(w, bounds1)
		}

	case splitVerticalPanelKind:
		bounds0, bounds1 := p.split.Vertical(w, bounds)

		if bounds0.W > 0 {
			p.child[0].updateIntl(w, bounds0)
		}

		if bounds1.W > 0 {
			p.child[1].updateIntl(w, bounds1)
		}
	}
}

func (p *panel) startReload() {
	switch p.kind {
	case fullPanelKind:
		p.child[0].startReload()
	case infoPanelKind:
		if p.infoMode >= 0 {
			if asyncLoad := infoNameToPanel[infoModes[p.infoMode]].asyncLoad; asyncLoad != nil {
				asyncLoad.startLoad()
			}
		}
	case splitHorizontalPanelKind, splitVerticalPanelKind:
		p.child[0].startReload()
		p.child[1].startReload()
	}
}

func (p *panel) splitMenu(w *nucular.Window) {
	w.LayoutSetWidth(headerSplitMenu)
	style := w.Master().Style()
	iconFace, style.Font = style.Font, iconFace
	mw := w.Menu(label.TA(splitIconChar, "CC"), 160, nil)
	iconFace, style.Font = style.Font, iconFace
	if w := mw; w != nil {
		w.Row(20).Dynamic(1)
		if w.MenuItem(label.TA("Split Horizontal", "LC")) {
			p.dosplit(splitHorizontalPanelKind)
		}
		if w.MenuItem(label.TA("Split Vertical", "LC")) {
			p.dosplit(splitVerticalPanelKind)
		}
		if w.MenuItem(label.TA("Close", "LC")) {
			p.closeMyself()
		}
	}
}

func (p *panel) toolbarHeaderCombo(sw *nucular.Window) {
	sw.LayoutResetStatic(0, headerCombo, 2)
	sw.Spacing(1)
	p.infoMode = sw.ComboSimple(infoModes, p.infoMode, 22)
}

func (p *panel) normalToolbar(sw *nucular.Window) {
	sw.Row(headerRow).Static()
	p.splitMenu(sw)
	p.toolbarHeaderCombo(sw)
}

func (p *panel) listingToolbar(sw *nucular.Window) {
	sw.Row(headerRow).Static()

	p.splitMenu(sw)

	showfilename := true

	if listingPanel.pinnedLoc != nil {
		sw.LayoutSetWidth(200)
		if sw.ButtonText("Back to current frame") {
			listingPanel.pinnedLoc = nil
			go refreshState(refreshToSameFrame, clearNothing, nil)
		}
		showfilename = false
	}

	if listingPanel.stale {
		sw.LayoutSetWidth(400)
		sw.LabelColored("Warning: listing may not match stale executable", "LC", color.RGBA{0xff, 0x00, 0x00, 0xff})
		showfilename = false
	}

	if showfilename {
		style := sw.Master().Style()
		sw.LayoutSetWidthScaled(sw.LayoutAvailableWidth() - style.Text.Padding.X*2)
		sw.Label(listingPanel.abbrevFile, "LC")
	}

	p.toolbarHeaderCombo(sw)
}

func (p *panel) commandToolbar(sw *nucular.Window) {
	style := sw.Master().Style()
	iconbtn := func(icon string, tooltip string) bool {
		iconFace, style.Font = style.Font, iconFace
		r := sw.ButtonText(icon)
		if sw.Input().Mouse.HoveringRect(sw.LastWidgetBounds) {
			sw.Tooltip(tooltip)
		}
		iconFace, style.Font = style.Font, iconFace
		return r
	}
	cmdbtn := func(icon, cmd string) {
		if iconbtn(icon, cmd) {
			doCommand(cmd)
		}
	}
	sw.Row(headerRow).Static()
	switch {
	case client == nil:
		p.splitMenu(sw)

	case running:
		p.splitMenu(sw)
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(interruptIconChar, "interrupt")

	case nextInProgress:
		p.splitMenu(sw)
		sw.LayoutSetWidth(controlBtnWidth)
		if iconbtn(continueIconChar, "continue next") {
			doCommand("continue")
		}
		sw.LayoutSetWidth(controlBtnWidth)
		if iconbtn(cancelIconChar, "cancel next") {
			doCommand("cancelnext")
		}

	default:
		p.splitMenu(sw)
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(continueIconChar, "continue")
		sw.LayoutSetWidth(controlBtnWidth / 2)
		sw.Spacing(1)
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(nextIconChar, "next")
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(stepIconChar, "step")
		sw.LayoutSetWidth(controlBtnWidth)
		cmdbtn(stepoutIconChar, "stepout")
	}
	p.toolbarHeaderCombo(sw)
}

func (p *panel) dosplit(kind panelKind) {
	if p.parent == nil {
		return
	}

	if p.parent.kind == fullPanelKind {
		p.parent.kind = kind
		p.parent.child[1] = &panel{kind: p.kind, name: randomname(), infoMode: p.infoMode, parent: p.parent}
		return
	}

	idx := p.parent.idx(p)
	if idx < 0 {
		return
	}

	newpanel := &panel{kind: kind, name: randomname(), parent: p.parent}
	newpanel.split.Size = 0
	switch kind {
	case splitHorizontalPanelKind:
		newpanel.split.MinSize = splitMinHeight
		newpanel.split.Spacing = horizontalSpacing
	case splitVerticalPanelKind:
		newpanel.split.MinSize = splitMinWidth
		newpanel.split.Spacing = verticalSpacing
	}

	newpanel.child[0] = p
	newpanel.child[1] = &panel{kind: p.kind, name: randomname(), infoMode: p.infoMode, parent: newpanel}

	p.parent.child[idx] = newpanel
	p.parent = newpanel
}

func (p *panel) idx(child *panel) int {
	for i := range p.child {
		if p.child[i] == child {
			return i
		}
	}
	return -1
}

func (p *panel) closeMyself() {
	if p.parent == nil || p.parent.kind == fullPanelKind {
		return
	}

	idx := p.parent.idx(p)
	if idx < 0 {
		return
	}
	p.parent.closeChild(idx)
}

func (p *panel) closeChild(idx int) {
	if p.parent == nil {
		p.kind = fullPanelKind
		if idx == 0 {
			p.child[0] = p.child[1]
		}
		return
	}

	ownidx := p.parent.idx(p)
	if ownidx < 0 {
		return
	}

	survivor := p.child[0]
	if idx == 0 {
		survivor = p.child[1]
	}

	p.parent.child[ownidx] = survivor
	survivor.parent = p.parent
}
