package harfbuzz

import (
	"fmt"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/font/opentype/tables"
)

func (c *aatApplyContext) applyMorx(chain font.MorxChain, accelerators []morxSubtableAccelerator) {
	//  Coverage, see https://developer.apple.com/fonts/TrueType-Reference-Manual/RM06/Chap6morx.html
	const (
		Vertical      = 0x80
		Backwards     = 0x40
		AllDirections = 0x20
		Logical       = 0x10
	)

	for i, subtable := range chain.Subtables {
		coverage := subtable.Coverage
		subtableFlags := subtable.Flags
		if !c.hasAnyFlags(subtableFlags) {
			continue
		}

		if coverage&AllDirections == 0 && c.buffer.Props.Direction.isVertical() !=
			(coverage&Vertical != 0) {
			continue
		}

		c.subtableFlags = subtableFlags
		c.firstSet = accelerators[i].glyphSet
		c.machineClassCache = accelerators[i].classCache

		if !c.bufferIntersectsMachine() {
			if debugMode {
				fmt.Printf("AAT morx : skipped subtable %d because no glyph matches\n", i)
			}
			continue
		}

		/* Buffer contents is always in logical direction.  Determine if
		we need to reverse before applying this subtable.  We reverse
		back after if we did reverse indeed.

		Quoting the spec:
		"""
		Bits 28 and 30 of the coverage field control the order in which
		glyphs are processed when the subtable is run by the layout engine.
		Bit 28 is used to indicate if the glyph processing direction is
		the same as logical order or layout order. Bit 30 is used to
		indicate whether glyphs are processed forwards or backwards within
		that order.

		Bit 30	Bit 28	Interpretation for Horizontal Text
		0	0	The subtable is processed in layout order 	(the same order as the glyphs, which is
			always left-to-right).
		1	0	The subtable is processed in reverse layout order (the order opposite that of the glyphs, which is
			always right-to-left).
		0	1	The subtable is processed in logical order (the same order as the characters, which may be
			left-to-right or right-to-left).
		1	1	The subtable is processed in reverse logical order 	(the order opposite that of the characters, which
			may be right-to-left or left-to-right).
		"""
		*/
		var reverse bool
		if coverage&Logical != 0 {
			reverse = coverage&Backwards != 0
		} else {
			reverse = coverage&Backwards != 0 != c.buffer.Props.Direction.isBackward()
		}

		if debugMode {
			fmt.Printf("MORX - start chainsubtable %d\n", i)
		}

		if reverse != c.bufferIsReversed {
			c.reverseBuffer()
		}

		c.applyMorxSubtable(subtable)

		if debugMode {
			fmt.Printf("MORX - end chainsubtable %d\n", i)
			fmt.Println(c.buffer.Info)
		}
	}

	if c.bufferIsReversed {
		c.reverseBuffer()
	}
}

func (c *aatApplyContext) applyMorxSubtable(subtable font.MorxSubtable) bool {
	if debugMode {
		fmt.Printf("\tMORX subtable %T\n", subtable.Data)
	}
	switch data := subtable.Data.(type) {
	case font.MorxRearrangementSubtable:
		var dc driverContextRearrangement
		driver := newStateTableDriver(font.AATStateTable(data), c.face)
		driver.drive(&dc, c)
	case font.MorxContextualSubtable:
		dc := driverContextContextual{c: c, table: data}
		driver := newStateTableDriver(data.Machine, c.face)
		driver.drive(&dc, c)
		return dc.ret
	case font.MorxLigatureSubtable:
		dc := driverContextLigature{c: c, table: data}
		driver := newStateTableDriver(data.Machine, c.face)
		driver.drive(&dc, c)
	case font.MorxInsertionSubtable:
		dc := driverContextInsertion{c: c, insertionAction: data.Insertions}
		driver := newStateTableDriver(data.Machine, c.face)
		driver.drive(&dc, c)
	case font.MorxNonContextualSubtable:
		return c.applyNonContextualSubtable(data)
	}
	return false
}

