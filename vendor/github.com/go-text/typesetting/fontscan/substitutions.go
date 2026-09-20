package fontscan

import (
	"strings"

	"github.com/go-text/typesetting/font"
)

// this file implements the family substitution feature,
// inspired by fontconfig.
// it works by defining a set of modifications to apply
// to a user provided family
// each of them may happen one (or more) alternative family to look for

// it is generated from fontconfig substitution rules
// the order matters, since the rules apply sequentially to the current
// state of the family list
func init() {
	// replace families keys by their no case no blank version
	for i, v := range familySubstitution {
		for i, s := range v.additionalFamilies {
			v.additionalFamilies[i] = font.NormalizeFamily(s)
		}

		familySubstitution[i].test = v.test.normalize()
	}
}

type weightedFamily struct {
	family string
	strong bool // stronger then lang in match ?
}

// familyList is a list of normalized families to match, order
// by user preference (first is best).
// It also implements helpers to insert at the start,
// the end and "around" an element
type familyList []weightedFamily

// normalize the families and add them with strong = true
func newFamilyList(families []string) familyList {
	// we'll guess that we end up with about ~140 items
	fl := make([]weightedFamily, 0, 140)
	fl = append(fl, make([]weightedFamily, len(families))...)
	for i, f := range families {
		fl[i] = weightedFamily{font.NormalizeFamily(f), true}
	}
	return fl
}

// returns the node equal to `family` or -1, if not found
func (fl familyList) elementEquals(family string) int {
	for i, v := range fl {
		if v.family == family {
			return i
		}
	}
	return -1
}

// returns the first node containing `family` or -1, if not found
func (fl familyList) elementContains(family string) int {
	for i, v := range fl {
		if strings.Contains(v.family, family) {
			return i
		}
	}
	return -1
}

// return the crible corresponding to the order
func (fl familyList) compileTo(dst familyCrible) {
	for i, item := range fl {
		current, has := dst[item.family]
		if has {
			// For duplicated families, keep the "best entry"
			// (strong is better than weak, then lower index is better).
			// The only replacement possible is if [current] is weak
			// and [item] is strong
			if !current.strong && item.strong {
				dst[item.family] = scoreStrong{i, true}
			}
		} else {
			dst[item.family] = scoreStrong{i, item.strong}
		}
	}
}

func withStrong(families []string, strong bool) familyList {
	out := make(familyList, len(families))
	for i, f := range families {
		out[i] = weightedFamily{f, strong}
	}
	return out
}

func (fl *familyList) insertStart(families []string, strong bool) {
	*fl = insertAt(*fl, 0, withStrong(families, strong))
}

func (fl *familyList) insertEnd(families []string, strong bool) {
	*fl = insertAt(*fl, len(*fl), withStrong(families, strong))
}

// insertAfter inserts families right after element
func (fl *familyList) insertAfter(element int, families []string, strong bool) {
	*fl = insertAt(*fl, element+1, withStrong(families, strong))
}

// insertBefore inserts families right before element
func (fl *familyList) insertBefore(element int, families []string, strong bool) {
	*fl = insertAt(*fl, element, withStrong(families, strong))
}

func (fl *familyList) replace(element int, families []string, strong bool) {
	*fl = replaceAt(*fl, element, element+1, withStrong(families, strong))
}

// ----- substitutions ------

// where to insert the families with respect to
// the current list
type substitutionOp uint8

const (
	opAppend substitutionOp = iota
	opAppendLast
	opPrepend
	opPrependFirst
	opReplace
)

type substitutionTest interface {
	// returns >= 0 if the substitution should be applied
	// for opAppendLast and opPrependFirst an arbitrary value could be returned
	test(list familyList, lang LangID) int

	// return a copy where families have been normalized
	// to their no blank no case version
	normalize() substitutionTest
}

// a family in the list must equal 'mf'
type familyEquals string

func (mf familyEquals) test(list familyList, _ LangID) int {
	return list.elementEquals(string(mf))
}

func (mf familyEquals) normalize() substitutionTest {
	return familyEquals(font.NormalizeFamily(string(mf)))
}

// a family in the list must contain 'mf'
type familyContains string

func (mf familyContains) test(list familyList, _ LangID) int {
	return list.elementContains(string(mf))
}

