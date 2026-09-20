// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package font

import (
	"math"

	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
)

type gID = tables.GlyphID

func (f *Font) GetGlyphContourPoint(glyph GID, pointIndex uint16) (x, y int32, ok bool) {
	// harfbuzz seems not to implement this feature
	return 0, 0, false
}

// GlyphName returns the name of the given glyph, or an empty
// string if the glyph is invalid or has no name.
func (f *Font) GlyphName(glyph GID) string {
	if postNames := f.post.names; postNames != nil {
		if name := postNames.glyphName(glyph); name != "" {
			return name
		}
	}
	if f.cff != nil {
		return f.cff.GlyphName(glyph)
	}
	return ""
}

// Upem returns the units per em of the font file.
// This value is only relevant for scalable fonts.
func (f *Font) Upem() uint16 { return f.upem }

var (
	metricsTagHorizontalAscender  = ot.MustNewTag("hasc")
	metricsTagHorizontalDescender = ot.MustNewTag("hdsc")
	metricsTagHorizontalLineGap   = ot.MustNewTag("hlgp")
	metricsTagVerticalAscender    = ot.MustNewTag("vasc")
	metricsTagVerticalDescender   = ot.MustNewTag("vdsc")
	metricsTagVerticalLineGap     = ot.MustNewTag("vlgp")
)

func fixAscenderDescender(value float32, metricsTag Tag) float32 {
	if metricsTag == metricsTagHorizontalAscender || metricsTag == metricsTagVerticalAscender {
		return float32(math.Abs(float64(value)))
	}
	if metricsTag == metricsTagHorizontalDescender || metricsTag == metricsTagVerticalDescender {
		return float32(-math.Abs(float64(value)))
	}
	return value
}

func (f *Font) getPositionCommon(metricTag Tag, varCoords []VarCoord) (float32, bool) {
	deltaVar := f.mvar.getVar(metricTag, varCoords)
	switch metricTag {
	case metricsTagHorizontalAscender:
		if f.os2.useTypoMetrics {
			return fixAscenderDescender(float32(f.os2.sTypoAscender)+deltaVar, metricTag), true
		} else if f.hhea != nil {
			return fixAscenderDescender(float32(f.hhea.Ascender)+deltaVar, metricTag), true
		}

	case metricsTagHorizontalDescender:
		if f.os2.useTypoMetrics {
			return fixAscenderDescender(float32(f.os2.sTypoDescender)+deltaVar, metricTag), true
		} else if f.hhea != nil {
			return fixAscenderDescender(float32(f.hhea.Descender)+deltaVar, metricTag), true
		}
	case metricsTagHorizontalLineGap:
		if f.os2.useTypoMetrics {
			return fixAscenderDescender(float32(f.os2.sTypoLineGap)+deltaVar, metricTag), true
		} else if f.hhea != nil {
			return fixAscenderDescender(float32(f.hhea.LineGap)+deltaVar, metricTag), true
		}
	case metricsTagVerticalAscender:
		if f.vhea != nil {
			return fixAscenderDescender(float32(f.vhea.Ascender)+deltaVar, metricTag), true
		}
	case metricsTagVerticalDescender:
		if f.vhea != nil {
			return fixAscenderDescender(float32(f.vhea.Descender)+deltaVar, metricTag), true
		}
	case metricsTagVerticalLineGap:
		if f.vhea != nil {
			return fixAscenderDescender(float32(f.vhea.LineGap)+deltaVar, metricTag), true
		}
	}
	return 0, false
}

// FontHExtents returns the extents of the font for horizontal text, or false
// it not available, in font units.
func (f *Face) FontHExtents() (FontExtents, bool) {
	var (
		out           FontExtents
		ok1, ok2, ok3 bool
	)
	out.Ascender, ok1 = f.Font.getPositionCommon(metricsTagHorizontalAscender, f.coords)
	out.Descender, ok2 = f.Font.getPositionCommon(metricsTagHorizontalDescender, f.coords)
	out.LineGap, ok3 = f.Font.getPositionCommon(metricsTagHorizontalLineGap, f.coords)
	return out, ok1 && ok2 && ok3
}

