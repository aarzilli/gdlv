// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package tables

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// ------------------------------------ fvar ------------------------------------

// Fvar is the Font Variations Table.
// See - https://learn.microsoft.com/fr-fr/typography/opentype/spec/fvar
type Fvar struct {
	majorVersion    uint16   //	Major version number of the font variations table — set to 1.
	minorVersion    uint16   //	Minor version number of the font variations table — set to 0.
	axesArrayOffset Offset16 // Offset in bytes from the beginning of the table to the start of the VariationAxisRecord array.
	reserved        uint16   //	This field is permanently reserved. Set to 2.
	axisCount       uint16   //	The number of variation axes in the font (the number of records in the axes array).
	axisSize        uint16   //	The size in bytes of each VariationAxisRecord — set to 20 (0x0014) for this version.
	instanceCount   uint16   //	The number of named instances defined in the font (the number of records in the instances array).
	instanceSize    uint16   //	The size in bytes of each InstanceRecord — set to either axisCount * sizeof(Fixed) + 4, or to axisCount * sizeof(Fixed) + 6.
	FvarRecords     `isOpaque:""`
}

func (fv *Fvar) parseFvarRecords(src []byte) (err error) {
	if L := len(src); L < int(fv.axesArrayOffset) {
		return fmt.Errorf("EOF: expected length: %d, got %d", fv.axesArrayOffset, L)
	}
	fv.FvarRecords, _, err = ParseFvarRecords(src[fv.axesArrayOffset:], int(fv.axisCount), int(fv.instanceCount), int(fv.axisCount))
	return
}

// binarygen: argument=instanceCount int
// binarygen: argument=instanceSize int
type FvarRecords struct {
	Axis      []VariationAxisRecord
	Instances []InstanceRecord `isOpaque:"" subsliceStart:"AtCurrent"`
}

func (fvr *FvarRecords) parseInstances(src []byte, axisCount, instanceCount, instanceSize int) error {
	if L := len(src); L < instanceCount*instanceSize {
		return fmt.Errorf("EOF: expected length: %d, got %d", instanceCount*instanceSize, L)
	}
	fvr.Instances = make([]InstanceRecord, instanceCount)
	for i := range fvr.Instances {
		var err error
		fvr.Instances[i], _, err = ParseInstanceRecord(src[instanceSize*i:], axisCount)
		if err != nil {
			return err
		}
	}
	return nil
}

type VariationAxisRecord struct {
	Tag     Tag       // Tag identifying the design variation for the axis.
	Minimum Float1616 // mininum value on the variation axis that the font covers
	Default Float1616 // default position on the axis
	Maximum Float1616 // maximum value on the variation axis that the font covers
	flags   uint16    // Axis qualifiers — see details below.
	strid   NameID    // name entry in the font's ‘name’ table
}

type InstanceRecord struct {
	SubfamilyNameID  uint16      // The name ID for entries in the 'name' table that provide subfamily names for this instance.
	flags            uint16      // Reserved for future use — set to 0.
	Coordinates      []Float1616 // [axisCount] The coordinates array for this instance.
	PostScriptNameID uint16      `isOpaque:"" subsliceStart:"AtCurrent"` // Optional. The name ID for entries in the 'name' table that provide PostScript names for this instance.
}

func (ir *InstanceRecord) parsePostScriptNameID(src []byte, _ int) (int, error) {
	if len(src) >= 2 {
		ir.PostScriptNameID = binary.BigEndian.Uint16(src)
		return 2, nil
	}
	return 0, nil
}

type ItemVarStore struct {
	format              uint16              // Format — set to 1
	VariationRegionList VariationRegionList `offsetSize:"Offset32"`                            // Offset in bytes from the start of the item variation store to the variation region list.
	ItemVariationDatas  []ItemVariationData `arrayCount:"FirstUint16" offsetsArray:"Offset32"` // [itemVariationDataCount] Offsets in bytes from the start of the item variation store to each item variation data subtable.
}

