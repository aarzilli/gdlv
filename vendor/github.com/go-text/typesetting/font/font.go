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

	STAT *STAT // optional

	COLR *tables.COLR1 // color glyphs, optional
	CPAL CPAL          // color glyphs, optional

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

	raw, _ = ld.RawTable(ot.MustNewTag("COLR"))
	if colr, err := tables.ParseCOLR(raw); err == nil {
		out.COLR = &colr
		// color table without CPAL is broken
		raw, _ = ld.RawTable(ot.MustNewTag("CPAL"))
		cpal, _, _ := tables.ParseCPAL(raw)
		out.CPAL, err = newCPAL(cpal)
		if err != nil {
			return nil, err
		}
	}

	raw, _ = ld.RawTable(ot.MustNewTag("STAT"))
	stat, _, err := tables.ParseSTAT(raw)
	if err == nil {
		out.STAT = &stat
	}

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
		vvar, _, err := tables.ParseVVAR(raw)
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

	gsubRaw, _ := ld.RawTable(ot.MustNewTag("GSUB"))
	layout, _, err := tables.ParseLayout(gsubRaw)
	// harfbuzz relies on GSUB.Loookups being nil when the table is absent
	if err == nil {
		out.GSUB, _ = newGSUB(layout)
	}

	gposRaw, _ := ld.RawTable(ot.MustNewTag("GPOS"))
	layout, _, err = tables.ParseLayout(gposRaw)
	// harfbuzz relies on GPOS.Loookups being nil when the table is absent
	if err == nil {
		out.GPOS, _ = newGPOS(layout)
	}

	out.GDEF, _ = loadGDEF(ld, len(out.fvar), gsubRaw, gposRaw)

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