// MorxRearrangemen flags
const (
	/* If set, make the current glyph the first
	* glyph to be rearranged. */
	mrMarkFirst = 0x8000
	/* If set, don't advance to the next glyph
	* before going to the new state. This means
	* that the glyph index doesn't change, even
	* if the glyph at that index has changed. */
	_ = 0x4000
	/* If set, make the current glyph the last
	* glyph to be rearranged. */
	mrMarkLast = 0x2000
	/* These bits are reserved and should be set to 0. */
	_ = 0x1FF0
	/* The type of rearrangement specified. */
	mrVerb = 0x000F
)

type driverContextRearrangement struct {
	start int
	end   int
}

func (driverContextRearrangement) inPlace() bool { return true }

func (d driverContextRearrangement) isActionable(entry tables.AATStateEntry) bool {
	return (entry.Flags & mrVerb) != 0
}

/* The following map has two nibbles, for start-side
 * and end-side. Values of 0,1,2 mean move that many
 * to the other side. Value of 3 means move 2 and
 * flip them. */
var mapRearrangement = [16]int{
	0x00, /* 0	no change */
	0x10, /* 1	Ax => xA */
	0x01, /* 2	xD => Dx */
	0x11, /* 3	AxD => DxA */
	0x20, /* 4	ABx => xAB */
	0x30, /* 5	ABx => xBA */
	0x02, /* 6	xCD => CDx */
	0x03, /* 7	xCD => DCx */
	0x12, /* 8	AxCD => CDxA */
	0x13, /* 9	AxCD => DCxA */
	0x21, /* 10	ABxD => DxAB */
	0x31, /* 11	ABxD => DxBA */
	0x22, /* 12	ABxCD => CDxAB */
	0x32, /* 13	ABxCD => CDxBA */
	0x23, /* 14	ABxCD => DCxAB */
	0x33, /* 15	ABxCD => DCxBA */
}

func (d *driverContextRearrangement) transition(buffer *Buffer, driver stateTableDriver, entry tables.AATStateEntry) {
	flags := entry.Flags

	if flags&mrMarkFirst != 0 {
		d.start = buffer.idx
	}

	if flags&mrMarkLast != 0 {
		d.end = min(buffer.idx+1, len(buffer.Info))
	}

	if (flags&mrVerb) != 0 && d.start < d.end {

		m := mapRearrangement[flags&mrVerb]
		l := min(2, m>>4)
		r := min(2, m&0x0F)
		reverseL := m>>4 == 3
		reverseR := m&0x0F == 3

		if d.end-d.start >= l+r && d.end-d.start <= maxContextLength {
			buffer.mergeClusters(d.start, min(buffer.idx+1, len(buffer.Info)))
			buffer.mergeClusters(d.start, d.end)

			info := buffer.Info
			var buf [4]GlyphInfo

			copy(buf[:], info[d.start:d.start+l])
			copy(buf[2:], info[d.end-r:d.end])

			if l != r {
				copy(info[d.start+r:], info[d.start+l:d.end-r])
			}

			copy(info[d.start:d.start+r], buf[2:])
			copy(info[d.end-l:d.end], buf[:])
			if reverseL {
				buf[0] = info[d.end-1]
				info[d.end-1] = info[d.end-2]
				info[d.end-2] = buf[0]
			}
			if reverseR {
				buf[0] = info[d.start]
				info[d.start] = info[d.start+1]
				info[d.start+1] = buf[0]
			}
		}
	}
}

// MorxContextualSubtable flags
const (
	mcSetMark = 0x8000 /* If set, make the current glyph the marked glyph. */
	/* If set, don't advance to the next glyph before
	* going to the new state. */
	_ = 0x4000
	_ = 0x3FFF /* These bits are reserved and should be set to 0. */
)

type driverContextContextual struct {
	c       *aatApplyContext
	table   font.MorxContextualSubtable
	mark    int
	markSet bool
	ret     bool
}

func (driverContextContextual) inPlace() bool { return true }

func (dc driverContextContextual) isActionable(entry tables.AATStateEntry) bool {
	markIndex, currentIndex := entry.AsMorxContextual()
	return markIndex != 0xFFFF || currentIndex != 0xFFFF
}