// GetDelta uses the variation [store] and the selected instance coordinates [coords]
// to compute the value at [index].
func (store ItemVarStore) GetDelta(index VariationStoreIndex, coords []Coord) float32 {
	if int(index.DeltaSetOuter) >= len(store.ItemVariationDatas) {
		return 0
	}
	varData := store.ItemVariationDatas[index.DeltaSetOuter]
	if int(index.DeltaSetInner) >= len(varData.DeltaSets) {
		return 0
	}
	deltaSet := varData.DeltaSets[index.DeltaSetInner]
	var delta float32
	for i, regionIndex := range varData.RegionIndexes {
		region := store.VariationRegionList.VariationRegions[regionIndex]
		v := region.Evaluate(coords)
		delta += float32(deltaSet[i]) * v
	}
	return delta
}

// AxisCount returns the number of axis found in the
// var store, which must be the same as the one in the 'fvar' table.
// It returns -1 if the store is empty
func (vs *ItemVarStore) AxisCount() int {
	if vs.format == 0 {
		return -1
	}
	return int(vs.VariationRegionList.axisCount)
}

type VariationRegionList struct {
	axisCount        uint16            // The number of variation axes for this font. This must be the same number as axisCount in the 'fvar' table.
	VariationRegions []VariationRegion `arrayCount:"FirstUint16" arguments:"regionAxesCount=.axisCount"` // [regionCount] Array of variation regions.
}

type VariationRegion struct {
	// Array of region axis coordinates records, in the order of axes given in the 'fvar' table.
	// Each RegionAxisCoordinates record provides coordinate values for a region along a single axis:
	RegionAxes []RegionAxisCoordinates // [axisCount]
}

// Evaluate returns the scalar factor of the region
func (vr VariationRegion) Evaluate(coords []Coord) float32 {
	v := float32(1)
	for axis, coord := range coords {
		factor := vr.RegionAxes[axis].evaluate(coord)
		v *= factor
	}
	return v
}

type RegionAxisCoordinates struct {
	StartCoord Coord // The region start coordinate value for the current axis.
	PeakCoord  Coord // The region peak coordinate value for the current axis.
	EndCoord   Coord // The region end coordinate value for the current axis.
}

// evaluate returns the factor corresponding to the given [coord],
// interpolating between start and end.
func (reg RegionAxisCoordinates) evaluate(coord Coord) float32 {
	start, peak, end := reg.StartCoord, reg.PeakCoord, reg.EndCoord
	if peak == 0 || coord == peak {
		return 1
	} else if coord == 0 { // Faster
		return 0
	}

	if coord <= start || end <= coord {
		return 0
	}

	// Interpolate
	if coord < peak {
		return float32(coord-start) / float32(peak-start)
	}
	return float32(end-coord) / float32(end-peak)
}

type ItemVariationData struct {
	itemCount        uint16    // The number of delta sets for distinct items.
	wordDeltaCount   uint16    // A packed field: the high bit is a flag—see details below.
	regionIndexCount uint16    // The number of variation regions referenced.
	RegionIndexes    []uint16  `arrayCount:"ComputedField-regionIndexCount"` //[regionIndexCount]	Array of indices into the variation region list for the regions referenced by this item variation data table.
	DeltaSets        [][]int16 `isOpaque:"" subsliceStart:"AtCurrent"`       //[itemCount]	Delta-set rows.
}

