package cff

import (
	"errors"

	"github.com/go-text/typesetting/font/opentype/tables"
)

//go:generate ../../../../../typesetting-utils/generators/binarygen/cmd/generator . _src.go

type header2 struct {
	majorVersion  uint8  //	Format major version. Set to 2.
	minorVersion  uint8  //	Format minor version. Set to zero.
	headerSize    uint8  //	Header size (bytes).
	topDictLength uint16 //	Length of Top DICT structure in bytes.
}

type indexStart struct {
	count   uint32 //	Number of objects stored in INDEX
	offSize uint8  //	Offset array element size
	// then
	// offset  []Offset
	// data    []byte
}

//lint:ignore U1000 this type is required so that the code generator add a ParseFdSelect function
type dummy struct {
	fd fdSelect
}

// fdSelect holds a CFF font's Font Dict Select data.
type fdSelect interface {
	isFdSelect()

	fontDictIndex(glyph tables.GlyphID) (byte, error)
	// return the maximum index + 1 (it's the length of an array
	// which can be safely indexed by the indexes)
	extent() int
}

func (fdSelect0) isFdSelect() {}
func (fdSelect3) isFdSelect() {}
func (fdSelect4) isFdSelect() {}

type fdSelect0 struct {
	format uint8   `unionTag:"0"` //	Set to 0
	fds    []uint8 // [nGlyphs]	FD selector array
}

var errGlyph = errors.New("invalid glyph index")

func (fds fdSelect0) fontDictIndex(glyph tables.GlyphID) (byte, error) {
	if int(glyph) >= len(fds.fds) {
		return 0, errGlyph
	}
	return fds.fds[glyph], nil
}

func (fds fdSelect0) extent() int {
	max := -1
	for _, b := range fds.fds {
		if int(b) > max {
			max = int(b)
		}
	}
	return max + 1
}

type fdSelect3 struct {
	format   uint8    `unionTag:"3"` //	Set to 3
	nRanges  uint16   //	Number of ranges
	ranges   []range3 `arrayCount:"ComputedField-nRanges"` // [nRanges]	Array of Range3 records (see below)
	sentinel uint16   //	Sentinel GID
}

type range3 struct {
	first tables.GlyphID //	First glyph index in range
	fd    uint8          //	FD index for all glyphs in range
}

func (fds fdSelect3) fontDictIndex(x tables.GlyphID) (byte, error) {
	lo, hi := 0, len(fds.ranges)
	for lo < hi {
		i := (lo + hi) / 2
		r := fds.ranges[i]
		xlo := r.first
		if x < xlo {
			hi = i
			continue
		}
		xhi := fds.sentinel
		if i < len(fds.ranges)-1 {
			xhi = fds.ranges[i+1].first
		}
		if xhi <= x {
			lo = i + 1
			continue
		}
		return r.fd, nil
	}
	return 0, errGlyph
}

func (fds fdSelect3) extent() int {
	max := -1
	for _, b := range fds.ranges {
		if int(b.fd) > max {
			max = int(b.fd)
		}
	}
	return max + 1
}

type fdSelect4 struct {
	format   uint8    `unionTag:"4"` //	Set to 4
	nRanges  uint32   //	Number of ranges
	ranges   []range4 `arrayCount:"ComputedField-nRanges"` // [nRanges]	Array of Range4 records (see below)
	sentinel uint32   //	Sentinel GID
}

type range4 struct {
	first uint32 //	First glyph index in range
	fd    uint16 //	FD index for all glyphs in range
}

func (fds fdSelect4) fontDictIndex(x tables.GlyphID) (byte, error) {
	fd, err := fds.fontDictIndex32(uint32(x))
	return byte(fd), err
}

func (fds fdSelect4) fontDictIndex32(x uint32) (uint16, error) {
	lo, hi := 0, len(fds.ranges)
	for lo < hi {
		i := (lo + hi) / 2
		r := fds.ranges[i]
		xlo := r.first
		if x < xlo {
			hi = i
			continue
		}
		xhi := fds.sentinel
		if i < len(fds.ranges)-1 {
			xhi = fds.ranges[i+1].first
		}
		if xhi <= x {
			lo = i + 1
			continue
		}
		return r.fd, nil
	}
	return 0, errGlyph
}

func (fds fdSelect4) extent() int {
	max := -1
	for _, b := range fds.ranges {
		if int(b.fd) > max {
			max = int(b.fd)
		}
	}
	return max + 1
}
