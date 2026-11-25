// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

// Package segmenter implements Unicode rules used
// to segment a paragraph of text according to several criteria.
// In particular, it provides a way of delimiting line break opportunities.
//
// The API of the package follows the very nice iterator pattern proposed
// in github.com/npillmayer/uax,
// but use a somewhat simpler internal implementation, inspired by Pango.
//
// The reference documentation is at https://unicode.org/reports/tr14
// and https://unicode.org/reports/tr29.
package segmenter

import (
	"fmt"
	"unicode/utf8"

	ucd "github.com/go-text/typesetting/internal/unicodedata"
)

// breakAttr is a flag storing the break properties between two runes of
// the input text.
type breakAttr uint8

const (
	lineBoundary          breakAttr = 1 << iota
	mandatoryLineBoundary           // implies LineBoundary

	// graphemeBoundary is on if the cursor can appear in front of a character,
	// i.e. if we are at a grapheme boundary.
	graphemeBoundary

	// wordBoundary is on if we are at the beginning or end of a word.
	//
	// To actually detect words, you should also look for runes
	// with the [Alphabetic] property, or with a General_Category of Number.
	//
	// See also https://unicode.org/reports/tr29/#Word_Boundary_Rules,
	// http://unicode.org/reports/tr44/#Alphabetic and
	// http://unicode.org/reports/tr44/#General_Category_Values
	wordBoundary
)

const paragraphSeparator rune = 0x2029

// cursor holds the information for the current index
// processed by `computeAttributes`, that is
// the context provided by previous and next runes in the text
type cursor struct {
	index, len    int  // index of the current rune and text length, used for sot and eot
	isPreviousSot bool // are we after sot, applying LB9 and LB10 ?

	prevPrev rune // the rune at index i-2
	prev     rune // the rune at index i-1
	r        rune // the rune at index i
	next     rune // the rune at index i+1

	// is r included in `ucd.Extended_Pictographic`,
	// cached for efficiency
	isExtentedPic bool

	// the following fields persists across iterations

	prevGeneralCategory ucd.GeneralCategory // for prev
	generalCategory     ucd.GeneralCategory // for r

	prevGrapheme ucd.GraphemeBreak // the Grapheme Break property at index i-1
	grapheme     ucd.GraphemeBreak // the Grapheme Break property at index i

	// true if the `prev` rune was an odd Regional_Indicator, false if it was even or not an RI
	// used for rules GB12 and GB13
	// see [updateGraphemeRIOdd]
	isPrevGraphemeRIOdd bool

	prevPrevWord     ucd.WordBreak // the Word Break property at the previous previous, non Extend rune
	prevWord         ucd.WordBreak // the Word Break property at the previous, non Extend rune
	word             ucd.WordBreak // the Word Break property at index i
	prevWordNoExtend int           // the index of the last rune NOT having a Extend word break property

	// true if the `prev` rune was an odd Regional_Indicator, false if it was even or not an RI
	// used for rules WB15 and WB16
	// see [updateWordRIOdd]
	isPrevWordRIOdd bool

	prevPrevLine           ucd.LineBreak // the Line Break Class at index i-2 (see rules LB9 and LB10 for edge cases)
	prevLine               ucd.LineBreak // the Line Break Class at index i-1 (see rules LB9 and LB10 for edge cases)
	prevLineRaw            ucd.LineBreak // always for prev (index i-1), despite LB9 and LB10
	line                   ucd.LineBreak // the Line Break Class at index i
	nextLine               ucd.LineBreak // the Line Break Class at index i+1
	isPrevPrevDottedCircle bool          // following LB9 and LB10
	isPrevDottedCircle     bool          // following LB9 and LB10

	isPrevNonAssignedExtendedPic bool // following LB9 and LB10

	// the last rune before spaces, used in rules LB14,LB15,LB16,LB17
	// to match ... SP* ...
	prevBeforeSpaces, beforeSpaces         rune
	prevBeforeSpacesLine, beforeSpacesLine ucd.LineBreak
	beforeSpaceLineRaw                     ucd.LineBreak // do not follow LB9 and LB10
	beforeSpacesIndex                      int

	// true if the `prev` rune was an odd Regional_Indicator, false if it was even or not an RI
	// used for rules LB30a
	isPrevLinebreakRIOdd bool

	// cached value of ucd.LookupIndicConjunctBreak(cr.r)
	indicConjunctBreak ucd.IndicConjunctBreak

	// are we in a numeric sequence, as defined in Example 7 of customisation for LB25
	numSequence numSequenceState

	// are we in an emoji sequence, as defined in rule GB11
	// see [updatePictoSequence]
	pictoSequence pictoSequenceState

	// are we in an indic sequence, as defined in rule GB9c
	// see [updateIndicConjunctBreakSequence]
	indicConjunctBreakSequence indicCBSequenceState
}