func (ivd *ItemVariationData) parseDeltaSets(src []byte) error {
	const (
		LONG_WORDS            = 0x8000 // Flag indicating that “word” deltas are long (int32)
		WORD_DELTA_COUNT_MASK = 0x7FFF // Count of “word” delt
	)
	if ivd.wordDeltaCount&LONG_WORDS != 0 {
		return errors.New("LONG_WORDS not implemented in DeltaSets")
	}
	itemCount := int(ivd.itemCount)
	shortDeltaCount := int(WORD_DELTA_COUNT_MASK & ivd.wordDeltaCount)
	regionIndexCount := int(ivd.regionIndexCount)

	rowLength := shortDeltaCount + regionIndexCount
	if L := len(src); L < itemCount*rowLength {
		return fmt.Errorf("EOF: expected length: %d, got %d", itemCount*rowLength, L)
	}
	if shortDeltaCount > regionIndexCount {
		return errors.New("invalid item variation data subtable")
	}
	ivd.DeltaSets = make([][]int16, itemCount)
	for i := range ivd.DeltaSets {
		vi := make([]int16, regionIndexCount)
		j := 0
		for ; j < shortDeltaCount; j++ {
			vi[j] = int16(binary.BigEndian.Uint16(src[2*j:]))
		}
		for ; j < regionIndexCount; j++ {
			vi[j] = int16(int8(src[shortDeltaCount+j]))
		}
		ivd.DeltaSets[i] = vi
		src = src[rowLength:]
	}
	return nil
}

// ------------------------------------ GVAR ------------------------------------

// See - https://learn.microsoft.com/fr-fr/typography/opentype/spec/gvar
type Gvar struct {
	majorVersion                  uint16                                                                                         // Major version number of the glyph variations table — set to 1.
	minorVersion                  uint16                                                                                         // Minor version number of the glyph variations table — set to 0.
	axisCount                     uint16                                                                                         // The number of variation axes for this font. This must be the same number as axisCount in the 'fvar' table.
	sharedTupleCount              uint16                                                                                         // The number of shared tuple records. Shared tuple records can be referenced within glyph variation data tables for multiple glyphs, as opposed to other tuple records stored directly within a glyph variation data table.
	SharedTuples                  `offsetSize:"Offset32" arguments:"sharedTuplesCount=.sharedTupleCount,valuesCount=.axisCount"` // Offset from the start of this table to the shared tuple records.
	glyphCount                    uint16                                                                                         // The number of glyphs in this font. This must match the number of glyphs stored elsewhere in the font.
	flags                         uint16                                                                                         // Bit-field that gives the format of the offset array that follows. If bit 0 is clear, the offsets are uint16; if bit 0 is set, the offsets are uint32.
	glyphVariationDataArrayOffset Offset32                                                                                       // Offset from the start of this table to the array of GlyphVariationData tables.
	glyphVariationDataOffsets     []uint32                                                                                       `isOpaque:"" subsliceStart:"AtCurrent"` // [glyphCount + 1]Offset16 or Offset32 Offsets from the start of the GlyphVariationData array to each GlyphVariationData table.
	GlyphVariationDatas           []GlyphVariationData                                                                           `isOpaque:""`
}

func (gv *Gvar) parseGlyphVariationDataOffsets(src []byte) error {
	var err error
	gv.glyphVariationDataOffsets, err = ParseLoca(src, int(gv.glyphCount), gv.flags&1 != 0)
	return err
}

func (gv *Gvar) parseGlyphVariationDatas(src []byte) error {
	gv.GlyphVariationDatas = make([]GlyphVariationData, gv.glyphCount)
	startArray := uint32(gv.glyphVariationDataArrayOffset)
	for i := range gv.GlyphVariationDatas {
		start, end := int(startArray+gv.glyphVariationDataOffsets[i]), int(startArray+gv.glyphVariationDataOffsets[i+1])
		if start == end {
			continue
		}

		if start > end {
			return fmt.Errorf("invalid offsets %d > %d", start, end)
		}

		if L := len(src); L < end {
			return fmt.Errorf("EOF: expected length: %d, got %d", end, L)
		}

		var err error
		gv.GlyphVariationDatas[i], _, err = ParseGlyphVariationData(src[start:end], int(gv.axisCount))
		if err != nil {
			return err
		}
	}
	return nil
}

type SharedTuples struct {
	SharedTuples []Tuple // [sharedTupleCount] Array of tuple records shared across all glyph variation data tables.
}

type Tuple struct {
	Values []Coord // [axisCount] Coordinate array specifying a position within the font’s variation space. The number of elements must match the axisCount specified in the 'fvar' table.
}

