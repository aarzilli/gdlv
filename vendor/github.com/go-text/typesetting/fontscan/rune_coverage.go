package fontscan

import (
	"encoding/binary"
	"errors"
	"math/bits"
	"sort"

	"github.com/go-text/typesetting/font"
	"github.com/go-text/typesetting/language"
)

// Rune coverage implementation, inspired by the fontconfig FcCharset type.
//
// The internal representation is a slice of `pageSet` pages, where each page is a boolean
// set of size 256, encoding the last byte of a rune.
// Each rune is then mapped to a page index (`pageNumber`), defined by it second and third bytes.

// pageSet is the base storage for a compact rune set.
// A rune is first reduced to its lower byte 'b'. Then the index
// of 'b' in the page is given by the 3 high bits (from 0 to 7)
// and the position in the resulting uint32 is given by the 5 lower bits (from 0 to 31)
type pageSet [8]uint32

func (a pageSet) includes(b pageSet) bool {
	for j, aPage := range b {
		bPage := a[j]
		// Does b have any bits not in a?
		if aPage & ^bPage != 0 {
			return false
		}
	}
	return true
}

// pageRef stores the second and third bytes of a rune (uint16(r >> 8)),
// shared by all the runes in a page.
type pageRef = uint16

type runePage struct {
	ref pageRef
	set pageSet
}

// RuneSet is an efficient implementation of a rune set (that is a map[rune]bool),
// used to store the Unicode points supported by a font, and optimized to deal with consecutive
// runes.
type RuneSet []runePage

// newCoveragesFromCmap iterates through the given `cmap`
// to build the corresponding rune set.
// buffer may be provided to reduce allocations, and is returned
func newCoveragesFromCmap(cmap font.Cmap, buffer [][2]rune) (RuneSet, ScriptSet, [][2]rune) {
	if ranger, ok := cmap.(font.CmapRuneRanger); ok { // use the fast range implementation
		return newCoveragesFromCmapRange(ranger, buffer)
	}

	// use the slower rune by rune API
	var (
		rs RuneSet
		ss ScriptSet
	)
	iter := cmap.Iter()
	for iter.Next() {
		r, _ := iter.Char()
		rs.Add(r)
		ss.insert(language.LookupScript(r))
	}
	return rs, ss, buffer
}

// assume a <= b
func addRangeToPage(page *pageSet, start, end byte) {
	// indexes in [0; 8[
	uintIndexStart := start >> 5
	uintIndexEnd := end >> 5

	// bit index, in [0; 32[
	bitIndexStart := (start & 0x1f)
	bitIndexEnd := (end & 0x1f)

	// handle the start uint
	bitEnd := byte(31)
	if uintIndexEnd == uintIndexStart {
		bitEnd = bitIndexEnd
	}
	b := &page[uintIndexStart]
	alt := (uint32(1)<<(bitEnd-bitIndexStart+1) - 1) << bitIndexStart // mask for bits from a to b (included)
	*b |= alt

	// handle the end uint, when required
	if uintIndexEnd != uintIndexStart {
		// fill uint between with ones
		for index := uintIndexStart + 1; index < uintIndexEnd; index++ {
			page[index] = 0xFFFFFFFF
		}

		// handle the last
		b := &page[uintIndexEnd]
		alt := (uint32(1)<<(bitIndexEnd+1) - 1) // mask for bits from a to b (included)
		*b |= alt
	}
}

// newCoveragesFromCmapRange iterates through the given `cmap`
// to build the corresponding rune set.
func newCoveragesFromCmapRange(cmap font.CmapRuneRanger, buffer [][2]rune) (RuneSet, ScriptSet, [][2]rune) {
	buffer = cmap.RuneRanges(buffer)

	ss := scriptsFromRanges(buffer)

	var rs RuneSet
	lastPage := &runePage{ref: 0xFFFF} // start with an invalid sentinel value
	for _, ra := range buffer {
		start, end := ra[0], ra[1]

		pageStart, pageEnd := uint16(start>>8), uint16(end>>8)

		// handle the starting page
		startByte, endByte := byte(start&0xff), byte(end&0xff)
		endByteClamped := byte(0xFF)
		if pageEnd == pageStart {
			endByteClamped = endByte
		}

		// check if we can reuse the last page
		var leaf *pageSet
		if pageStart == lastPage.ref { // use the same page
			leaf = &lastPage.set
		} else {
			rs = append(rs, runePage{ref: pageStart})
			leaf = &rs[len(rs)-1].set
		}
		addRangeToPage(leaf, startByte, endByteClamped)

		// handle the next
		if pageEnd != pageStart { // this means pageStart < pageEnd
			// fill the strictly intermediate pages with ones
			for pageIndex := pageStart + 1; pageIndex < pageEnd; pageIndex++ {
				rs = append(rs, runePage{
					ref: pageIndex,
					set: pageSet{0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF},
				})
			}

			// hande the last
			rs = append(rs, runePage{ref: pageEnd})
			leaf = &rs[len(rs)-1].set
			addRangeToPage(leaf, 0, endByte)
		}

		lastPage = &rs[len(rs)-1]
	}
	return rs, ss, buffer
}

