// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package segmenter

import (
	ucd "github.com/go-text/typesetting/internal/unicodedata"
)

// Apply the Line Breaking Rules and returns the computed break opportunity
// See https://unicode.org/reports/tr14/#BreakingRules
func (cr *cursor) applyLineBoundaryRules() breakOpportunity {
	// start by attributing the break class for the current rune
	cr.ruleLB1()

	triggerNumSequence := cr.updateNumSequence()

	brm1, br0, br1, br2 := cr.prevPrevLine, cr.prevLine, cr.line, cr.nextLine

	// LB4 and LB5
	// BK !
	// CR !
	// LF !
	// NL !
	// (CR × LF is actually handled in rule LB6)
	if br0&(ucd.LB_BK|ucd.LB_LF|ucd.LB_NL) != 0 ||
		(br0 == ucd.LB_CR && cr.r != '\n') {
		return breakMandatory
	}

	// LB6 : × ( BK | CR | LF | NL )
	// LB7
	// × SP
	// × ZW
	if br1&(ucd.LB_BK|ucd.LB_CR|ucd.LB_LF|ucd.LB_NL|ucd.LB_SP|ucd.LB_ZW) != 0 {
		return breakProhibited
	}

	// LB 8
	// there is a catch here : prevLine or beforeSpace are not always
	// computed at index i-1, because of rules LB9 and LB10
	// however, rule LB8 and LB8a applies before LB9 and LB10, meaning
	// we need to use the real class
	if cr.beforeSpaceLineRaw == ucd.LB_ZW { // rule LB8 : ZW SP* ÷
		return breakAllowed
	} else if cr.prevLineRaw == ucd.LB_ZWJ { // rule LB8a : ZWJ ×
		return breakProhibited
	}

	// rule LB9 : "Do not break a combining character sequence"
	// where X is any line break class except BK, CR, LF, NL, SP, or ZW.
	// see also [endIteration]
	if br1&(ucd.LB_CM|ucd.LB_ZWJ) != 0 && br0&(ucd.LB_BK|ucd.LB_CR|ucd.LB_LF|ucd.LB_NL|ucd.LB_SP|ucd.LB_ZW) == 0 {
		return breakProhibited
	}

	// LB11
	// × WJ
	// WJ ×
	if br0 == ucd.LB_WJ || br1 == ucd.LB_WJ {
		return breakProhibited
	}

	// LB12 : GL ×
	// LB12a : [^SP BA HY HH] × GL
	if br0 == ucd.LB_GL ||
		br0&(ucd.LB_SP|ucd.LB_BA|ucd.LB_HY|ucd.LB_HH) == 0 && br1 == ucd.LB_GL {
		return breakProhibited
	}

	// rule LB13
	// × CL
	// × CP
	// × EX
	// × SY
	if br1&(ucd.LB_CL|ucd.LB_CP|ucd.LB_EX|ucd.LB_SY) != 0 {
		return breakProhibited
	}

	spaceM1 := cr.beforeSpacesLine

	// LB14 : OP SP* ×
	if spaceM1 == ucd.LB_OP {
		return breakProhibited
	}

	// LB15a Do not break after an unresolved initial punctuation that lies at the start of the line, after a space, after opening punctuation, or after an unresolved quotation mark, even after spaces.
	// (sot | BK | CR | LF | NL | OP | QU | GL | SP | ZW) [\p{Pi}&QU] SP* ×
	spaceM2 := cr.prevBeforeSpacesLine
	if (cr.beforeSpacesIndex == 0 || spaceM2&(ucd.LB_BK|ucd.LB_CR|ucd.LB_LF|ucd.LB_NL|ucd.LB_OP|ucd.LB_QU|ucd.LB_GL|ucd.LB_SP|ucd.LB_ZW) != 0) &&
		(ucd.LookupGeneralCategory(cr.beforeSpaces) == ucd.Pi && spaceM1 == ucd.LB_QU) {
		return breakProhibited
	}
	// LB15b Do not break before an unresolved final punctuation that lies at the end of the line, before a space, before a prohibited break, or before an unresolved quotation mark, even after spaces.
	// × [\p{Pf}&QU] ( SP | GL | WJ | CL | QU | CP | EX | IS | SY | BK | CR | LF | NL | ZW | eot)
	if (cr.generalCategory == ucd.Pf && br1 == ucd.LB_QU) && (br2&(ucd.LB_SP|ucd.LB_GL|ucd.LB_WJ|ucd.LB_CL|ucd.LB_QU|ucd.LB_CP|ucd.LB_EX|ucd.LB_IS|ucd.LB_SY|ucd.LB_BK|ucd.LB_CR|ucd.LB_LF|ucd.LB_NL|ucd.LB_ZW) != 0 || cr.index == cr.len-1) {
		return breakProhibited
	} else if br0 == ucd.LB_SP && br1 == ucd.LB_IS && br2 == ucd.LB_NU {
		// LB15c Break before a decimal mark that follows a space, for instance, in ‘subtract .5’.
		// SP ÷ IS NU
		return breakAllowed
	} else if br1 == ucd.LB_IS {
		// LB15d Otherwise, do not break before ‘;’, ‘,’, or ‘.’, even after spaces.
		// × IS
		return breakProhibited
	}

	// LB16 : (CL | CP) SP* × NS
	if spaceM1&(ucd.LB_CL|ucd.LB_CP) != 0 && br1 == ucd.LB_NS {
		return breakProhibited
	}

	// LB17 : B2 SP* × B2
	if spaceM1 == ucd.LB_B2 && br1 == ucd.LB_B2 {
		return breakProhibited
	}

	// LB18 : SP ÷
	if br0 == ucd.LB_SP {
		return breakAllowed
	}

	// LB19
	// × [ QU - \p{Pi} ]
	// [ QU - \p{Pf} ] ×
	if (br1 == ucd.LB_QU && cr.generalCategory != ucd.Pi) || (br0 == ucd.LB_QU && cr.prevGeneralCategory != ucd.Pf) {
		return breakProhibited
	}
	// LB 19a
	// [^$EastAsian] × QU
	// × QU ( [^$EastAsian] | eot )
	// QU × [^$EastAsian]
	// ( sot | [^$EastAsian] ) QU ×
	if (br1 == ucd.LB_QU && !ucd.IsLargeEastAsian(cr.prev)) ||
		(br1 == ucd.LB_QU && (cr.index == cr.len-1 || !ucd.IsLargeEastAsian(cr.next))) ||
		(br0 == ucd.LB_QU && !ucd.IsLargeEastAsian(cr.r)) ||
		((cr.isPreviousSot || !ucd.IsLargeEastAsian(cr.prevPrev)) && br0 == ucd.LB_QU) {
		return breakProhibited
	}

	// LB20
	// ÷ CB
	// CB ÷
	if br0 == ucd.LB_CB || br1 == ucd.LB_CB {
		return breakAllowed
	}
	// LB20a Do not break after a word-initial hyphen.
	// ( sot | BK | CR | LF | NL | SP | ZW | CB | GL ) ( HY | HH ) × ( AL | HL )
	if (cr.isPreviousSot || brm1&(ucd.LB_BK|ucd.LB_CR|ucd.LB_LF|ucd.LB_NL|ucd.LB_SP|ucd.LB_ZW|ucd.LB_CB|ucd.LB_GL) != 0) &&
		br0&(ucd.LB_HY|ucd.LB_HH) != 0 && br1&(ucd.LB_AL|ucd.LB_HL) != 0 {
		return breakProhibited
	}

	// LB21
	// × BA
	// × HH
	// × HY
	// × NS
	// BB ×
	if br1&(ucd.LB_BA|ucd.LB_HH|ucd.LB_HY|ucd.LB_NS) != 0 || br0 == ucd.LB_BB {
		return breakProhibited
	}
	// LB21a : HL (HY | HH) × [^HL]
	if cr.prevPrevLine == ucd.LB_HL && br0&(ucd.LB_HY|ucd.LB_HH) != 0 && br1 != ucd.LB_HL {
		return breakProhibited
	}
	// LB21b : SY × HL
	if br0 == ucd.LB_SY && br1 == ucd.LB_HL {
		return breakProhibited
	}

	// LB22 : × IN
	if br1 == ucd.LB_IN {
		return breakProhibited
	}

	// LB23
	// (AL | HL) × NU
	if br0&(ucd.LB_AL|ucd.LB_HL) != 0 && br1 == ucd.LB_NU {
		return breakProhibited
	}
	// NU × (AL | HL)
	if br0 == ucd.LB_NU && br1&(ucd.LB_AL|ucd.LB_HL) != 0 {
		return breakProhibited
	}
	// LB23a
	// PR × (ID | EB | EM)
	if br0 == ucd.LB_PR && br1&(ucd.LB_ID|ucd.LB_EB|ucd.LB_EM) != 0 {
		return breakProhibited
	}
	// (ID | EB | EM) × PO
	if br0&(ucd.LB_ID|ucd.LB_EB|ucd.LB_EM) != 0 && br1 == ucd.LB_PO {
		return breakProhibited
	}

	// LB24
	// (PR | PO) × (AL | HL)
	if br0&(ucd.LB_PR|ucd.LB_PO) != 0 && br1&(ucd.LB_AL|ucd.LB_HL) != 0 {
		return breakProhibited
	}
	// (AL | HL) × (PR | PO)
	if br0&(ucd.LB_AL|ucd.LB_HL) != 0 && br1&(ucd.LB_PR|ucd.LB_PO) != 0 {
		return breakProhibited
	}

	// LB25
	// (PR | PO) × ( OP | HY )? NU
	if br0&(ucd.LB_PR|ucd.LB_PO) != 0 && (br1 == ucd.LB_NU ||
		br1&(ucd.LB_OP|ucd.LB_HY) != 0 && cr.nextLine == ucd.LB_NU) {
		return breakProhibited
	}
	// ( OP | HY | IS ) × NU
	if br0&(ucd.LB_OP|ucd.LB_HY|ucd.LB_IS) != 0 && br1 == ucd.LB_NU {
		return breakProhibited
	}
	// NU × (NU | SY | IS)
	if br0 == ucd.LB_NU && br1&(ucd.LB_NU|ucd.LB_SY|ucd.LB_IS) != 0 {
		return breakProhibited
	}
	// NU (NU | SY | IS)* × (NU | SY | IS | CL | CP )
	if triggerNumSequence {
		return breakProhibited
	}

	// LB26
	// JL × (JL | JV | H2 | H3)
	if br0 == ucd.LB_JL && br1&(ucd.LB_JL|ucd.LB_JV|ucd.LB_H2|ucd.LB_H3) != 0 {
		return breakProhibited
	}
	// (JV | H2) × (JV | JT)
	if br0&(ucd.LB_JV|ucd.LB_H2) != 0 && br1&(ucd.LB_JV|ucd.LB_JT) != 0 {
		return breakProhibited
	}
	// (JT | H3) × JT
	if br0&(ucd.LB_JT|ucd.LB_H3) != 0 && br1 == ucd.LB_JT {
		return breakProhibited
	}

	// LB27
	// (JL | JV | JT | H2 | H3) × PO
	if br0&(ucd.LB_JL|ucd.LB_JV|ucd.LB_JT|ucd.LB_H2|ucd.LB_H3) != 0 && br1 == ucd.LB_PO {
		return breakProhibited
	}
	// PR × (JL | JV | JT | H2 | H3)
	if br0 == ucd.LB_PR && br1&(ucd.LB_JL|ucd.LB_JV|ucd.LB_JT|ucd.LB_H2|ucd.LB_H3) != 0 {
		return breakProhibited
	}

	// LB28a Do not break inside the orthographic syllables of Brahmic scripts.
	// AP × (AK | [◌] | AS)
	if br0 == ucd.LB_AP && (cr.r == 0x25CC || br1&(ucd.LB_AK|ucd.LB_AS) != 0) ||
		// (AK | [◌] | AS) × (VF | VI)
		(cr.isPrevDottedCircle || br0&(ucd.LB_AK|ucd.LB_AS) != 0) && br1&(ucd.LB_VF|ucd.LB_VI) != 0 ||
		// (AK | [◌] | AS) VI × (AK | [◌])
		(cr.isPrevPrevDottedCircle || brm1&(ucd.LB_AK|ucd.LB_AS) != 0) && br0 == ucd.LB_VI && (br1 == ucd.LB_AK || cr.r == 0x25CC) ||
		// (AK | [◌] | AS) × (AK | [◌] | AS) VF
		(cr.isPrevDottedCircle || br0&(ucd.LB_AK|ucd.LB_AS) != 0) && (cr.r == 0x25CC || br1&(ucd.LB_AK|ucd.LB_AS) != 0) && br2 == ucd.LB_VF {
		return breakProhibited
	}
	// LB28 : (AL | HL) × (AL | HL)
	if br0&(ucd.LB_AL|ucd.LB_HL) != 0 && br1&(ucd.LB_AL|ucd.LB_HL) != 0 {
		return breakProhibited
	}

	// LB29 : IS × (AL | HL)
	if br0 == ucd.LB_IS && br1&(ucd.LB_AL|ucd.LB_HL) != 0 {
		return breakProhibited
	}

	// LB30a
	// (RI RI)* RI × RI
	if cr.isPrevLinebreakRIOdd && cr.line == ucd.LB_RI {
		return breakProhibited
	}

	// LB30b
	// EB × EM
	if cr.prevLine == ucd.LB_EB && cr.line == ucd.LB_EM {
		return breakProhibited
	}
	// [\p{Extended_Pictographic}&\p{Cn}] × EM
	if cr.isPrevNonAssignedExtendedPic && cr.line == ucd.LB_EM {
		return breakProhibited
	}

	// LB 30
	// (AL | HL | NU) × [OP-[\p{ea=F}\p{ea=W}\p{ea=H}]]
	if cr.prevLine&(ucd.LB_AL|ucd.LB_HL|ucd.LB_NU) != 0 &&
		cr.line == ucd.LB_OP && !ucd.IsLargeEastAsian(cr.r) {
		return breakProhibited
	}
	// [CP-[\p{ea=F}\p{ea=W}\p{ea=H}]] × (AL | HL | NU)
	if cr.prevLine == ucd.LB_CP && !ucd.IsLargeEastAsian(cr.prev) &&
		cr.line&(ucd.LB_AL|ucd.LB_HL|ucd.LB_NU) != 0 {
		return breakProhibited
	}

	return breakEmpty
}