// FontVExtents is the same as `FontHExtents`, but for vertical text.
func (f *Face) FontVExtents() (FontExtents, bool) {
	var (
		out           FontExtents
		ok1, ok2, ok3 bool
	)
	out.Ascender, ok1 = f.Font.getPositionCommon(metricsTagVerticalAscender, f.coords)
	out.Descender, ok2 = f.Font.getPositionCommon(metricsTagVerticalDescender, f.coords)
	out.LineGap, ok3 = f.Font.getPositionCommon(metricsTagVerticalLineGap, f.coords)
	return out, ok1 && ok2 && ok3
}

var (
	tagStrikeoutSize      = ot.MustNewTag("strs")
	tagStrikeoutOffset    = ot.MustNewTag("stro")
	tagUnderlineSize      = ot.MustNewTag("unds")
	tagUnderlineOffset    = ot.MustNewTag("undo")
	tagSuperscriptYSize   = ot.MustNewTag("spys")
	tagSuperscriptXOffset = ot.MustNewTag("spxo")
	tagSubscriptYSize     = ot.MustNewTag("sbys")
	tagSubscriptYOffset   = ot.MustNewTag("sbyo")
	tagSubscriptXOffset   = ot.MustNewTag("sbxo")
	tagXHeight            = ot.MustNewTag("xhgt")
	tagCapHeight          = ot.MustNewTag("cpht")
)

// return the height from baseline (in font units)
func (f *Face) runeHeight(r rune) float32 {
	gid, ok := f.NominalGlyph(r)
	if !ok {
		return 0
	}
	extents, ok := f.GlyphExtents(gid)
	if !ok {
		return 0
	}
	return extents.YBearing
}

// LineMetric returns the metric identified by `metric` (in fonts units).
func (f *Face) LineMetric(metric LineMetric) float32 {
	switch metric {
	case UnderlinePosition:
		return f.post.underlinePosition + f.mvar.getVar(tagUnderlineOffset, f.coords)
	case UnderlineThickness:
		return f.post.underlineThickness + f.mvar.getVar(tagUnderlineSize, f.coords)
	case StrikethroughPosition:
		return float32(f.os2.yStrikeoutPosition) + f.mvar.getVar(tagStrikeoutOffset, f.coords)
	case StrikethroughThickness:
		return float32(f.os2.yStrikeoutSize) + f.mvar.getVar(tagStrikeoutSize, f.coords)
	case SuperscriptEmYSize:
		return float32(f.os2.ySuperscriptYSize) + f.mvar.getVar(tagSuperscriptYSize, f.coords)
	case SuperscriptEmXOffset:
		return float32(f.os2.ySuperscriptXOffset) + f.mvar.getVar(tagSuperscriptXOffset, f.coords)
	case SubscriptEmYSize:
		return float32(f.os2.ySubscriptYSize) + f.mvar.getVar(tagSubscriptYSize, f.coords)
	case SubscriptEmYOffset:
		return float32(f.os2.ySubscriptYOffset) + f.mvar.getVar(tagSubscriptYOffset, f.coords)
	case SubscriptEmXOffset:
		return float32(f.os2.ySubscriptXOffset) + f.mvar.getVar(tagSubscriptXOffset, f.coords)
	case CapHeight:
		if f.os2.version < 2 {
			// sCapHeight may be set equal to the top of the unscaled and unhinted glyph
			// bounding box of the glyph encoded at U+0048 (LATIN CAPITAL LETTER H).
			return f.runeHeight('H')
		}
		return float32(f.os2.sCapHeight) + f.mvar.getVar(tagCapHeight, f.coords)
	case XHeight:
		if f.os2.version < 2 {
			// sxHeight equal to the top of the unscaled and unhinted glyph bounding box
			// of the glyph encoded at U+0078 (LATIN SMALL LETTER X).
			return f.runeHeight('x')
		}
		return float32(f.os2.sxHeigh) + f.mvar.getVar(tagXHeight, f.coords)
	default:
		return 0
	}
}

