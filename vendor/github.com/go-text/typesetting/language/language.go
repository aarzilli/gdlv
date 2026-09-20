// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package language

import (
	"os"
	"sort"
	"strings"
)

var canonMap = [256]byte{
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, '-', 0, 0,
	'0', '1', '2', '3', '4', '5', '6', '7', '8', '9', 0, 0, 0, 0, 0, 0,
	'-', 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 0, 0, 0, 0, '-',
	0, 'a', 'b', 'c', 'd', 'e', 'f', 'g', 'h', 'i', 'j', 'k', 'l', 'm', 'n', 'o',
	'p', 'q', 'r', 's', 't', 'u', 'v', 'w', 'x', 'y', 'z', 0, 0, 0, 0, 0,
}

// Language store the canonicalized BCP 47 tag,
// which has the generic form <lang>-<country>-<other tags>...
type Language string

// NewLanguage canonicalizes the language input (as a BCP 47 language tag), by converting it to
// lowercase, mapping '_' to '-', and stripping all characters other
// than letters, numbers and '-'.
func NewLanguage(language string) Language {
	out := make([]byte, 0, len(language))
	for _, r := range language {
		if r >= 0xFF {
			continue
		}
		can := canonMap[r]
		if can != 0 {
			out = append(out, can)
		}
	}
	return Language(out)
}

// Primary returns the root language of l, that is
// the part before the first '-' separator
func (l Language) Primary() Language {
	if index := strings.IndexByte(string(l), '-'); index != -1 {
		l = l[:index]
	}
	return l
}

// SimpleInheritance returns the list of matching language, using simple truncation inheritance.
// The resulting slice starts with the given whole
// See http://www.unicode.org/reports/tr35/#Locale_Inheritance for more information.
func (l Language) SimpleInheritance() []Language {
	tags := strings.Split(string(l), "-")
	out := make([]Language, 0, len(tags))
	for len(tags) != 0 {
		out = append(out, Language(strings.Join(tags, "-")))
		tags = tags[:len(tags)-1]
	}
	return out
}

// IsDerivedFrom returns `true` if `l` has
// the `root` as primary
func (l Language) IsDerivedFrom(root Language) bool { return l.Primary() == root }

// IsUndetermined returns `true` if its primary language is "und".
// It is a shortcut for IsDerivedFrom("und").
func (l Language) IsUndetermined() bool { return l.IsDerivedFrom("und") }

// SplitExtensionTags splits the language at the extension and private-use subtags, which are
// marked by a "-<one char>-" pattern.
// It returns the language before the first pattern, and, if any, the private-use subtag.
//
// (l, "") is returned if the language has no extension or private-use tag.
func (l Language) SplitExtensionTags() (prefix, private Language) {
	if len(l) >= 2 && l[0] == 'x' && l[1] == '-' { // x-<....> 'fully' private
		return "", l
	}

	firstExtension := -1
	for i := 0; i+3 < len(l); i++ {
		if l[i] == '-' && l[i+2] == '-' {
			if firstExtension == -1 { // mark the end of the prefix
				firstExtension = i
			}

			if l[i+1] == 'x' { // private-use tag
				return l[:firstExtension], l[i+1:]
			}
			// else keep looking for private sub tags
		}
	}

	if firstExtension == -1 {
		return l, ""
	}
	return l[:firstExtension], ""
}

// LanguageComparison is a three state enum resulting from comparing two languages
type LanguageComparison uint8

const (
	LanguagesDiffer      LanguageComparison = iota // the two languages are totally differents
	LanguagesExactMatch                            // the two languages are exactly the same
	LanguagePrimaryMatch                           // the two languages have the same primary language, but differs.
)

// Compare compares `other` and `l`.
// Undetermined languages are only compared using the remaining tags,
// meaning that "und-fr" and "und-be" are compared as LanguagesDiffer, not
// LanguagePrimaryMatch.
func (l Language) Compare(other Language) LanguageComparison {
	if l == other {
		return LanguagesExactMatch
	}

	primary1, primary2 := l.Primary(), other.Primary()
	if primary1 != primary2 {
		return LanguagesDiffer
	}

	// check for the undetermined special case
	if primary1 == "und" {
		return LanguagesDiffer
	}
	return LanguagePrimaryMatch
}

func languageFromLocale(locale string) Language {
	if i := strings.IndexByte(locale, '.'); i >= 0 {
		locale = locale[:i]
	}
	return NewLanguage(locale)
}

// DefaultLanguage returns the language found in environment variables LC_ALL, LC_CTYPE or
// LANG (in that order), or the zero value if not found.
func DefaultLanguage() Language {
	p, ok := os.LookupEnv("LC_ALL")
	if ok {
		return languageFromLocale(p)
	}

	p, ok = os.LookupEnv("LC_CTYPE")
	if ok {
		return languageFromLocale(p)
	}

	p, ok = os.LookupEnv("LANG")
	if ok {
		return languageFromLocale(p)
	}

	return ""
}