// breakOpportunity is a convenient enum,
// mapped to the LineBreak and MandatoryBreak properties,
// avoiding too many bit operations
type breakOpportunity uint8

const (
	breakEmpty      breakOpportunity = iota // not specified
	breakProhibited                         // no break
	breakAllowed                            // direct break (can always break here)
	breakMandatory                          // break is mandatory (implies breakAllowed)
)

// apply rule LB1 to resolve break classses AI, SG, XX, SA and CJ.
// We use the default values specified in https://unicode.org/reports/tr14/#BreakingRules.
func (cr *cursor) ruleLB1() {
	switch cr.line {
	case ucd.LB_AI, ucd.LB_SG, 0:
		cr.line = ucd.LB_AL
	case ucd.LB_SA:
		if cr.generalCategory == ucd.Mn || cr.generalCategory == ucd.Mc {
			cr.line = ucd.LB_CM
		} else {
			cr.line = ucd.LB_AL
		}
	case ucd.LB_CJ:
		cr.line = ucd.LB_NS
	}
}

type numSequenceState uint8

const (
	noNumSequence numSequenceState = iota // we are not in a sequence
	inNumSequence                         // we are in NU (NU | SY | IS)*
	seenCloseNum                          // we are at NU (NU | SY | IS)* (CL | CP)?
)

