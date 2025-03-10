// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

// Package opentype provides the low level routines
// required to read and write Opentype font files, including collections.
//
// This package is designed to provide an efficient, lazy, reading API.
//
// For the parsing of the various tables, see package [tables].
package opentype

type Tag uint32

// NewTag returns the tag for <abcd>.
func NewTag(a, b, c, d byte) Tag {
	return Tag(uint32(d) | uint32(c)<<8 | uint32(b)<<16 | uint32(a)<<24)
}

// MustNewTag gives you the Tag corresponding to the acronym.
// This function will panic if the string passed in is not 4 bytes long.
func MustNewTag(str string) Tag {
	if len(str) != 4 {
		panic("invalid tag: must be exactly 4 bytes")
	}
	_ = str[3]
	return NewTag(str[0], str[1], str[2], str[3])
}

// String return the ASCII form of the tag.
func (t Tag) String() string {
	return string([]byte{
		byte(t >> 24),
		byte(t >> 16),
		byte(t >> 8),
		byte(t),
	})
}

type GID uint32

type GlyphExtents struct {
	XBearing float32 // Left side of glyph from origin
	YBearing float32 // Top side of glyph from origin
	Width    float32 // Distance from left to right side
	Height   float32 // Distance from top to bottom side
}

type SegmentOp uint8

const (
	SegmentOpMoveTo SegmentOp = iota
	SegmentOpLineTo
	SegmentOpQuadTo
	SegmentOpCubeTo
)

type SegmentPoint struct {
	X, Y float32 // expressed in fonts units
}

// Move translates the point.
func (pt *SegmentPoint) Move(dx, dy float32) {
	pt.X += dx
	pt.Y += dy
}

type Segment struct {
	Op SegmentOp
	// Args is up to three (x, y) coordinates, depending on the
	// operation.
	// The Y axis increases up.
	Args [3]SegmentPoint
}

// ArgsSlice returns the effective slice of points
// used (whose length is between 1 and 3).
func (s *Segment) ArgsSlice() []SegmentPoint {
	switch s.Op {
	case SegmentOpMoveTo, SegmentOpLineTo:
		return s.Args[0:1]
	case SegmentOpQuadTo:
		return s.Args[0:2]
	case SegmentOpCubeTo:
		return s.Args[0:3]
	default:
		panic("unreachable")
	}
}
