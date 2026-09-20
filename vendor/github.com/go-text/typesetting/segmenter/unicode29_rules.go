// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package segmenter

import (
	ucd "github.com/go-text/typesetting/internal/unicodedata"
)

// -----------------------------------------------------------------------
// ------------------------- Grapheme boundaries -------------------------
// -----------------------------------------------------------------------

// Apply the Grapheme_Cluster_Boundary_Rules and returns a true if we are
// at a grapheme break.
// See https://unicode.org/reports/tr29/#Grapheme_Cluster_Boundary_Rules
func (cr *cursor) applyGraphemeBoundaryRules() bool {
	triggerGB9c := cr.updateIndicConjunctBreakSequence() // apply rule GB9c
	triggerGB11 := cr.updatePictoSequence()              // apply rule GB11
	triggerGB12_13 := cr.updateGraphemeRIOdd()           // apply rule GB12 and GB13

	br0, br1 := cr.prevGrapheme, cr.grapheme
	if cr.r == '\n' && cr.prev == '\r' {
		return false // Rule GB3
	} else if br0&(ucd.GB_Control|ucd.GB_CR|ucd.GB_LF) != 0 ||
		br1&(ucd.GB_Control|ucd.GB_CR|ucd.GB_LF) != 0 {
		return true // Rules GB4 && GB5
	} else if br0 == ucd.GB_L && br1&(ucd.GB_L|ucd.GB_V|ucd.GB_LV|ucd.GB_LVT) != 0 { // rule GB6
		return false
	} else if br0&(ucd.GB_LV|ucd.GB_V) != 0 && br1&(ucd.GB_V|ucd.GB_T) != 0 {
		return false // rule GB7
	} else if br0&(ucd.GB_LVT|ucd.GB_T) != 0 && br1 == ucd.GB_T {
		return false // rule GB8
	} else if br1&(ucd.GB_Extend|ucd.GB_ZWJ) != 0 {
		return false // Rule GB9
	} else if br1 == ucd.GB_SpacingMark {
		return false // Rule GB9a
	} else if br0 == ucd.GB_Prepend {
		return false // Rule GB9b
	} else if triggerGB9c {
		return false // Rule GB9c
	} else if triggerGB11 { // Rule GB11
		return false
	} else if triggerGB12_13 {
		return false // Rule GB12 && GB13
	}

	return true // Rule GB999
}

// update `isPrevGraphemeRIOdd` used for the rules GB12 and GB13
// and returns `true` if one of them triggered
func (cr *cursor) updateGraphemeRIOdd() (trigger bool) {
	if cr.grapheme == ucd.GB_Regional_Indicator {
		trigger = cr.isPrevGraphemeRIOdd
		cr.isPrevGraphemeRIOdd = !cr.isPrevGraphemeRIOdd // switch the parity
	} else {
		cr.isPrevGraphemeRIOdd = false
	}
	return trigger
}

// see rule GB11
type pictoSequenceState uint8

const (
	noPictoSequence pictoSequenceState = iota // we are not in a sequence
	inPictoExtend                             // we are in (ExtendedPic)(Extend*) pattern
	seenPictoZWJ                              // we have seen (ExtendedPic)(Extend*)(ZWJ)
)

// update the `pictoSequence` state used for rule GB11 pattern :
// (ExtendedPic)(Extend*)(ZWJ)(ExtendedPic)
// and returns true if we matched one
func (cr *cursor) updatePictoSequence() bool {
	switch cr.pictoSequence {
	case noPictoSequence:
		// we are not in a sequence yet, start it if we have an ExtendedPic
		if cr.isExtentedPic {
			cr.pictoSequence = inPictoExtend
		}
		return false
	case inPictoExtend:
		if cr.grapheme == ucd.GB_Extend {
			// continue the sequence with an Extend rune
		} else if cr.grapheme == ucd.GB_ZWJ {
			// close the variable part of the sequence with (ZWJ)
			cr.pictoSequence = seenPictoZWJ
		} else {
			// stop the sequence
			cr.pictoSequence = noPictoSequence
		}
		return false
	case seenPictoZWJ:
		// trigger GB11 if we have an ExtendedPic,
		// and reset the sequence
		if cr.isExtentedPic {
			cr.pictoSequence = inPictoExtend
			return true
		}
		cr.pictoSequence = noPictoSequence
		return false
	default:
		panic("exhaustive switch")
	}
}

// see rule GB9c
type indicCBSequenceState uint8

const (
	noIndicCBSequence   indicCBSequenceState = iota // we are not in a sequence
	inIndicCBSequence                               // we are in (Consonant) (Extend Linker)* pattern
	seenIndicCBSequence                             // we have seen (Consonant) (Extend Linker)* (Linker) (Extend Linker)*
)