// NominalGlyph returns the glyph used to represent the given rune,
// or false if not found.
// Note that it only looks into the cmap, without taking account substitutions
// nor variation selectors.
func (f *Font) NominalGlyph(ch rune) (GID, bool) { return f.Cmap.Lookup(ch) }

// VariationGlyph retrieves the glyph ID for a specified Unicode code point
// followed by a specified Variation Selector code point, or false if not found
func (f *Font) VariationGlyph(ch, varSelector rune) (GID, bool) {
	gid, kind := f.cmapVar.GetGlyphVariant(ch, varSelector)
	switch kind {
	case VariantNotFound:
		return 0, false
	case VariantFound:
		return gid, true
	default: // VariantUseDefault
		return f.NominalGlyph(ch)
	}
}

// do not take into account variations
func (f *Font) getBaseAdvance(gid gID, table tables.Hmtx, isVertical bool) int16 {
	/* If `table` is empty, it means we don't have the metrics table
	 * for this direction: return default advance.  Otherwise, it means that the
	 * glyph index is out of bound: return zero. */
	if table.IsEmpty() {
		if isVertical {
			return int16(f.upem)
		}
		return int16(f.upem / 2)
	}

	return table.Advance(gid)
}

func clamp(v float32) float32 {
	if v < 0 {
		v = 0
	}
	return v
}

func (f *Face) getGlyphAdvanceVar(gid gID, isVertical bool) float32 {
	_, phantoms := f.getGlyfPoints(gid, false)
	if isVertical {
		return clamp(phantoms[phantomTop].Y - phantoms[phantomBottom].Y)
	}
	return clamp(phantoms[phantomRight].X - phantoms[phantomLeft].X)
}

func (f *Face) HorizontalAdvance(gid GID) float32 {
	advance := f.getBaseAdvance(gID(gid), f.hmtx, false)
	if !f.isVar() {
		return float32(advance)
	}
	if f.hvar != nil {
		return float32(advance) + f.hvar.AdvanceDelta(gID(gid), f.coords)
	}
	return f.getGlyphAdvanceVar(gID(gid), false)
}

// return `true` is the font is variable and `Coords` is valid
func (f *Face) isVar() bool {
	return len(f.coords) != 0 && len(f.coords) == len(f.Font.fvar)
}

// HasVerticalMetrics returns true if a the 'vmtx' table is present.
// If not, client should avoid calls to [VerticalAdvance], which will returns a
// defaut value.
func (f *Font) HasVerticalMetrics() bool { return !f.vmtx.IsEmpty() }

func (f *Face) VerticalAdvance(gid GID) float32 {
	// return the opposite of the advance from the font
	advance := f.getBaseAdvance(gID(gid), f.vmtx, true)
	if !f.isVar() {
		return -float32(advance)
	}
	if f.vvar != nil {
		return -float32(advance) - f.vvar.AdvanceDelta(gID(gid), f.coords)
	}
	return -f.getGlyphAdvanceVar(gID(gid), true)
}

func (f *Font) GlyphHOrigin(GID) (x, y int32, found bool) {
	// zero is the right value here
	return 0, 0, true
}

func (f *Face) GlyphVOrigin(glyph GID) (x, y float32) {
	// First, set the x value to half the advance width.
	x = f.HorizontalAdvance(glyph) / 2

	// If there is VORG, always use it. It uses VVAR for variations if necessary.
	if f.vorg != nil {
		y = float32(f.vorg.YOrigin(gID(glyph)))
		if f.isVar() && f.vvar != nil {
			y += f.vvar.VorgDelta(gID(glyph), f.coords)
		}
		return x, y
	}

	// If and only if `vmtx` is present and it's a `glyf` font,
	// we use the top phantom point, deduced from vmtx,glyf[,gvar].
	if !f.vmtx.IsEmpty() && f.glyf != nil {
		y = f.getVOriginWithVar(gID(glyph))
		return x, y
	}

	// Otherwise, use glyph extents to center the glyph vertically.
	// If getting glyph extents failed, just use the font ascender.
	fontExtents, _ := f.FontHExtents()
	fontAdvance := fontExtents.Ascender - fontExtents.Descender

	if extents, ok := f.getExtentsFromGlyf(gID(glyph)); ok {
		diff := fontAdvance - -extents.Height
		y = extents.YBearing + float32(int(diff)/2)
		return x, y
	}

	y = fontExtents.Ascender
	return x, y
}

