// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package segmenter

import (
	ucd "github.com/go-text/typesetting/unicodedata"
)

// -----------------------------------------------------------------------
// ------------------------- Grapheme boundaries -------------------------
// -----------------------------------------------------------------------

// Apply the Grapheme_Cluster_Boundary_Rules and returns a true if we are
// at a grapheme break.
// See https://unicode.org/reports/tr29/#Grapheme_Cluster_Boundary_Rules
func (cr *cursor) applyGraphemeBoundaryRules() bool {
	triggerGB11 := cr.updatePictoSequence()    // apply rule GB11
	triggerGB12_13 := cr.updateGraphemeRIOdd() // apply rule GB12 and GB13

	br0, br1 := cr.prevGrapheme, cr.grapheme
	if cr.r == '\n' && cr.prev == '\r' {
		return false // Rule GB3
	} else if br0 == ucd.GraphemeBreakControl || br0 == ucd.GraphemeBreakCR || br0 == ucd.GraphemeBreakLF ||
		br1 == ucd.GraphemeBreakControl || br1 == ucd.GraphemeBreakCR || br1 == ucd.GraphemeBreakLF {
		return true // Rules GB4 && GB5
	} else if br0 == ucd.GraphemeBreakL &&
		(br1 == ucd.GraphemeBreakL || br1 == ucd.GraphemeBreakV || br1 == ucd.GraphemeBreakLV || br1 == ucd.GraphemeBreakLVT) { // rule GB6
		return false
	} else if (br0 == ucd.GraphemeBreakLV || br0 == ucd.GraphemeBreakV) && (br1 == ucd.GraphemeBreakV || br1 == ucd.GraphemeBreakT) {
		return false // rule GB7
	} else if (br0 == ucd.GraphemeBreakLVT || br0 == ucd.GraphemeBreakT) && br1 == ucd.GraphemeBreakT {
		return false // rule GB8
	} else if br1 == ucd.GraphemeBreakExtend || br1 == ucd.GraphemeBreakZWJ {
		return false // Rule GB9
	} else if br1 == ucd.GraphemeBreakSpacingMark {
		return false // Rule GB9a
	} else if br0 == ucd.GraphemeBreakPrepend {
		return false // Rule GB9b
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
	if cr.grapheme == ucd.GraphemeBreakRegional_Indicator {
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
		if cr.grapheme == ucd.GraphemeBreakExtend {
			// continue the sequence with an Extend rune
		} else if cr.grapheme == ucd.GraphemeBreakZWJ {
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

// -----------------------------------------------------------------------
// ------------------------- Word boundaries -----------------------------
// -----------------------------------------------------------------------

// update `isPrevWordRIOdd` used for the rules WB15 and WB16
// and returns `true` if one of them triggered
func (cr *cursor) updateWordRIOdd() (trigger bool) {
	if cr.word == ucd.WordBreakExtendFormat {
		return false // skip
	}

	if cr.word == ucd.WordBreakRegional_Indicator {
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
	} else if prev == ucd.WordBreakNewlineCRLF && isAfterNoExtend {
		// The extra check for prevWordNoExtend is to correctly handle sequences like
		// Newline ÷ Extend × Extend
		// since we have not skipped ExtendFormat yet.
		isWordBoundary = true // Rule WB3a
	} else if current == ucd.WordBreakNewlineCRLF {
		isWordBoundary = true // Rule WB3b
	} else if cr.prev == 0x200D && cr.isExtentedPic {
		isWordBoundary = false // Rule WB3c
	} else if prev == ucd.WordBreakWSegSpace &&
		current == ucd.WordBreakWSegSpace && isAfterNoExtend {
		isWordBoundary = false // Rule WB3d
	} else if current == ucd.WordBreakExtendFormat {
		isWordBoundary = false // Rules WB4
	} else if (prev == ucd.WordBreakALetter || prev == ucd.WordBreakHebrew_Letter || prev == ucd.WordBreakNumeric) &&
		(current == ucd.WordBreakALetter || current == ucd.WordBreakHebrew_Letter || current == ucd.WordBreakNumeric) {
		isWordBoundary = false // Rules WB5, WB8, WB9, WB10
	} else if prev == ucd.WordBreakKatakana && current == ucd.WordBreakKatakana {
		isWordBoundary = false // Rule WB13
	} else if (prev == ucd.WordBreakALetter ||
		prev == ucd.WordBreakHebrew_Letter ||
		prev == ucd.WordBreakNumeric ||
		prev == ucd.WordBreakKatakana ||
		prev == ucd.WordBreakExtendNumLet) &&
		current == ucd.WordBreakExtendNumLet {
		isWordBoundary = false // Rule WB13a
	} else if prev == ucd.WordBreakExtendNumLet &&
		(current == ucd.WordBreakALetter || current == ucd.WordBreakHebrew_Letter || current == ucd.WordBreakNumeric ||
			current == ucd.WordBreakKatakana) {
		isWordBoundary = false // Rule WB13b
	} else if (prevPrev == ucd.WordBreakALetter || prevPrev == ucd.WordBreakHebrew_Letter) &&
		(prev == ucd.WordBreakMidLetter || prev == ucd.WordBreakMidNumLet || prev == ucd.WordBreakSingle_Quote) &&
		(current == ucd.WordBreakALetter || current == ucd.WordBreakHebrew_Letter) {
		removePrevNoExtend = true // Rule WB6
		isWordBoundary = false    // Rule WB7
	} else if prev == ucd.WordBreakHebrew_Letter && current == ucd.WordBreakSingle_Quote {
		isWordBoundary = false // Rule WB7a
	} else if prevPrev == ucd.WordBreakHebrew_Letter && cr.prev == 0x0022 &&
		current == ucd.WordBreakHebrew_Letter {
		removePrevNoExtend = true // Rule WB7b
		isWordBoundary = false    // Rule WB7c
	} else if (prevPrev == ucd.WordBreakNumeric && current == ucd.WordBreakNumeric) &&
		(prev == ucd.WordBreakMidNum || prev == ucd.WordBreakMidNumLet ||
			prev == ucd.WordBreakSingle_Quote) {
		isWordBoundary = false    // Rule WB11
		removePrevNoExtend = true // Rule WB12
	} else if triggerWB15_16 {
		isWordBoundary = false // Rule WB15 and WB16
	} else {
		isWordBoundary = true // Rule WB999
	}

	return isWordBoundary, removePrevNoExtend
}
