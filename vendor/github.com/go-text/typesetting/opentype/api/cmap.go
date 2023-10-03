// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package api

import (
	"encoding/binary"
	"errors"

	"github.com/go-text/typesetting/opentype/tables"
)

// This file implements the logic needed to use a cmap.

var (
	_ Cmap = cmap0(nil)
	_ Cmap = cmap4(nil)
	_ Cmap = (*cmap6or10)(nil)
	_ Cmap = cmap12(nil)
	_ Cmap = cmap13(nil)

	_ CmapIter = (*cmap0Iter)(nil)
	_ CmapIter = (*cmap4Iter)(nil)
	_ CmapIter = (*cmap6Or10Iter)(nil)
	_ CmapIter = (*cmap12Iter)(nil)
	_ CmapIter = (*cmap13Iter)(nil)
)

// cmapID groups the platform and encoding of a Cmap subtable.
type cmapID struct {
	platform tables.PlatformID
	encoding tables.EncodingID
}

func (c cmapID) key() uint32 { return uint32(c.platform)<<16 | uint32(c.encoding) }

// ProcessCmap sanitize the given 'cmap' subtable, and select the best encoding
// when several subtables are given.
// When present, the variation selectors are returned.
// [os2FontPage] is used for legacy arabic fonts.
//
// The returned values are copied from the input 'cmap', meaning they do not
// retain any reference on the input storage.
func ProcessCmap(cmap tables.Cmap, os2FontPage tables.FontPage) (Cmap, UnicodeVariations, error) {
	var (
		candidateIds []cmapID
		candidates   []Cmap
		uv           UnicodeVariations
	)
	for _, table := range cmap.Records {
		id := cmapID{platform: table.PlatformID, encoding: table.EncodingID}
		switch table := table.Subtable.(type) {
		case tables.CmapSubtable0:
			candidates = append(candidates, newCmap0(table))
			candidateIds = append(candidateIds, id)
		case tables.CmapSubtable2:
			// we dont support this deprecated format
			continue
		case tables.CmapSubtable4:
			cmap, err := newCmap4(table)
			if err != nil {
				return nil, nil, err
			}
			candidates = append(candidates, cmap)
			candidateIds = append(candidateIds, id)
		case tables.CmapSubtable6:
			candidates = append(candidates, newCmap6(table))
			candidateIds = append(candidateIds, id)
		case tables.CmapSubtable10:
			candidates = append(candidates, newCmap10(table))
			candidateIds = append(candidateIds, id)
		case tables.CmapSubtable12:
			candidates = append(candidates, newCmap12(table))
			candidateIds = append(candidateIds, id)
		case tables.CmapSubtable13:
			candidates = append(candidates, newCmap13(table))
			candidateIds = append(candidateIds, id)
		case tables.CmapSubtable14:
			// quoting the spec :
			// This subtable format must only be used under platform ID 0 and encoding ID 5.
			if !(id.platform == 0 && id.encoding == 5) {
				return nil, nil, errors.New("invalid cmap subtable format 14 platform or encoding")
			}
			uv = newUnicodeVariations(table)
		}
	}

	// now find the best cmap, following harfbuzz/src/hb-ot-cmap-table.hh

	// Prefer symbol if available.
	if index := findSubtable(cmapID{tables.PlatformMicrosoft, tables.PEMicrosoftSymbolCs}, candidateIds); index != -1 {
		cm := candidates[index]
		switch os2FontPage {
		case tables.FPNone:
			cm = remaperSymbol{cm}
		case tables.FPSimpArabic:
			cm = remaperPUASimp{cm}
		case tables.FPTradArabic:
			cm = remaperPUATrad{cm}
		}
		return cm, uv, nil
	}

	/* 32-bit subtables. */
	if index := findSubtable(cmapID{tables.PlatformMicrosoft, tables.PEMicrosoftUcs4}, candidateIds); index != -1 {
		return candidates[index], uv, nil
	}
	if index := findSubtable(cmapID{tables.PlatformUnicode, tables.PEUnicodeFull13}, candidateIds); index != -1 {
		return candidates[index], uv, nil
	}
	if index := findSubtable(cmapID{tables.PlatformUnicode, tables.PEUnicodeFull}, candidateIds); index != -1 {
		return candidates[index], uv, nil
	}

	/* 16-bit subtables. */
	if index := findSubtable(cmapID{tables.PlatformMicrosoft, tables.PEMicrosoftUnicodeCs}, candidateIds); index != -1 {
		return candidates[index], uv, nil
	}
	if index := findSubtable(cmapID{tables.PlatformUnicode, tables.PEUnicodeBMP}, candidateIds); index != -1 {
		return candidates[index], uv, nil
	}
	if index := findSubtable(cmapID{tables.PlatformUnicode, 2}, candidateIds); index != -1 { // deprecated
		return candidates[index], uv, nil
	}
	if index := findSubtable(cmapID{tables.PlatformUnicode, 1}, candidateIds); index != -1 { // deprecated
		return candidates[index], uv, nil
	}
	if index := findSubtable(cmapID{tables.PlatformUnicode, 0}, candidateIds); index != -1 { // deprecated
		return candidates[index], uv, nil
	}

	// uuh... fallback to the first cmap and hope for the best
	if len(candidates) != 0 {
		return candidates[0], uv, nil
	}
	return nil, nil, errors.New("unsupported cmap table")
}