func (f *Face) getVOriginWithVar(gid gID) float32 {
	if int(gid) >= f.nGlyphs {
		return 0
	}
	_, phantoms := f.getGlyfPoints(gid, false)
	return phantoms[phantomTop].Y
}

func (f *Face) getExtentsFromGlyf(glyph gID) (GlyphExtents, bool) {
	if int(glyph) >= len(f.glyf) {
		return GlyphExtents{}, false
	}
	if f.isVar() { // we have to compute the outline points and apply variations
		extents, _ := f.getGlyfPoints(glyph, true)
		return extents, true
	}
	return getGlyphExtents(f.glyf[glyph], f.hmtx, glyph), true
}

func (f *Font) getExtentsFromBitmap(glyph gID, xPpem, yPpem uint16) (GlyphExtents, bool) {
	strike := f.bitmap.chooseStrike(xPpem, yPpem)
	if strike == nil || strike.ppemX == 0 || strike.ppemY == 0 {
		return GlyphExtents{}, false
	}
	subtable := strike.findTable(glyph)
	if subtable == nil {
		return GlyphExtents{}, false
	}
	image := subtable.image(glyph)
	if image == nil {
		return GlyphExtents{}, false
	}
	extents := GlyphExtents{
		XBearing: float32(image.metrics.BearingX),
		YBearing: float32(image.metrics.BearingY),
		Width:    float32(image.metrics.Width),
		Height:   -float32(image.metrics.Height),
	}

	/* convert to font units. */
	xScale := float32(f.upem) / float32(strike.ppemX)
	yScale := float32(f.upem) / float32(strike.ppemY)
	extents.XBearing *= xScale
	extents.YBearing *= yScale
	extents.Width *= xScale
	extents.Height *= yScale
	return extents, true
}

func (f *Font) getExtentsFromSbix(glyph gID, xPpem, yPpem uint16) (GlyphExtents, bool) {
	strike := f.sbix.chooseStrike(xPpem, yPpem)
	if strike == nil || strike.Ppem == 0 {
		return GlyphExtents{}, false
	}
	data := strikeGlyph(strike, glyph, 0)
	if data.GraphicType == 0 {
		return GlyphExtents{}, false
	}
	extents, ok := bitmapGlyphExtents(data)

	/* convert to font units. */
	scale := float32(f.upem) / float32(strike.Ppem)
	extents.XBearing *= scale
	extents.YBearing *= scale
	extents.Width *= scale
	extents.Height *= scale
	return extents, ok
}

func (f *Font) getExtentsFromCff1(glyph gID) (GlyphExtents, bool) {
	if f.cff == nil {
		return GlyphExtents{}, false
	}
	_, bounds, err := f.cff.LoadGlyph(glyph)
	if err != nil {
		return GlyphExtents{}, false
	}
	return bounds.ToExtents(), true
}

func (f *Face) getExtentsFromCff2(glyph gID) (GlyphExtents, bool) {
	if f.cff2 == nil {
		return GlyphExtents{}, false
	}
	_, bounds, err := f.cff2.LoadGlyph(glyph, f.coords)
	if err != nil {
		return GlyphExtents{}, false
	}
	return bounds.ToExtents(), true
}

func (f *Face) glyphExtentsRaw(glyph GID) (GlyphExtents, bool) {
	out, ok := f.getExtentsFromSbix(gID(glyph), f.xPpem, f.yPpem)
	if ok {
		return out, ok
	}
	out, ok = f.getExtentsFromBitmap(gID(glyph), f.xPpem, f.yPpem)
	if ok {
		return out, ok
	}
	out, ok = f.getExtentsFromGlyf(gID(glyph))
	if ok {
		return out, ok
	}
	out, ok = f.getExtentsFromCff2(gID(glyph))
	if ok {
		return out, ok
	}
	out, ok = f.getExtentsFromCff1(gID(glyph))
	return out, ok
}
