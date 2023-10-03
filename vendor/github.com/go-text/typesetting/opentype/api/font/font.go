// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package font

import (
	"errors"
	"fmt"

	"github.com/go-text/typesetting/opentype/api"
	"github.com/go-text/typesetting/opentype/api/font/cff"
	"github.com/go-text/typesetting/opentype/loader"
	"github.com/go-text/typesetting/opentype/tables"
)

type (
	GID = api.GID
	Tag = loader.Tag
)

// Font represents one Opentype font file (or one sub font of a collection).
// It is an educated view of the underlying font file, optimized for quick access
// to information required by text layout engines.
//
// All its methods are read-only and a [*Font] object is thus safe for concurrent use.
type Font struct {
	// Cmap is the 'cmap' table
	Cmap    api.Cmap
	cmapVar api.UnicodeVariations

	hhea *tables.Hhea
	vhea *tables.Vhea
	vorg *tables.VORG // optional
	cff  *cff.Font
	post post // optional
	svg  svg  // optional

	// Optional, only present in variable fonts

	fvar fvar         // optional
	hvar *tables.HVAR // optional
	vvar *tables.VVAR // optional
	avar tables.Avar
	mvar mvar
	gvar gvar

	glyf   tables.Glyf
	hmtx   tables.Hmtx
	vmtx   tables.Vmtx
	bitmap bitmap
	sbix   sbix

	os2 os2

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

	head tables.Head

	upem uint16 // cached value
}

// NewFont loads all the font tables, sanitizing them.
// An error is returned only when required tables 'cmap', 'head', 'maxp' are invalid (or missing).
// More control on errors is available by using package [tables].
func NewFont(ld *loader.Loader) (*Font, error) {
	var (
		out Font
		err error
	)

	// 'cmap' handling depend on os2
	raw, _ := ld.RawTable(loader.MustNewTag("OS/2"))
	os2, _, _ := tables.ParseOs2(raw)
	fontPage := os2.FontPage()
	out.os2, _ = newOs2(os2)

	raw, err = ld.RawTable(loader.MustNewTag("cmap"))
	if err != nil {
		return nil, err
	}
	tb, _, err := tables.ParseCmap(raw)
	if err != nil {
		return nil, err
	}
	out.Cmap, out.cmapVar, err = api.ProcessCmap(tb, fontPage)
	if err != nil {
		return nil, err
	}

	out.head, _, err = LoadHeadTable(ld, nil)
	if err != nil {
		return nil, err
	}

	raw, err = ld.RawTable(loader.MustNewTag("maxp"))
	if err != nil {
		return nil, err
	}
	maxp, _, err := tables.ParseMaxp(raw)
	if err != nil {
		return nil, err
	}

	// We considerer all the following tables as optional,
	// since, in practice, users won't have much control on the
	// font files they use
	//
	// Ignoring the errors on `RawTable` is OK : it will trigger an error on the next tables.ParseXXX,
	// which in turn will return a zero value

	raw, _ = ld.RawTable(loader.MustNewTag("fvar"))
	fvar, _, _ := tables.ParseFvar(raw)
	out.fvar = newFvar(fvar)

	raw, _ = ld.RawTable(loader.MustNewTag("avar"))
	out.avar, _, _ = tables.ParseAvar(raw)

	out.upem = out.head.Upem()

	raw, _ = ld.RawTable(loader.MustNewTag("glyf"))
	locaRaw, _ := ld.RawTable(loader.MustNewTag("loca"))
	loca, err := tables.ParseLoca(locaRaw, int(maxp.NumGlyphs), out.head.IndexToLocFormat == 1)
	if err == nil { // ParseGlyf panics if len(loca) == 0
		out.glyf, _ = tables.ParseGlyf(raw, loca)
	}

	out.bitmap = selectBitmapTable(ld)

	raw, _ = ld.RawTable(loader.MustNewTag("sbix"))
	sbix, _, _ := tables.ParseSbix(raw, int(maxp.NumGlyphs))
	out.sbix = newSbix(sbix)

	out.cff, _ = loadCff(ld, int(maxp.NumGlyphs))

	raw, _ = ld.RawTable(loader.MustNewTag("post"))
	post, _, _ := tables.ParsePost(raw)
	out.post, _ = newPost(post)

	raw, _ = ld.RawTable(loader.MustNewTag("SVG "))
	svg, _, _ := tables.ParseSVG(raw)
	out.svg, _ = newSvg(svg)

	out.hhea, out.hmtx, _ = LoadHmtx(ld, int(maxp.NumGlyphs))
	out.vhea, out.vmtx, _ = loadVmtx(ld, int(maxp.NumGlyphs))

	if len(out.fvar) != 0 {
		raw, _ = ld.RawTable(loader.MustNewTag("MVAR"))
		mvar, _, _ := tables.ParseMVAR(raw)
		out.mvar = newMvar(mvar)

		raw, _ = ld.RawTable(loader.MustNewTag("gvar"))
		gvar, _, _ := tables.ParseGvar(raw)
		out.gvar, _ = newGvar(gvar, out.glyf)

		raw, _ = ld.RawTable(loader.MustNewTag("HVAR"))
		hvar, _, err := tables.ParseHVAR(raw)
		if err == nil {
			out.hvar = &hvar
		}

		raw, _ = ld.RawTable(loader.MustNewTag("VVAR"))
		vvar, _, err := tables.ParseHVAR(raw)
		if err == nil {
			out.vvar = &vvar
		}
	}

	raw, _ = ld.RawTable(loader.MustNewTag("VORG"))
	vorg, _, err := tables.ParseVORG(raw)
	if err == nil {
		out.vorg = &vorg
	}

	// layout tables
	out.GDEF, _ = loadGDEF(ld, len(out.fvar))

	raw, _ = ld.RawTable(loader.MustNewTag("GSUB"))
	layout, _, err := tables.ParseLayout(raw)
	// harfbuzz relies on GSUB.Loookups being nil when the table is absent
	if err == nil {
		out.GSUB, _ = newGSUB(layout)
	}

	raw, _ = ld.RawTable(loader.MustNewTag("GPOS"))
	layout, _, err = tables.ParseLayout(raw)
	// harfbuzz relies on GPOS.Loookups being nil when the table is absent
	if err == nil {
		out.GPOS, _ = newGPOS(layout)
	}

	raw, _ = ld.RawTable(loader.MustNewTag("morx"))
	morx, _, _ := tables.ParseMorx(raw, int(maxp.NumGlyphs))
	out.Morx = newMorx(morx)

	raw, _ = ld.RawTable(loader.MustNewTag("kerx"))
	kerx, _, _ := tables.ParseKerx(raw, int(maxp.NumGlyphs))
	out.Kerx = newKernxFromKerx(kerx)

	raw, _ = ld.RawTable(loader.MustNewTag("kern"))
	kern, _, _ := tables.ParseKern(raw)
	out.Kern = newKernxFromKern(kern)

	raw, _ = ld.RawTable(loader.MustNewTag("ankr"))
	out.Ankr, _, _ = tables.ParseAnkr(raw, int(maxp.NumGlyphs))

	raw, _ = ld.RawTable(loader.MustNewTag("trak"))
	out.Trak, _, _ = tables.ParseTrak(raw)

	raw, _ = ld.RawTable(loader.MustNewTag("feat"))
	out.Feat, _, _ = tables.ParseFeat(raw)

	raw, _ = ld.RawTable(loader.MustNewTag("ltag"))
	out.Ltag, _, _ = tables.ParseLtag(raw)

	return &out, nil
}

