package harfbuzz

import (
	ot "github.com/go-text/typesetting/font/opentype"
	"github.com/go-text/typesetting/font/opentype/tables"
	ucd "github.com/go-text/typesetting/internal/unicodedata"
	"github.com/go-text/typesetting/language"
)

type zeroWidthMarks uint8

const (
	zeroWidthMarksNone zeroWidthMarks = iota
	zeroWidthMarksByGdefEarly
	zeroWidthMarksByGdefLate
)

// implements the specialisation for a script
type otComplexShaper interface {
	marksBehavior() (zwm zeroWidthMarks, fallbackPosition bool)
	normalizationPreference() normalizationMode
	// If not 0, then must match found GPOS script tag for
	// GPOS to be applied. Otherwise, fallback positioning will be used.
	gposTag() tables.Tag

	// collectFeatures is alled during shape_plan().
	// Shapers should use plan.map to add their features and callbacks.
	collectFeatures(plan *otShapePlanner)

	// overrideFeatures is called during shape_plan().
	// Shapers should use plan.map to override features and add callbacks after
	// common features are added.
	overrideFeatures(plan *otShapePlanner)

	// dataCreate is called at the end of shape_plan().
	dataCreate(plan *otShapePlan)

	// called during shape(), shapers can use to modify text before shaping starts.
	preprocessText(plan *otShapePlan, buffer *Buffer, font *Font)

	// called during shape()'s normalization: may use decompose_unicode as fallback
	decompose(c *otNormalizeContext, ab rune) (a, b rune, ok bool)

	// called during shape()'s normalization: may use compose_unicode as fallback
	compose(c *otNormalizeContext, a, b rune) (ab rune, ok bool)

	// called during shape(), shapers should use map to get feature masks and set on buffer.
	// Shapers may NOT modify characters.
	setupMasks(plan *otShapePlan, buffer *Buffer, font *Font)

	// called during shape(), shapers can use to modify ordering of combining marks.
	reorderMarks(plan *otShapePlan, buffer *Buffer, start, end int)

	// called during shape(), shapers can use to modify glyphs after shaping ends.
	postprocessGlyphs(plan *otShapePlan, buffer *Buffer, font *Font)
}

/*
 * For lack of a better place, put Zawgyi script hack here.
 * https://github.com/harfbuzz/harfbuzz/issues/1162
 */
var scriptMyanmarZawgyi = language.Script(ot.NewTag('Q', 'a', 'a', 'g'))