func (dc *driverContextContextual) transition(buffer *Buffer, driver stateTableDriver, entry tables.AATStateEntry) {
	/* Looks like CoreText applies neither mark nor current substitution for
	 * end-of-text if mark was not explicitly set. */
	if buffer.idx == len(buffer.Info) && !dc.markSet {
		return
	}

	var (
		replacement             uint16 // intepreted as GlyphIndex
		hasReplacement          bool
		markIndex, currentIndex = entry.AsMorxContextual()
	)
	if markIndex != 0xFFFF {
		lookup := dc.table.Substitutions[markIndex]
		replacement, hasReplacement = lookup.Class(gID(buffer.Info[dc.mark].Glyph))
	}
	if hasReplacement {
		buffer.unsafeToBreak(dc.mark, min(buffer.idx+1, len(buffer.Info)))
		dc.c.replace_glyph_inplace(dc.mark, replacement)
		dc.ret = true
	}

	hasReplacement = false
	idx := min(buffer.idx, len(buffer.Info)-1)
	if currentIndex != 0xFFFF {
		lookup := dc.table.Substitutions[currentIndex]
		replacement, hasReplacement = lookup.Class(gID(buffer.Info[idx].Glyph))
	}

	if hasReplacement {
		dc.c.replace_glyph_inplace(idx, replacement)
		dc.ret = true
	}

	if entry.Flags&mcSetMark != 0 {
		dc.markSet = true
		dc.mark = buffer.idx
	}
}

type driverContextLigature struct {
	c              *aatApplyContext
	table          font.MorxLigatureSubtable
	matchLength    int
	matchPositions [maxContextLength]int
}

func (driverContextLigature) inPlace() bool { return false }

func (driverContextLigature) isActionable(entry tables.AATStateEntry) bool {
	return entry.Flags&tables.MLOffset != 0
}

func (dc *driverContextLigature) transition(buffer *Buffer, driver stateTableDriver, entry tables.AATStateEntry) {
	if debugMode {
		fmt.Printf("\tLigature - Ligature transition at %d\n", buffer.idx)
	}

	if entry.Flags&tables.MLSetComponent != 0 {
		/* Never mark same index twice, in case DontAdvance was used... */
		if dc.matchLength != 0 && dc.matchPositions[(dc.matchLength-1)%len(dc.matchPositions)] == len(buffer.outInfo) {
			dc.matchLength--
		}

		dc.matchPositions[dc.matchLength%len(dc.matchPositions)] = len(buffer.outInfo)
		dc.matchLength++

		if debugMode {
			fmt.Printf("\tLigature - Set component at %d\n", len(buffer.outInfo))
		}

	}

	if dc.isActionable(entry) {

		if debugMode {
			fmt.Printf("\tLigature - Perform action with %d\n", dc.matchLength)
		}

		end := len(buffer.outInfo)

		if dc.matchLength == 0 {
			return
		}

		if buffer.idx >= len(buffer.Info) {
			return
		}
		cursor := dc.matchLength

		actionIdx := entry.AsMorxLigature()
		actionData := dc.table.LigatureAction[actionIdx:]

		ligatureIdx := 0
		var action uint32
		for do := true; do; do = action&tables.MLActionLast == 0 {
			if cursor == 0 {
				/* Stack underflow.  Clear the stack. */
				if debugMode {
					fmt.Println("\tLigature - Stack underflow")
				}
				dc.matchLength = 0
				break
			}

			if debugMode {
				fmt.Printf("\tLigature - Moving to stack position %d\n", cursor-1)
			}

			cursor--
			buffer.moveTo(dc.matchPositions[cursor%len(dc.matchPositions)])

			if len(actionData) == 0 {
				break
			}
			action = actionData[0]

			uoffset := action & tables.MLActionOffset
			if uoffset&0x20000000 != 0 {
				uoffset |= 0xC0000000 /* Sign-extend. */
			}
			offset := int32(uoffset)
			componentIdx := int32(buffer.cur(0).Glyph) + offset
			if int(componentIdx) >= len(dc.table.Components) {
				break
			}
			componentData := dc.table.Components[componentIdx]
			ligatureIdx += int(componentData)

			if debugMode {
				fmt.Printf("\tLigature - Action store %d last %d\n", action&tables.MLActionStore, action&tables.MLActionLast)
			}

			if action&(tables.MLActionStore|tables.MLActionLast) != 0 {
				if ligatureIdx >= len(dc.table.Ligatures) {
					break
				}
				lig := dc.table.Ligatures[ligatureIdx]

				if debugMode {
					fmt.Printf("\tLigature - Produced ligature %d\n", lig)
				}

				dc.c.replaceGlyph(lig)

				ligEnd := dc.matchPositions[(dc.matchLength-1)%len(dc.matchPositions)] + 1
				/* Now go and delete all subsequent components. */
				for dc.matchLength-1 > cursor {

					if debugMode {
						fmt.Println("\tLigature - Skipping ligature component")
					}

					dc.matchLength--
					buffer.moveTo(dc.matchPositions[dc.matchLength%len(dc.matchPositions)])
					dc.c.deleteGlyph()
				}

				buffer.moveTo(ligEnd)
				buffer.mergeOutClusters(dc.matchPositions[cursor%len(dc.matchPositions)], len(buffer.outInfo))
			}

			actionData = actionData[1:]
		}
		buffer.moveTo(end)
	}
}