func (mf familyContains) normalize() substitutionTest {
	return familyContains(font.NormalizeFamily(string(mf)))
}

// the family list has no "serif", "sans-serif" or "monospace" generic fallback
type noGenericFamily struct{}

func (noGenericFamily) test(list familyList, _ LangID) int {
	for _, v := range list {
		switch v.family {
		case "serif", "sans-serif", "monospace":
			return -1
		}
	}
	return 0
}

func (noGenericFamily) normalize() substitutionTest {
	return noGenericFamily{}
}

// one family must equals `family`, and the queried language
// must equals `lang`
type langAndFamilyEqual struct {
	lang   LangID
	family string
}

func (t langAndFamilyEqual) test(list familyList, lang LangID) int {
	if t.lang == lang {
		return list.elementEquals(t.family)
	}
	return -1
}

func (t langAndFamilyEqual) normalize() substitutionTest {
	t.family = font.NormalizeFamily(t.family)
	return t
}

// no family must equals `family`, and the queried language
// must equals `lang`
type langEqualsAndNoFamily struct {
	lang   LangID
	family string
}

func (t langEqualsAndNoFamily) test(list familyList, lang LangID) int {
	if t.lang == lang {
		for _, v := range list {
			if v.family == t.family {
				return -1
			}
		}
		return 0
	}
	return -1
}

func (t langEqualsAndNoFamily) normalize() substitutionTest {
	t.family = font.NormalizeFamily(t.family)
	return t
}

type substitution struct {
	test               substitutionTest // the condition to apply
	additionalFamilies []string         // the families to add
	op                 substitutionOp   // how to insert the families
	// how important is the family with respect to lang/script
	// 'e' (equals) : same as the replacement
	// 's' (strong) : stronger than the lang
	// 'w' (weak) : weaker than the lang
	importance byte
}

func (fl *familyList) execute(subs substitution, lang LangID) {
	element := subs.test.test(*fl, lang)
	if element < 0 {
		return
	}

	// resolve importance
	var strong bool
	switch subs.importance {
	case 'w':
		strong = false
	case 's':
		strong = true
	case 'e': // keep the same importance
		strong = (*fl)[element].strong
	}

	switch subs.op {
	case opAppend:
		fl.insertAfter(element, subs.additionalFamilies, strong)
	case opAppendLast:
		fl.insertEnd(subs.additionalFamilies, strong)
	case opPrepend:
		fl.insertBefore(element, subs.additionalFamilies, strong)
	case opPrependFirst:
		fl.insertStart(subs.additionalFamilies, strong)
	case opReplace:
		fl.replace(element, subs.additionalFamilies, strong)
	default:
		panic("exhaustive switch")
	}
}

// ----- list manipulation -----

func insertAt(s []weightedFamily, i int, v []weightedFamily) []weightedFamily {
	if len(v) == 0 {
		return s
	}
	if len(s) == i {
		return append(s, v...)
	}
	if len(s)+len(v) > cap(s) {
		// create a new slice with sufficient capacity
		r := append(s[:i], make([]weightedFamily, len(s)+len(v)-i)...)
		// copy the inserted values
		copy(r[i:], v)
		// copy rest of the items from source
		copy(r[i+len(v):], s[i:])
		return r
	}

	// resize the slice
	s = s[:len(s)+len(v)]
	// move items to make space for v
	copy(s[i+len(v):], s[i:])
	// copy v
	copy(s[i:], v)
	return s
}

func replaceAt(s []weightedFamily, i, j int, v []weightedFamily) []weightedFamily {
	// just cutting
	if len(v) == 0 {
		return append(s[:i], s[j:]...)
	}
	// cutting the original til the end
	if len(s) == j {
		return append(s[:i], v...)
	}
	// calculate the final length
	tot := len(s) + len(v) - (j - i)
	if tot > cap(s) {
		// create a new slice with sufficient capacity
		r := append(s[:i], make([]weightedFamily, tot-i)...)
		// copy the inserted values
		copy(r[i:], v)
		// add the tail from the source
		copy(r[i+len(v):], s[j:])
		return r
	}

	n := len(s)
	s = s[:tot]
	// move items to make space for v
	copy(s[i+len(v):], s[j:n])
	// copy v
	copy(s[i:], v)
	return s
}
