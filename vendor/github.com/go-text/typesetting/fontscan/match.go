package fontscan

import (
	"math"
	"sort"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
)

// Query exposes the intention of an author about the
// font to use to shape and render text.
type Query struct {
	// Families is a list of required families,
	// the first having the highest priority.
	// Each of them is tried until a suitable match is found.
	Families []string

	// Aspect selects which particular face to use among
	// the font matching the family criteria.
	Aspect font.Aspect
}

// fontSet stores the list of fonts available for text shaping.
// It is usually build from a system font index or by manually appending
// fonts.
// footprint family names are normalized
type fontSet []Footprint

type scoreStrong struct {
	score  int
	strong bool
}

// stores the possible matches with their score:
// lower is better
type familyCrible map[string]scoreStrong

// clear fc but keep the underlying storage
func (fc familyCrible) reset() {
	for k := range fc {
		delete(fc, k)
	}
}

// fillWithSubstitutions starts from `family`
// and applies all the substitutions coded in the package
// to add substitutes values
func (fc familyCrible) fillWithSubstitutions(family string, lang LangID) {
	fc.fillWithSubstitutionsList([]string{family}, lang)
}

func (fc familyCrible) fillWithSubstitutionsList(families []string, lang LangID) {
	fl := newFamilyList(families)
	for _, subs := range familySubstitution {
		fl.execute(subs, lang)
	}

	fl.compileTo(fc)
}

// scoredFootprints is used to sort the fooprints (see the [Less] method)
type scoredFootprints struct {
	footprints []int         // indices into [database]
	scores     []scoreStrong // with same length as footprints

	database fontSet
	script   language.Script
}

// keep the underlying storage
func (sf *scoredFootprints) reset(fs fontSet, script language.Script) {
	sf.footprints = sf.footprints[:0]
	sf.scores = sf.scores[:0]

	sf.database = fs
	sf.script = script
}

// Len is the number of elements in the collection.
func (sf scoredFootprints) Len() int { return len(sf.footprints) }

// less compares by scores, by userProvided, selects "regular" over "mono",
// then selects TTF over CFF
func less(scorei, scorej int, fpi, fpj *Footprint) bool {
	if scorei < scorej {
		return true
	} else if scorei > scorej {
		return false
	}
	if fpi.isUserProvided && !fpj.isUserProvided {
		return true
	} else if !fpi.isUserProvided && fpj.isUserProvided {
		return false
	}
	isMonoi, isMonoj := fpi.isMonoHint(), fpj.isMonoHint()
	if !isMonoi && isMonoj {
		return true
	} else if isMonoi && !isMonoj {
		return false
	}
	isTTi, isTTj := fpi.isTruetypeHint(), fpj.isTruetypeHint()
	return isTTi && !isTTj
}

// Less compares footprints following these rules :
//   - 'strong' replacements come before 'weak' ones
//   - among 'strong' families, only the score matters
//   - among 'weak' families, the footprints compatible with the given script come first
//   - if two footprints have the same score (meaning they have the same family),
//     user provided ones come first, then "regular" over "mono" then TTF before CFF.
func (sf scoredFootprints) Less(i int, j int) bool {
	scorei, scorej := sf.scores[i], sf.scores[j]
	fpi, fpj := &sf.database[sf.footprints[i]], &sf.database[sf.footprints[j]]
	// strong better then weak
	if scorei.strong && !scorej.strong {
		return true
	} else if !scorei.strong && scorej.strong {
		return false
	}
	// among strong substitutions, only use the score
	if scorei.strong {
		return less(scorei.score, scorej.score, fpi, fpj)
	}
	// among weak substitutions, sort by script ...
	hasScripti, hasScriptj := fpi.Scripts.contains(sf.script), fpj.Scripts.contains(sf.script)
	if hasScripti && !hasScriptj {
		return true
	} else if !hasScripti && hasScriptj {
		return false
	}
	// ... then by score
	return less(scorei.score, scorej.score, fpi, fpj)
}

// Swap swaps the elements with indexes i and j.
func (sf scoredFootprints) Swap(i int, j int) {
	sf.footprints[i], sf.footprints[j] = sf.footprints[j], sf.footprints[i]
	sf.scores[i], sf.scores[j] = sf.scores[j], sf.scores[i]
}

// Generic families as defined by
// https://www.w3.org/TR/css-fonts-4/#generic-font-families
const (
	Fantasy   = "fantasy"
	Math      = "math"
	Emoji     = "emoji"
	Serif     = "serif"
	SansSerif = "sans-serif"
	Cursive   = "cursive"
	Monospace = "monospace"
)

func isGenericFamily(family string) bool {
	switch family {
	case Serif, SansSerif, Monospace, Cursive, Fantasy, Math, Emoji:
		return true
	default:
		return false
	}
}

