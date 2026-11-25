package bidi

import (
	ucd "github.com/go-text/typesetting/internal/unicodedata"
)

// This implementation is a port based on the reference implementation found at:
// https://www.unicode.org/Public/PROGRAMS/BidiReferenceJava/
//
// described in Unicode Bidirectional Algorithm (UAX #9).
//
// Input:
// There are two levels of input to the algorithm, since clients may prefer to
// supply some information from out-of-band sources rather than relying on the
// default behavior.
//
// - Bidi class array
// - Bidi class array, with externally supplied base line direction
//
// Output:
// Output is separated into several stages:
//
//  - levels array over entire paragraph
//  - reordering array over entire paragraph
//  - levels array over line
//  - reordering array over line
//
// Note that for conformance to the Unicode Bidirectional Algorithm,
// implementations are only required to generate correct reordering and
// character directionality (odd or even levels) over a line. Generating
// identical level arrays over a line is not required. Bidi explicit format
// codes (LRE, RLE, LRO, RLO, PDF) and BN can be assigned arbitrary levels and
// positions as long as the rest of the input is properly reordered.
//
// As the algorithm is defined to operate on a single paragraph at a time, this
// implementation is written to handle single paragraphs. Thus rule P1 is
// presumed by this implementation-- the data provided to the implementation is
// assumed to be a single paragraph, and either contains no 'B' codes, or a
// single 'B' code at the end of the input. 'B' is allowed as input to
// illustrate how the algorithm assigns it a level.
//
// Also note that rules L3 and L4 depend on the rendering engine that uses the
// result of the bidi algorithm. This implementation assumes that the rendering
// engine expects combining marks in visual order (e.g. to the left of their
// base character in RTL runs) and that it adjusts the glyphs used to render
// mirrored characters that are in RTL runs so that they render appropriately.

// Level is the embedding level of a character. Even embedding levels indicate
// left-to-right order and odd levels indicate right-to-left order.
type Level int8

// The special level of -1 is reserved for undefined order,
const implicitLevel Level = -1

const unknownClass = 0

func (p *Paragraph) len() int { return len(p.initialTypes) }

// The algorithm. Does not include line-based processing (Rules L1, L2).
// These are applied later in the line-based phase of the algorithm.
func (p *Paragraph) run() {
	p.determineMatchingIsolates()

	// 1) determining the paragraph level
	// Rule P1 is the requirement for entering this algorithm.
	// Rules P2, P3.
	// If no externally supplied paragraph embedding level, use default.
	if p.embeddingLevel == implicitLevel {
		p.embeddingLevel = p.determineParagraphEmbeddingLevel(0, p.len())
	}

	// Initialize result levels to paragraph embedding level.
	setLevels(p.resultLevels, p.embeddingLevel)

	// 2) Explicit levels and directions
	// Rules X1-X8.
	p.determineExplicitEmbeddingLevels()

	// Rule X9.
	// We do not remove the embeddings, the overrides, the PDFs, and the BNs
	// from the string explicitly. But they are not copied into isolating run
	// sequences when they are created, so they are removed for all
	// practical purposes.

	// Rule X10.
	// Run remainder of algorithm one isolating run sequence at a time
	for _, seq := range p.determineIsolatingRunSequences() {
		// 3) resolving weak types
		// Rules W1-W7.
		seq.resolveWeakTypes()

		// 4a) resolving paired brackets
		// Rule N0
		resolvePairedBrackets(seq)

		// 4b) resolving neutral types
		// Rules N1-N3.
		seq.resolveNeutralTypes()

		// 5) resolving implicit embedding levels
		// Rules I1, I2.
		seq.resolveImplicitLevels()

		// Apply the computed levels and types
		seq.applyLevelsAndTypes()
	}

	// Assign appropriate levels to 'hide' LREs, RLEs, LROs, RLOs, PDFs, and
	// BNs. This is for convenience, so the resulting level array will have
	// a value for every character.
	p.assignLevelsToCharactersRemovedByX9()
}