// categorizeComplex choose the complex shaper implementation
func categorizeComplex(script language.Script, direction Direction, gsubScript ot.Tag) otComplexShaper {
	switch script {
	case language.Arabic, language.Syriac:
		/* For Arabic script, use the Arabic shaper even if no OT script tag was found.
		 * This is because we do fallback shaping for Arabic script (and not others).
		 * But note that Arabic shaping is applicable only to horizontal layout; for
		 * vertical text, just use the generic shaper instead. */
		if (gsubScript != tagDefaultScript || script == language.Arabic) &&
			direction.isHorizontal() {
			return &complexShaperArabic{}
		}
		return complexShaperDefault{}
	case language.Thai, language.Lao:
		return complexShaperThai{}
	case language.Hangul:
		return &complexShaperHangul{}
	case language.Hebrew:
		return complexShaperHebrew{}
	case language.Bengali, language.Devanagari, language.Gujarati, language.Gurmukhi, language.Kannada,
		language.Malayalam, language.Oriya, language.Tamil, language.Telugu:
		/* If the designer designed the font for the 'DFLT' script,
		 * (or we ended up arbitrarily pick 'latn'), use the default shaper.
		 * Otherwise, use the specific shaper.
		 *
		 * If it's indy3 tag, send to USE. */
		if gsubScript == ot.NewTag('D', 'F', 'L', 'T') ||
			gsubScript == ot.NewTag('l', 'a', 't', 'n') {
			return complexShaperDefault{}
		} else if (gsubScript & 0x000000FF) == '3' {
			return &complexShaperUSE{}
		}
		return &complexShaperIndic{}
	case language.Khmer:
		return &complexShaperKhmer{}
	case language.Myanmar:
		/* If the designer designed the font for the 'DFLT' script,
		 * (or we ended up arbitrarily pick 'latn'), use the default shaper.
		 * Otherwise, use the specific shaper.
		 *
		 * If designer designed for 'mymr' tag, also send to default
		 * shaper.  That's tag used from before Myanmar shaping spec
		 * was developed.  The shaping spec uses 'mym2' tag. */
		if gsubScript == ot.NewTag('D', 'F', 'L', 'T') ||
			gsubScript == ot.NewTag('l', 'a', 't', 'n') ||
			gsubScript == ot.NewTag('m', 'y', 'm', 'r') {
			return complexShaperDefault{}
		}
		return complexShaperMyanmar{}

	case scriptMyanmarZawgyi:
		/* Ugly Zawgyi encoding.
		 * Disable all auto processing.
		 * https://github.com/harfbuzz/harfbuzz/issues/1162 */
		return complexShaperDefault{dumb: true, disableNorm: true}
	case language.Tibetan, /* Unicode-2.0 additions */
		/* Unicode-3.0 additions */
		language.Mongolian, language.Sinhala,
		/* Unicode-3.2 additions */
		language.Buhid, language.Hanunoo, language.Tagalog, language.Tagbanwa,
		/* Unicode-4.0 additions */
		language.Limbu, language.Tai_Le,
		/* Unicode-4.1 additions */
		language.Buginese, language.Kharoshthi, language.Syloti_Nagri, language.Tifinagh,
		/* Unicode-5.0 additions */
		language.Balinese, language.Nko, language.Phags_Pa,
		/* Unicode-5.1 additions */
		language.Cham, language.Kayah_Li, language.Lepcha, language.Rejang, language.Saurashtra, language.Sundanese,
		/* Unicode-5.2 additions */
		language.Egyptian_Hieroglyphs, language.Javanese, language.Kaithi, language.Meetei_Mayek, language.Tai_Tham, language.Tai_Viet,
		/* Unicode-6.0 additions */
		language.Batak, language.Brahmi, language.Mandaic,
		/* Unicode-6.1 additions */
		language.Chakma, language.Miao, language.Sharada, language.Takri,
		/* Unicode-7.0 additions */
		language.Duployan, language.Grantha, language.Khojki, language.Khudawadi,
		language.Mahajani, language.Manichaean, language.Modi, language.Pahawh_Hmong,
		language.Psalter_Pahlavi, language.Siddham, language.Tirhuta,
		/* Unicode-8.0 additions */
		language.Ahom, language.Multani,
		/* Unicode-9.0 additions */
		language.Adlam, language.Bhaiksuki, language.Marchen, language.Newa,
		/* Unicode-10.0 additions */
		language.Masaram_Gondi, language.Soyombo, language.Zanabazar_Square,
		/* Unicode-11.0 additions */
		language.Dogra, language.Gunjala_Gondi, language.Hanifi_Rohingya, language.Makasar, language.Medefaidrin, language.Old_Sogdian,
		/* Unicode-12.0 additions */
		language.Sogdian, language.Elymaic, language.Nandinagari, language.Nyiakeng_Puachue_Hmong, language.Wancho,
		/* Unicode-13.0 additions */
		language.Chorasmian, language.Dives_Akuru, language.Khitan_Small_Script, language.Yezidi,
		/* Unicode-14.0 additions */
		language.Cypro_Minoan, language.Old_Uyghur, language.Tangsa, language.Toto, language.Vithkuqi,
		/* Unicode-15.0 additions */
		language.Kawi, language.Nag_Mundari,
		/* Unicode-16.0 additions */
		language.Garay, language.Gurung_Khema, language.Kirat_Rai, language.Ol_Onal, language.Sunuwar, language.Todhri, language.Tulu_Tigalari,
		/* Unicode-17.0 additions */
		language.Beria_Erfe, language.Sidetic, language.Tai_Yo, language.Tolong_Siki:

		/* If the designer designed the font for the 'DFLT' script,
		 * (or we ended up arbitrarily pick 'latn'), use the default shaper.
		 * Otherwise, use the specific shaper.
		 * Note that for some simple scripts, there may not be *any*
		 * GSUB/GPOS needed, so there may be no scripts found! */
		if gsubScript == ot.NewTag('D', 'F', 'L', 'T') ||
			gsubScript == ot.NewTag('l', 'a', 't', 'n') {
			return complexShaperDefault{}
		}
		return &complexShaperUSE{}
	default:
		return complexShaperDefault{}
	}
}