// findSubtable returns the cmap index for the given platform and encoding, or -1 if not found.
func findSubtable(id cmapID, cmaps []cmapID) int {
	key := id.key()
	// binary search
	for i, j := 0, len(cmaps); i < j; {
		h := i + (j-i)/2
		entryKey := cmaps[h].key()
		if key < entryKey {
			j = h
		} else if entryKey < key {
			i = h + 1
		} else {
			return h
		}
	}
	return -1
}

// ---------------------------------- Format 0 ----------------------------------

// use Macintosh encoding, storing indexIntoEncoding -> glyphIndex
type cmap0 map[rune]uint8

func newCmap0(cm tables.CmapSubtable0) cmap0 {
	out := make(cmap0)
	for b, gid := range cm.GlyphIdArray {
		if b == 0 {
			continue
		}
		out[tables.DecodeMacintoshByte(byte(b))] = gid
	}
	return out
}

type cmap0Iter struct {
	data cmap0
	keys []rune
	pos  int
}

func (it *cmap0Iter) Next() bool {
	return it.pos < len(it.keys)
}

func (it *cmap0Iter) Char() (rune, GID) {
	r := it.keys[it.pos]
	it.pos++
	return r, GID(it.data[r])
}

func (s cmap0) Iter() CmapIter {
	keys := make([]rune, 0, len(s))
	for k := range s {
		keys = append(keys, k)
	}
	return &cmap0Iter{data: s, keys: keys}
}

func (s cmap0) Lookup(r rune) (GID, bool) {
	v, ok := s[r] // will be 0 if r is not in s
	return GID(v), ok
}

// ---------------------------------- Format 4 ----------------------------------

// if indexes is nil, delta is used
type cmapEntry16 struct {
	// we prefere not to keep a link to a buffer (via an offset)
	// and eagerly resolve it
	indexes    []tables.GlyphID // length end - start + 1
	end, start uint16
	delta      uint16 // arithmetic modulo 0xFFFF
}

type cmap4 []cmapEntry16

func newCmap4(cm tables.CmapSubtable4) (cmap4, error) {
	segCount := len(cm.EndCode)
	out := make(cmap4, segCount)
	for i := range out {
		entry := cmapEntry16{
			end:   cm.EndCode[i],
			start: cm.StartCode[i],
			delta: cm.IdDelta[i],
		}
		idRangeOffset := int(cm.IdRangeOffsets[i])

		// some fonts use 0xFFFF for idRangeOff for the last segment
		if entry.start != 0xFFFF && idRangeOffset != 0 {
			// we resolve the indexes
			entry.indexes = make([]tables.GlyphID, entry.end-entry.start+1)
			indexStart := idRangeOffset/2 + i - segCount
			if len(cm.GlyphIDArray) < 2*(indexStart+len(entry.indexes)) {
				return nil, errors.New("invalid cmap subtable format 4 glyphs array length")
			}
			for j := range entry.indexes {
				index := indexStart + j
				entry.indexes[j] = tables.GlyphID(binary.BigEndian.Uint16(cm.GlyphIDArray[2*index:]))
			}
		}
		out[i] = entry
	}
	return out, nil
}