var bhedTag = loader.MustNewTag("bhed")

// LoadHeadTable loads the table corresponding to the 'head' tag.
// If a 'bhed' Apple table is present, it replaces the 'head' one.
//
// 'buffer' may be provided to reduce allocations; the return Head is guaranteed
// not to retain any reference on 'buffer'.
// If 'buffer' is nil or has not enough capacity, a new slice is allocated (and returned).
func LoadHeadTable(ld *loader.Loader, buffer []byte) (tables.Head, []byte, error) {
	var err error
	// check 'bhed' first
	if ld.HasTable(bhedTag) {
		buffer, err = ld.RawTableTo(bhedTag, buffer)
	} else {
		buffer, err = ld.RawTableTo(loader.MustNewTag("head"), buffer)
	}
	if err != nil {
		return tables.Head{}, nil, errors.New("missing required head (or bhed) table")
	}
	out, _, err := tables.ParseHead(buffer)
	return out, buffer, err
}

// return nil if no table is valid (or present)
func selectBitmapTable(ld *loader.Loader) bitmap {
	color, err := loadBitmap(ld, loader.MustNewTag("CBLC"), loader.MustNewTag("CBDT"))
	if err == nil {
		return color
	}

	gray, err := loadBitmap(ld, loader.MustNewTag("EBLC"), loader.MustNewTag("EBDT"))
	if err == nil {
		return gray
	}

	apple, err := loadBitmap(ld, loader.MustNewTag("bloc"), loader.MustNewTag("bdat"))
	if err == nil {
		return apple
	}

	return nil
}

// return nil if the table is missing or invalid
func loadCff(ld *loader.Loader, numGlyphs int) (*cff.Font, error) {
	raw, err := ld.RawTable(loader.MustNewTag("CFF "))
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

func LoadHmtx(ld *loader.Loader, numGlyphs int) (*tables.Hhea, tables.Hmtx, error) {
	rawHead, err := ld.RawTable(loader.MustNewTag("hhea"))
	if err != nil {
		return nil, tables.Hmtx{}, err
	}

	rawMetrics, err := ld.RawTable(loader.MustNewTag("hmtx"))
	if err != nil {
		return nil, tables.Hmtx{}, err
	}

	return loadHVtmx(rawHead, rawMetrics, numGlyphs)
}

func loadVmtx(ld *loader.Loader, numGlyphs int) (*tables.Hhea, tables.Hmtx, error) {
	rawHead, err := ld.RawTable(loader.MustNewTag("vhea"))
	if err != nil {
		return nil, tables.Hmtx{}, err
	}

	rawMetrics, err := ld.RawTable(loader.MustNewTag("vmtx"))
	if err != nil {
		return nil, tables.Hmtx{}, err
	}

	return loadHVtmx(rawHead, rawMetrics, numGlyphs)
}

func loadGDEF(ld *loader.Loader, axisCount int) (tables.GDEF, error) {
	raw, err := ld.RawTable(loader.MustNewTag("GDEF"))
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
// It is a lightweight wrapper around [*Font], NOT safe for concurrent use.
type Face struct {
	*Font

	// Coords are the current variable coordinates, expressed in normalized units.
	// It is empty for non variable fonts.
	// Use `SetVariations` to convert from design (user) space units.
	Coords []float32

	// Horizontal and vertical pixels-per-em (ppem), used to select bitmap sizes.
	XPpem, YPpem uint16
}