// initialise the cursor properties
// some of them are set in [startIteration]
func newCursor(text []rune) cursor {
	cr := cursor{
		len:              len(text),
		prevWordNoExtend: -1,
	}

	// `startIteration` set `breakCl` from `nextBreakCl`
	// so we need to init this field before the first iteration
	if len(text) != 0 {
		cr.nextLine = ucd.LookupLineBreak(text[0])
	}
	return cr
}

// computeBreakAttributes does the heavy lifting of text segmentation,
// by computing a break attribute for each rune.
//
// More precisely, `attributes` must be a slice of length len(text)+1,
// which will be filled at index i by the attribute describing the
// break between rune at index i-1 and index i.
//
// Unicode defines a lot of properties; for now we only handle
// grapheme, word and line breaking.
func computeBreakAttributes(text []rune, attributes []breakAttr) {
	// The rules are somewhat complex, but the general logic is pretty simple:
	// iterate through the input slice, fetch context information
	// from previous and following runes required by the rules,
	// and finaly apply them.
	// Some rules require variable length lookup, which we handle by keeping
	// a state in a [cursor] object.

	// initialise the cursor properties
	cr := newCursor(text)

	for i := 0; i <= len(text); i++ { // note that we accept i == len(text) to fill the last attribute
		cr.startIteration(text, i)

		var attr breakAttr

		// UAX#29 Grapheme and word Boundaries

		isGraphemeBoundary := cr.applyGraphemeBoundaryRules()
		if isGraphemeBoundary {
			attr |= graphemeBoundary
		}

		isWordBoundary, removePrevNoExtend := cr.applyWordBoundaryRules(i)
		if isWordBoundary {
			attr |= wordBoundary
		}
		if removePrevNoExtend {
			attributes[cr.prevWordNoExtend] &^= wordBoundary
		}

		// UAX#14 Line Breaking

		bo := cr.applyLineBoundaryRules()
		switch bo {
		case breakEmpty:
			// rule LB31 : default to allow line break
			attr |= lineBoundary
		case breakProhibited:
			attr &^= lineBoundary
		case breakAllowed:
			attr |= lineBoundary
		case breakMandatory:
			attr |= lineBoundary
			attr |= mandatoryLineBoundary
		}

		cr.endIteration()

		attributes[i] = attr
	}

	// start and end of the paragraph are always
	// grapheme boundaries and word boundaries
	attributes[0] |= graphemeBoundary | wordBoundary         // Rule GB1 and WB1
	attributes[len(text)] |= graphemeBoundary | wordBoundary // Rule GB2 and WB2

	// never break before the first char,
	// but always break after the last
	attributes[0] &^= lineBoundary                 // Rule LB2
	attributes[len(text)] |= lineBoundary          // Rule LB3
	attributes[len(text)] |= mandatoryLineBoundary // Rule LB3
}

// Segmenter is the entry point of the package.
//
// Usage :
//
//	var seg Segmenter
//	seg.Init(...)
//	iter := seg.LineIterator()
//	for iter.Next() {
//	  ... // do something with iter.Line()
//	}
type Segmenter struct {
	text []rune
	// with length len(text) + 1 :
	// the attribute at indice i is about the
	// rune at i-1 and i.
	// See also [ComputeBreakAttributes]
	// Example :
	// 	text : 			[b, 		u, 	l, 	l]
	// 	attributes :	[<start> b, b u, u l, l l, l <end>]
	attributes []breakAttr
}

// Init resets the segmenter storage with the given input,
// and computes the attributes required to segment the text.
//
// If paragraph includes an invalid rune like out of range, some outputs like
// [Line.OffsetInBytes] and [Line.LengthInBytes] are undefined.
func (seg *Segmenter) Init(paragraph []rune) {
	seg.text = append(seg.text[:0], paragraph...)
	seg.initAttributes()
}