// update the `numSequence` state used for rule LB25
// and returns true if we matched one
func (cr *cursor) updateNumSequence() bool {
	// note that rule LB9 also apply : (CM|ZWJ) do not change
	// the flag
	if cr.line&(ucd.LB_CM|ucd.LB_ZWJ) != 0 {
		return false
	}

	switch cr.numSequence {
	case noNumSequence:
		if cr.line == ucd.LB_NU { // start a sequence
			cr.numSequence = inNumSequence
		}
		return false
	case inNumSequence:
		switch cr.line {
		case ucd.LB_NU, ucd.LB_SY, ucd.LB_IS:
			// NU (NU | SY | IS)* × (NU | SY | IS) : the sequence continue
			return true
		case ucd.LB_CL, ucd.LB_CP:
			// NU (NU | SY | IS)* × (CL | CP)
			cr.numSequence = seenCloseNum
			return true
		case ucd.LB_PO, ucd.LB_PR:
			// NU (NU | SY | IS)* × (PO | PR) : close the sequence
			cr.numSequence = noNumSequence
			return true
		default:
			cr.numSequence = noNumSequence
			return false
		}
	case seenCloseNum:
		cr.numSequence = noNumSequence // close the sequence anyway
		if cr.line&(ucd.LB_PO|ucd.LB_PR) != 0 {
			// NU (NU | SY | IS)* (CL | CP) × (PO | PR)
			return true
		}
		return false
	default:
		panic("exhaustive switch")
	}
}

