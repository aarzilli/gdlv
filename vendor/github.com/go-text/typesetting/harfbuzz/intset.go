package harfbuzz

import (
	"math/bits"
)

// Rune/GID set implementation, inspired by the fontconfig FcCharset type.
//
// The internal representation is a slice of `pageSet` pages, where each page is a boolean
// set of size 256, encoding the last byte of an uint32.
// Each uint32 is then mapped to a page index (`pageNumber`), defined by it second and third bytes.

// pageSet is the base storage for a compact uint32 set.
// A uint32 is first reduced to its lower byte 'b'. Then the index
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

func (a pageSet) intersects(b pageSet) bool {
	for j, aPage := range b {
		bPage := a[j]
		// Does a and b have any bits in common ?
		if aPage&bPage != 0 {
			return true
		}
	}
	return false
}

// assume start <= end
func (page *pageSet) addRange(start, end byte) {
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

// pageRef stores the second and third bytes of a rune (uint16(r >> 8)),
// shared by all the runes in a page.
type pageRef = uint16

type intPage struct {
	ref pageRef
	set pageSet
}

// intSet is an efficient implementation of a rune set (that is a map[rune]bool),
// used to store the Unicode points (typically supported by a font), and optimized to deal with consecutive
// runes.
type intSet []intPage

// findPageFrom is the same as findPagePos, but
// start the binary search with the given `low` index
func (rs intSet) findPageFrom(low int, ref pageRef) int {
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
func (rs intSet) findPagePos(page pageRef) int { return rs.findPageFrom(0, page) }

// findPage returns the page containing the specified char, or nil
// if it doesn't exists
func (rs intSet) findPage(ref pageRef) *pageSet {
	pos := rs.findPagePos(ref)
	if pos >= 0 {
		return &rs[pos].set
	}
	return nil
}

// findOrCreatePage locates the page containing the specified char, creating it if needed,
// and returns a pointer to it
func (rs *intSet) findOrCreatePage(ref pageRef) *pageSet {
	pos := rs.findPagePos(ref)
	if pos < 0 { // the page doest not exists, create it
		pos = -pos - 1
		rs.insertPage(intPage{ref: ref}, pos)
	}

	return &(*rs)[pos].set
}

// insertPage inserts the given `page` at `pos`, meaning the resulting page can be accessed via &rs[pos]
func (rs *intSet) insertPage(page intPage, pos int) {
	// insert in slice
	*rs = append(*rs, intPage{})
	copy((*rs)[pos+1:], (*rs)[pos:])
	(*rs)[pos] = page
}

// add adds `r` to the rune set.
func (rs *intSet) add(r uint32) {
	leaf := rs.findOrCreatePage(uint16(r >> 8))
	b := &leaf[(r&0xff)>>5] // (r&0xff)>>5 is the index in the page
	*b |= (1 << (r & 0x1f)) // r & 0x1f is the bit in the uint32
}

// delete removes the rune from the rune set.
func (rs intSet) delete(r uint32) {
	leaf := rs.findPage(uint16(r >> 8))
	if leaf == nil {
		return
	}
	b := &leaf[(r&0xff)>>5]  // (r&0xff)>>5 is the index in the page
	*b &= ^(1 << (r & 0x1f)) // r & 0x1f is the bit in the uint32
	// we don't bother removing the leaf if it's empty
}

// has returns `true` if `r` is in the set.
func (rs intSet) has(r uint32) bool {
	leaf := rs.findPage(uint16(r >> 8))
	if leaf == nil {
		return false
	}
	return leaf[(r&0xff)>>5]&(1<<(r&0x1f)) != 0
}

// Clear remove all values but keep the underlying storage
func (rs *intSet) Clear() { *rs = (*rs)[:0] }

// Includes return true iff a includes b, that is if b is a subset of a, that is if all runes
// of b are in a
func (a intSet) Includes(b intSet) bool {
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
func (a intSet) Len() int {
	count := 0
	for _, page := range a {
		for _, am := range page.set {
			count += bits.OnesCount32(am)
		}
	}
	return count
}

// Intersects returns if [a] and [b] have at least
// one rune in common
func (a intSet) Intersects(b intSet) bool {
	bi, ai := 0, 0 // index in b and a
	for bi < len(b) && ai < len(a) {
		bEntry, aEntry := b[bi], a[ai]
		// Check matching pages
		if bEntry.ref == aEntry.ref {
			if aEntry.set.intersects(bEntry.set) {
				return true
			}
			bi++
			ai++
		} else if bEntry.ref < aEntry.ref {
			// increment bi
			bi++
		} else {
			// increment ai
			ai++
		}
	}
	return false
}

// assume a <= b; inclusive
func (s *intSet) addRange(start, end uint32) {
	pageStart, pageEnd := uint16(start>>8), uint16(end>>8)

	// handle the starting page
	startByte, endByte := byte(start&0xff), byte(end&0xff)
	endByteClamped := byte(0xFF)
	if pageEnd == pageStart {
		endByteClamped = endByte
	}

	leaf := s.findOrCreatePage(pageStart)
	leaf.addRange(startByte, endByteClamped)

	// handle the next
	if pageEnd != pageStart { // this means pageStart < pageEnd
		// fill the strictly intermediate pages with ones
		for pageIndex := pageStart + 1; pageIndex < pageEnd; pageIndex++ {
			leaf := s.findOrCreatePage(pageIndex)
			*leaf = pageSet{0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF}
		}

		// hande the last
		leaf := s.findOrCreatePage(pageEnd)
		leaf.addRange(0, endByte)
	}
}

// ints is an helper method returning a copy of the runes in the set.
func (rs intSet) ints() (out []uint32) {
	for _, page := range rs {
		pageLow := uint32(page.ref) << 8
		for j, set := range page.set {
			for k := uint32(0); k < 32; k++ {
				if set&uint32(1<<k) != 0 {
					out = append(out, pageLow|uint32(j)<<5|k)
				}
			}
		}
	}
	return out
}

// typed API

func (s intSet) hasGlyph(g GID) bool { return s.has(uint32(g)) }
func (s *intSet) addGlyph(g GID)     { s.add(uint32(g)) }
func (s *intSet) addGlyphs(gs []GID) {
	for _, g := range gs {
		s.add(uint32(g))
	}
}
