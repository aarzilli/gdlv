package harfbuzz

import (
	"github.com/go-text/typesetting/font/opentype/tables"
)

// ported from src/hb-set-digest.hh Copyright © 2012  Google, Inc. Behdad Esfahbod

const (
	maskBits = 8 * 8 // 4 = size(setDigestLowestBits)
	mb1      = maskBits - 1
	one      = maskT(1)
	all      = ^maskT(0)
)

type setType = gID

type maskT uint64

func addRangeTo(dst *maskT, a, b setType, shift uint) {
	if (b>>shift)-(a>>shift) >= mb1 {
		*dst = ^maskT(0)
	} else {
		ma := one << ((a >> shift) & mb1)
		mb := one << ((b >> shift) & mb1)
		var op maskT
		if mb < ma {
			op = 1
		}
		*dst |= mb + (mb - ma) - op
	}
}

/* This is a combination of digests that performs "best".
 * There is not much science to this: it's a result of intuition
 * and testing. */
const (
	shift0 = 4
	shift1 = 0
	shift2 = 6
)

// The set-digests implement "filters" that support "approximate
// member query".  Conceptually these are like Bloom Filter and
// Quotient Filter, however, much smaller, faster, and designed
// to fit the requirements of our uses for glyph coverage queries.
//
// Our filters are highly accurate if the lookup covers fairly local
// set of glyphs, but fully flooded and ineffective if coverage is
// all over the place.
//
// The way these are used is that the filter is first populated by
// a lookup's or subtable's Coverage table(s), and then when we
// want to apply the lookup or subtable to a glyph, before trying
// to apply, we ask the filter if the glyph may be covered. If it's
// not, we return early.  We can also match a digest against another
// digest.
//
// We use these filters at three levels:
//   - If the digest for all the glyphs in the buffer as a whole
//     does not match the digest for the lookup, skip the lookup.
//   - For each glyph, if it doesn't match the lookup digest,
//     skip it.
//   - For each glyph, if it doesn't match the subtable digest,
//     skip it.
//
// The filter we use is a combination of three bits-pattern
// filters. A bits-pattern filter checks a number of bits (5 or 6)
// of the input number (glyph-id in most cases) and checks whether
// its pattern is amongst the patterns of any of the accepted values.
// The accepted patterns are represented as a "long" integer. Each
// check is done using four bitwise operations only.
type setDigest [3]maskT

// add adds the given rune to the set.
func (sd *setDigest) add(g setType) {
	sd[0] |= one << ((g >> shift0) & mb1)
	sd[1] |= one << ((g >> shift1) & mb1)
	sd[2] |= one << ((g >> shift2) & mb1)
}

// addRange adds the given, inclusive range to the set,
// in an efficient manner.
func (sd *setDigest) addRange(a, b setType) {
	if sd[0] == all || sd[1] == all || sd[2] == all {
		return
	}
	addRangeTo(&sd[0], a, b, shift0)
	addRangeTo(&sd[1], a, b, shift1)
	addRangeTo(&sd[2], a, b, shift2)
}

// addArray is a convenience method to add
// many runes.
func (sd *setDigest) addArray(arr []setType) {
	for _, v := range arr {
		sd.add(v)
	}
}

// mayHave performs an "approximate member query": if the return value
// is `false`, then it is certain that `g` is not in the set.
// Otherwise, we don't kwow, it might be a false positive.
// Note that runes in the set are certain to return `true`.
func (sd setDigest) mayHave(g setType) bool {
	return sd[0]&(one<<((g>>shift0)&mb1)) != 0 &&
		sd[1]&(one<<((g>>shift1)&mb1)) != 0 &&
		sd[2]&(one<<((g>>shift2)&mb1)) != 0
}

func (sd setDigest) mayIntersects(o setDigest) bool {
	return sd[0]&o[0] != 0 && sd[1]&o[1] != 0 && sd[2]&o[2] != 0
}

func (sd *setDigest) collectCoverage(cov tables.Coverage) {
	switch cov := cov.(type) {
	case tables.Coverage1:
		sd.addArray(cov.Glyphs)
	case tables.Coverage2:
		for _, r := range cov.Ranges {
			sd.addRange(r.StartGlyphID, r.EndGlyphID)
		}
	}
}