type GlyphVariationData struct {
	tupleVariationCount   uint16                 // A packed field. The high 4 bits are flags, and the low 12 bits are the number of tuple variation tables for this glyph. The number of tuple variation tables can be any number between 1 and 4095.
	SerializedData        []byte                 `offsetSize:"Offset16" arrayCount:"ToEnd"`              // Offset from the start of the GlyphVariationData table to the serialized data
	TupleVariationHeaders []TupleVariationHeader `arrayCount:"ComputedField-tupleVariationCount&0x0FFF"` //[tupleCount]	Array of tuple variation headers.
}

// HasSharedPointNumbers returns true if the  'sharedPointNumbers' is on.
func (gv *GlyphVariationData) HasSharedPointNumbers() bool {
	const sharedPointNumbers = 0x8000
	return gv.tupleVariationCount&sharedPointNumbers != 0
}

// binarygen: argument=axisCount int
type TupleVariationHeader struct {
	VariationDataSize uint16 //	The size in bytes of the serialized data for this tuple variation table.
	tupleIndex        uint16 //	A packed field. The high 4 bits are flags (see below). The low 12 bits are an index into a shared tuple records array.
	// Peak tuple record for this tuple variation table — optional, determined by flags in the tupleIndex value.
	// Note that this must always be included in the 'cvar' table.
	PeakTuple          Tuple    `isOpaque:"" subsliceStart:"AtCurrent"`
	IntermediateTuples [2]Tuple `isOpaque:"" subsliceStart:"AtCurrent"` //	Intermediate start/end tuple record for this tuple variation table — optional, determined by flags in the tupleIndex value.
}

func (tv *TupleVariationHeader) parsePeakTuple(src []byte, axisCount int) (read int, err error) {
	const embeddedPeakTuple = 0x8000
	if hasPeak := tv.tupleIndex&embeddedPeakTuple != 0; hasPeak {
		tv.PeakTuple, read, err = ParseTuple(src, axisCount)
		if err != nil {
			return 0, err
		}
	}
	return
}

func (tv *TupleVariationHeader) parseIntermediateTuples(src []byte, axisCount int) (read int, err error) {
	const intermediateRegion = 0x4000
	if hasRegions := tv.tupleIndex&intermediateRegion != 0; hasRegions {
		tv.IntermediateTuples[0], read, err = ParseTuple(src, axisCount)
		if err != nil {
			return 0, err
		}
		tv.IntermediateTuples[1], _, err = ParseTuple(src[read:], axisCount)
		read *= 2
	}
	return
}

// HasPrivatePointNumbers returns true if the flag 'privatePointNumbers' is on
func (t *TupleVariationHeader) HasPrivatePointNumbers() bool {
	const privatePointNumbers = 0x2000
	return t.tupleIndex&privatePointNumbers != 0
}

// Index returns the tuple index, after masking
func (t *TupleVariationHeader) Index() uint16 {
	const TupleIndexMask = 0x0FFF
	return t.tupleIndex & TupleIndexMask
}

// ---------------------------------- HVAR/VVAR ----------------------------------

// See - https://learn.microsoft.com/fr-fr/typography/opentype/spec/hvar
type HVAR struct {
	majorVersion        uint16           // Major version number of the horizontal metrics variations table — set to 1.
	minorVersion        uint16           // Minor version number of the horizontal metrics variations table — set to 0.
	ItemVariationStore  ItemVarStore     `offsetSize:"Offset32"` // Offset in bytes from the start of this table to the item variation store table.
	AdvanceWidthMapping DeltaSetMapping  `offsetSize:"Offset32"` // Offset in bytes from the start of this table to the delta-set index mapping for advance widths (may be NULL).
	LsbMapping          *DeltaSetMapping `offsetSize:"Offset32"` // Offset in bytes from the start of this table to the delta-set index mapping for left side bearings (may be NULL).
	RsbMapping          *DeltaSetMapping `offsetSize:"Offset32"` // Offset in bytes from the start of this table to the delta-set index mapping for right side bearings (may be NULL).
}