// determineMatchingIsolates determines the matching PDI for each isolate
// initiator and vice versa.
//
// Definition BD9.
//
// At the end of this function:
//
//   - The member variable matchingPDI is set to point to the index of the
//     matching PDI character for each isolate initiator character. If there is
//     no matching PDI, it is set to the length of the input text. For other
//     characters, it is set to -1.
//   - The member variable matchingIsolateInitiator is set to point to the
//     index of the matching isolate initiator character for each PDI character.
//     If there is no matching isolate initiator, or the character is not a PDI,
//     it is set to -1.
func (p *Paragraph) determineMatchingIsolates() {
	for i := range p.matchingIsolateInitiator {
		p.matchingIsolateInitiator[i] = -1
	}

	for i := range p.matchingPDI {
		p.matchingPDI[i] = -1

		if t := p.resultTypes[i]; t&(ucd.BD_LRI|ucd.BD_RLI|ucd.BD_FSI) != 0 {
			depthCounter := 1
			for j := i + 1; j < p.len(); j++ {
				if u := p.resultTypes[j]; u&(ucd.BD_LRI|ucd.BD_RLI|ucd.BD_FSI) != 0 {
					depthCounter++
				} else if u == ucd.BD_PDI {
					if depthCounter--; depthCounter == 0 {
						p.matchingPDI[i] = j
						p.matchingIsolateInitiator[j] = i
						break
					}
				}
			}
			if p.matchingPDI[i] == -1 {
				p.matchingPDI[i] = p.len()
			}
		}
	}
}

// determineParagraphEmbeddingLevel reports the resolved paragraph direction of
// the substring limited by the given range [start, end).
//
// Determines the paragraph level based on rules P2, P3. This is also used
// in rule X5c to find if an FSI should resolve to LRI or RLI.
func (p *Paragraph) determineParagraphEmbeddingLevel(start, end int) Level {
	var strongType ucd.BidiClass = unknownClass

	// Rule P2.
	for i := start; i < end; i++ {
		if t := p.resultTypes[i]; t&(ucd.BD_L|ucd.BD_AL|ucd.BD_R) != 0 {
			strongType = t
			break
		} else if t&(ucd.BD_FSI|ucd.BD_LRI|ucd.BD_RLI) != 0 {
			i = p.matchingPDI[i] // skip over to the matching PDI
			// assert (i <= end)
		}
	}
	// Rule P3.
	switch strongType {
	case unknownClass: // none found
		// default embedding level when no strong types found is 0.
		return 0
	case ucd.BD_L:
		return 0
	default: // AL, R
		return 1
	}
}

const maxDepth = 125

// This stack will store the embedding levels and override and isolated
// statuses
type directionalStatusStack struct {
	stackCounter        int
	embeddingLevelStack [maxDepth + 1]Level
	overrideStatusStack [maxDepth + 1]ucd.BidiClass
	isolateStatusStack  [maxDepth + 1]bool
}

func (s *directionalStatusStack) empty()     { s.stackCounter = 0 }
func (s *directionalStatusStack) pop()       { s.stackCounter-- }
func (s *directionalStatusStack) depth() int { return s.stackCounter }

func (s *directionalStatusStack) push(level Level, overrideStatus ucd.BidiClass, isolateStatus bool) {
	s.embeddingLevelStack[s.stackCounter] = level
	s.overrideStatusStack[s.stackCounter] = overrideStatus
	s.isolateStatusStack[s.stackCounter] = isolateStatus
	s.stackCounter++
}

func (s *directionalStatusStack) lastEmbeddingLevel() Level {
	return s.embeddingLevelStack[s.stackCounter-1]
}

func (s *directionalStatusStack) lastDirectionalOverrideStatus() ucd.BidiClass {
	return s.overrideStatusStack[s.stackCounter-1]
}

func (s *directionalStatusStack) lastDirectionalIsolateStatus() bool {
	return s.isolateStatusStack[s.stackCounter-1]
}

