// Package unicodedata provides additional lookup functions for unicode
// properties, not covered by the standard package unicode.
package unicodedata

import (
	"sort"
	"unicode"

	"github.com/go-text/typesetting/language"
)

// GeneralCategory is an enum storing the Unicode General Category of a rune.
type GeneralCategory uint8

// LookupGeneralCategory returns the unicode general categorie of the rune,
// or [Unassigned] if not found.
func LookupGeneralCategory(r rune) GeneralCategory { return GeneralCategory(gcLookup(r)) }

// IsMark returns true for Spacing_Mark, Enclosing_Mark, Nonspacing_Mark
func (gc GeneralCategory) IsMark() bool {
	return gc == Mc || gc == Me || gc == Mn
}

// IsLetter returns true for Lowercase_Letter, Modifier_Letter, Other_Letter, Titlecase_Letter, Uppercase_Letter
func (gc GeneralCategory) IsLetter() bool {
	return gc == Ll || gc == Lm || gc == Lo || gc == Lt || gc == Lu
}

// LookupCombiningClass returns the class used for the Canonical Ordering Algorithm in the Unicode Standard,
// defaulting to 0.
//
// From http://www.unicode.org/reports/tr44/#Canonical_Combining_Class:
// "This property could be considered either an enumerated property or a numeric property:
// the principal use of the property is in terms of the numeric values.
// For the property value names associated with different numeric values,
// see DerivedCombiningClass.txt and Canonical Combining Class Values."
func LookupCombiningClass(ch rune) uint8 { return cccLookup(ch) }

// LineBreak is a flag storing the Unicode Line Break Property.
//
// See https://unicode.org/reports/tr14/#Properties
type LineBreak uint64

// LookupLineBreak returns the break class for the rune,
// or 0 (the XX, Unknown class)
func LookupLineBreak(ch rune) LineBreak {
	i := lbLookup(ch)
	if i == 0 {
		return 0
	}
	return 1 << (i - 1)
}

// GraphemeBreak is a flag storing the Unicode Grapheme Cluster Break Property.
//
// See https://unicode.org/reports/tr29/#Grapheme_Cluster_Break_Property_Values
type GraphemeBreak uint16

// LookupGraphemeBreak returns the grapheme break property for the rune, or 0.
func LookupGraphemeBreak(ch rune) GraphemeBreak {
	i := gbLookup(ch)
	if i == 0 {
		return 0
	}
	return 1 << (i - 1)
}

// WordBreak is a flag storing the Unicode Word Break Property.
//
// See https://unicode.org/reports/tr29/#Table_Word_Break_Property_Values
type WordBreak uint16

// LookupWordBreak returns the word break property for the rune, or 0.
func LookupWordBreak(ch rune) WordBreak {
	i := wbLookup(ch)
	if i == 0 {
		return 0
	}
	return 1 << (i - 1)
}

// IsWord returns true for all the runes we may found in a word,
// that is either a Number (Nd, Nl, No) or a rune with the Alphabetic property
func IsWord(ch rune) bool { return wordLookup(ch) == 1 }

// IndicConjunctBreak is a flag storing the Indic_Conjunct_Break property used for UAX29, rule GB9c.
type IndicConjunctBreak uint8

// LookupIndicConjunctBreak return the value of the Indic_Conjunct_Break,
// or zero.
func LookupIndicConjunctBreak(r rune) IndicConjunctBreak {
	i := indicCBLookup(r)
	if i == 0 {
		return 0
	}
	return 1 << (i - 1)
}

// LookupMirrorChar finds the mirrored equivalent of a character as defined in
// the file BidiMirroring.txt of the Unicode Character Database available at
// http://www.unicode.org/Public/UNIDATA/BidiMirroring.txt.
//
// If the input character is declared as a mirroring character in the
// Unicode standard and has a mirrored equivalent, it is returned.
// Otherwise the input character itself is returned
func LookupMirrorChar(ch rune) rune {
	return ch + rune(mirLookup(ch))
}

func IsExtendedPictographic(ch rune) bool { return emojiLookup(ch) == 1 }

// IsLargeEastAsian matches runes with East_Asian_Width property of
// F, W or H, and is used for UAX14, rule LB30.
func IsLargeEastAsian(ch rune) bool { return eastAsianWidthLookup(ch) == 1 }

// Algorithmic hangul syllables [de]composition, used
// in Compose and Decompose, but also exported for additional shaper
// processing.
const (
	HangulSBase  = 0xAC00
	HangulLBase  = 0x1100
	HangulVBase  = 0x1161
	HangulTBase  = 0x11A7
	HangulSCount = 11172
	HangulLCount = 19
	HangulVCount = 21
	HangulTCount = 28
	HangulNCount = HangulVCount * HangulTCount
)

func decomposeHangul(ab rune) (a, b rune, ok bool) {
	si := ab - HangulSBase

	if si < 0 || si >= HangulSCount {
		return 0, 0, false
	}

	if si%HangulTCount != 0 { // LV,T
		return HangulSBase + (si/HangulTCount)*HangulTCount, HangulTBase + (si % HangulTCount), true
	} // L,V
	return HangulLBase + (si / HangulNCount), HangulVBase + (si%HangulNCount)/HangulTCount, true
}