// selectByFamilyExact returns all the fonts in the fontmap matching
// the given `family`, with the best matches coming first.
//
// The match is performed without substituting family names,
// expect for the generic families, which are always expanded to concrete families.
//
// If two fonts have the same family, user provided are returned first.
//
// The returned slice may be empty if no font matches the given `family`.
//
// The buffers are used to reduce allocations and the returned slice is owned by them.
func (fm fontSet) selectByFamilyExact(family string, cribleBuffer familyCrible, footprintsBuffer *scoredFootprints,
) []int {
	if isGenericFamily(family) {
		// See the CSS spec (https://www.w3.org/TR/css-fonts-4/#font-style-matching) :
		// "If the family name is a generic family keyword, the user agent looks up the appropriate
		// font family name to be used. User agents may choose the generic font family to use
		// based on the language of the containing element or the Unicode range of the character."
		//
		// and font-kit implementation :
		// https://docs.rs/font-kit/latest/src/font_kit/sources/fontconfig.rs.html#119-152
		//
		// Our strategy is
		//	- performs family substitutions
		//	- match fonts against all these families
		//	- restrict the result to the first (best) family

		cribleBuffer.reset()
		cribleBuffer.fillWithSubstitutions(family, 0)

		footprints := fm.selectByFamiliesAndScript(cribleBuffer, 0, footprintsBuffer)

		// restrict to one 'concrete' family name
		if len(footprints) == 0 {
			return nil
		}
		selectedFamily := fm[footprints[0]].Family
		// only keep the first footprints with same family name
		var i int
		for ; i < len(footprints); i++ {
			if fp := fm[footprints[i]]; fp.Family != selectedFamily {
				break
			}
		}
		return footprints[:i]
	}

	// regular family : perform a simple match against the exact family name, without substitutions
	// nor script matching
	cribleBuffer = familyCrible{font.NormalizeFamily(family): scoreStrong{0, true}}
	return fm.selectByFamiliesAndScript(cribleBuffer, 0, footprintsBuffer)
}

// selectByFamilyExact returns all the fonts in the fontmap matching
// the given query, with the best matches coming first.
//
// `queryFamilies` is expanded with family substitutions
func (fm fontSet) selectByFamilyWithSubs(queryFamilies []string, queryScript language.Script,
	cribleBuffer familyCrible, footprintsBuffer *scoredFootprints,
) []int {
	// if not found, the zero value is fine (language based substitutions will be disabled)
	queryLang := language.ScriptToLang[queryScript]

	// build the crible, handling substitutions
	cribleBuffer.reset()
	cribleBuffer.fillWithSubstitutionsList(queryFamilies, queryLang)
	return fm.selectByFamiliesAndScript(cribleBuffer, queryScript, footprintsBuffer)
}

// select the fonts in the fontSet matching [crible], returning their (sorted) indices.
// [footprintsBuffer] is used to reduce allocations.
// If [script] is 0, no font with matching script is added
func (fm fontSet) selectByFamiliesAndScript(crible familyCrible, script language.Script, footprintsBuffer *scoredFootprints) []int {
	footprintsBuffer.reset(fm, script)

	// loop through the font set and stores the matching fonts into
	// the footprintsBuffer, to be sorted.
	for index, footprint := range fm {
		if score, has := crible[footprint.Family]; has {
			// match by family
			footprintsBuffer.footprints = append(footprintsBuffer.footprints, index)
			footprintsBuffer.scores = append(footprintsBuffer.scores, score)
		} else if footprint.Scripts.contains(script) {
			// match by script: add with a score worse than any family match
			footprintsBuffer.footprints = append(footprintsBuffer.footprints, index)
			footprintsBuffer.scores = append(footprintsBuffer.scores, scoreStrong{math.MaxInt, false})
		}
	}

	// sort the matched fonts (see [scoredFootprints.Less])
	sort.Stable(*footprintsBuffer)

	return footprintsBuffer.footprints
}

// matchStretch look for the given stretch in the font set,
// or, if not found, the closest stretch
// if always return a valid value (contained in `candidates`) if `candidates` is not empty
func (fs fontSet) matchStretch(candidates []int, query font.Stretch) font.Stretch {
	// narrower and wider than the query
	var narrower, wider font.Stretch

	for _, index := range candidates {
		stretch := fs[index].Aspect.Stretch
		if stretch > query { // wider candidate
			if wider == 0 || stretch-query < wider-query { // closer
				wider = stretch
			}
		} else if stretch < query { // narrower candidate
			// if narrower == 0, it is always more distant to queryStretch than stretch
			if query-stretch < query-narrower { // closer
				narrower = stretch
			}
		} else {
			// found an exact match, just return it
			return query
		}
	}

	// default to closest
	if query <= font.StretchNormal { // narrow first
		if narrower != 0 {
			return narrower
		}
		return wider
	} else { // wide first
		if wider != 0 {
			return wider
		}
		return narrower
	}
}