// Determine explicit levels using rules X1 - X8
func (p *Paragraph) determineExplicitEmbeddingLevels() {
	var stack directionalStatusStack
	var overflowIsolateCount, overflowEmbeddingCount, validIsolateCount int

	// Rule X1.
	stack.push(p.embeddingLevel, ucd.BD_ON, false)

	for i, t := range p.resultTypes {
		// Rules X2, X3, X4, X5, X5a, X5b, X5c
		switch t {
		case ucd.BD_RLE, ucd.BD_LRE, ucd.BD_RLO, ucd.BD_LRO, ucd.BD_RLI, ucd.BD_LRI, ucd.BD_FSI:
			isIsolate := t&(ucd.BD_RLI|ucd.BD_LRI|ucd.BD_FSI) != 0
			isRTL := t&(ucd.BD_RLE|ucd.BD_RLO|ucd.BD_RLI) != 0

			// override if this is an FSI that resolves to RLI
			if t == ucd.BD_FSI {
				isRTL = (p.determineParagraphEmbeddingLevel(i+1, p.matchingPDI[i]) == 1)
			}
			if isIsolate {
				p.resultLevels[i] = stack.lastEmbeddingLevel()
				if stack.lastDirectionalOverrideStatus() != ucd.BD_ON {
					p.resultTypes[i] = stack.lastDirectionalOverrideStatus()
				}
			}

			var newLevel Level
			if isRTL {
				// least greater odd
				newLevel = (stack.lastEmbeddingLevel() + 1) | 1
			} else {
				// least greater even
				newLevel = (stack.lastEmbeddingLevel() + 2) &^ 1
			}

			if newLevel <= maxDepth && overflowIsolateCount == 0 && overflowEmbeddingCount == 0 {
				if isIsolate {
					validIsolateCount++
				}
				// Push new embedding level, override status, and isolated
				// status.
				// No check for valid stack counter, since the level check
				// suffices.
				switch t {
				case ucd.BD_LRO:
					stack.push(newLevel, ucd.BD_L, isIsolate)
				case ucd.BD_RLO:
					stack.push(newLevel, ucd.BD_R, isIsolate)
				default:
					stack.push(newLevel, ucd.BD_ON, isIsolate)
				}
				// Not really part of the spec
				if !isIsolate {
					p.resultLevels[i] = newLevel
				}
			} else {
				// This is an invalid explicit formatting character,
				// so apply the "Otherwise" part of rules X2-X5b.
				if isIsolate {
					overflowIsolateCount++
				} else { // !isIsolate
					if overflowIsolateCount == 0 {
						overflowEmbeddingCount++
					}
				}
			}

		// Rule X6a
		case ucd.BD_PDI:
			if overflowIsolateCount > 0 {
				overflowIsolateCount--
			} else if validIsolateCount == 0 {
				// do nothing
			} else {
				overflowEmbeddingCount = 0
				for !stack.lastDirectionalIsolateStatus() {
					stack.pop()
				}
				stack.pop()
				validIsolateCount--
			}
			p.resultLevels[i] = stack.lastEmbeddingLevel()

		// Rule X7
		case ucd.BD_PDF:
			// Not really part of the spec
			p.resultLevels[i] = stack.lastEmbeddingLevel()

			if overflowIsolateCount > 0 {
				// do nothing
			} else if overflowEmbeddingCount > 0 {
				overflowEmbeddingCount--
			} else if !stack.lastDirectionalIsolateStatus() && stack.depth() >= 2 {
				stack.pop()
			}

		case ucd.BD_B: // paragraph separator.
			// Rule X8.

			// These values are reset for clarity, in this implementation B
			// can only occur as the last code in the array.
			stack.empty()
			overflowIsolateCount = 0
			overflowEmbeddingCount = 0
			validIsolateCount = 0
			p.resultLevels[i] = p.embeddingLevel

		default:
			p.resultLevels[i] = stack.lastEmbeddingLevel()
			if stack.lastDirectionalOverrideStatus() != ucd.BD_ON {
				p.resultTypes[i] = stack.lastDirectionalOverrideStatus()
			}
		}
	}
}

