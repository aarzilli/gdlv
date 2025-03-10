package font

import (
	"strings"

	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

// name values corresponding to the xxxConsts arrays
var (
	styleStrings   [len(styleConsts)]string
	weightStrings  [len(weightConsts)]string
	stretchStrings [len(stretchConsts)]string
)

func init() {
	for i, v := range styleConsts {
		styleStrings[i] = v.name
	}
	for i, v := range weightConsts {
		weightStrings[i] = v.name
	}
	for i, v := range stretchConsts {
		stretchStrings[i] = v.name
	}
}

var styleConsts = [...]struct {
	name  string
	value Style
}{
	{"italic", StyleItalic},
	{"kursiv", StyleItalic},
	{"oblique", StyleItalic}, // map Oblique to Italic
}

var weightConsts = [...]struct {
	name  string
	value Weight
}{
	{"thin", WeightThin},
	{"extralight", WeightExtraLight},
	{"ultralight", WeightExtraLight},
	{"light", WeightLight},
	{"demilight", (WeightLight + WeightNormal) / 2},
	{"semilight", (WeightLight + WeightNormal) / 2},
	{"book", WeightNormal - 20},
	{"regular", WeightNormal},
	{"normal", WeightNormal},
	{"medium", WeightMedium},
	{"demibold", WeightSemibold},
	{"demi", WeightSemibold},
	{"semibold", WeightSemibold},
	{"extrabold", WeightExtraBold},
	{"superbold", WeightExtraBold},
	{"ultrabold", WeightExtraBold},
	{"bold", WeightBold},
	{"ultrablack", WeightBlack + 20},
	{"superblack", WeightBlack + 20},
	{"extrablack", WeightBlack + 20},
	{"black", WeightBlack},
	{"heavy", WeightBlack},
}

var stretchConsts = [...]struct {
	name  string
	value Stretch
}{
	{"ultracondensed", StretchUltraCondensed},
	{"extracondensed", StretchExtraCondensed},
	{"semicondensed", StretchSemiCondensed},
	{"condensed", StretchCondensed},
	{"normal", StretchNormal},
	{"semiexpanded", StretchSemiExpanded},
	{"extraexpanded", StretchExtraExpanded},
	{"ultraexpanded", StretchUltraExpanded},
	{"expanded", StretchExpanded},
	{"extended", StretchExpanded},
}

// Style (also called slant) allows italic or oblique faces to be selected.
type Style uint8

// note that we use the 0 value to indicate no style has been found yet
const (
	// A face that is neither italic not obliqued.
	StyleNormal Style = iota + 1
	// A form that is generally cursive in nature or slanted.
	// This groups what is usually called Italic or Oblique.
	StyleItalic
)

// Weight is the degree of blackness or stroke thickness of a font.
// This value ranges from 100.0 to 900.0, with 400.0 as normal.
type Weight float32

const (
	// Thin weight (100), the thinnest value.
	WeightThin Weight = 100
	// Extra light weight (200).
	WeightExtraLight Weight = 200
	// Light weight (300).
	WeightLight Weight = 300
	// Normal (400).
	WeightNormal Weight = 400
	// Medium weight (500, higher than normal).
	WeightMedium Weight = 500
	// Semibold weight (600).
	WeightSemibold Weight = 600
	// Bold weight (700).
	WeightBold Weight = 700
	// Extra-bold weight (800).
	WeightExtraBold Weight = 800
	// Black weight (900), the thickest value.
	WeightBlack Weight = 900
)

// Stretch is the width of a font as an approximate fraction of the normal width.
// Widths range from 0.5 to 2.0 inclusive, with 1.0 as the normal width.
type Stretch float32

const (
	// Ultra-condensed width (50%), the narrowest possible.
	StretchUltraCondensed Stretch = 0.5
	// Extra-condensed width (62.5%).
	StretchExtraCondensed Stretch = 0.625
	// Condensed width (75%).
	StretchCondensed Stretch = 0.75
	// Semi-condensed width (87.5%).
	StretchSemiCondensed Stretch = 0.875
	// Normal width (100%).
	StretchNormal Stretch = 1.0
	// Semi-expanded width (112.5%).
	StretchSemiExpanded Stretch = 1.125
	// Expanded width (125%).
	StretchExpanded Stretch = 1.25
	// Extra-expanded width (150%).
	StretchExtraExpanded Stretch = 1.5
	// Ultra-expanded width (200%), the widest possible.
	StretchUltraExpanded Stretch = 2.0
)

// Aspect stores the properties that specify which font in a family to use:
// style, weight, and stretchiness.
type Aspect struct {
	Style   Style
	Weight  Weight
	Stretch Stretch
}

// aspect returns the [aspect] of the font,
// defaulting to regular style.
func (fd *fontDescriptor) aspect() Aspect {
	// use rawAspect and additionalStyle to infer the Aspect

	out := fd.rawAspect() // load the aspect properties ...

	// ... try to fill the missing one with the "style"
	out.inferFromStyle(fd.additionalStyle())

	// ... and finally add default to regular values :
	// StyleNormal, WeightNormal, StretchNormal
	out.SetDefaults()

	return out
}

// some fonts includes aspect information in a string description,
// usually called "style"
// inferFromStyle scans such a string and fills the missing fields,
func (as *Aspect) inferFromStyle(additionalStyle string) {
	additionalStyle = NormalizeFamily(additionalStyle)

	if as.Style == 0 {
		if index := stringContainsConst(additionalStyle, styleStrings[:]); index != -1 {
			as.Style = styleConsts[index].value
		}
	}

	if as.Weight == 0 {
		if index := stringContainsConst(additionalStyle, weightStrings[:]); index != -1 {
			as.Weight = weightConsts[index].value
		}
	}

	if as.Stretch == 0 {
		if index := stringContainsConst(additionalStyle, stretchStrings[:]); index != -1 {
			as.Stretch = stretchConsts[index].value
		}
	}
}

// SetDefaults replace unspecified values by the default values: StyleNormal, WeightNormal, StretchNormal
func (as *Aspect) SetDefaults() {
	if as.Style == 0 {
		as.Style = StyleNormal
	}

	if as.Stretch == 0 {
		as.Stretch = StretchNormal
	}

	if as.Weight == 0 {
		as.Weight = WeightNormal
	}
}

func (fd *fontDescriptor) additionalStyle() string {
	var style string
	if fd.os2 != nil && fd.os2.fsSelection&256 != 0 {
		style = fd.names.Name(namePreferredSubfamily)
		if style == "" {
			style = fd.names.Name(nameFontSubfamily)
		}
	} else {
		style = fd.names.Name(nameWWSSubfamily)
		if style == "" {
			style = fd.names.Name(namePreferredSubfamily)
		}
		if style == "" {
			style = fd.names.Name(nameFontSubfamily)
		}
	}
	style = strings.TrimSpace(style)
	return style
}

func (fd *fontDescriptor) rawAspect() Aspect {
	var (
		style   Style
		weight  Weight
		stretch Stretch
	)

	if fd.os2 != nil {
		// We have an OS/2 table; use the `fsSelection' field.  Bit 9
		// indicates an oblique font face.  This flag has been
		// introduced in version 1.5 of the OpenType specification.
		if fd.os2.fsSelection&(1<<9) != 0 || fd.os2.fsSelection&1 != 0 {
			style = StyleItalic
		}

		weight = Weight(fd.os2.usWeightClass)

		switch fd.os2.usWidthClass {
		case 1:
			stretch = StretchUltraCondensed
		case 2:
			stretch = StretchExtraCondensed
		case 3:
			stretch = StretchCondensed
		case 4:
			stretch = StretchSemiCondensed
		case 5:
			stretch = StretchNormal
		case 6:
			stretch = StretchSemiExpanded
		case 7:
			stretch = StretchExpanded
		case 8:
			stretch = StretchExtraExpanded
		case 9:
			stretch = StretchUltraExpanded
		}

	} else {
		// this is an old Mac font, use the header field
		if isItalic := fd.head.MacStyle&2 != 0; isItalic {
			style = StyleItalic
		}
		if isBold := fd.head.MacStyle&1 != 0; isBold {
			weight = WeightBold
		}
	}

	return Aspect{style, weight, stretch}
}

var rp = strings.NewReplacer(" ", "", "\t", "")

// NormalizeFamily removes spaces and lower the given string.
func NormalizeFamily(family string) string { return rp.Replace(strings.ToLower(family)) }

// returns the index in `constants` of a constant contained in `str`,
// or -1
func stringContainsConst(str string, constants []string) int {
	for i, c := range constants {
		if strings.Contains(str, c) {
			return i
		}
	}
	return -1
}

const (
	nameFontFamily         tables.NameID = 1
	nameFontSubfamily      tables.NameID = 2
	namePreferredFamily    tables.NameID = 16 // or Typographic Family
	namePreferredSubfamily tables.NameID = 17 // or Typographic Subfamily
	nameWWSFamily          tables.NameID = 21 //
	nameWWSSubfamily       tables.NameID = 22 //
)

type os2Desc struct {
	usWeightClass uint16
	usWidthClass  uint16
	fsSelection   uint16
}

func newOS2Desc(os tables.Os2) *os2Desc {
	return &os2Desc{
		usWeightClass: os.USWeightClass,
		usWidthClass:  os.USWidthClass,
		fsSelection:   os.FsSelection,
	}
}

// fontDescriptor provides access to family and aspect
type fontDescriptor struct {
	// these tables are required both in Family
	// and Aspect
	os2   *os2Desc // optional
	names tables.Name
	head  tables.Head
}

func newFontDescriptor(ld *ot.Loader, buffer []byte) (fontDescriptor, []byte) {
	var desc fontDescriptor

	// load tables, all considered optional
	buffer, _ = ld.RawTableTo(ot.MustNewTag("OS/2"), buffer)
	if os2, _, err := tables.ParseOs2(buffer); err == nil {
		desc.os2 = newOS2Desc(os2)
	}

	desc.head, buffer, _ = LoadHeadTable(ld, buffer)

	buffer, _ = ld.RawTableTo(ot.MustNewTag("name"), buffer)
	desc.names, _, _ = tables.ParseName(buffer)

	return desc, buffer
}

// family returns the font family name.
func (fd *fontDescriptor) family() string {
	var family string
	if fd.os2 != nil && fd.os2.fsSelection&256 != 0 {
		family = fd.names.Name(namePreferredFamily)
		if family == "" {
			family = fd.names.Name(nameFontFamily)
		}
	} else {
		family = fd.names.Name(nameWWSFamily)
		if family == "" {
			family = fd.names.Name(namePreferredFamily)
		}
		if family == "" {
			family = fd.names.Name(nameFontFamily)
		}
	}
	return family
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func approximatelyEqual(x, y int) bool { return abs(x-y)*33 <= max(abs(x), abs(y)) }

// IsMonospace returns 'true' if the font is monospace,
// by inspecting the horizontal advances of its glyphs.
func (fd *Font) IsMonospace() bool {
	// code adapted from fontconfig

	// try the fast shortcuts
	if fd.post.isFixedPitch {
		return true
	}

	if fd.hmtx.IsEmpty() {
		// we can't be sure, so be conservative
		return false
	}

	if len(fd.hmtx.Metrics) == 1 {
		return true
	}

	// directly read the advances in the 'hmtx' table
	var firstAdvance int
	for gid, metric := range fd.hmtx.Metrics {
		if gid == 0 { // ignore the 'unset' glyph, which may be different
			continue
		}
		advance := int(metric.AdvanceWidth)
		if advance == 0 { // do not count zero as a proper width
			continue
		}

		if firstAdvance == 0 {
			firstAdvance = advance
			continue
		}

		if approximatelyEqual(advance, firstAdvance) {
			continue
		}

		// two distinct advances : the font is not monospace
		return false
	}

	return true
}

// Description provides font metadata.
type Description struct {
	Family string
	Aspect Aspect
}

// Describe provides access to family and aspect.
//
// 'buffer' may be provided to reduce allocations.
//
// It provides an efficient API, loading only the mininum
// tables required. See also the method [Font.Describe]
// if you already have loaded the font.
func Describe(ld *ot.Loader, buffer []byte) (Description, []byte) {
	desc, buffer := newFontDescriptor(ld, buffer)
	return Description{desc.family(), desc.aspect()}, buffer
}

// Describe provides access to family and aspect.
//
// See also the package level function [Describe],
// which is more efficient if you only need the font
// metadata.
func (ft *Font) Describe() Description {
	desc := fontDescriptor{ft.os2.os2Desc, ft.names, ft.head}
	return Description{desc.family(), desc.aspect()}
}