// startIteration updates the cursor properties, setting the current
// rune to text[i].
// Some properties depending on the context are rather
// updated in the previous `endIteration` call.
func (cr *cursor) startIteration(text []rune, i int) {
	cr.index = i

	cr.prevPrev = cr.prev
	cr.prev = cr.r
	if i < len(text) {
		cr.r = text[i]
	} else {
		cr.r = paragraphSeparator
	}
	if i == len(text) {
		cr.next = 0
	} else if i == len(text)-1 {
		// we fill in the last element of `attrs` by assuming
		// there's a paragraph separators off the end of text
		cr.next = paragraphSeparator
	} else {
		cr.next = text[i+1]
	}

	// query general unicode properties for the current rune
	cr.prevGeneralCategory = cr.generalCategory
	cr.generalCategory = ucd.LookupGeneralCategory(cr.r)

	cr.isExtentedPic = ucd.IsExtendedPictographic(cr.r)
	cr.indicConjunctBreak = ucd.LookupIndicConjunctBreak(cr.r)

	// prevPrevLine and prevLine are handled in endIteration
	cr.line = cr.nextLine
	cr.nextLine = ucd.LookupLineBreak(cr.next)

	cr.prevGrapheme = cr.grapheme
	cr.grapheme = ucd.LookupGraphemeBreak(cr.r)

	if cr.word != ucd.WB_ExtendFormat {
		cr.prevPrevWord = cr.prevWord
		cr.prevWord = cr.word
		cr.prevWordNoExtend = i - 1
	}
	cr.word = ucd.LookupWordBreak(cr.r)
}