type isolatingRunSequence struct {
	p *Paragraph

	indexes []int // indexes to the original string

	types          []ucd.BidiClass // type of each character using the index
	resolvedLevels []Level         // resolved levels after application of rules
	level          Level
	sos, eos       ucd.BidiClass
}

func (i *isolatingRunSequence) Len() int { return len(i.indexes) }

// Rule X10, second bullet: Determine the start-of-sequence (sos) and end-of-sequence (eos) types,
// either L or R, for each isolating run sequence.
func (p *Paragraph) isolatingRunSequence(indexes []int) *isolatingRunSequence {
	length := len(indexes)
	types := make([]ucd.BidiClass, length)
	for i, x := range indexes {
		types[i] = p.resultTypes[x]
	}

	// assign level, sos and eos
	prevChar := indexes[0] - 1
	for prevChar >= 0 && isRemovedByX9(p.initialTypes[prevChar]) {
		prevChar--
	}
	prevLevel := p.embeddingLevel
	if prevChar >= 0 {
		prevLevel = p.resultLevels[prevChar]
	}

	var succLevel Level
	lastType := types[length-1]
	if lastType&(ucd.BD_LRI|ucd.BD_RLI|ucd.BD_FSI) != 0 {
		succLevel = p.embeddingLevel
	} else {
		// the first character after the end of run sequence
		limit := indexes[length-1] + 1
		for ; limit < p.len() && isRemovedByX9(p.initialTypes[limit]); limit++ {
		}
		succLevel = p.embeddingLevel
		if limit < p.len() {
			succLevel = p.resultLevels[limit]
		}
	}
	level := p.resultLevels[indexes[0]]
	return &isolatingRunSequence{
		p:       p,
		indexes: indexes,
		types:   types,
		level:   level,
		sos:     typeForLevel(max(prevLevel, level)),
		eos:     typeForLevel(max(succLevel, level)),
	}
}

// Resolving weak types Rules W1-W7.
//
// Note that some weak types (EN, AN) remain after this processing is
// complete.
func (s *isolatingRunSequence) resolveWeakTypes() {
	// on entry, only these types remain
	// s.assertOnly(L, R, AL, EN, ES, ET, AN, CS, B, S, WS, ON, NSM, LRI, RLI, FSI, PDI)

	// Rule W1.
	// Changes all NSMs.
	precedingCharacterType := s.sos
	for i, t := range s.types {
		if t == ucd.BD_NSM {
			s.types[i] = precedingCharacterType
		} else {
			// if t.in(LRI, RLI, FSI, PDI) {
			// 	precedingCharacterType = ON
			// }
			precedingCharacterType = t
		}
	}

	// Rule W2.
	// EN does not change at the start of the run, because sos != AL.
	for i, t := range s.types {
		if t == ucd.BD_EN {
			for j := i - 1; j >= 0; j-- {
				if t := s.types[j]; t&(ucd.BD_L|ucd.BD_R|ucd.BD_AL) != 0 {
					if t == ucd.BD_AL {
						s.types[i] = ucd.BD_AN
					}
					break
				}
			}
		}
	}

	// Rule W3.
	for i, t := range s.types {
		if t == ucd.BD_AL {
			s.types[i] = ucd.BD_R
		}
	}

	// Rule W4.
	// Since there must be values on both sides for this rule to have an
	// effect, the scan skips the first and last value.
	//
	// Although the scan proceeds left to right, and changes the type
	// values in a way that would appear to affect the computations
	// later in the scan, there is actually no problem. A change in the
	// current value can only affect the value to its immediate right,
	// and only affect it if it is ES or CS. But the current value can
	// only change if the value to its right is not ES or CS. Thus
	// either the current value will not change, or its change will have
	// no effect on the remainder of the analysis.

	for i := 1; i < s.Len()-1; i++ {
		t := s.types[i]
		if t == ucd.BD_ES || t == ucd.BD_CS {
			prevSepType := s.types[i-1]
			succSepType := s.types[i+1]
			if prevSepType == ucd.BD_EN && succSepType == ucd.BD_EN {
				s.types[i] = ucd.BD_EN
			} else if s.types[i] == ucd.BD_CS && prevSepType == ucd.BD_AN && succSepType == ucd.BD_AN {
				s.types[i] = ucd.BD_AN
			}
		}
	}

	// Rule W5.
	for i, t := range s.types {
		if t == ucd.BD_ET {
			// locate end of sequence
			runStart := i
			runEnd := s.findRunLimit(runStart, ucd.BD_ET)

			// check values at ends of sequence
			t := s.sos
			if runStart > 0 {
				t = s.types[runStart-1]
			}
			if t != ucd.BD_EN {
				t = s.eos
				if runEnd < len(s.types) {
					t = s.types[runEnd]
				}
			}
			if t == ucd.BD_EN {
				setTypes(s.types[runStart:runEnd], ucd.BD_EN)
			}
			// continue at end of sequence
		}
	}

	// Rule W6.
	for i, t := range s.types {
		if t&(ucd.BD_ES|ucd.BD_ET|ucd.BD_CS) != 0 {
			s.types[i] = ucd.BD_ON
		}
	}

	// Rule W7.
	for i, t := range s.types {
		if t == ucd.BD_EN {
			// set default if we reach start of run
			prevStrongType := s.sos
			for j := i - 1; j >= 0; j-- {
				t = s.types[j]
				if t == ucd.BD_L || t == ucd.BD_R { // AL's have been changed to R
					prevStrongType = t
					break
				}
			}
			if prevStrongType == ucd.BD_L {
				s.types[i] = ucd.BD_L
			}
		}
	}
}