// findPageFrom is the same as findPagePos, but
// start the binary search with the given `low` index
func (rs RuneSet) findPageFrom(low int, ref pageRef) int {
	high := len(rs) - 1
	for low <= high {
		mid := (low + high) >> 1
		page := rs[mid].ref
		if page == ref {
			return mid // found the page
		}
		if page < ref {
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	if high < 0 || (high < len(rs) && rs[high].ref < ref) {
		high++
	}
	return -(high + 1) // the page is not in the set, but should be inserted at high
}

// findPagePos searches for the leaf containing the specified number.
// It returns its index if it exists, otherwise it returns the negative of
// the (`position` + 1) where `position` is the index where it should be inserted
func (rs RuneSet) findPagePos(page pageRef) int { return rs.findPageFrom(0, page) }

// findPage returns the page containing the specified char, or nil
// if it doesn't exists
func (rs RuneSet) findPage(ref pageRef) *pageSet {
	pos := rs.findPagePos(ref)
	if pos >= 0 {
		return &rs[pos].set
	}
	return nil
}

// findOrCreatePage locates the page containing the specified char, creating it if needed,
// and returns a pointer to it
func (rs *RuneSet) findOrCreatePage(ref pageRef) *pageSet {
	pos := rs.findPagePos(ref)
	if pos < 0 { // the page doest not exists, create it
		pos = -pos - 1
		rs.insertPage(runePage{ref: ref}, pos)
	}

	return &(*rs)[pos].set
}

// insertPage inserts the given `page` at `pos`, meaning the resulting page can be accessed via &rs[pos]
func (rs *RuneSet) insertPage(page runePage, pos int) {
	// insert in slice
	*rs = append(*rs, runePage{})
	copy((*rs)[pos+1:], (*rs)[pos:])
	(*rs)[pos] = page
}

// Add adds `r` to the rune set.
func (rs *RuneSet) Add(r rune) {
	leaf := rs.findOrCreatePage(uint16(r >> 8))
	b := &leaf[(r&0xff)>>5] // (r&0xff)>>5 is the index in the page
	*b |= (1 << (r & 0x1f)) // r & 0x1f is the bit in the uint32
}

// Delete removes the rune from the rune set.
func (rs RuneSet) Delete(r rune) {
	leaf := rs.findPage(uint16(r >> 8))
	if leaf == nil {
		return
	}
	b := &leaf[(r&0xff)>>5]  // (r&0xff)>>5 is the index in the page
	*b &= ^(1 << (r & 0x1f)) // r & 0x1f is the bit in the uint32
	// we don't bother removing the leaf if it's empty
}

// Contains returns `true` if `r` is in the set.
func (rs RuneSet) Contains(r rune) bool {
	leaf := rs.findPage(uint16(r >> 8))
	if leaf == nil {
		return false
	}
	return leaf[(r&0xff)>>5]&(1<<(r&0x1f)) != 0
}

// return true iff a includes b, that is if b is a subset of a, that is if all runes
// of b are in a
func (a RuneSet) includes(b RuneSet) bool {
	bi, ai := 0, 0 // index in b and a
	for bi < len(b) && ai < len(a) {
		bEntry, aEntry := b[bi], a[ai]
		// Check matching pages
		if bEntry.ref == aEntry.ref {
			if ok := aEntry.set.includes(bEntry.set); !ok {
				return false
			}
			bi++
			ai++
		} else if bEntry.ref < aEntry.ref { // Does b have any pages not in a?
			return false
		} else {
			// increment ai to match the page of b
			ai = a.findPageFrom(ai+1, bEntry.ref)
			if ai < 0 { // the page is not even in a
				return false
			}
		}
	}
	//  did we look at every page?
	return bi >= len(b)
}

// Len returns the number of runes in the set.
func (a RuneSet) Len() int {
	count := 0
	for _, page := range a {
		for _, am := range page.set {
			count += bits.OnesCount32(am)
		}
	}
	return count
}

const runePageSize = 2 + 8*4 // uint16 + 8 * uint32

// serialize serializes the rune coverage in binary format
func (rs RuneSet) serialize() []byte {
	buffer := make([]byte, 2+runePageSize*len(rs))
	binary.BigEndian.PutUint16(buffer, uint16(len(rs)))
	for i, page := range rs {
		binary.BigEndian.PutUint16(buffer[2+runePageSize*i:], page.ref)
		slice := buffer[2+runePageSize*i+2:]
		for j, k := range page.set {
			binary.BigEndian.PutUint32(slice[4*j:], k)
		}
	}
	return buffer
}

// deserializeFrom reads the binary format produced by serialize.
// it returns the number of bytes read from `data`
func (rs *RuneSet) deserializeFrom(data []byte) (int, error) {
	if len(data) < 2 {
		return 0, errors.New("invalid rune set (EOF)")
	}
	L := int(binary.BigEndian.Uint16(data))
	if len(data) < 2+runePageSize*L {
		return 0, errors.New("invalid rune set size (EOF)")
	}
	v := make(RuneSet, L)
	for i := range v {
		v[i].ref = binary.BigEndian.Uint16(data[2+runePageSize*i:])
		slice := data[2+runePageSize*i+2:]
		for j := range v[i].set {
			v[i].set[j] = binary.BigEndian.Uint32(slice[4*j:])
		}
	}

	*rs = v

	return 2 + runePageSize*L, nil
}

// ScriptSet is a set of scripts, implemented as
// a sorted slice of unique, increasing scripts
type ScriptSet []language.Script

// contains returns true is [s] is in the set.
func (ss ScriptSet) contains(s language.Script) bool {
	// we use a linear search since in practice the script sets are
	// rather small (see BenchmarkScriptSet_contains)
	for _, script := range ss {
		if script > s { // optimization, using the fact the slice is increasing
			return false
		}
		if script == s {
			return true
		}
	}
	return false
}

// insert adds the given script to the set if it is not already present.
func (ss *ScriptSet) insert(newScript language.Script) {
	scriptIdx := sort.Search(len([]language.Script(*ss)), func(i int) bool {
		return (*ss)[i] >= newScript
	})
	if scriptIdx != len(*ss) && (*ss)[scriptIdx] == newScript {
		return
	}
	// Grow the slice if necessary.
	startLen := len(*ss)
	*ss = append(*ss, language.Script(0))[:startLen]
	// Shift all elements from scriptIdx onward to the right one position.
	*ss = append((*ss)[:scriptIdx+1], (*ss)[scriptIdx:]...)
	// Insert newScript at the correct position.
	(*ss)[scriptIdx] = newScript
}

const scriptSize = 4

// serialize serializes the script set in binary format
func (ss ScriptSet) serialize() []byte {
	buffer := make([]byte, 1+scriptSize*len(ss))
	buffer[0] = byte(len(ss)) // there are about 190 scripts, a byte is enough
	for i, script := range ss {
		binary.BigEndian.PutUint32(buffer[1+scriptSize*i:], uint32(script))
	}
	return buffer
}

// deserializeFrom reads the binary format produced by serialize
// it returns the number of bytes read from `data`
func (ss *ScriptSet) deserializeFrom(data []byte) (int, error) {
	if len(data) < 1 {
		return 0, errors.New("invalid Script set (EOF)")
	}
	L := int(data[0])
	if len(data) < 1+scriptSize*L {
		return 0, errors.New("invalid Script set size (EOF)")
	}
	v := make(ScriptSet, L)
	for i := range v {
		v[i] = language.Script(binary.BigEndian.Uint32(data[1+scriptSize*i:]))
	}

	*ss = v

	return 1 + scriptSize*L, nil
}

// scriptsFromRanges returns the set of scripts used in [ranges],
// which must be sorted (in ascending order), and have inclusive bounds.
func scriptsFromRanges(ranges [][2]rune) ScriptSet {
	const LR = uint(len(language.ScriptRanges))
	out := make(ScriptSet, 0, 2)

	var hasUnknown bool
	// we leverage the fact that both ranges and scriptRanges are sorted
	// to loop through both slices at the same time
	indexS := uint(0) // index in scriptRanges
	for _, ra := range ranges {
		start, end := ra[0], ra[1]

		// advance, skipping the items entirely to the left of 'ra'
		for indexS < LR && language.ScriptRanges[indexS].End < start {
			indexS++
		}

		if indexS >= LR {
			// the incomming ranges are higher than known scripts :
			// add Unknown and break early
			out.insert(language.Unknown)
			break
		}

		// loop through the 'interesting' items,
		// that is the ones with item.Start <= end
		for indexS < LR {
			item := language.ScriptRanges[indexS]
			if item.Start > end {
				// check for Unknown
				if !hasUnknown && indexS > 0 {
					previousItem := language.ScriptRanges[indexS-1]
					if previousItem.End < end { // then previousItem.End < end < item.Start
						out.insert(language.Unknown)
						hasUnknown = true
					}
				}
				break
			}

			// here, item.End >= start and item.Start <= end

			// detect the Unknown script;
			// as an optimization, skip the check if we have already found it
			if !hasUnknown && indexS > 0 {
				previousItem := language.ScriptRanges[indexS-1]
				// do we have a gap between items, inside the range
				if previousItem.End+1 < item.Start && item.Start > start {
					out.insert(language.Unknown)
					hasUnknown = true
				}
			}

			// since item.End >= start and item.Start <= end,
			// 'item' and 'ra' have an intersection : add the script
			out.insert(item.Script)

			indexS++
		}

		if indexS >= LR {
			// the incomming ranges are higher than known scripts :
			// add Unknown and break early
			out.insert(language.Unknown)
			break
		}
	}

	return out
}