func (t *HVAR) AdvanceDelta(glyph GlyphID, coords []Coord) float32 {
	index := t.AdvanceWidthMapping.Index(glyph)
	return t.ItemVariationStore.GetDelta(index, coords)
}

// VariationStoreIndex reference an item in the variation store
type VariationStoreIndex struct {
	DeltaSetOuter, DeltaSetInner uint16
}

type DeltaSetMapping struct {
	format      uint8 // DeltaSetIndexMap format: 0 or 1
	entryFormat uint8 // A packed field that describes the compressed representation of delta-set indices. See details below.
	// uint16 or uint32	mapCount : The number of mapping entries.
	Map []VariationStoreIndex `isOpaque:"" subsliceStart:"AtCurrent"`
}

// Index returns the [VariationStoreIndex] for the given index.
func (m DeltaSetMapping) Index(glyph GlyphID) VariationStoreIndex {
	// If a mapping table is not provided, glyph indices are used as implicit delta-set indices.
	// [...] the delta-set outer-level index is zero, and the glyph ID is used as the inner-level index.
	if len(m.Map) == 0 {
		return VariationStoreIndex{DeltaSetInner: uint16(glyph)}
	}

	// If a given glyph ID is greater than mapCount - 1, then the last entry is used.
	if int(glyph) >= len(m.Map) {
		glyph = GlyphID(len(m.Map) - 1)
	}

	return m.Map[glyph]
}

func (ds *DeltaSetMapping) parseMap(src []byte) error {
	var mapCount int
	switch ds.format {
	case 0:
		if L := len(src); L < 2 {
			return fmt.Errorf("EOF: expected length: %d, got %d", 2, L)
		}
		mapCount = int(binary.BigEndian.Uint16(src))
		src = src[2:]
	case 1:
		if L := len(src); L < 4 {
			return fmt.Errorf("EOF: expected length: %d, got %d", 4, L)
		}
		mapCount = int(binary.BigEndian.Uint32(src))
		src = src[4:]
	default:
		return fmt.Errorf("unsupported DeltaSetMapping format %d", ds.format)
	}

	const (
		INNER_INDEX_BIT_COUNT_MASK = 0x0F // Mask for the low 4 bits, which give the count of bits minus one that are used in each entry for the inner-level index.
		MAP_ENTRY_SIZE_MASK        = 0x30 // Mask for bits that indicate the size in bytes minus one of each entry.
	)
	innerBitSize := ds.entryFormat&INNER_INDEX_BIT_COUNT_MASK + 1
	entrySize := int((ds.entryFormat&MAP_ENTRY_SIZE_MASK)>>4 + 1)
	if entrySize > 4 || len(src) < entrySize*mapCount {
		return fmt.Errorf("invalid delta-set mapping (length %d, entrySize %d, mapCount %d)", len(src), entrySize, mapCount)
	}
	ds.Map = make([]VariationStoreIndex, mapCount)
	for i := range ds.Map {
		var v uint32
		for _, b := range src[entrySize*i : entrySize*(i+1)] { // 1 to 4 bytes
			v = v<<8 + uint32(b)
		}
		ds.Map[i].DeltaSetOuter = uint16(v >> innerBitSize)
		ds.Map[i].DeltaSetInner = uint16(v & (1<<innerBitSize - 1))
	}
	return nil
}

// See - https://learn.microsoft.com/fr-fr/typography/opentype/spec/vvar
type VVAR struct {
	HVAR
	VOrgMapping *DeltaSetMapping `offsetSize:"Offset32"` // Offset in bytes from the start of this table to the delta-set index mapping for Y coordinates of vertical origins (may be NULL).
}

func (vv *VVAR) VorgDelta(glyph GlyphID, coords []Coord) float32 {
	if vv.VOrgMapping == nil {
		return 0
	}
	varidx := vv.VOrgMapping.Index(glyph)
	return vv.ItemVariationStore.GetDelta(varidx, coords)
}

