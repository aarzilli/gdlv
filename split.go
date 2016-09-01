// Copyright 2016, Alessandro Arzilli

package main

import (
	"bytes"
	"fmt"
	"io"
	"math/rand"
	"strconv"

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
	infoLocals      = "Locals"
	infoGlobal      = "Globals"
	infoBps         = "Breakpoints"
	infoThreads     = "Threads"
	infoRegisters   = "Registers"
	infoSources     = "Sources"
	infoFuncs       = "Functions"
	infoTypes       = "Types"
	infoExprs       = "Expressions"
)

var infoNameToFunc = map[string]func(w *nucular.Window){
	infoCommand:     updateCommandPanel,
	infoListing:     updateListingPanel,
	infoDisassembly: updateDisassemblyPanel,
	infoGoroutines:  updateGoroutines,
	infoStacktrace:  updateStacktrace,
	infoLocals:      updateLocals,
	infoGlobal:      updateGlobals,
	infoBps:         updateBreakpoints,
	infoThreads:     updateThreads,
	infoRegisters:   updateRegs,
	infoSources:     sourcesPanel.update,
	infoFuncs:       funcsPanel.update,
	infoTypes:       typesPanel.update,
	infoExprs:       updateExprs,
}

var infoModes = []string{
	infoCommand, infoListing, infoDisassembly, infoGoroutines, infoStacktrace, infoLocals, infoGlobal, infoExprs, infoBps, infoThreads, infoRegisters, infoSources, infoFuncs, infoTypes,
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
	'e': infoExprs,
}

var infoModeToCode = map[string]byte{}

func init() {
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
	headerSplitMenu   = 70
	verticalSpacing   = 8
	horizontalSpacing = 8
	splitMinHeight    = 20
	splitMinWidth     = 20
	splitFlags        = nucular.WindowNoScrollbar | nucular.WindowBorder
)

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
	w.Row(0).SpaceBegin(0)

	bounds := rect.Rect{0, 0, w.LayoutAvailableWidth(), w.LayoutAvailableHeight()}

	p.updateIntl(w, bounds)
}

func (p *panel) updateIntl(w *nucular.Window, bounds rect.Rect) {
	switch p.kind {
	case fullPanelKind:
		p.child[0].updateIntl(w, bounds)

	case infoPanelKind:
		w.LayoutSpacePushScaled(bounds)
		if sw := w.GroupBegin(p.name, splitFlags); sw != nil {
			sw.Row(headerRow).Static(headerSplitMenu, 0, headerCombo, 2)
			sw.Menu(label.TA("Split", "CC"), 120, p.splitMenu)
			sw.Spacing(1)
			sw.ComboSimple(infoModes, &p.infoMode, 22)
			sw.Row(0).Dynamic(1)
			if p.infoMode >= 0 {
				infoNameToFunc[infoModes[p.infoMode]](sw)
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

func (p *panel) splitMenu(w *nucular.Window) {
	w.Row(20).Dynamic(1)
	if w.MenuItem(label.TA("Horizontal", "LC")) {
		p.dosplit(splitHorizontalPanelKind)
	}
	if w.MenuItem(label.TA("Vertical", "LC")) {
		p.dosplit(splitVerticalPanelKind)
	}
	if w.MenuItem(label.TA("Close", "LC")) {
		p.closeMyself()
	}
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