// in practice, italic and oblique are synonymous
const styleOblique = font.StyleItalic

// matchStyle look for the given style in the font set,
// or, if not found, the closest style
// if always return a valid value (contained in `fs`) if `fs` is not empty
func (fs fontSet) matchStyle(candidates []int, query font.Style) font.Style {
	var crible [font.StyleItalic + 1]bool

	for _, index := range candidates {
		crible[fs[index].Aspect.Style] = true
	}

	switch query {
	case font.StyleNormal: // StyleNormal, StyleOblique, StyleItalic
		if crible[font.StyleNormal] {
			return font.StyleNormal
		} else if crible[styleOblique] {
			return styleOblique
		} else {
			return font.StyleItalic
		}
	case font.StyleItalic: // StyleItalic, StyleOblique, StyleNormal
		if crible[font.StyleItalic] {
			return font.StyleItalic
		} else if crible[styleOblique] {
			return styleOblique
		} else {
			return font.StyleNormal
		}
	}

	panic("should not happen") // query.Style is sanitized by SetDefaults
}

// matchWeight look for the given weight in the font set,
// or, if not found, the closest weight
// if always return a valid value (contained in `fs`) if `fs` is not empty
// we follow https://drafts.csswg.org/css-fonts/#font-style-matching
func (fs fontSet) matchWeight(candidates []int, query font.Weight) font.Weight {
	var fatter, thinner font.Weight // approximate match
	for _, index := range candidates {
		weight := fs[index].Aspect.Weight
		if weight > query { // fatter candidate
			if fatter == 0 || weight-query < fatter-query { // weight is closer to query
				fatter = weight
			}
		} else if weight < query {
			if query-weight < query-thinner { // weight is closer to query
				thinner = weight
			}
		} else {
			// found an exact match, just return it
			return query
		}
	}

	// approximate match
	if 400 <= query && query <= 500 { // fatter until 500, then thinner then fatter
		if fatter != 0 && fatter <= 500 {
			return fatter
		} else if thinner != 0 {
			return thinner
		}
		return fatter
	} else if query < 400 { // thinner then fatter
		if thinner != 0 {
			return thinner
		}
		return fatter
	} else { // fatter then thinner
		if fatter != 0 {
			return fatter
		}
		return thinner
	}
}

// filter `candidates` in place and returns the updated slice
func (fs fontSet) filterByStretch(candidates []int, stretch font.Stretch) []int {
	n := 0
	for _, index := range candidates {
		if fs[index].Aspect.Stretch == stretch {
			candidates[n] = index
			n++
		}
	}
	candidates = candidates[:n]
	return candidates
}

// filter `candidates` in place and returns the updated slice
func (fs fontSet) filterByStyle(candidates []int, style font.Style) []int {
	n := 0
	for _, index := range candidates {
		if fs[index].Aspect.Style == style {
			candidates[n] = index
			n++
		}
	}
	candidates = candidates[:n]
	return candidates
}

// filter `candidates` in place and returns the updated slice
func (fs fontSet) filterByWeight(candidates []int, weight font.Weight) []int {
	n := 0
	for _, index := range candidates {
		if fs[index].Aspect.Weight == weight {
			candidates[n] = index
			n++
		}
	}
	candidates = candidates[:n]
	return candidates
}

// retainsBestMatches narrows `candidates` to the closest footprints to `query`, according to the CSS font rules
// `candidates` is a slice of indexes into `fs`, which is mutated and returned
// if `candidates` is not empty, the returned slice is guaranteed not to be empty
func (fs fontSet) retainsBestMatches(candidates []int, query font.Aspect) []int {
	// this follows CSS Fonts Level 3 § 5.2 [1].
	// https://drafts.csswg.org/css-fonts-3/#font-style-matching

	query.SetDefaults()

	// First step: font-stretch
	matchingStretch := fs.matchStretch(candidates, query.Stretch)
	candidates = fs.filterByStretch(candidates, matchingStretch) // only retain matching stretch

	// Second step : font-style
	matchingStyle := fs.matchStyle(candidates, query.Style)
	candidates = fs.filterByStyle(candidates, matchingStyle)

	// Third step : font-weight
	matchingWeight := fs.matchWeight(candidates, query.Weight)
	candidates = fs.filterByWeight(candidates, matchingWeight)

	return candidates
}

// filterUserProvided selects the user inserted fonts, appending to
// `candidates`, which is returned
func (fs fontSet) filterUserProvided(candidates []int) []int {
	for index, fp := range fs {
		if fp.isUserProvided {
			candidates = append(candidates, index)
		}
	}
	return candidates
}