// 6) resolving neutral types Rules N1-N2.
func (s *isolatingRunSequence) resolveNeutralTypes() {
	// on entry, only these types can be in resultTypes
	// s.assertOnly(L, R, EN, AN, B, S, WS, ON, RLI, LRI, FSI, PDI)

	for i, t := range s.types {
		switch t {
		case ucd.BD_WS, ucd.BD_ON, ucd.BD_B, ucd.BD_S, ucd.BD_RLI, ucd.BD_LRI, ucd.BD_FSI, ucd.BD_PDI:
			// find bounds of run of neutrals
			runStart := i
			runEnd := s.findRunLimit(runStart, ucd.BD_B|ucd.BD_S|ucd.BD_WS|ucd.BD_ON|ucd.BD_RLI|ucd.BD_LRI|ucd.BD_FSI|ucd.BD_PDI)

			// determine effective types at ends of run
			var leadType, trailType ucd.BidiClass

			// Note that the character found can only be L, R, AN, or
			// EN.
			if runStart == 0 {
				leadType = s.sos
			} else {
				leadType = s.types[runStart-1]
				if leadType&(ucd.BD_AN|ucd.BD_EN) != 0 {
					leadType = ucd.BD_R
				}
			}
			if runEnd == len(s.types) {
				trailType = s.eos
			} else {
				trailType = s.types[runEnd]
				if trailType&(ucd.BD_AN|ucd.BD_EN) != 0 {
					trailType = ucd.BD_R
				}
			}

			var resolvedType ucd.BidiClass
			if leadType == trailType {
				// Rule N1.
				resolvedType = leadType
			} else {
				// Rule N2.
				// Notice the embedding level of the run is used, not
				// the paragraph embedding level.
				resolvedType = typeForLevel(s.level)
			}

			setTypes(s.types[runStart:runEnd], resolvedType)

			// skip over run of (former) neutrals
			i = runEnd
		}
	}
}

func setLevels(levels []Level, newLevel Level) {
	for i := range levels {
		levels[i] = newLevel
	}
}

func setTypes(types []ucd.BidiClass, newType ucd.BidiClass) {
	for i := range types {
		types[i] = newType
	}
}

