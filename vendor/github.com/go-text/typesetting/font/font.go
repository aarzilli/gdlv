// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

// Package font provides an high level API to access
// Opentype font properties.
// See packages [opentype] and [opentype/tables] for a lower level, more detailled API.
package font

import (
	"errors"
	"fmt"
	"math"

	"github.com/go-text/typesetting/font/cff"
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

type (
	// GID is used to identify glyphs in a font.
	// It is mostly internal to the font and should not be confused with
	// Unicode code points.
	// Note that, despite Opentype font files using uint16, we choose to use uint32,
	// to allow room for future extension.
	GID = ot.GID

	// Tag represents an open-type name.
	// These are technically uint32's, but are usually
	// displayed in ASCII as they are all acronyms.
	// See https://developer.apple.com/fonts/TrueType-Reference-Manual/RM06/Chap6.html#Overview
	Tag = ot.Tag

	// VarCoord stores font variation coordinates,
	// which are real numbers in [-1;1], stored as fixed 2.14 integer.
	VarCoord = tables.Coord

	// Resource is a combination of io.Reader, io.Seeker and io.ReaderAt.
	// This interface is satisfied by most things that you'd want
	// to parse, for example *os.File, io.SectionReader or *bytes.Reader.
	Resource = ot.Resource

	// GlyphExtents exposes extent values, measured in font units.
	// Note that height is negative in coordinate systems that grow up.
	GlyphExtents = ot.GlyphExtents
)

// ParseTTF parse an Opentype font file (.otf, .ttf).
// See ParseTTC for support for collections.
func ParseTTF(file Resource) (*Face, error) {
	ld, err := ot.NewLoader(file)
	if err != nil {
		return nil, err
	}
	ft, err := NewFont(ld)
	if err != nil {
		return nil, err
	}
	return NewFace(ft), nil
}

// ParseTTC parse an Opentype font file, with support for collections.
// Single font files are supported, returning a slice with length 1.
func ParseTTC(file Resource) ([]*Face, error) {
	lds, err := ot.NewLoaders(file)
	if err != nil {
		return nil, err
	}
	out := make([]*Face, len(lds))
	for i, ld := range lds {
		ft, err := NewFont(ld)
		if err != nil {
			return nil, fmt.Errorf("reading font %d of collection: %s", i, err)
		}
		out[i] = NewFace(ft)
	}

	return out, nil
}

// EmptyGlyph represents an invisible glyph, which should not be drawn,
// but whose advance and offsets should still be accounted for when rendering.
const EmptyGlyph GID = math.MaxUint32

// FontExtents exposes font-wide extent values, measured in font units.
// Note that typically ascender is positive and descender negative in coordinate systems that grow up.
type FontExtents struct {
	Ascender  float32 // Typographic ascender.
	Descender float32 // Typographic descender.
	LineGap   float32 // Suggested line spacing gap.
}

// LineMetric identifies one metric about the font.
type LineMetric uint8

const (
	// Distance above the baseline of the top of the underline.
	// Since most fonts have underline positions beneath the baseline, this value is typically negative.
	UnderlinePosition LineMetric = iota

	// Suggested thickness to draw for the underline.
	UnderlineThickness

	// Distance above the baseline of the top of the strikethrough.
	StrikethroughPosition

	// Suggested thickness to draw for the strikethrough.
	StrikethroughThickness

	SuperscriptEmYSize
	SuperscriptEmXOffset

	SubscriptEmYSize
	SubscriptEmYOffset
	SubscriptEmXOffset

	CapHeight
	XHeight
)

// FontID represents an identifier of a font (possibly in a collection),
// and an optional variable instance.
type FontID struct {
	File string // The filename or identifier of the font file.

	// The index of the face in a collection. It is always 0 for
	// single font files.
	Index uint16

	// For variable fonts, stores 1 + the instance index.
	// It is set to 0 to ignore variations, or for non variable fonts.
	Instance uint16
}

// Font represents one Opentype font file (or one sub font of a collection).
// It is an educated view of the underlying font file, optimized for quick access
// to information required by text layout engines.
//
// All its methods are read-only and a [*Font] object is thus safe for concurrent use.
type Font struct {
	// Cmap is the 'cmap' table
	Cmap    Cmap
	cmapVar UnicodeVariations

	hhea *tables.Hhea
	vhea *tables.Vhea
	vorg *tables.VORG // optional
	cff  *cff.CFF     // optional
	cff2 *cff.CFF2    // optional
	post post         // optional
	svg  svg          // optional

	glyf   tables.Glyf
	hmtx   tables.Hmtx
	vmtx   tables.Vmtx
	bitmap bitmap
	sbix   sbix

	os2   os2
	names tables.Name
	head  tables.Head

	// Optional, only present in variable fonts

	fvar fvar         // optional
	hvar *tables.HVAR // optional
	vvar *tables.VVAR // optional
	avar tables.Avar
	mvar mvar
	gvar gvar

	// Advanced layout tables.

	GDEF tables.GDEF // An absent table has a nil GlyphClassDef
	Trak tables.Trak
	Ankr tables.Ankr
	Feat tables.Feat
	Ltag tables.Ltag
	Morx Morx
	Kern Kernx
	Kerx Kernx
	GSUB GSUB // An absent table has a nil slice of lookups
	GPOS GPOS // An absent table has a nil slice of lookups

	upem    uint16 // cached value
	nGlyphs int
}

// NewFont loads all the font tables, sanitizing them.
// An error is returned only when required tables 'cmap', 'head', 'maxp' are invalid (or missing).
// More control on errors is available by using package [tables].
func NewFont(ld *ot.Loader) (*Font, error) {
	var (
		out Font
		err error
	)

	// 'cmap' handling depend on os2
	raw, _ := ld.RawTable(ot.MustNewTag("OS/2"))
	os2, _, _ := tables.ParseOs2(raw)
	fontPage := os2.FontPage()
	out.os2, _ = newOs2(os2)

	raw, err = ld.RawTable(ot.MustNewTag("cmap"))
	if err != nil {
		return nil, err
	}
	tb, _, err := tables.ParseCmap(raw)
	if err != nil {
		return nil, err
	}
	out.Cmap, out.cmapVar, err = ProcessCmap(tb, fontPage)
	if err != nil {
		return nil, err
	}

	out.head, _, err = LoadHeadTable(ld, nil)
	if err != nil {
		return nil, err
	}

	raw, err = ld.RawTable(ot.MustNewTag("maxp"))
	if err != nil {
		return nil, err
	}
	maxp, _, err := tables.ParseMaxp(raw)
	if err != nil {
		return nil, err
	}
	out.nGlyphs = int(maxp.NumGlyphs)

	// We considerer all the following tables as optional,
	// since, in practice, users won't have much control on the
	// font files they use
	//
	// Ignoring the errors on `RawTable` is OK : it will trigger an error on the next tables.ParseXXX,
	// which in turn will return a zero value

	raw, _ = ld.RawTable(ot.MustNewTag("fvar"))
	fvar, _, _ := tables.ParseFvar(raw)
	out.fvar = newFvar(fvar)

	raw, _ = ld.RawTable(ot.MustNewTag("avar"))
	out.avar, _, _ = tables.ParseAvar(raw)

	out.upem = out.head.Upem()

	raw, _ = ld.RawTable(ot.MustNewTag("glyf"))
	locaRaw, _ := ld.RawTable(ot.MustNewTag("loca"))
	loca, err := tables.ParseLoca(locaRaw, out.nGlyphs, out.head.IndexToLocFormat == 1)
	if err == nil { // ParseGlyf panics if len(loca) == 0
		out.glyf, _ = tables.ParseGlyf(raw, loca)
	}

	out.bitmap = selectBitmapTable(ld)

	raw, _ = ld.RawTable(ot.MustNewTag("sbix"))
	sbix, _, _ := tables.ParseSbix(raw, out.nGlyphs)
	out.sbix = newSbix(sbix)

	out.cff, _ = loadCff(ld, out.nGlyphs)
	out.cff2, _ = loadCff2(ld, out.nGlyphs, len(out.fvar))

	raw, _ = ld.RawTable(ot.MustNewTag("post"))
	post, _, _ := tables.ParsePost(raw)
	out.post, _ = newPost(post)

	raw, _ = ld.RawTable(ot.MustNewTag("SVG "))
	svg, _, _ := tables.ParseSVG(raw)
	out.svg, _ = newSvg(svg)

	out.hhea, out.hmtx, _ = loadHmtx(ld, out.nGlyphs)
	out.vhea, out.vmtx, _ = loadVmtx(ld, out.nGlyphs)

	if axisCount := len(out.fvar); axisCount != 0 {
		raw, _ = ld.RawTable(ot.MustNewTag("MVAR"))
		mvar, _, _ := tables.ParseMVAR(raw)
		out.mvar, _ = newMvar(mvar, axisCount)

		raw, _ = ld.RawTable(ot.MustNewTag("gvar"))
		gvar, _, _ := tables.ParseGvar(raw)
		out.gvar, _ = newGvar(gvar, out.glyf)

		raw, _ = ld.RawTable(ot.MustNewTag("HVAR"))
		hvar, _, err := tables.ParseHVAR(raw)
		if err == nil {
			out.hvar = &hvar
		}

		raw, _ = ld.RawTable(ot.MustNewTag("VVAR"))
		vvar, _, err := tables.ParseHVAR(raw)
		if err == nil {
			out.vvar = &vvar
		}
	}

	raw, _ = ld.RawTable(ot.MustNewTag("VORG"))
	vorg, _, err := tables.ParseVORG(raw)
	if err == nil {
		out.vorg = &vorg
	}

	raw, _ = ld.RawTable(ot.MustNewTag("name"))
	out.names, _, _ = tables.ParseName(raw)

	// layout tables
	out.GDEF, _ = loadGDEF(ld, len(out.fvar))

	raw, _ = ld.RawTable(ot.MustNewTag("GSUB"))
	layout, _, err := tables.ParseLayout(raw)
	// harfbuzz relies on GSUB.Loookups being nil when the table is absent
	if err == nil {
		out.GSUB, _ = newGSUB(layout)
	}

	raw, _ = ld.RawTable(ot.MustNewTag("GPOS"))
	layout, _, err = tables.ParseLayout(raw)
	// harfbuzz relies on GPOS.Loookups being nil when the table is absent
	if err == nil {
		out.GPOS, _ = newGPOS(layout)
	}

	raw, _ = ld.RawTable(ot.MustNewTag("morx"))
	morx, _, _ := tables.ParseMorx(raw, out.nGlyphs)
	out.Morx = newMorx(morx)

	raw, _ = ld.RawTable(ot.MustNewTag("kerx"))
	kerx, _, _ := tables.ParseKerx(raw, out.nGlyphs)
	out.Kerx = newKernxFromKerx(kerx)

	raw, _ = ld.RawTable(ot.MustNewTag("kern"))
	kern, _, _ := tables.ParseKern(raw)
	out.Kern = newKernxFromKern(kern)

	raw, _ = ld.RawTable(ot.MustNewTag("ankr"))
	out.Ankr, _, _ = tables.ParseAnkr(raw, out.nGlyphs)

	raw, _ = ld.RawTable(ot.MustNewTag("trak"))
	out.Trak, _, _ = tables.ParseTrak(raw)

	raw, _ = ld.RawTable(ot.MustNewTag("feat"))
	out.Feat, _, _ = tables.ParseFeat(raw)

	raw, _ = ld.RawTable(ot.MustNewTag("ltag"))
	out.Ltag, _, _ = tables.ParseLtag(raw)

	return &out, nil
}

var bhedTag = ot.MustNewTag("bhed")

// LoadHeadTable loads the 'head' or the 'bhed' table.
//
// If a 'bhed' Apple table is present, it replaces the 'head' one.
//
// [buffer] may be provided to reduce allocations; the returned [tables.Head] is guaranteed
// not to retain any reference on [buffer].
// If [buffer] is nil or has not enough capacity, a new slice is allocated (and returned).
func LoadHeadTable(ld *ot.Loader, buffer []byte) (tables.Head, []byte, error) {
	var err error
	// check 'bhed' first
	if ld.HasTable(bhedTag) {
		buffer, err = ld.RawTableTo(bhedTag, buffer)
	} else {
		buffer, err = ld.RawTableTo(ot.MustNewTag("head"), buffer)
	}
	if err != nil {
		return tables.Head{}, nil, errors.New("missing required head (or bhed) table")
	}
	out, _, err := tables.ParseHead(buffer)
	return out, buffer, err
}

// return nil if no table is valid (or present)
func selectBitmapTable(ld *ot.Loader) bitmap {
	color, err := loadBitmap(ld, ot.MustNewTag("CBLC"), ot.MustNewTag("CBDT"))
	if err == nil {
		return color
	}

	gray, err := loadBitmap(ld, ot.MustNewTag("EBLC"), ot.MustNewTag("EBDT"))
	if err == nil {
		return gray
	}

	apple, err := loadBitmap(ld, ot.MustNewTag("bloc"), ot.MustNewTag("bdat"))
	if err == nil {
		return apple
	}

	return nil
}

// return nil if the table is missing or invalid
func loadCff(ld *ot.Loader, numGlyphs int) (*cff.CFF, error) {
	raw, err := ld.RawTable(ot.MustNewTag("CFF "))
	if err != nil {
		return nil, err
	}
	cff, err := cff.Parse(raw)
	if err != nil {
		return nil, err
	}

	if N := len(cff.Charstrings); N != numGlyphs {
		return nil, fmt.Errorf("invalid number of glyphs in CFF table (%d != %d)", N, numGlyphs)
	}
	return cff, nil
}

// return nil if the table is missing or invalid
func loadCff2(ld *ot.Loader, numGlyphs, axisCount int) (*cff.CFF2, error) {
	raw, err := ld.RawTable(ot.MustNewTag("CFF2"))
	if err != nil {
		return nil, err
	}
	cff2, err := cff.ParseCFF2(raw)
	if err != nil {
		return nil, err
	}

	if N := len(cff2.Charstrings); N != numGlyphs {
		return nil, fmt.Errorf("invalid number of glyphs in CFF table (%d != %d)", N, numGlyphs)
	}

	if got := cff2.VarStore.AxisCount(); got != -1 && got != axisCount {
		return nil, fmt.Errorf("invalid number of axis in CFF table (%d != %d)", got, axisCount)
	}
	return cff2, nil
}

func loadHVtmx(hheaRaw, htmxRaw []byte, numGlyphs int) (*tables.Hhea, tables.Hmtx, error) {
	hhea, _, err := tables.ParseHhea(hheaRaw)
	if err != nil {
		return nil, tables.Hmtx{}, err
	}

	hmtx, _, err := tables.ParseHmtx(htmxRaw, int(hhea.NumOfLongMetrics), numGlyphs-int(hhea.NumOfLongMetrics))
	if err != nil {
		return nil, tables.Hmtx{}, err
	}
	return &hhea, hmtx, nil
}

func loadHmtx(ld *ot.Loader, numGlyphs int) (*tables.Hhea, tables.Hmtx, error) {
	rawHead, err := ld.RawTable(ot.MustNewTag("hhea"))
	if err != nil {
		return nil, tables.Hmtx{}, err
	}

	rawMetrics, err := ld.RawTable(ot.MustNewTag("hmtx"))
	if err != nil {
		return nil, tables.Hmtx{}, err
	}

	return loadHVtmx(rawHead, rawMetrics, numGlyphs)
}

func loadVmtx(ld *ot.Loader, numGlyphs int) (*tables.Hhea, tables.Hmtx, error) {
	rawHead, err := ld.RawTable(ot.MustNewTag("vhea"))
	if err != nil {
		return nil, tables.Hmtx{}, err
	}

	rawMetrics, err := ld.RawTable(ot.MustNewTag("vmtx"))
	if err != nil {
		return nil, tables.Hmtx{}, err
	}

	return loadHVtmx(rawHead, rawMetrics, numGlyphs)
}

func loadGDEF(ld *ot.Loader, axisCount int) (tables.GDEF, error) {
	raw, err := ld.RawTable(ot.MustNewTag("GDEF"))
	if err != nil {
		return tables.GDEF{}, err
	}
	GDEF, _, err := tables.ParseGDEF(raw)
	if err != nil {
		return tables.GDEF{}, err
	}

	err = sanitizeGDEF(GDEF, axisCount)
	if err != nil {
		return tables.GDEF{}, err
	}
	return GDEF, nil
}

// Face is a font with user-provided settings.
// Contrary to the [*Font] objects, Faces are NOT safe for concurrent use.
// A Face caches glyph extents and should be reused when possible.
type Face struct {
	*Font

	extentsCache extentsCache

	coords       []tables.Coord
	xPpem, yPpem uint16
}

// NewFace wraps [font] and initializes glyph caches.
func NewFace(font *Font) *Face {
	return &Face{Font: font, extentsCache: make(extentsCache, font.nGlyphs)}
}

// Ppem returns the horizontal and vertical pixels-per-em (ppem), used to select bitmap sizes.
func (f *Face) Ppem() (x, y uint16) { return f.xPpem, f.yPpem }

// SetPpem applies horizontal and vertical pixels-per-em (ppem).
func (f *Face) SetPpem(x, y uint16) {
	f.xPpem, f.yPpem = x, y
	// invalid the cache
	f.extentsCache.reset()
}

// Coords return a read-only slice of the current variable coordinates, expressed in normalized units.
// It is empty for non variable fonts.
func (f *Face) Coords() []tables.Coord { return f.coords }

// SetCoords applies a list of variation coordinates, expressed in normalized units.
// Use [NormalizeVariations] to convert from design (user) space units.
func (f *Face) SetCoords(coords []tables.Coord) {
	f.coords = coords
	// invalid the cache
	f.extentsCache.reset()
}