// ------------------------------------ avar ------------------------------------

// avar — Axis Variations Table
type Avar struct {
	majorVersion    uint16        // Major version number of the axis variations table — set to 1.
	minorVersion    uint16        // Minor version number of the axis variations table — set to 0.
	reserved        uint16        // Permanently reserved; set to zero.
	AxisSegmentMaps []SegmentMaps `arrayCount:"FirstUint16"` //[axisCount]	The segment maps array — one segment map for each axis, in the order of axes specified in the 'fvar' table.
}

type SegmentMaps struct {
	// [positionMapCount]	The array of axis value map records for this axis.
	// Each axis value map record provides a single axis-value mapping correspondence.
	AxisValueMaps []AxisValueMap `arrayCount:"FirstUint16"`
}

func (sm SegmentMaps) Map(value Coord) Coord {
	// copied from harfbuzz/src/hb-ot-var-avar-table.hh

	l := sm.AxisValueMaps

	// The following special-cases are not part of OpenType, which requires
	// that at least -1, 0, and +1 must be mapped. But we include these as
	// part of a better error recovery scheme.
	if len(l) == 0 {
		return value
	} else if len(l) == 1 {
		return value - l[0].FromCoordinate + l[0].ToCoordinate
	}

	// At least two mappings now.

	// CoreText is wild...
	// PingFangUI avar needs all this special-casing...
	// So we implement an extended version of the spec here,
	// which is more robust and more likely to be compatible with
	// the wild.

	const p1 = Coord(1 << 14)
	const m1 = -p1

	start := 0
	end := len(l)
	if l[start].FromCoordinate == m1 && l[start].ToCoordinate == m1 && l[start+1].FromCoordinate == m1 {
		start++
	}
	if l[end-1].FromCoordinate == p1 && l[end-1].ToCoordinate == p1 && l[end-2].FromCoordinate == p1 {
		end--
	}

	// Look for exact match first, and do lots of special-casing.
	var i int
	for i = start; i < end; i++ {
		if value == l[i].FromCoordinate {
			break
		}
	}
	if i < end {
		// There's at least one exact match. See if there are more.
		j := i
		for ; j+1 < end; j++ {
			if value != l[j+1].FromCoordinate {
				break
			}
		}

		// [i,j] inclusive are all exact matches:

		// If there's only one, return it. This is the only spec-compliant case.
		if i == j {
			return l[i].ToCoordinate
		}
		// If there's exactly three, return the middle one.
		if i+2 == j {
			return l[i+1].ToCoordinate
		}

		// Ignore the middle ones. Return the one mapping closer to 0.
		if value < 0 {
			return l[j].ToCoordinate
		}
		if value > 0 {
			return l[i].ToCoordinate
		}

		// Mapping 0 ? CoreText seems confused. It seems to prefer 0 here...
		// So we'll just return the smallest one. lol
		if abs(l[i].ToCoordinate) < abs(l[j].ToCoordinate) {
			return l[i].ToCoordinate
		}
		return l[j].ToCoordinate
	}

	// There's at least two and we're not an exact match. Prepare to lerp.

	// Find the segment we're in.
	for i = start; i < end; i++ {
		if value < l[i].FromCoordinate {
			break
		}
	}

	if i == 0 {
		// Value before all segments; Shift.
		return value - l[0].FromCoordinate + l[0].ToCoordinate
	}
	if i == end {
		// Value after all segments; Shift.
		return value - l[end-1].FromCoordinate + l[end-1].ToCoordinate
	}

	// Actually interpolate.
	before := l[i-1]
	after := l[i]
	denom := float64(after.FromCoordinate - before.FromCoordinate) // Can't be zero by now.
	return before.ToCoordinate + Coord(math.Round(float64(after.ToCoordinate-before.ToCoordinate)*float64(value-before.FromCoordinate))/denom)
}

