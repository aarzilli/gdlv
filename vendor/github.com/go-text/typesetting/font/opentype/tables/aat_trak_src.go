// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package tables

// Trak is the tracking table.
// See - https://developer.apple.com/fonts/TrueType-Reference-Manual/RM06/Chap6trak.html
type Trak struct {
	version  uint32    // Version number of the tracking table (0x00010000 for the current version).
	format   uint16    // Format of the tracking table (set to 0).
	Horiz    TrackData `offsetSize:"Offset16"` // Offset from start of tracking table to TrackData for horizontal text (or 0 if none).
	Vert     TrackData `offsetSize:"Offset16"` // Offset from start of tracking table to TrackData for vertical text (or 0 if none).
	reserved uint16    // Reserved. Set to 0.
}

// IsEmpty return `true` it the table has no entries.
func (t Trak) IsEmpty() bool {
	return len(t.Horiz.TrackTable)+len(t.Vert.TrackTable) == 0
}

type TrackData struct {
	nTracks    uint16            // Number of separate tracks included in this table.
	nSizes     uint16            // Number of point sizes included in this table.
	SizeTable  []Float1616       `offsetSize:"Offset32" offsetRelativeTo:"Parent" arrayCount:"ComputedField-nSizes"` // Offset from start of the tracking table to the start of the size subtable.
	TrackTable []TrackTableEntry `arrayCount:"ComputedField-nTracks" arguments:"perSizeTrackingCount=.nSizes"`       // Array[nTracks] of TrackTableEntry records.
}

// GetTracking selects the tracking for the given `track` and applies it
// for `ptem`. It returns 0 if not found.
func (td TrackData) GetTracking(ptem float32, track float32) float32 {
	count := len(td.TrackTable)
	if count == 0 {
		return 0
	} else if count == 1 {
		return td.TrackTable[0].value(ptem, td.SizeTable)
	}

	// At least two entries.

	i := 0
	j := count - 1

	// Find the two entries that track is between.
	for i+1 < count && td.TrackTable[i+1].Track <= track {
		i++
	}
	for j > 0 && td.TrackTable[j-1].Track >= track {
		j--
	}

	// Exact match.
	if i == j {
		return td.TrackTable[i].value(ptem, td.SizeTable)
	}

	// Interpolate.

	t0 := td.TrackTable[i].Track
	t1 := td.TrackTable[j].Track

	t := (track - t0) / (t1 - t0)

	a := td.TrackTable[i].value(ptem, td.SizeTable)
	b := td.TrackTable[j].value(ptem, td.SizeTable)
	return a + t*(b-a)
}

type TrackTableEntry struct {
	Track           Float1616 // Track value for this record.
	NameIndex       uint16    // The 'name' table index for this track (a short word or phrase like "loose" or "very tight"). NameIndex has a value greater than 255 and less than 32768.
	PerSizeTracking []int16   `offsetSize:"Offset16" offsetRelativeTo:"GrandParent"` // in font units, with length len(SizeTable)
}

func (entry *TrackTableEntry) value(ptem float32, sizeTable []Float1616) float32 {
	values := entry.PerSizeTracking
	nSizes := len(sizeTable)

	// Choose size.
	if nSizes == 0 {
		return 0
	}
	if nSizes == 1 {
		return float32(values[0])
	}

	// At least two entries.

	var i int
	for i = 0; i < nSizes; i++ {
		if sizeTable[i] >= ptem {
			break
		}
	}

	// Boundary conditions.
	if i == 0 {
		return float32(values[0])
	}
	if i == nSizes {
		return float32(values[nSizes-1])
	}

	// Exact match.
	if sizeTable[i] == ptem {
		return float32(values[i])
	}

	// Interpolate.
	return entry.interpolateAt(i-1, ptem, sizeTable)
}

// idx is assumed to verify idx <= len(sizeTable) - 2
func (td *TrackTableEntry) interpolateAt(idx int, ptem float32, sizeTable []Float1616) float32 {
	values := td.PerSizeTracking

	s0 := sizeTable[idx]
	s1 := sizeTable[idx+1]
	v0 := float32(values[idx])
	v1 := float32(values[idx+1])

	// Deal with font bugs.
	if s1 < s0 {
		s0, s1 = s1, s0
		v0, v1 = v1, v0
	}
	if ptem < s0 {
		return v0
	}
	if ptem > s1 {
		return v1
	}
	if s0 == s1 {
		return (v0 + v1) * 0.5
	}

	t := (ptem - s0) / (s1 - s0)
	return v0 + t*(v1-v0)
}