// update the `indicConjunctBreakSequence` state used for rule CB9c pattern :
// (Consonant) (Extend Linker)* (Linker) (Extend Linker)* (Consonant)
// and returns true if we matched one
func (cr *cursor) updateIndicConjunctBreakSequence() bool {
	cb := cr.indicConjunctBreak
	switch cr.indicConjunctBreakSequence {
	case noIndicCBSequence:
		// we are not in a sequence yet, start it if we have a Consonant
		if cb == ucd.ICBConsonant {
			cr.indicConjunctBreakSequence = inIndicCBSequence
		}
		return false
	case inIndicCBSequence:
		if cb == ucd.ICBExtend {
			// continue the sequence
		} else if cb == ucd.ICBLinker {
			// we now have at least on Linker
			cr.indicConjunctBreakSequence = seenIndicCBSequence
		} else if cb == ucd.ICBConsonant {
			// reset the sequence
		} else {
			// stop the sequence
			cr.indicConjunctBreakSequence = noIndicCBSequence
		}
		return false
	case seenIndicCBSequence:
		if cb&(ucd.ICBExtend|ucd.ICBLinker) != 0 {
			// continue the sequence
			return false
		} else if cb == ucd.ICBConsonant {
			// start a new sequence
			cr.indicConjunctBreakSequence = inIndicCBSequence
			return true
		} else {
			// stop the sequence
			cr.indicConjunctBreakSequence = noIndicCBSequence
			return false
		}
	default:
		panic("exhaustive switch")
	}
}

// -----------------------------------------------------------------------
// ------------------------- Word boundaries -----------------------------
// -----------------------------------------------------------------------

// update `isPrevWordRIOdd` used for the rules WB15 and WB16
// and returns `true` if one of them triggered
func (cr *cursor) updateWordRIOdd() (trigger bool) {
	if cr.word == ucd.WB_ExtendFormat {
		return false // skip
	}

	if cr.word == ucd.WB_Regional_Indicator {
		trigger = cr.isPrevWordRIOdd
		cr.isPrevWordRIOdd = !cr.isPrevWordRIOdd // switch the parity
	} else {
		cr.isPrevWordRIOdd = false
	}
	return trigger
}

// Apply the Word_Boundary_Rules and returns true if we are at a
// word boundary.
// removePrevNoExtend is true if the index [prevWordNoExtend]
// should be marked as NOT being a word boundary
// See https://unicode.org/reports/tr29/#Word_Boundary_Rules
func (cr *cursor) applyWordBoundaryRules(i int) (isWordBoundary, removePrevNoExtend bool) {
	triggerWB15_16 := cr.updateWordRIOdd()

	prevPrev, prev, current := cr.prevPrevWord, cr.prevWord, cr.word

	// we apply Rules WB1 and WB2 at the end of the main loop

	isAfterNoExtend := cr.prevWordNoExtend == i-1

	if cr.prev == '\u000D' && cr.r == '\u000A' { // Rule WB3
		isWordBoundary = false
	} else if prev == ucd.WB_NewlineCRLF && isAfterNoExtend {
		// The extra check for prevWordNoExtend is to correctly handle sequences like
		// Newline ÷ Extend × Extend
		// since we have not skipped ExtendFormat yet.
		isWordBoundary = true // Rule WB3a
	} else if current == ucd.WB_NewlineCRLF {
		isWordBoundary = true // Rule WB3b
	} else if cr.prev == 0x200D && cr.isExtentedPic {
		isWordBoundary = false // Rule WB3c
	} else if prev == ucd.WB_WSegSpace &&
		current == ucd.WB_WSegSpace && isAfterNoExtend {
		isWordBoundary = false // Rule WB3d
	} else if current == ucd.WB_ExtendFormat {
		isWordBoundary = false // Rules WB4
	} else if prev&(ucd.WB_ALetter|ucd.WB_Hebrew_Letter|ucd.WB_Numeric) != 0 &&
		current&(ucd.WB_ALetter|ucd.WB_Hebrew_Letter|ucd.WB_Numeric) != 0 {
		isWordBoundary = false // Rules WB5, WB8, WB9, WB10
	} else if prev == ucd.WB_Katakana && current == ucd.WB_Katakana {
		isWordBoundary = false // Rule WB13
	} else if prev&(ucd.WB_ALetter|ucd.WB_Hebrew_Letter|ucd.WB_Numeric|ucd.WB_Katakana|ucd.WB_ExtendNumLet) != 0 &&
		current == ucd.WB_ExtendNumLet {
		isWordBoundary = false // Rule WB13a
	} else if prev == ucd.WB_ExtendNumLet &&
		current&(ucd.WB_ALetter|ucd.WB_Hebrew_Letter|ucd.WB_Numeric|ucd.WB_Katakana) != 0 {
		isWordBoundary = false // Rule WB13b
	} else if prevPrev&(ucd.WB_ALetter|ucd.WB_Hebrew_Letter) != 0 &&
		prev&(ucd.WB_MidLetter|ucd.WB_MidNumLet|ucd.WB_Single_Quote) != 0 &&
		current&(ucd.WB_ALetter|ucd.WB_Hebrew_Letter) != 0 {
		removePrevNoExtend = true // Rule WB6
		isWordBoundary = false    // Rule WB7
	} else if prev == ucd.WB_Hebrew_Letter && current == ucd.WB_Single_Quote {
		isWordBoundary = false // Rule WB7a
	} else if prevPrev == ucd.WB_Hebrew_Letter && cr.prev == 0x0022 &&
		current == ucd.WB_Hebrew_Letter {
		removePrevNoExtend = true // Rule WB7b
		isWordBoundary = false    // Rule WB7c
	} else if (prevPrev == ucd.WB_Numeric && current == ucd.WB_Numeric) &&
		prev&(ucd.WB_MidNum|ucd.WB_MidNumLet|ucd.WB_Single_Quote) != 0 {
		isWordBoundary = false    // Rule WB11
		removePrevNoExtend = true // Rule WB12
	} else if triggerWB15_16 {
		isWordBoundary = false // Rule WB15 and WB16
	} else {
		isWordBoundary = true // Rule WB999
	}

	return isWordBoundary, removePrevNoExtend
}
