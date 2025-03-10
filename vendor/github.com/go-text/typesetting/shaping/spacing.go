package shaping

import (
	"golang.org/x/image/math/fixed"
)

// AddWordSpacing alters the run, adding [additionalSpacing] on each
// word separator.
// [text] is the input slice used to create the run.
// Note that space is always added, even on boundaries.
//
// See also the convenience function [AddSpacing] to handle a slice of runs.
//
// See also https://www.w3.org/TR/css-text-3/#word-separator
func (run *Output) AddWordSpacing(text []rune, additionalSpacing fixed.Int26_6) {
	isVertical := run.Direction.IsVertical()
	for i, g := range run.Glyphs {
		// find the corresponding runes :
		// to simplify, we assume a simple one to one rune/glyph mapping
		// which should be common in practice for word separators
		if !(g.RuneCount == 1 && g.GlyphCount == 1) {
			continue
		}
		r := text[g.ClusterIndex]
		switch r {
		case '\u0020', // space
			'\u00A0',                   // no-break space
			'\u1361',                   // Ethiopic word space
			'\U00010100', '\U00010101', // Aegean word separators
			'\U0001039F', // Ugaritic word divider
			'\U0001091F': // Phoenician word separator
		default:
			continue
		}
		// we have a word separator: add space
		// we do it by enlarging the separator glyph advance
		// and distributing space around the glyph content
		if isVertical {
			run.Glyphs[i].YAdvance += additionalSpacing
			run.Glyphs[i].YOffset += additionalSpacing / 2
		} else {
			run.Glyphs[i].XAdvance += additionalSpacing
			run.Glyphs[i].XOffset += additionalSpacing / 2
		}
	}
	run.RecomputeAdvance()
}

// AddLetterSpacing alters the run, adding [additionalSpacing] between
// each Harfbuzz clusters.
//
// Space is added at the boundaries if and only if there is an adjacent run, as specified by [isStartRun] and [isEndRun].
//
// See also the convenience function [AddSpacing] to handle a slice of runs.
//
// See also https://www.w3.org/TR/css-text-3/#letter-spacing-property
func (run *Output) AddLetterSpacing(additionalSpacing fixed.Int26_6, isStartRun, isEndRun bool) {
	isVertical := run.Direction.IsVertical()

	halfSpacing := additionalSpacing / 2
	for startGIdx := 0; startGIdx < len(run.Glyphs); {
		startGlyph := run.Glyphs[startGIdx]
		endGIdx := startGIdx + startGlyph.GlyphCount - 1

		// start : apply spacing at boundary only if the run is not the first
		if startGIdx > 0 || !isStartRun {
			if isVertical {
				run.Glyphs[startGIdx].YAdvance += halfSpacing
				run.Glyphs[startGIdx].YOffset += halfSpacing
			} else {
				run.Glyphs[startGIdx].XAdvance += halfSpacing
				run.Glyphs[startGIdx].XOffset += halfSpacing
			}
			run.Glyphs[startGIdx].startLetterSpacing += halfSpacing
		}

		// end : apply spacing at boundary only if the run is not the last
		isLastCluster := startGIdx+startGlyph.GlyphCount >= len(run.Glyphs)
		if !isLastCluster || !isEndRun {
			if isVertical {
				run.Glyphs[endGIdx].YAdvance += halfSpacing
			} else {
				run.Glyphs[endGIdx].XAdvance += halfSpacing
			}
			run.Glyphs[endGIdx].endLetterSpacing += halfSpacing
		}

		// go to next cluster
		startGIdx += startGlyph.GlyphCount
	}

	run.RecomputeAdvance()
}

// does not run RecomputeAdvance
func (run *Output) trimStartLetterSpacing() {
	if len(run.Glyphs) == 0 {
		return
	}
	firstG := &run.Glyphs[0]
	halfSpacing := firstG.startLetterSpacing
	if run.Direction.IsVertical() {
		firstG.YAdvance -= halfSpacing
		firstG.YOffset -= halfSpacing
	} else {
		firstG.XAdvance -= halfSpacing
		firstG.XOffset -= halfSpacing
	}
	firstG.startLetterSpacing = 0
}

// AddSpacing adds additionnal spacing between words and letters, mutating the given [runs].
// [text] is the input slice the [runs] refer to.
//
// See the method [Output.AddWordSpacing] and [Output.AddLetterSpacing] for details
// about what spacing actually is.
func AddSpacing(runs []Output, text []rune, wordSpacing, letterSpacing fixed.Int26_6) {
	for i := range runs {
		isStartRun, isEndRun := i == 0, i == len(runs)-1
		if wordSpacing != 0 {
			runs[i].AddWordSpacing(text, wordSpacing)
		}
		if letterSpacing != 0 {
			runs[i].AddLetterSpacing(letterSpacing, isStartRun, isEndRun)
		}
	}
}