// MorxInsertionSubtable flags
const (
	// If set, mark the current glyph.
	miSetMark = 0x8000
	// If set, don't advance to the next glyph before
	// going to the new state.  This does not mean
	// that the glyph pointed to is the same one as
	// before. If you've made insertions immediately
	// downstream of the current glyph, the next glyph
	// processed would in fact be the first one
	// inserted.
	miDontAdvance = 0x4000
	// If set, and the currentInsertList is nonzero,
	// then the specified glyph list will be inserted
	// as a kashida-like insertion, either before or
	// after the current glyph (depending on the state
	// of the currentInsertBefore flag). If clear, and
	// the currentInsertList is nonzero, then the
	// specified glyph list will be inserted as a
	// split-vowel-like insertion, either before or
	// after the current glyph (depending on the state
	// of the currentInsertBefore flag).
	_ = 0x2000
	// If set, and the markedInsertList is nonzero,
	// then the specified glyph list will be inserted
	// as a kashida-like insertion, either before or
	// after the marked glyph (depending on the state
	// of the markedInsertBefore flag). If clear, and
	// the markedInsertList is nonzero, then the
	// specified glyph list will be inserted as a
	// split-vowel-like insertion, either before or
	// after the marked glyph (depending on the state
	// of the markedInsertBefore flag).
	_ = 0x1000
	// If set, specifies that insertions are to be made
	// to the left of the current glyph. If clear,
	// they're made to the right of the current glyph.
	miCurrentInsertBefore = 0x0800
	// If set, specifies that insertions are to be
	// made to the left of the marked glyph. If clear,
	// they're made to the right of the marked glyph.
	miMarkedInsertBefore = 0x0400
	// This 5-bit field is treated as a count of the
	// number of glyphs to insert at the current
	// position. Since zero means no insertions, the
	// largest number of insertions at any given
	// current location is 31 glyphs.
	miCurrentInsertCount = 0x3E0
	// This 5-bit field is treated as a count of the
	// number of glyphs to insert at the marked
	// position. Since zero means no insertions, the
	// largest number of insertions at any given
	// marked location is 31 glyphs.
	miMarkedInsertCount = 0x001F
)

type driverContextInsertion struct {
	c               *aatApplyContext
	insertionAction []GID
	mark            int
}

func (driverContextInsertion) inPlace() bool { return false }

func (driverContextInsertion) isActionable(entry tables.AATStateEntry) bool {
	current, marked := entry.AsMorxInsertion()
	return entry.Flags&(miCurrentInsertCount|miMarkedInsertCount) != 0 && (current != 0xFFFF || marked != 0xFFFF)
}