type cmap4Iter struct {
	data cmap4
	pos1 int // into data
	pos2 int // either into data[pos1].indexes or an offset between start and end
}

func (it *cmap4Iter) Next() bool {
	return it.pos1 < len(it.data)
}

func (it *cmap4Iter) Char() (r rune, gy GID) {
	entry := it.data[it.pos1]
	if entry.indexes == nil {
		r = rune(it.pos2 + int(entry.start))
		gy = GID(uint16(it.pos2) + entry.start + entry.delta)
		if uint16(it.pos2) == entry.end-entry.start {
			// we have read the last glyph in this part
			it.pos2 = 0
			it.pos1++
		} else {
			it.pos2++
		}
	} else { // pos2 is the array index
		r = rune(it.pos2) + rune(entry.start)
		gy = GID(entry.indexes[it.pos2])
		if gy != 0 {
			gy += GID(entry.delta)
		}
		if it.pos2 == len(entry.indexes)-1 {
			// we have read the last glyph in this part
			it.pos2 = 0
			it.pos1++
		} else {
			it.pos2++
		}
	}

	return r, gy
}

func (s cmap4) Iter() CmapIter { return &cmap4Iter{data: s} }

func (s cmap4) Lookup(r rune) (GID, bool) {
	if uint32(r) > 0xffff {
		return 0, false
	}
	// binary search
	c := uint16(r)
	for i, j := 0, len(s); i < j; {
		h := i + (j-i)/2
		entry := s[h]
		if c < entry.start {
			j = h
		} else if entry.end < c {
			i = h + 1
		} else if entry.indexes == nil {
			return GID(c + entry.delta), true
		} else {
			glyph := entry.indexes[c-entry.start]
			if glyph == 0 {
				return 0, false
			}
			return GID(uint16(glyph) + entry.delta), true
		}
	}
	return 0, false
}

// ---------------------------------- Format 6 and 10  ----------------------------------

type cmap6or10 struct {
	entries   []tables.GlyphID
	firstCode rune
}

func newCmap6(cm tables.CmapSubtable6) cmap6or10 {
	return cmap6or10{entries: cm.GlyphIdArray, firstCode: rune(cm.FirstCode)}
}

func newCmap10(cm tables.CmapSubtable10) cmap6or10 {
	return cmap6or10{entries: cm.GlyphIdArray, firstCode: rune(cm.StartCharCode)}
}

type cmap6Or10Iter struct {
	data cmap6or10
	pos  int // index into data.entries
}

func (it *cmap6Or10Iter) Next() bool {
	return it.pos < len(it.data.entries)
}

func (it *cmap6Or10Iter) Char() (rune, GID) {
	entry := it.data.entries[it.pos]
	r := rune(it.pos) + it.data.firstCode
	gy := GID(entry)
	it.pos++
	return r, gy
}

func (s cmap6or10) Iter() CmapIter {
	return &cmap6Or10Iter{data: s}
}

func (s cmap6or10) Lookup(r rune) (GID, bool) {
	if r < s.firstCode {
		return 0, false
	}
	c := int(r - s.firstCode)
	if c >= len(s.entries) {
		return 0, false
	}
	return GID(s.entries[c]), true
}

// ---------------------------------- Format 12 ----------------------------------

type cmap12 []tables.SequentialMapGroup

func newCmap12(cm tables.CmapSubtable12) cmap12 { return cm.Groups }

type cmap12Iter struct {
	data cmap12
	pos1 int // into data
	pos2 int // offset from start
}

func (it *cmap12Iter) Next() bool { return it.pos1 < len(it.data) }

func (it *cmap12Iter) Char() (r rune, gy GID) {
	entry := it.data[it.pos1]
	r = rune(it.pos2 + int(entry.StartCharCode))
	gy = GID(it.pos2 + int(entry.StartGlyphID))
	if uint32(it.pos2) == entry.EndCharCode-entry.StartCharCode {
		// we have read the last glyph in this part
		it.pos2 = 0
		it.pos1++
	} else {
		it.pos2++
	}

	return r, gy
}

func (s cmap12) Iter() CmapIter { return &cmap12Iter{data: s} }

func (s cmap12) Lookup(r rune) (GID, bool) {
	c := uint32(r)
	// binary search
	for i, j := 0, len(s); i < j; {
		h := i + (j-i)/2
		entry := s[h]
		if c < entry.StartCharCode {
			j = h
		} else if entry.EndCharCode < c {
			i = h + 1
		} else {
			return GID(c - entry.StartCharCode + entry.StartGlyphID), true
		}
	}
	return 0, false
}