type AxisValueMap struct {
	FromCoordinate Coord // A normalized coordinate value obtained using default normalization.
	ToCoordinate   Coord // The modified, normalized coordinate value.
}

// ----------------------------------------- MVAR -----------------------------------------

type MVAR struct {
	majorVersion       uint16           //	Major version number of the metrics variations table — set to 1.
	minorVersion       uint16           //	Minor version number of the metrics variations table — set to 0.
	reserved           uint16           //	Not used; set to 0.
	valueRecordSize    uint16           //	The size in bytes of each value record — must be greater than zero.
	valueRecordCount   uint16           //	The number of value records — may be zero.
	ItemVariationStore ItemVarStore     `offsetSize:"Offset16"`                 // Offset in bytes from the start of this table to the item variation store table. If valueRecordCount is zero, set to zero; if valueRecordCount is greater than zero, must be greater than zero.
	ValueRecords       []VarValueRecord `isOpaque:"" subsliceStart:"AtCurrent"` // [valueRecordCount]	Array of value records that identify target items and the associated delta-set index for each. The valueTag records must be in binary order of their valueTag field.
}

// Quoting the spec:
// "The valueRecordSize field indicates the size of each value record.
// Future, minor version updates of the MVAR table may define compatible
// extensions to the value record format with additional fields.
// Implementations must use the valueRecordSize field to determine the start of each record."
func (mv *MVAR) parseValueRecords(src []byte) error {
	expectedL := int(mv.valueRecordSize) * int(mv.valueRecordCount)
	if L := len(src); L < expectedL {
		return fmt.Errorf("EOF: expected length: %d, got %d", expectedL, L)
	}
	mv.ValueRecords = make([]VarValueRecord, mv.valueRecordCount)
	for i := range mv.ValueRecords {
		mv.ValueRecords[i].mustParse(src[int(mv.valueRecordSize)*i:])
	}
	return nil
}

type VarValueRecord struct {
	ValueTag Tag                 // Four-byte tag identifying a font-wide measure.
	Index    VariationStoreIndex // A delta-set index — used to select an item variation data subtable within the item variation store.
}

// ------------------------------------------------- STAT -------------------------------------------------

// STAT is the Style Attributes Table
// See https://learn.microsoft.com/en-us/typography/opentype/spec/stat
type STAT struct {
	majorVersion         uint16         // Major version number of the style attributes table — set to 1.
	minorVersion         uint16         // Minor version number of the style attributes table — set to 2.
	designAxisSize       uint16         // The size in bytes of each axis record.
	designAxisCount      uint16         // The number of axis records. In a font with an 'fvar' table, this value must be greater than or equal to the axisCount value in the 'fvar' table. In all fonts, must be greater than zero if axisValueCount is greater than zero.
	designAxes           []AxisRecord   `offsetSize:"Offset32" arrayCount:"ComputedField-designAxisCount"` // Offset in bytes from the beginning of the STAT table to the start of the design axes array. If designAxisCount is zero, set to zero; if designAxisCount is greater than zero, must be greater than zero.
	axisValueCount       uint16         // The number of axis value tables.
	axisValues           AxisValueArray `offsetSize:"Offset32" arguments:"valuesCount=.axisValueCount"` // Offset in bytes from the beginning of the STAT table to the start of the design axes value offsets array. If axisValueCount is zero, set to zero; if axisValueCount is greater than zero, must be greater than zero.
	elidedFallbackNameID uint16         // Name ID used as fallback when projection of names into a particular font model produces a subfamily name containing only elidable elements.
}

func (st *STAT) getAxisIndex(tag Tag) (uint16, bool) {
	for index, record := range st.designAxes {
		if record.Tag == tag {
			return uint16(index), true
		}
	}
	return 0, false
}

func (st STAT) Value(tag Tag) (float32, bool) {
	axisIndex, ok := st.getAxisIndex(tag)
	if !ok {
		return 0, false
	}

	for _, axisValue := range st.axisValues.Values {
		if axisValue.index() == axisIndex {
			return axisValue.valueFor(axisIndex), true
		}
	}

	return 0, false
}