func (dc *driverContextInsertion) transition(buffer *Buffer, driver stateTableDriver, entry tables.AATStateEntry) {
	flags := entry.Flags

	markLoc := len(buffer.outInfo)
	currentInsertIndex, markedInsertIndex := entry.AsMorxInsertion()
	if markedInsertIndex != 0xFFFF {
		count := int(flags & miMarkedInsertCount)
		buffer.maxOps -= count
		if buffer.maxOps <= 0 {
			return
		}
		start := markedInsertIndex
		glyphs := dc.insertionAction[start:]

		before := flags&miMarkedInsertBefore != 0

		end := len(buffer.outInfo)
		buffer.moveTo(dc.mark)

		if buffer.idx < len(buffer.Info) && !before {
			buffer.copyGlyph()
		}

		/* TODO We ignore KashidaLike setting. */
		dc.c.outputGlyphs(glyphs[:count])

		if buffer.idx < len(buffer.Info) && !before {
			buffer.skipGlyph()
		}

		buffer.moveTo(end + count)

		buffer.unsafeToBreakFromOutbuffer(dc.mark, min(buffer.idx+1, len(buffer.Info)))
	}

	if flags&miSetMark != 0 {
		dc.mark = markLoc
	}

	if currentInsertIndex != 0xFFFF {
		count := int(flags&miCurrentInsertCount) >> 5
		if buffer.maxOps <= 0 {
			buffer.maxOps -= count
			return
		}
		buffer.maxOps -= count
		start := currentInsertIndex
		glyphs := dc.insertionAction[start:]

		before := flags&miCurrentInsertBefore != 0

		end := len(buffer.outInfo)

		if buffer.idx < len(buffer.Info) && !before {
			buffer.copyGlyph()
		}

		/* TODO We ignore KashidaLike setting. */
		dc.c.outputGlyphs(glyphs[:count])

		if buffer.idx < len(buffer.Info) && !before {
			buffer.skipGlyph()
		}

		/* Humm. Not sure where to move to.  There's this wording under
		 * DontAdvance flag:
		 *
		 * "If set, don't update the glyph index before going to the new state.
		 * This does not mean that the glyph pointed to is the same one as
		 * before. If you've made insertions immediately downstream of the
		 * current glyph, the next glyph processed would in fact be the first
		 * one inserted."
		 *
		 * This suggests that if DontAdvance is NOT set, we should move to
		 * end+count.  If it *was*, then move to end, such that newly inserted
		 * glyphs are now visible.
		 *
		 * https://github.com/harfbuzz/harfbuzz/issues/1224#issuecomment-427691417
		 */
		moveTo := end
		if flags&miDontAdvance == 0 {
			moveTo = end + count
		}
		buffer.moveTo(moveTo)
	}
}

func (c *aatApplyContext) applyNonContextualSubtable(data font.MorxNonContextualSubtable) bool {
	var ret bool
	info := c.buffer.Info
	// If there's only one range, we already checked the flag.
	var lastRange int = -1 // index in ac.rangeFlags, or -1
	if len(c.rangeFlags) > 1 {
		lastRange = 0
	}
	for i := range info {
		// This block is copied in NoncontextualSubtable::apply. Keep in sync.
		if lastRange != -1 {
			range_ := lastRange
			cluster := info[i].Cluster
			for cluster < c.rangeFlags[range_].clusterFirst {
				range_--
			}
			for cluster > c.rangeFlags[range_].clusterLast {
				range_++
			}

			lastRange = range_
			if c.rangeFlags[range_].flags&c.subtableFlags == 0 {
				continue
			}
		}

		replacement, hasReplacement := data.Class.Class(gID(info[i].Glyph))
		if hasReplacement {
			c.replace_glyph_inplace(i, replacement)
			ret = true
		}
	}
	return ret
}

type morxSubtableAccelerator struct {
	glyphSet   intSet
	classCache aatClassCache
}

func (ma *morxSubtableAccelerator) init(subtable font.MorxSubtable) {
	ma.classCache.clear()
	switch st := subtable.Data.(type) {
	case font.MorxRearrangementSubtable:
		collectLookupGlyphs(&ma.glyphSet, st.Class)
	case font.MorxContextualSubtable:
		collectLookupGlyphs(&ma.glyphSet, st.Machine.Class)
	case font.MorxLigatureSubtable:
		collectLookupGlyphs(&ma.glyphSet, st.Machine.Class)
	case font.MorxNonContextualSubtable:
		collectLookupGlyphs(&ma.glyphSet, st.Class)
	case font.MorxInsertionSubtable:
		collectLookupGlyphs(&ma.glyphSet, st.Machine.Class)
	}
}

func newMorxChainAccelerator(chain font.MorxChain) []morxSubtableAccelerator {
	out := make([]morxSubtableAccelerator, len(chain.Subtables))
	for i, s := range chain.Subtables {
		out[i].init(s)
	}
	return out
}