// InitWithString resets the segmenter storage with the given string input,
// and computes the attributes required to segment the text.
//
// InitWithString returns an error if paragraph includes an invalid UTF-8 sequence.
//
// InitWithString is more efficient than [Init] if the input is a string.
// No allocation for the text is made if its internal buffer capacity is already large enough.
func (seg *Segmenter) InitWithString(paragraph string) (err error) {
	defer func() {
		if err != nil {
			seg.text = seg.text[:0]
			seg.attributes = seg.attributes[:0]
		}
	}()

	seg.text = seg.text[:0]
	for i, r := range paragraph {
		if r == utf8.RuneError {
			// Check whether the rune is acually U+FFFD, or an invalid UTF-8 sequence.
			if r, l := utf8.DecodeRuneInString(paragraph[i:]); r == utf8.RuneError && l == 1 {
				return fmt.Errorf("invalid UTF-8 sequence at index %d", i)
			}
		}
		seg.text = append(seg.text, r)
	}
	seg.initAttributes()
	return nil
}

// InitWithBytes resets the segmenter storage with the given byte slice input,
// and computes the attributes required to segment the text.
//
// InitWithBytes returns an error if paragraph includes an invalid UTF-8 sequence.
//
// InitWithBytes is more efficient than [Init] if the input is a byte slice.
// No allocation for the text is made if its internal buffer capacity is already large enough.
func (seg *Segmenter) InitWithBytes(paragraph []byte) (err error) {
	defer func() {
		if err != nil {
			seg.text = seg.text[:0]
			seg.attributes = seg.attributes[:0]
		}
	}()

	seg.text = seg.text[:0]
	// The Go compiler should optimize this without allocating a string.
	for i, r := range string(paragraph) {
		if r == utf8.RuneError {
			// Check whether the rune is acually U+FFFD, or an invalid UTF-8 sequence.
			if r, l := utf8.DecodeRune(paragraph[i:]); r == utf8.RuneError && l == 1 {
				return fmt.Errorf("invalid UTF-8 sequence at index %d", i)
			}
		}
		seg.text = append(seg.text, r)
	}
	seg.initAttributes()
	return nil
}

func (seg *Segmenter) initAttributes() {
	seg.attributes = append(seg.attributes[:0], make([]breakAttr, len(seg.text)+1)...)
	computeBreakAttributes(seg.text, seg.attributes)
}

// attributeIterator is an helper type used to
// handle iterating over a slice of runeAttr
type attributeIterator struct {
	src              *Segmenter
	pos              int       // the current position in the input slice (in runes)
	lastBreak        int       // the start of the current segment (in runes)
	posInBytes       int       // the current position in the input (in UTF-8 bytes)
	lastBreakInBytes int       // the start of the current segment (in UTF-8 bytes)
	flag             breakAttr // break where this flag is on
}

// next returns true if there is still a segment to process,
// and advances the iterator; or return false.
// if returning true, the segment is at [iter.lastBreak:iter.pos]
func (iter *attributeIterator) next() bool {
	iter.lastBreak = iter.pos // remember the start of the next segment
	iter.lastBreakInBytes = iter.posInBytes
	iter.incrementPos()
	for iter.pos <= len(iter.src.text) {
		// can we break before i ?
		if iter.src.attributes[iter.pos]&iter.flag != 0 {
			return true
		}
		iter.incrementPos()
	}
	return false
}

func (iter *attributeIterator) incrementPos() {
	if iter.pos < len(iter.src.text) {
		r := iter.src.text[iter.pos]
		if l := utf8.RuneLen(r); l > 0 {
			iter.posInBytes += l
		}
		// If l <= 0, it means that the rune is an invalid code point like out of range.
		// There is no correct way to update the byte position.
		// This case is treated as an undefined behavior. Just skip it.
	}
	iter.pos++
}

// Line is the content of a line delimited by the segmenter.
type Line struct {
	// Text is a subslice of the original input slice, containing the delimited line
	Text []rune
	// Offset is the start of the line in the input rune slice
	Offset int
	// OffsetInBytes is the start of the line in the input, in UTF-8 bytes
	OffsetInBytes int
	// LengthInBytes is the length of the line in the input, in UTF-8 bytes
	LengthInBytes int
	// IsMandatoryBreak is true if breaking (at the end of the line)
	// is mandatory
	IsMandatoryBreak bool
}

// LineIterator provides a convenient way of
// iterating over the lines delimited by a `Segmenter`.
type LineIterator struct {
	attributeIterator
}