// 7) resolving implicit embedding levels Rules I1, I2.
func (s *isolatingRunSequence) resolveImplicitLevels() {
	// on entry, only these types can be in resultTypes
	// s.assertOnly(L, R, EN, AN)

	s.resolvedLevels = make([]Level, len(s.types))
	setLevels(s.resolvedLevels, s.level)

	if (s.level & 1) == 0 { // even level
		for i, t := range s.types {
			// Rule I1.
			if t == ucd.BD_L {
				// no change
			} else if t == ucd.BD_R {
				s.resolvedLevels[i] += 1
			} else { // t == AN || t == EN
				s.resolvedLevels[i] += 2
			}
		}
	} else { // odd level
		for i, t := range s.types {
			// Rule I2.
			if t == ucd.BD_R {
				// no change
			} else { // t == L || t == AN || t == EN
				s.resolvedLevels[i] += 1
			}
		}
	}
}

// Applies the levels and types resolved in rules W1-I2 to the
// resultLevels array.
func (s *isolatingRunSequence) applyLevelsAndTypes() {
	for i, x := range s.indexes {
		s.p.resultTypes[x] = s.types[i]
		s.p.resultLevels[x] = s.resolvedLevels[i]
	}
}

// Return the limit of the run consisting only of the types in validSet
// starting at index. This checks the value at index, and will return
// index if that value is not in validSet.
func (s *isolatingRunSequence) findRunLimit(index int, validSet ucd.BidiClass) int {
	for ; index < len(s.types); index++ {
		t := s.types[index]
		if t&validSet != 0 {
			continue
		}
		return index // didn't find a match in validSet
	}
	return len(s.types)
}

// // Algorithm validation. Assert that all values in types are in the
// // provided set.
// func (s *isolatingRunSequence) assertOnly(codes ...ucd.BidiClass) {
// loop:
// 	for i, t := range s.types {
// 		for _, c := range codes {
// 			if t == c {
// 				continue loop
// 			}
// 		}
// 		log.Panicf("invalid bidi code %v present in assertOnly at position %d", t, s.indexes[i])
// 	}
// }

// determineLevelRuns returns an array of level runs. Each level run is
// described as an array of indexes into the input string.
//
// Determines the level runs. Rule X9 will be applied in determining the
// runs, in the way that makes sure the characters that are supposed to be
// removed are not included in the runs.
func (p *Paragraph) determineLevelRuns() [][]int {
	run := []int{}
	allRuns := [][]int{}
	currentLevel := implicitLevel

	for i := range p.initialTypes {
		if !isRemovedByX9(p.initialTypes[i]) {
			if p.resultLevels[i] != currentLevel {
				// we just encountered a new run; wrap up last run
				if currentLevel >= 0 { // only wrap it up if there was a run
					allRuns = append(allRuns, run)
					run = nil
				}
				// Start new run
				currentLevel = p.resultLevels[i]
			}
			run = append(run, i)
		}
	}
	// Wrap up the final run, if any
	if len(run) > 0 {
		allRuns = append(allRuns, run)
	}
	return allRuns
}

// Definition BD13. Determine isolating run sequences.
func (p *Paragraph) determineIsolatingRunSequences() []*isolatingRunSequence {
	levelRuns := p.determineLevelRuns()

	// Compute the run that each character belongs to
	for i, run := range levelRuns {
		for _, index := range run {
			p.runForCharacter[index] = i
		}
	}

	sequences := []*isolatingRunSequence{}

	var currentRunSequence []int

	for _, run := range levelRuns {
		first := run[0]
		if p.initialTypes[first] != ucd.BD_PDI || p.matchingIsolateInitiator[first] == -1 {
			currentRunSequence = nil
			// int run = i;
			for {
				// Copy this level run into currentRunSequence
				currentRunSequence = append(currentRunSequence, run...)

				last := currentRunSequence[len(currentRunSequence)-1]
				lastT := p.initialTypes[last]
				if lastT&(ucd.BD_LRI|ucd.BD_RLI|ucd.BD_FSI) != 0 && p.matchingPDI[last] != p.len() {
					run = levelRuns[p.runForCharacter[p.matchingPDI[last]]]
				} else {
					break
				}
			}
			sequences = append(sequences, p.isolatingRunSequence(currentRunSequence))
		}
	}
	return sequences
}

