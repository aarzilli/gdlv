// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package tables

import "fmt"

// PostScript table
// See https://learn.microsoft.com/en-us/typography/opentype/spec/post
type Post struct {
	version     postVersion
	italicAngle uint32
	// UnderlinePosition is the suggested distance of the top of the
	// underline from the baseline (negative values indicate below baseline).
	UnderlinePosition int16
	// Suggested values for the underline thickness.
	UnderlineThickness int16
	// IsFixedPitch indicates that the font is not proportionally spaced
	// (i.e. monospaced).
	IsFixedPitch uint32
	memoryUsage  [4]uint32
	Names        PostNames `unionField:"version"`
}

type PostNames interface {
	isPostNames()
}

func (PostNames10) isPostNames() {}
func (PostNames20) isPostNames() {}
func (PostNames30) isPostNames() {}

type postVersion uint32

const (
	postVersion10 postVersion = 0x00010000
	postVersion20 postVersion = 0x00020000
	postVersion30 postVersion = 0x00030000
)

type PostNames10 struct{}

type PostNames20 struct {
	GlyphNameIndexes []uint16 `arrayCount:"FirstUint16"` // size numGlyph
	Strings          []string `isOpaque:"" subsliceStart:"AtCurrent"`
}

// see https://learn.microsoft.com/en-us/typography/opentype/spec/post#version-20
func (ps *PostNames20) parseStrings(src []byte) error {
	// "Strings are in Pascal string format, meaning that the first byte of
	// a given string is a length: the number of characters in that string.
	// The length byte is not included; for example, a length byte of 8 indicates
	// that the 8 bytes following the length byte comprise the string character data."
	for i := 0; i < len(src); {
		length := int(src[i]) // read the length
		end := i + 1 + length
		if L := len(src); L < end {
			return fmt.Errorf("invalid Postscript names tables format 20: EOF: expected %d, got %d", end, L)
		}
		ps.Strings = append(ps.Strings, string(src[i+1:end]))
		i = end
	}
	return nil
}

type PostNames30 PostNames10
