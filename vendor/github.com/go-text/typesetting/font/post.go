// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package font

import (
	"errors"

	"github.com/go-text/typesetting/font/opentype/tables"
)

const numBuiltInPostNames = len(builtInPostNames)

// names is the built-in post table names listed at
// https://developer.apple.com/fonts/TrueType-Reference-Manual/RM06/Chap6post.html
var builtInPostNames = [...]string{
	".notdef",
	".null",
	"nonmarkingreturn",
	"space",
	"exclam",
	"quotedbl",
	"numbersign",
	"dollar",
	"percent",
	"ampersand",
	"quotesingle",
	"parenleft",
	"parenright",
	"asterisk",
	"plus",
	"comma",
	"hyphen",
	"period",
	"slash",
	"zero",
	"one",
	"two",
	"three",
	"four",
	"five",
	"six",
	"seven",
	"eight",
	"nine",
	"colon",
	"semicolon",
	"less",
	"equal",
	"greater",
	"question",
	"at",
	"A",
	"B",
	"C",
	"D",
	"E",
	"F",
	"G",
	"H",
	"I",
	"J",
	"K",
	"L",
	"M",
	"N",
	"O",
	"P",
	"Q",
	"R",
	"S",
	"T",
	"U",
	"V",
	"W",
	"X",
	"Y",
	"Z",
	"bracketleft",
	"backslash",
	"bracketright",
	"asciicircum",
	"underscore",
	"grave",
	"a",
	"b",
	"c",
	"d",
	"e",
	"f",
	"g",
	"h",
	"i",
	"j",
	"k",
	"l",
	"m",
	"n",
	"o",
	"p",
	"q",
	"r",
	"s",
	"t",
	"u",
	"v",
	"w",
	"x",
	"y",
	"z",
	"braceleft",
	"bar",
	"braceright",
	"asciitilde",
	"Adieresis",
	"Aring",
	"Ccedilla",
	"Eacute",
	"Ntilde",
	"Odieresis",
	"Udieresis",
	"aacute",
	"agrave",
	"acircumflex",
	"adieresis",
	"atilde",
	"aring",
	"ccedilla",
	"eacute",
	"egrave",
	"ecircumflex",
	"edieresis",
	"iacute",
	"igrave",
	"icircumflex",
	"idieresis",
	"ntilde",
	"oacute",
	"ograve",
	"ocircumflex",
	"odieresis",
	"otilde",
	"uacute",
	"ugrave",
	"ucircumflex",
	"udieresis",
	"dagger",
	"degree",
	"cent",
	"sterling",
	"section",
	"bullet",
	"paragraph",
	"germandbls",
	"registered",
	"copyright",
	"trademark",
	"acute",
	"dieresis",
	"notequal",
	"AE",
	"Oslash",
	"infinity",
	"plusminus",
	"lessequal",
	"greaterequal",
	"yen",
	"mu",
	"partialdiff",
	"summation",
	"product",
	"pi",
	"integral",
	"ordfeminine",
	"ordmasculine",
	"Omega",
	"ae",
	"oslash",
	"questiondown",
	"exclamdown",
	"logicalnot",
	"radical",
	"florin",
	"approxequal",
	"Delta",
	"guillemotleft",
	"guillemotright",
	"ellipsis",
	"nonbreakingspace",
	"Agrave",
	"Atilde",
	"Otilde",
	"OE",
	"oe",
	"endash",
	"emdash",
	"quotedblleft",
	"quotedblright",
	"quoteleft",
	"quoteright",
	"divide",
	"lozenge",
	"ydieresis",
	"Ydieresis",
	"fraction",
	"currency",
	"guilsinglleft",
	"guilsinglright",
	"fi",
	"fl",
	"daggerdbl",
	"periodcentered",
	"quotesinglbase",
	"quotedblbase",
	"perthousand",
	"Acircumflex",
	"Ecircumflex",
	"Aacute",
	"Edieresis",
	"Egrave",
	"Iacute",
	"Icircumflex",
	"Idieresis",
	"Igrave",
	"Oacute",
	"Ocircumflex",
	"apple",
	"Ograve",
	"Uacute",
	"Ucircumflex",
	"Ugrave",
	"dotlessi",
	"circumflex",
	"tilde",
	"macron",
	"breve",
	"dotaccent",
	"ring",
	"cedilla",
	"hungarumlaut",
	"ogonek",
	"caron",
	"Lslash",
	"lslash",
	"Scaron",
	"scaron",
	"Zcaron",
	"zcaron",
	"brokenbar",
	"Eth",
	"eth",
	"Yacute",
	"yacute",
	"Thorn",
	"thorn",
	"minus",
	"multiply",
	"onesuperior",
	"twosuperior",
	"threesuperior",
	"onehalf",
	"onequarter",
	"threequarters",
	"franc",
	"Gbreve",
	"gbreve",
	"Idotaccent",
	"Scedilla",
	"scedilla",
	"Cacute",
	"cacute",
	"Ccaron",
	"ccaron",
	"dcroat",
}

type post struct {
	// suggested distance of the top of the
	// underline from the baseline (negative values indicate below baseline).
	underlinePosition float32
	// suggested values for the underline thickness.
	underlineThickness float32

	names postGlyphNames

	isFixedPitch bool
}

func newPost(pst tables.Post) (post, error) {
	out := post{
		underlinePosition:  float32(pst.UnderlinePosition),
		underlineThickness: float32(pst.UnderlineThickness),
		isFixedPitch:       pst.IsFixedPitch != 0,
	}
	switch names := pst.Names.(type) {
	case tables.PostNames10:
		out.names = postNames10or30{}
	case tables.PostNames20:
		n := postNames20(names)
		if err := n.sanitize(); err != nil {
			return out, err
		}
		out.names = n
	case tables.PostNames30:
		// no-op, do not use the post name tables
	}
	return out, nil
}

// postGlyphNames stores the names of a 'post' table.
type postGlyphNames interface {
	// GlyphName return the postscript name of a
	// glyph, or an empty string if it not found
	glyphName(x GID) string
}

type postNames10or30 struct{}

func (p postNames10or30) glyphName(x GID) string {
	if int(x) >= numBuiltInPostNames {
		return ""
	}
	// https://developer.apple.com/fonts/TrueType-Reference-Manual/RM06/Chap6post.html
	return builtInPostNames[x]
}

type postNames20 tables.PostNames20

func (p postNames20) glyphName(x GID) string {
	if int(x) >= len(p.GlyphNameIndexes) {
		return ""
	}
	u := int(p.GlyphNameIndexes[x])
	if u < numBuiltInPostNames {
		return builtInPostNames[u]
	}
	u -= numBuiltInPostNames
	return p.Strings[u]
}

// check that all the indexes are valid
func (p postNames20) sanitize() error {
	var maxIndex uint16
	// find the maximum
	for _, u := range p.GlyphNameIndexes {
		// https://developer.apple.com/fonts/TrueType-Reference-Manual/RM06/Chap6post.html
		// says that "32768 through 65535 are reserved for future use".
		if u > 32767 {
			return errors.New("invalid index in Postscript names table format 20")
		}
		if u > maxIndex {
			maxIndex = u
		}
	}

	if int(maxIndex) >= numBuiltInPostNames && len(p.Strings) < (int(maxIndex)-numBuiltInPostNames) {
		return errors.New("invalid index in Postscript names table format 20")
	}
	return nil
}