// see harfbuzz/src/hb-ot-layout.cc
func isGDEFBlocklisted(gdef, gsub, gpos []byte) bool {
	id := uint64(len(gdef))<<42 | uint64(len(gsub))<<21 | uint64(len(gpos))
	switch id {
	/* sha1sum:c5ee92f0bca4bfb7d06c4d03e8cf9f9cf75d2e8a Windows 7? timesi.ttf */
	case 442<<42 | 2874<<21 | 42038,
		/* sha1sum:37fc8c16a0894ab7b749e35579856c73c840867b Windows 7? timesbi.ttf */
		430<<42 | 2874<<21 | 40662,
		/* sha1sum:19fc45110ea6cd3cdd0a5faca256a3797a069a80 Windows 7 timesi.ttf */
		442<<42 | 2874<<21 | 39116,
		/* sha1sum:6d2d3c9ed5b7de87bc84eae0df95ee5232ecde26 Windows 7 timesbi.ttf */
		430<<42 | 2874<<21 | 39374,
		/* sha1sum:8583225a8b49667c077b3525333f84af08c6bcd8 OS X 10.11.3 Times New Roman Italic.ttf */
		490<<42 | 3046<<21 | 41638,
		/* sha1sum:ec0f5a8751845355b7c3271d11f9918a966cb8c9 OS X 10.11.3 Times New Roman Bold Italic.ttf */
		478<<42 | 3046<<21 | 41902,
		/* sha1sum:96eda93f7d33e79962451c6c39a6b51ee893ce8c  tahoma.ttf from Windows 8 */
		898<<42 | 12554<<21 | 46470,
		/* sha1sum:20928dc06014e0cd120b6fc942d0c3b1a46ac2bc  tahomabd.ttf from Windows 8 */
		910<<42 | 12566<<21 | 47732,
		/* sha1sum:4f95b7e4878f60fa3a39ca269618dfde9721a79e  tahoma.ttf from Windows 8.1 */
		928<<42 | 23298<<21 | 59332,
		/* sha1sum:6d400781948517c3c0441ba42acb309584b73033  tahomabd.ttf from Windows 8.1 */
		940<<42 | 23310<<21 | 60732,
		/* tahoma.ttf v6.04 from Windows 8.1 x64, see https://bugzilla.mozilla.org/show_bug.cgi?id=1279925 */
		964<<42 | 23836<<21 | 60072,
		/* tahomabd.ttf v6.04 from Windows 8.1 x64, see https://bugzilla.mozilla.org/show_bug.cgi?id=1279925 */
		976<<42 | 23832<<21 | 61456,
		/* sha1sum:e55fa2dfe957a9f7ec26be516a0e30b0c925f846  tahoma.ttf from Windows 10 */
		994<<42 | 24474<<21 | 60336,
		/* sha1sum:7199385abb4c2cc81c83a151a7599b6368e92343  tahomabd.ttf from Windows 10 */
		1006<<42 | 24470<<21 | 61740,
		/* tahoma.ttf v6.91 from Windows 10 x64, see https://bugzilla.mozilla.org/show_bug.cgi?id=1279925 */
		1006<<42 | 24576<<21 | 61346,
		/* tahomabd.ttf v6.91 from Windows 10 x64, see https://bugzilla.mozilla.org/show_bug.cgi?id=1279925 */
		1018<<42 | 24572<<21 | 62828,
		/* sha1sum:b9c84d820c49850d3d27ec498be93955b82772b5  tahoma.ttf from Windows 10 AU */
		1006<<42 | 24576<<21 | 61352,
		/* sha1sum:2bdfaab28174bdadd2f3d4200a30a7ae31db79d2  tahomabd.ttf from Windows 10 AU */
		1018<<42 | 24572<<21 | 62834,
		/* sha1sum:b0d36cf5a2fbe746a3dd277bffc6756a820807a7  Tahoma.ttf from Mac OS X 10.9 */
		832<<42 | 7324<<21 | 47162,
		/* sha1sum:12fc4538e84d461771b30c18b5eb6bd434e30fba  Tahoma Bold.ttf from Mac OS X 10.9 */
		844<<42 | 7302<<21 | 45474,
		/* sha1sum:eb8afadd28e9cf963e886b23a30b44ab4fd83acc  himalaya.ttf from Windows 7 */
		180<<42 | 13054<<21 | 7254,
		/* sha1sum:73da7f025b238a3f737aa1fde22577a6370f77b0  himalaya.ttf from Windows 8 */
		192<<42 | 12638<<21 | 7254,
		/* sha1sum:6e80fd1c0b059bbee49272401583160dc1e6a427  himalaya.ttf from Windows 8.1 */
		192<<42 | 12690<<21 | 7254,
		/* 8d9267aea9cd2c852ecfb9f12a6e834bfaeafe44  cantarell-fonts-0.0.21/otf/Cantarell-Regular.otf */
		/* 983988ff7b47439ab79aeaf9a45bd4a2c5b9d371  cantarell-fonts-0.0.21/otf/Cantarell-Oblique.otf */
		188<<42 | 248<<21 | 3852,
		/* 2c0c90c6f6087ffbfea76589c93113a9cbb0e75f  cantarell-fonts-0.0.21/otf/Cantarell-Bold.otf */
		/* 55461f5b853c6da88069ffcdf7f4dd3f8d7e3e6b  cantarell-fonts-0.0.21/otf/Cantarell-Bold-Oblique.otf */
		188<<42 | 264<<21 | 3426,
		/* d125afa82a77a6475ac0e74e7c207914af84b37a padauk-2.80/Padauk.ttf RHEL 7.2 */
		1058<<42 | 47032<<21 | 11818,
		/* 0f7b80437227b90a577cc078c0216160ae61b031 padauk-2.80/Padauk-Bold.ttf RHEL 7.2*/
		1046<<42 | 47030<<21 | 12600,
		/* d3dde9aa0a6b7f8f6a89ef1002e9aaa11b882290 padauk-2.80/Padauk.ttf Ubuntu 16.04 */
		1058<<42 | 71796<<21 | 16770,
		/* 5f3c98ccccae8a953be2d122c1b3a77fd805093f padauk-2.80/Padauk-Bold.ttf Ubuntu 16.04 */
		1046<<42 | 71790<<21 | 17862,
		/* 6c93b63b64e8b2c93f5e824e78caca555dc887c7 padauk-2.80/Padauk-book.ttf */
		1046<<42 | 71788<<21 | 17112,
		/* d89b1664058359b8ec82e35d3531931125991fb9 padauk-2.80/Padauk-bookbold.ttf */
		1058<<42 | 71794<<21 | 17514,
		/* 824cfd193aaf6234b2b4dc0cf3c6ef576c0d00ef padauk-3.0/Padauk-book.ttf */
		1330<<42 | 109904<<21 | 57938,
		/* 91fcc10cf15e012d27571e075b3b4dfe31754a8a padauk-3.0/Padauk-bookbold.ttf */
		1330<<42 | 109904<<21 | 58972,
		/* sha1sum: c26e41d567ed821bed997e937bc0c41435689e85  Padauk.ttf
		 *  "Padauk Regular" "Version 2.5", see https://crbug.com/681813 */
		1004<<42 | 59092<<21 | 14836,
		/* 88d2006ca084f04af2df1954ed714a8c71e8400f  Courier New.ttf from macOS 15 */
		588<<42 | 5078<<21 | 14418,
		/* 608e3ebb6dd1aee521cff08eb07d500a2c59df68  Courier New Bold.ttf from macOS 15 */
		588<<42 | 5078<<21 | 14238,
		/* d13221044ff054efd78f1cd8631b853c3ce85676  cour.ttf from Windows 10 */
		894<<42 | 17162<<21 | 33960,
		/* 68ed4a22d8067fcf1622ac6f6e2f4d3a2e3ec394  courbd.ttf from Windows 10 */
		894<<42 | 17154<<21 | 34472,
		/* 4cdb0259c96b7fd7c103821bb8f08f7cc6b211d7  cour.ttf from Windows 8.1 */
		816<<42 | 7868<<21 | 17052,
		/* 920483d8a8ed37f7f0afdabbe7f679aece7c75d8  courbd.ttf from Windows 8.1 */
		816<<42 | 7868<<21 | 17138:
		return true
	}
	return false
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

func loadGDEF(ld *ot.Loader, axisCount int, gsub, gpos []byte) (tables.GDEF, error) {
	raw, err := ld.RawTable(ot.MustNewTag("GDEF"))
	if err != nil {
		return tables.GDEF{}, err
	}

	// Nuke the GDEF tables of to avoid unwanted width-zeroing.
	if isGDEFBlocklisted(raw, gsub, gpos) {
		return tables.GDEF{}, nil
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
// A Face caches glyph extents and rune to glyph mapping, and should be reused when possible.
//
// Also note that an empty [Face] is invalid : the [NewFace] constructor is required to properly init caches.
type Face struct {
	*Font

	extentsCache extentsCache
	cmapCache    cache21_19_8

	coords       []tables.Coord
	xPpem, yPpem uint16
}

// NewFace wraps [font] and initializes glyph caches.
func NewFace(font *Font) *Face {
	out := &Face{Font: font, extentsCache: make(extentsCache, font.nGlyphs)}
	out.cmapCache.clear()
	return out
}

// NominalGlyph returns the glyph used to represent the given rune,
// or false if not found.
// Note that it only looks into the cmap, without taking account substitutions
// nor variation selectors.
func (f *Face) NominalGlyph(ch rune) (GID, bool) {
	if g, ok := f.cmapCache.get(uint32(ch)); ok {
		return GID(g), ok
	}
	g, ok := f.Cmap.Lookup(ch)
	if ok {
		f.cmapCache.set(uint32(ch), uint32(g))
	}
	return g, ok
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