// ---------------------------------- Format 13 ----------------------------------

type cmap13 []tables.SequentialMapGroup

func newCmap13(cm tables.CmapSubtable13) cmap13 { return cm.Groups }

type cmap13Iter struct {
	data cmap13
	pos1 int // into data
	pos2 int // offset from start
}

func (it *cmap13Iter) Next() bool {
	return it.pos1 < len(it.data)
}

func (it *cmap13Iter) Char() (r rune, gy GID) {
	entry := it.data[it.pos1]
	r = rune(it.pos2 + int(entry.StartCharCode))
	gy = GID(entry.StartGlyphID)
	if uint32(it.pos2) == entry.EndCharCode-entry.StartCharCode {
		// we have read the last glyph in this part
		it.pos2 = 0
		it.pos1++
	} else {
		it.pos2++
	}

	return r, gy
}

func (s cmap13) Iter() CmapIter { return &cmap13Iter{data: s} }

func (s cmap13) Lookup(r rune) (GID, bool) {
	c := uint32(r)
	// binary search
	for i, j := 0, len(s); i < j; {
		h := i + (j-i)/2
		entry := s[h]
		if c < entry.StartCharCode {
			j = h
		} else if entry.EndCharCode < c {
			i = h + 1
		} else {
			return GID(entry.StartGlyphID), true
		}
	}
	return 0, false
}

// -------------------------------- Unicode selectors --------------------------------

type unicodeRange struct {
	start           rune
	additionalCount uint8 // 0 for a singleton range
}

type uvsMapping struct {
	unicode rune
	glyphID tables.GlyphID
}

type variationSelector struct {
	defaultUVS    []unicodeRange
	nonDefaultUVS []uvsMapping
	varSelector   rune
}

func (vs variationSelector) getGlyph(r rune) (GID, uint8) {
	// binary search
	for i, j := 0, len(vs.defaultUVS); i < j; {
		h := i + (j-i)/2
		entry := vs.defaultUVS[h]
		if r < entry.start {
			j = h
		} else if entry.start+rune(entry.additionalCount) < r {
			i = h + 1
		} else {
			return 0, VariantUseDefault
		}
	}

	for i, j := 0, len(vs.nonDefaultUVS); i < j; {
		h := i + (j-i)/2
		entry := vs.nonDefaultUVS[h].unicode
		if r < entry {
			j = h
		} else if entry < r {
			i = h + 1
		} else {
			return GID(vs.nonDefaultUVS[h].glyphID), VariantFound
		}
	}

	return 0, VariantNotFound
}

// same as binary.BigEndian.Uint32, but for 24 bit uint
func parseUint24(b [3]byte) rune {
	return rune(b[0])<<16 | rune(b[1])<<8 | rune(b[2])
}

type UnicodeVariations []variationSelector

func newUnicodeVariations(cm tables.CmapSubtable14) UnicodeVariations {
	out := make([]variationSelector, len(cm.VarSelectors))
	for i, sel := range cm.VarSelectors {
		vs := variationSelector{
			varSelector:   parseUint24(sel.VarSelector),
			defaultUVS:    make([]unicodeRange, len(sel.DefaultUVS.Ranges)),
			nonDefaultUVS: make([]uvsMapping, len(sel.NonDefaultUVS.Ranges)),
		}
		for i, r := range sel.DefaultUVS.Ranges {
			vs.defaultUVS[i] = unicodeRange{start: parseUint24(r.StartUnicodeValue), additionalCount: r.AdditionalCount}
		}
		for i, r := range sel.NonDefaultUVS.Ranges {
			vs.nonDefaultUVS[i] = uvsMapping{unicode: parseUint24(r.UnicodeValue), glyphID: r.GlyphID}
		}
		out[i] = vs
	}
	return out
}

const (
	// VariantNotFound is returned when the font does not have a glyph for
	// the given rune and selector.
	VariantNotFound = iota
	// VariantUseDefault is returned when the regular glyph should be used (ignoring the selector).
	VariantUseDefault
	// VariantFound is returned when the font has a variant for the glyph and selector.
	VariantFound
)

