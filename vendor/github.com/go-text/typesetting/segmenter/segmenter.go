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
	"unicode"

	ucd "github.com/go-text/typesetting/unicodedata"
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

// lineBreakClass stores the Line Break Property
// See https://unicode.org/reports/tr14/#Properties
type lineBreakClass = *unicode.RangeTable

// graphemeBreakClass stores the Unicode Grapheme Cluster Break Property
// See https://unicode.org/reports/tr29/#Grapheme_Cluster_Break_Property_Values
type graphemeBreakClass = *unicode.RangeTable

// wordBreakClass stores the Unicode Word Break Property
// See https://unicode.org/reports/tr29/#Table_Word_Break_Property_Values
type wordBreakClass = *unicode.RangeTable

// cursor holds the information for the current index
// processed by `computeAttributes`, that is
// the context provided by previous and next runes in the text
type cursor struct {
	prev rune // the rune at index i-1
	r    rune // the rune at index i
	next rune // the rune at index i+1

	// is r included in `ucd.Extended_Pictographic`,
	// cached for efficiency
	isExtentedPic bool

	// the following fields persists across iterations

	prevGrapheme graphemeBreakClass // the Grapheme Break property at index i-1
	grapheme     graphemeBreakClass // the Grapheme Break property at index i

	// true if the `prev` rune was an odd Regional_Indicator, false if it was even or not an RI
	// used for rules GB12 and GB13
	// see [updateGraphemeRIOdd]
	isPrevGraphemeRIOdd bool

	prevPrevWord     wordBreakClass // the Word Break property at the previous previous, non Extend rune
	prevWord         wordBreakClass // the Word Break property at the previous, non Extend rune
	word             wordBreakClass // the Word Break property at index i
	prevWordNoExtend int            // the index of the last rune NOT having a Extend word break property

	// true if the `prev` rune was an odd Regional_Indicator, false if it was even or not an RI
	// used for rules WB15 and WB16
	// see [updateWordRIOdd]
	isPrevWordRIOdd bool

	prevPrevLine lineBreakClass // the Line Break Class at index i-2 (see rules LB9 and LB10 for edge cases)
	prevLine     lineBreakClass // the Line Break Class at index i-1 (see rules LB9 and LB10 for edge cases)
	line         lineBreakClass // the Line Break Class at index i
	nextLine     lineBreakClass // the Line Break Class at index i+1

	// the last rune after spaces, used in rules LB14,LB15,LB16,LB17
	// to match ... SP* ...
	beforeSpaces lineBreakClass

	// true if the `prev` rune was an odd Regional_Indicator, false if it was even or not an RI
	// used for rules LB30a
	isPrevLinebreakRIOdd bool

	// are we in a numeric sequence, as defined in Example 7 of customisation for LB25
	numSequence numSequenceState

	// are we in an emoji sequence, as defined in rule GB11
	// see [updatePictoSequence]
	pictoSequence pictoSequenceState
}

// initialise the cursor properties
// some of them are set in [startIteration]
func newCursor(text []rune) *cursor {
	cr := cursor{
		prevPrevLine:     ucd.BreakXX,
		prevWordNoExtend: -1,
	}

	// `startIteration` set `breakCl` from `nextBreakCl`
	// so we need to init this field before the first iteration
	cr.nextLine = ucd.BreakXX
	if len(text) != 0 {
		cr.nextLine = ucd.LookupLineBreakClass(text[0])
	}
	return &cr
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

		cr.endIteration(i == 0)

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
func (seg *Segmenter) Init(paragraph []rune) {
	seg.text = append(seg.text[:0], paragraph...)
	seg.attributes = append(seg.attributes[:0], make([]breakAttr, len(paragraph)+1)...)
	computeBreakAttributes(seg.text, seg.attributes)
}

// attributeIterator is an helper type used to
// handle iterating over a slice of runeAttr
type attributeIterator struct {
	src       *Segmenter
	pos       int       // the current position in the input slice
	lastBreak int       // the start of the current segment
	flag      breakAttr // break where this flag is on
}

// next returns true if there is still a segment to process,
// and advances the iterator; or return false.
// if returning true, the segment is at [iter.lastBreak:iter.pos]
func (iter *attributeIterator) next() bool {
	iter.lastBreak = iter.pos // remember the start of the next segment
	iter.pos++
	for iter.pos <= len(iter.src.text) {
		// can we break before i ?
		if iter.src.attributes[iter.pos]&iter.flag != 0 {
			return true
		}
		iter.pos++
	}
	return false
}

// Line is the content of a line delimited by the segmenter.
type Line struct {
	// Text is a subslice of the original input slice, containing the delimited line
	Text []rune
	// Offset is the start of the line in the input rune slice
	Offset int
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
		Offset: gr.lastBreak,
		Text:   gr.src.text[gr.lastBreak:gr.pos],
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
		gr.inWord = unicode.Is(ucd.Word, gr.src.text[gr.pos])
	}
	// in any case, advance again
	return gr.Next()
}

// Word returns the current `Word`
func (gr *WordIterator) Word() Word {
	return Word{
		Offset: gr.lastBreak,
		Text:   gr.src.text[gr.lastBreak:gr.pos],
	}
}

// WordIterator returns an iterator over the word
// delimited in [Init].
func (sg *Segmenter) WordIterator() *WordIterator {
	// check is we start at a word
	inWord := false
	if len(sg.text) != 0 {
		inWord = unicode.Is(ucd.Word, sg.text[0])
	}
	return &WordIterator{attributeIterator: attributeIterator{src: sg, flag: wordBoundary}, inWord: inWord}
}