type AxisRecord struct {
	Tag      Tag    // A tag identifying the axis of design variation.
	NameID   NameID // The name ID for entries in the 'name' table that provide a display string for this axis.
	Ordering uint16 // A value that applications can use to determine primary sorting of face names, or for ordering of labels when composing family or face names.
}

type AxisValueArray struct {
	Values []AxisValue `offsetsArray:"Offset16"` // Offset in bytes from the beginning of the STAT table to the start of the design axes value offsets array. If axisValueCount is zero, set to zero; if axisValueCount is greater than zero, must be greater than zero.
}

type AxisValue interface {
	name() NameID
	index() uint16
	valueFor(index uint16) float32
}

func (av AxisValue1) name() NameID { return av.valueNameID }
func (av AxisValue2) name() NameID { return av.valueNameID }
func (av AxisValue3) name() NameID { return av.valueNameID }
func (av AxisValue4) name() NameID { return av.valueNameID }

func (av AxisValue1) index() uint16 { return av.axisIndex }
func (av AxisValue2) index() uint16 { return av.axisIndex }
func (av AxisValue3) index() uint16 { return av.axisIndex }
func (av AxisValue4) index() uint16 { return 0xFFFF }

func (av AxisValue1) valueFor(index uint16) float32 { return av.value }
func (av AxisValue2) valueFor(index uint16) float32 { return av.nominalValue }
func (av AxisValue3) valueFor(index uint16) float32 { return av.value }
func (av AxisValue4) valueFor(index uint16) float32 {
	return av.axisValues[index].value
}

type AxisValue1 struct {
	format      uint16    `unionTag:"1"` // Format identifier — set to 1.
	axisIndex   uint16    // Zero-base index into the axis record array identifying the axis of design variation to which the axis value table applies. Must be less than designAxisCount.
	flags       uint16    // Flags — see below for details.
	valueNameID NameID    // The name ID for entries in the 'name' table that provide a display string for this attribute value.
	value       Float1616 // A numeric value for this attribute value.
}

type AxisValue2 struct {
	format        uint16    `unionTag:"2"` // Format identifier — set to 2.
	axisIndex     uint16    // Zero-base index into the axis record array identifying the axis of design variation to which the axis value table applies. Must be less than designAxisCount.
	flags         uint16    // Flags — see below for details.
	valueNameID   NameID    // The name ID for entries in the 'name' table that provide a display string for this attribute value.
	nominalValue  Float1616 // A nominal numeric value for this attribute value.
	rangeMinValue Float1616 // The minimum value for a range associated with the specified name ID.
	rangeMaxValue Float1616 // The maximum value for a range associated with the specified name ID
}

type AxisValue3 struct {
	format      uint16    `unionTag:"3"` // Format identifier — set to 3.
	axisIndex   uint16    // Zero-base index into the axis record array identifying the axis of design variation to which the axis value table applies. Must be less than designAxisCount.
	flags       uint16    // Flags — see below for details.
	valueNameID NameID    // The name ID for entries in the 'name' table that provide a display string for this attribute value.
	value       Float1616 // A numeric value for this attribute value.
	linkedValue Float1616 // The numeric value for a style-linked mapping from this value.
}

type AxisValue4 struct {
	format      uint16            `unionTag:"4"` //	Format identifier — set to 4.
	axisCount   uint16            // The total number of axes contributing to this axis-values combination.
	flags       uint16            // Flags — see below for details.
	valueNameID NameID            // The name ID for entries in the 'name' table that provide a display string for this combination of axis values.
	axisValues  []AxisValueRecord `arrayCount:"ComputedField-axisCount"` //[axisCount]	Array of AxisValue records that provide the combination of axis values, one for each contributing axis.
}

type AxisValueRecord struct {
	axisIndex uint16    //	Zero-base index into the axis record array identifying the axis to which this value applies. Must be less than designAxisCount.
	value     Float1616 //	A numeric value for this attribute value.
}