// end the current iteration, computing some of the properties
// required for the next rune and respecting rule LB9 and LB10
func (cr *cursor) endIteration() {
	// start by handling rule LB9 and LB10
	if cr.line&(ucd.LB_CM|ucd.LB_ZWJ) != 0 {
		isLB10 := cr.prevLine&(ucd.LB_BK|ucd.LB_CR|ucd.LB_LF|ucd.LB_NL|ucd.LB_SP|ucd.LB_ZW) != 0
		if cr.index == 0 || isLB10 { // Rule LB10
			cr.prevLine = ucd.LB_AL
		} // else rule LB9 : ignore the rune for prevLine and prevPrevLine

	} else { // regular update
		cr.isPreviousSot = cr.index == 0

		// keep track of the rune before the spaces
		if cr.line != ucd.LB_SP {
			cr.beforeSpaces = cr.r
			cr.prevBeforeSpaces = cr.prev
			cr.beforeSpacesIndex = cr.index

			cr.beforeSpacesLine = cr.line
			cr.prevBeforeSpacesLine = cr.prevLine
		}

		cr.prevPrevLine = cr.prevLine
		cr.prevLine = cr.line

		cr.isPrevPrevDottedCircle = cr.isPrevDottedCircle
		cr.isPrevDottedCircle = cr.r == 0x25CC

		cr.isPrevNonAssignedExtendedPic = cr.isExtentedPic && cr.generalCategory == ucd.Unassigned
	}

	cr.prevLineRaw = cr.line

	// keep track of the rune before the spaces
	if cr.prevLine != ucd.LB_SP {
		cr.beforeSpaceLineRaw = cr.line
	}

	// update RegionalIndicator parity used for LB30a
	if cr.line == ucd.LB_RI {
		cr.isPrevLinebreakRIOdd = !cr.isPrevLinebreakRIOdd
	} else if cr.line&(ucd.LB_CM|ucd.LB_ZWJ) == 0 { // beware of the rule LB9: (CM|ZWJ) ignore the update
		cr.isPrevLinebreakRIOdd = false
	}
}