func composeHangul(a, b rune) (rune, bool) {
	if a >= HangulSBase && a < (HangulSBase+HangulSCount) && b > HangulTBase && b < (HangulTBase+HangulTCount) && (a-HangulSBase)%HangulTCount == 0 {
		// LV,T
		return a + (b - HangulTBase), true
	} else if a >= HangulLBase && a < (HangulLBase+HangulLCount) && b >= HangulVBase && b < (HangulVBase+HangulVCount) {
		// L,V
		li := a - HangulLBase
		vi := b - HangulVBase
		return HangulSBase + li*HangulNCount + vi*HangulTCount, true
	}
	return 0, false
}

// Decompose decomposes an input Unicode code point,
// returning the two decomposed code points, if successful.
// It returns `false` otherwise.
func Decompose(ab rune) (a, b rune, ok bool) {
	if a, b, ok = decomposeHangul(ab); ok {
		return a, b, true
	}

	i := int(dmLookup(ab))

	// If no data, there's no decomposition.
	if i == 0 {
		return ab, 0, false
	}
	i--

	/* Check if it's a single-character decomposition. */
	if i < len(dm1P0Map)+len(dm1P2Map) {
		/* Single-character decompositions currently are only in plane 0 or plane 2. */
		if i < len(dm1P0Map) {
			/* Plane 0. */
			a = rune(dm1P0Map[i])
		} else {
			/* Plane 2. */
			i -= len(dm1P0Map)
			a = 0x20000 | rune(dm1P2Map[i])
		}
		b = 0
		return a, b, true
	}
	i -= len(dm1P0Map) + len(dm1P2Map)

	/* Otherwise they are encoded either in a 32bit array or a 64bit array. */
	if i < len(dm2U32Map) {
		/* 32bit array. */
		v := dm2U32Map[i]
		a = rune(v >> 21)               // HB_CODEPOINT_DECODE3_11_7_14_1(v)
		b = rune(v>>14)&0x007F | 0x0300 // HB_CODEPOINT_DECODE3_11_7_14_2(v)
		return a, b, true
	}
	i -= len(dm2U32Map)

	/* 64bit array. */
	v := dm2U64Map[i]
	a = rune(v >> 42)              // HB_CODEPOINT_DECODE3_1(v)
	b = rune((v >> 21) & 0x1FFFFF) // HB_CODEPOINT_DECODE3_2(v)
	return a, b, true
}

// Compose composes a sequence of two input Unicode code
// points by canonical equivalence, returning the composed code, if successful.
// It returns `false` otherwise
func Compose(a, b rune) (rune, bool) {
	// Hangul is handled algorithmically.
	if ab, ok := composeHangul(a, b); ok {
		return ab, true
	}
	var u rune
	if (a&0x7FFFF800) == 0x0000 && (b&0x7FFFFF80) == 0x0300 {
		// If "a" is small enough and "b" is in the U+0300 range,
		// the composition data is encoded in a 32bit array sorted by "a,b" pair.
		k := uint32(((a)&0x07FF)<<21 | (b&0x007F)<<14)                    // HB_CODEPOINT_ENCODE3_11_7_14(a, b, 0)
		const mask uint32 = (0x1FFFFF&0x07FF)<<21 | (0x1FFFFF&0x007F)<<14 // HB_CODEPOINT_ENCODE3_11_7_14(0x1FFFFFu, 0x1FFFFFu, 0)
		i := sort.Search(len(dm2U32Map), func(i int) bool { return dm2U32Map[i]&mask >= k })
		if !(i < len(dm2U32Map) && dm2U32Map[i]&mask == k) {
			return 0, false
		}
		v := dm2U32Map[i]
		u = rune(v & 0x3FFF) // HB_CODEPOINT_DECODE3_11_7_14_3(v)
	} else {
		// Otherwise it is stored in a 64bit array sorted by "a,b" pair.
		k := uint64(a)<<42 | uint64(b)<<21                       // HB_CODEPOINT_ENCODE3(a, b, 0)
		const mask = uint64(0x1FFFFF)<<42 | uint64(0x1FFFFF)<<21 // HB_CODEPOINT_ENCODE3(0x1FFFFFu, 0x1FFFFFu, 0);
		i := sort.Search(len(dm2U64Map), func(i int) bool { return dm2U64Map[i]&mask >= k })
		if !(i < len(dm2U64Map) && dm2U64Map[i]&mask == k) {
			return 0, false
		}
		v := dm2U64Map[i]
		u = rune(v & 0x1FFFFF) // HB_CODEPOINT_DECODE3_3(v)
	}

	return u, u != 0
}

// LookupVerticalOrientation returns the prefered orientation
// for the given script.
func LookupVerticalOrientation(s language.Script) ScriptVerticalOrientation {
	for _, script := range uprightOrMixedScripts {
		if script.script == s {
			return script
		}
	}

	// all other scripts have full R (sideways)
	return ScriptVerticalOrientation{exceptions: nil, script: s, isMainSideways: true}
}

// Orientation returns the prefered orientation
// for the given rune.
// If the rune does not belong to this script, the default orientation of this script
// is returned (regardless of the actual script of the given rune).
func (sv ScriptVerticalOrientation) Orientation(r rune) (isSideways bool) {
	if sv.exceptions == nil || !unicode.Is(sv.exceptions, r) {
		return sv.isMainSideways
	}
	return !sv.isMainSideways
}