// Assign level information to characters removed by rule X9. This is for
// ease of relating the level information to the original input data. Note
// that the levels assigned to these codes are arbitrary, they're chosen so
// as to avoid breaking level runs.
func (p *Paragraph) assignLevelsToCharactersRemovedByX9() {
	for i, t := range p.initialTypes {
		if t&(ucd.BD_LRE|ucd.BD_RLE|ucd.BD_LRO|ucd.BD_RLO|ucd.BD_PDF|ucd.BD_BN) != 0 {
			p.resultTypes[i] = t
			p.resultLevels[i] = -1
		}
	}
	// now propagate forward the levels information (could have
	// propagated backward, the main thing is not to introduce a level
	// break where one doesn't already exist).

	if p.resultLevels[0] == -1 {
		p.resultLevels[0] = p.embeddingLevel
	}
	for i := 1; i < len(p.initialTypes); i++ {
		if p.resultLevels[i] == -1 {
			p.resultLevels[i] = p.resultLevels[i-1]
		}
	}
	// Embedding information is for informational purposes only so need not be
	// adjusted.
}

//
// Output
//

// computeLevels computes levels array breaking lines at offsets in linebreaks,
// mutating [resultLevels]
// Rule L1.
func (p *Paragraph) computeLevels() {
	// Note that since the previous processing has removed all
	// P, S, and WS values from resultTypes, the values referred to
	// in these rules are the initial types, before any processing
	// has been applied (including processing of overrides).
	//
	// This example implementation has reinserted explicit format codes
	// and BN, in order that the levels array correspond to the
	// initial text. Their final placement is not normative.
	// These codes are treated like WS in this implementation,
	// so they don't interrupt sequences of WS.

	// don't worry about linebreaks since if there is a break within
	// a series of WS values preceding S, the linebreak itself
	// causes the reset.
	for i, t := range p.initialTypes {
		if t&(ucd.BD_B|ucd.BD_S) != 0 {
			// Rule L1, clauses one and two.
			p.resultLevels[i] = p.embeddingLevel

			// Rule L1, clause three.
			for j := i - 1; j >= 0; j-- {
				if isWhitespace(p.initialTypes[j]) { // including format codes
					p.resultLevels[j] = p.embeddingLevel
				} else {
					break
				}
			}
		}
	}

	// Rule L1, clause four.
	limit := len(p.initialTypes)
	for j := limit - 1; j >= 0; j-- {
		if isWhitespace(p.initialTypes[j]) { // including format codes
			p.resultLevels[j] = p.embeddingLevel
		} else {
			break
		}
	}
}

// isWhitespace reports whether the type is considered a whitespace type for the
// line break rules.
func isWhitespace(c ucd.BidiClass) bool {
	return c&(ucd.BD_LRE|ucd.BD_RLE|ucd.BD_LRO|ucd.BD_RLO|ucd.BD_PDF|ucd.BD_LRI|ucd.BD_RLI|ucd.BD_FSI|ucd.BD_PDI|ucd.BD_BN|ucd.BD_WS) != 0
}

// isRemovedByX9 reports whether the type is one of the types removed in X9.
func isRemovedByX9(c ucd.BidiClass) bool {
	return c&(ucd.BD_LRE|ucd.BD_RLE|ucd.BD_LRO|ucd.BD_RLO|ucd.BD_PDF|ucd.BD_BN) != 0
}

// typeForLevel reports the strong type (L or R) corresponding to the level.
func typeForLevel(level Level) ucd.BidiClass {
	if (level & 0x1) == 0 {
		return ucd.BD_L
	}
	return ucd.BD_R
}