// Next returns true if there is still a line to process,
// and advances the iterator; or return false.
func (li *LineIterator) Next() bool { return li.next() }

// Line returns the current `Line`
func (li *LineIterator) Line() Line {
	return Line{
		Offset:           li.lastBreak,
		OffsetInBytes:    li.lastBreakInBytes,
		LengthInBytes:    li.posInBytes - li.lastBreakInBytes,
		Text:             li.src.text[li.lastBreak:li.pos], // pos is not included since we break right before
		IsMandatoryBreak: li.src.attributes[li.pos]&mandatoryLineBoundary != 0,
	}
}

// LineIterator returns an iterator on the lines
// delimited in [Init].
func (sg *Segmenter) LineIterator() *LineIterator {
	return &LineIterator{attributeIterator: attributeIterator{src: sg, flag: lineBoundary}}
}

// Grapheme is the content of a grapheme delimited by the segmenter.
type Grapheme struct {
	// Text is a subslice of the original input slice, containing the delimited grapheme
	Text []rune
	// Offset is the start of the grapheme in the input rune slice
	Offset int
	// OffsetInBytes is the start of the grapheme in the input, in UTF-8 bytes
	OffsetInBytes int
	// LengthInBytes is the length of the grapheme in the input, in UTF-8 bytes
	LengthInBytes int
}

// GraphemeIterator provides a convenient way of
// iterating over the graphemes delimited by a `Segmenter`.
type GraphemeIterator struct {
	attributeIterator
}

// Next returns true if there is still a grapheme to process,
// and advances the iterator; or return false.
func (gr *GraphemeIterator) Next() bool { return gr.next() }

// Grapheme returns the current `Grapheme`
func (gr *GraphemeIterator) Grapheme() Grapheme {
	return Grapheme{
		Offset:        gr.lastBreak,
		OffsetInBytes: gr.lastBreakInBytes,
		LengthInBytes: gr.posInBytes - gr.lastBreakInBytes,
		Text:          gr.src.text[gr.lastBreak:gr.pos],
	}
}

// GraphemeIterator returns an iterator over the graphemes
// delimited in [Init].
func (sg *Segmenter) GraphemeIterator() *GraphemeIterator {
	return &GraphemeIterator{attributeIterator: attributeIterator{src: sg, flag: graphemeBoundary}}
}

// Word is the content of a word delimited by the segmenter.
//
// More precisely, a word is formed by runes
// with the [Alphabetic] property, or with a General_Category of Number,
// delimited by the Word Boundary Unicode Property.
//
// See also https://unicode.org/reports/tr29/#Word_Boundary_Rules,
// http://unicode.org/reports/tr44/#Alphabetic and
// http://unicode.org/reports/tr44/#General_Category_Values
type Word struct {
	// Text is a subslice of the original input slice, containing the delimited word
	Text []rune
	// Offset is the start of the word in the input rune slice
	Offset int
	// OffsetInBytes is the start of the word in the input, in UTF-8 bytes
	OffsetInBytes int
	// LengthInBytes is the length of the word in the input, in UTF-8 bytes
	LengthInBytes int
}

type WordIterator struct {
	attributeIterator

	inWord bool // true if we have seen the start of a word
}

// Next returns true if there is still a word to process,
// and advances the iterator; or return false.
func (gr *WordIterator) Next() bool {
	hasBoundary := gr.next()
	if !hasBoundary {
		return false
	}

	if gr.inWord { // we are have reached the END of a word
		gr.inWord = false
		return true
	}

	// do we start a word ? if so, mark it
	if gr.pos < len(gr.src.text) {
		gr.inWord = ucd.IsWord(gr.src.text[gr.pos])
	}
	// in any case, advance again
	return gr.Next()
}

// Word returns the current `Word`
func (gr *WordIterator) Word() Word {
	return Word{
		Offset:        gr.lastBreak,
		OffsetInBytes: gr.lastBreakInBytes,
		LengthInBytes: gr.posInBytes - gr.lastBreakInBytes,
		Text:          gr.src.text[gr.lastBreak:gr.pos],
	}
}

// WordIterator returns an iterator over the word
// delimited in [Init].
func (sg *Segmenter) WordIterator() *WordIterator {
	// check is we start at a word
	inWord := false
	if len(sg.text) != 0 {
		inWord = ucd.IsWord(sg.text[0])
	}
	return &WordIterator{attributeIterator: attributeIterator{src: sg, flag: wordBoundary}, inWord: inWord}
}