// GetGlyphVariant returns the glyph index to used to [r] combined with [selector],
// with one of the tri-state flags [VariantNotFound, VariantUseDefault, VariantFound]
func (t UnicodeVariations) GetGlyphVariant(r, selector rune) (GID, uint8) {
	// binary search
	for i, j := 0, len(t); i < j; {
		h := i + (j-i)/2
		entryKey := t[h].varSelector
		if selector < entryKey {
			j = h
		} else if entryKey < selector {
			i = h + 1
		} else {
			return t[h].getGlyph(r)
		}
	}
	return 0, VariantNotFound
}

// Handle legacy font with remap
// TODO: the Iter() and RuneRanges() method does not include the additional mapping

type remaperSymbol struct {
	Cmap
}

func (rs remaperSymbol) Lookup(r rune) (GID, bool) {
	// try without map first
	if g, ok := rs.Cmap.Lookup(r); ok {
		return g, true
	}

	if r <= 0x00FF {
		/* For symbol-encoded OpenType fonts, we duplicate the
		 * U+F000..F0FF range at U+0000..U+00FF.  That's what
		 * Windows seems to do, and that's hinted about at:
		 * https://docs.microsoft.com/en-us/typography/opentype/spec/recom
		 * under "Non-Standard (Symbol) Fonts". */
		mapped := 0xF000 + r
		return rs.Lookup(mapped)
	}

	return 0, false
}

type remaperPUASimp struct {
	Cmap
}

func (rs remaperPUASimp) Lookup(r rune) (GID, bool) {
	// try without map first
	if g, ok := rs.Cmap.Lookup(r); ok {
		return g, true
	}

	if mapped := arabicPUASimpMap(r); mapped != 0 {
		return rs.Lookup(mapped)
	}

	return 0, false
}

type remaperPUATrad struct {
	Cmap
}

func (rs remaperPUATrad) Lookup(r rune) (GID, bool) {
	// try without map first
	if g, ok := rs.Cmap.Lookup(r); ok {
		return g, true
	}

	if mapped := arabicPUATradMap(r); mapped != 0 {
		return rs.Lookup(mapped)
	}

	return 0, false
}

// ---------------------------- efficent rune set support -----------------------------------------

// CmapRuneRanger is implemented by cmaps whose coverage is defined in terms
// of rune ranges
type CmapRuneRanger interface {
	// RuneRanges returns a list of (start, end) rune pairs, both included.
	// `dst` is an optional buffer used to reduce allocations
	RuneRanges(dst [][2]rune) [][2]rune
}

var (
	_ CmapRuneRanger = cmap4(nil)
	_ CmapRuneRanger = (*cmap6or10)(nil)
	_ CmapRuneRanger = cmap12(nil)
	_ CmapRuneRanger = cmap13(nil)
)

func (cm cmap4) RuneRanges(dst [][2]rune) [][2]rune {
	if cap(dst) < len(cm) {
		dst = make([][2]rune, 0, len(cm))
	}
	dst = dst[:0]
	for _, e := range cm {
		start, end := rune(e.start), rune(e.end)
		if L := len(dst); L != 0 && dst[L-1][1] == start {
			// grow the previous range
			dst[L-1][1] = end
		} else {
			dst = append(dst, [2]rune{start, end})
		}
	}
	return dst
}

func (cm *cmap6or10) RuneRanges(dst [][2]rune) [][2]rune {
	if cap(dst) < 1 {
		dst = [][2]rune{{}}
	}
	dst = dst[:1]
	dst[0] = [2]rune{cm.firstCode, cm.firstCode + rune(len(cm.entries)) - 1}
	return dst
}

func (cm cmap12) RuneRanges(dst [][2]rune) [][2]rune {
	if cap(dst) < len(cm) {
		dst = make([][2]rune, 0, len(cm))
	}
	dst = dst[:0]
	for _, e := range cm {
		start, end := rune(e.StartCharCode), rune(e.EndCharCode)
		if L := len(dst); L != 0 && dst[L-1][1] == start {
			// grow the previous range
			dst[L-1][1] = end
		} else {
			dst = append(dst, [2]rune{start, end})
		}
	}
	return dst
}

func (cm cmap13) RuneRanges(dst [][2]rune) [][2]rune { return cmap12(cm).RuneRanges(dst) }