// zero byte struct providing no-ops, used to reduced boilerplate
type complexShaperNil struct{}

func (complexShaperNil) gposTag() tables.Tag { return 0 }

func (complexShaperNil) collectFeatures(plan *otShapePlanner)  {}
func (complexShaperNil) overrideFeatures(plan *otShapePlanner) {}
func (complexShaperNil) dataCreate(plan *otShapePlan)          {}
func (complexShaperNil) decompose(_ *otNormalizeContext, ab rune) (a, b rune, ok bool) {
	return ucd.Decompose(ab)
}

func (complexShaperNil) compose(_ *otNormalizeContext, a, b rune) (ab rune, ok bool) {
	return ucd.Compose(a, b)
}
func (complexShaperNil) preprocessText(*otShapePlan, *Buffer, *Font) {}
func (complexShaperNil) postprocessGlyphs(*otShapePlan, *Buffer, *Font) {
}
func (complexShaperNil) setupMasks(*otShapePlan, *Buffer, *Font)      {}
func (complexShaperNil) reorderMarks(*otShapePlan, *Buffer, int, int) {}

type complexShaperDefault struct {
	complexShaperNil

	/* if true, no mark advance zeroing / fallback positioning.
	 * Dumbest shaper ever, basically. */
	dumb        bool
	disableNorm bool
}

func (cs complexShaperDefault) marksBehavior() (zeroWidthMarks, bool) {
	if cs.dumb {
		return zeroWidthMarksNone, false
	}
	return zeroWidthMarksByGdefLate, true
}

func (cs complexShaperDefault) normalizationPreference() normalizationMode {
	if cs.disableNorm {
		return nmNone
	}
	return nmDefault
}

func syllabicInsertDottedCircles(font *Font, buffer *Buffer, brokenSyllableType,
	dottedcircleCategory uint8, rephaCategory, dottedCirclePosition int,
) bool {
	if (buffer.Flags & DoNotinsertDottedCircle) != 0 {
		return false
	}

	if (buffer.scratchFlags & bsfHasBrokenSyllable) == 0 {
		return false
	}

	dottedcircleGlyph, ok := font.face.NominalGlyph(0x25CC)
	if !ok {
		return false
	}

	dottedcircle := GlyphInfo{
		Glyph:           dottedcircleGlyph,
		complexCategory: dottedcircleCategory,
	}

	if dottedCirclePosition != -1 {
		dottedcircle.complexAux = uint8(dottedCirclePosition)
	}

	buffer.clearOutput()

	buffer.idx = 0
	var lastSyllable uint8
	for buffer.idx < len(buffer.Info) {
		syllable := buffer.cur(0).syllable
		if lastSyllable != syllable && (syllable&0x0F) == brokenSyllableType {
			lastSyllable = syllable

			ginfo := dottedcircle
			ginfo.Cluster = buffer.cur(0).Cluster
			ginfo.Mask = buffer.cur(0).Mask
			ginfo.syllable = buffer.cur(0).syllable

			/* Insert dottedcircle after possible Repha. */
			if rephaCategory != -1 {
				for buffer.idx < len(buffer.Info) &&
					lastSyllable == buffer.cur(0).syllable &&
					buffer.cur(0).complexCategory == uint8(rephaCategory) {
					buffer.nextGlyph()
				}
			}
			buffer.outInfo = append(buffer.outInfo, ginfo)
		} else {
			buffer.nextGlyph()
		}
	}
	buffer.swapBuffers()
	return true
}