// LangID is a compact representation of a language
// this package has orthographic knowledge of.
//
// The zero value represents a language not known by the package.
type LangID uint16

// NewLangID returns the compact index of the given language,
// or false if it is not supported by this package.
//
// Derived languages not exactly supported are mapped to their primary part : for instance,
// 'fr-be' is mapped to 'fr'
func NewLangID(l Language) (LangID, bool) {
	if i, ok := binarySearchLang(l, languagesInfos[:knownLangsCount]); ok {
		return LangID(i), true
	}
	if i, ok := binarySearchLang(l, languagesInfos[knownLangsCount:]); ok {
		return knownLangsCount + LangID(i), true
	}
	return 0, false
}

func binarySearchLang(l Language, records []languageInfo) (int, bool) {
	// binary search
	index := sort.Search(len(records), func(i int) bool {
		return (records)[i].lang >= l
	})
	if index != len(records) && records[index].lang == l { // extact match
		return index, true
	}
	if index == len(records) {
		index--
	}
	// i is the index where l should be :
	// try to match the primary part
	root := l.Primary()
	for ; index >= 0; index-- {
		entry := records[index]
		if entry.lang > root { // keep going
			continue
		} else if entry.lang < root {
			// no root match
			return 0, false
		} else { // found the root
			return index, true
		}
	}
	return 0, false
}

func (lang LangID) Language() Language {
	if int(lang) >= len(languagesInfos) {
		return "<invalid language>"
	}
	return languagesInfos[lang].lang
}

// UseScript returns true if 's' is used to to write the language.
//
// If nothing is known about the language (including if 'lang' is 0),
// true will be returned.
func (lang LangID) UseScript(s Script) bool {
	if !s.Strong() { // Common and Inherited are never included in the table
		return true
	}
	if lang == 0 || lang >= knownLangsCount {
		return true
	}
	usedScripts := languagesInfos[lang].scripts
	return usedScripts[0] == s || usedScripts[1] == s || usedScripts[2] == s
}

// ScriptToLang maps a script to a language that is reasonably
// representative of the script. This will usually be the
// most widely spoken or used language written in that script:
// for instance, the sample language for `Cyrillic`
// is 'ru' (Russian), the sample language for `Arabic` is 'ar'.
//
// For some scripts, no sample language will be returned because there
// is no language that is sufficiently representative. The best
// example of this is `Han`, where various different
// variants of written Chinese, Japanese, and Korean all use
// significantly different sets of Han characters and forms
// of shared characters. No sample language can be provided
// for many historical scripts as well.
//
// inspired by pango/pango-language.c
var ScriptToLang = map[Script]LangID{
	Arabic:   LangAr,
	Armenian: LangHy,
	Bengali:  LangBn,
	// Used primarily in Taiwan, but not part of the standard
	// zh-tw orthography
	Bopomofo: 0,
	Cherokee: LangChr,
	Coptic:   LangCop,
	Cyrillic: LangRu,
	// Deseret was used to write English
	Deseret:    0,
	Devanagari: LangHi,
	Ethiopic:   LangAm,
	Georgian:   LangKa,
	Gothic:     0,
	Greek:      LangEl,
	Gujarati:   LangGu,
	Gurmukhi:   LangPa,
	Han:        0,
	Hangul:     LangKo,
	Hebrew:     LangHe,
	Hiragana:   LangJa,
	Kannada:    LangKn,
	Katakana:   LangJa,
	Khmer:      LangKm,
	Lao:        LangLo,
	Latin:      LangEn,
	Malayalam:  LangMl,
	Mongolian:  LangMn,
	Myanmar:    LangMy,
	// Ogham was used to write old Irish
	Ogham:               0,
	Old_Italic:          0,
	Oriya:               LangOr,
	Runic:               0,
	Sinhala:             LangSi,
	Syriac:              LangSyr,
	Tamil:               LangTa,
	Telugu:              LangTe,
	Thaana:              LangDv,
	Thai:                LangTh,
	Tibetan:             LangBo,
	Canadian_Aboriginal: LangIu,
	Yi:                  0,
	Tagalog:             LangTl,
	// Phillipino Languages/scripts
	Hanunoo:  LangHnn,
	Buhid:    LangBku,
	Tagbanwa: LangTbw,

	Braille: 0,
	Cypriot: 0,
	Limbu:   0,
	// Used for Somali (so) in the past
	Osmanya: 0,
	// The Shavian alphabet was designed for English
	Shavian:  0,
	Linear_B: 0,
	Tai_Le:   0,
	Ugaritic: LangUga,

	New_Tai_Lue: 0,
	Buginese:    LangBug,
	// The original script for Old Church Slavonic (chu), later
	// written with Cyrillic
	Glagolitic: 0,
	// Used for for Berber (ber), but Arabic script is more common
	Tifinagh:     0,
	Syloti_Nagri: LangSyl,
	Old_Persian:  LangPeo,

	Nko: LangNqo,
}
