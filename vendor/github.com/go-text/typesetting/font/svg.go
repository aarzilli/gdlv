// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package font

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/go-text/typesetting/font/opentype/tables"
)

type svg []svgDocument

func newSvg(table tables.SVG) (svg, error) {
	rawData := table.SVGDocumentList.SVGRawData
	out := make(svg, len(table.SVGDocumentList.DocumentRecords))
	for i, rec := range table.SVGDocumentList.DocumentRecords {
		start, end := rec.SvgDocOffset, rec.SvgDocOffset+tables.Offset32(rec.SvgDocLength)
		if len(rawData) < int(end) {
			return nil, fmt.Errorf("invalid svg table (EOF: expected %d, got %d)", end, len(rawData))
		}
		out[i] = svgDocument{
			first: rec.StartGlyphID,
			last:  rec.EndGlyphID,
			svg:   rawData[start:end],
		}
	}
	return out, nil
}

type svgDocument struct {
	// svg document
	// each glyph description must be written
	// in an element with id=glyphXXX
	svg   []byte
	first gID // The first glyph ID in the range described by this index entry.
	last  gID // The last glyph ID in the range described by this index entry. Must be >= startGlyphID.
}

// rawGlyphData returns the SVG document for [gid], or false.
func (s svg) rawGlyphData(gid gID) ([]byte, bool) {
	// binary search
	for i, j := 0, len(s); i < j; {
		h := i + (j-i)/2
		entry := s[h]
		if gid < entry.first {
			j = h
		} else if entry.last < gid {
			i = h + 1
		} else {
			return entry.svg, true
		}
	}
	return nil, false
}

// SVGViewBox is a rectangle in the "user" coordinate space
// of an SVG document.
type SVGViewBox struct {
	MinX, MinY, Width, Height float32
}

// svgViewBox returns the initial viewport of the SVG document [doc],
// resolved from the root <svg> element attributes, defaulting to the
// em square given by [upem], as required by the specification.
func svgViewBox(doc []byte, upem uint16) SVGViewBox {
	viewBox, width, height, ok := svgRootAttributes(doc)
	if ok {
		if vb, ok := parseSVGViewBox(viewBox); ok {
			return vb
		}
		if w, okW := parseSVGLength(width); okW {
			if h, okH := parseSVGLength(height); okH {
				return SVGViewBox{0, 0, w, h}
			}
		}
	}
	em := float32(upem)
	return SVGViewBox{0, 0, em, em}
}

// parseSVGNumber parses a finite number, rejecting the "NaN" and
// "Inf" forms accepted by [strconv.ParseFloat].
func parseSVGNumber(s string) (float32, bool) {
	v, err := strconv.ParseFloat(s, 32)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return float32(v), true
}

// parseSVGViewBox parses the value of a viewBox attribute:
// four numbers separated by whitespace and/or commas.
func parseSVGViewBox(s string) (SVGViewBox, bool) {
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
	if len(fields) != 4 {
		return SVGViewBox{}, false
	}
	var nums [4]float32
	for i, field := range fields {
		v, ok := parseSVGNumber(field)
		if !ok {
			return SVGViewBox{}, false
		}
		nums[i] = v
	}
	if nums[2] <= 0 || nums[3] <= 0 {
		return SVGViewBox{}, false
	}
	return SVGViewBox{nums[0], nums[1], nums[2], nums[3]}, true
}

// parseSVGLength parses a positive width or height attribute value
// expressed in user units, such as "12" or "12px".
func parseSVGLength(s string) (float32, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "px")
	v, ok := parseSVGNumber(s)
	if !ok || v <= 0 {
		return 0, false
	}
	return v, true
}

// svgNamespace is the namespace of SVG elements.
const svgNamespace = "http://www.w3.org/2000/svg"

// svgRootAttributes returns the raw viewBox, width and height attribute
// values of the root element of the XML document [doc], or false if the
// document has no root element or if it is not an svg element.
func svgRootAttributes(doc []byte) (viewBox, width, height string, ok bool) {
	dec := xml.NewDecoder(bytes.NewReader(doc))
	var root xml.StartElement
	for {
		tok, err := dec.Token()
		if err != nil { // includes io.EOF, i.e. no root element
			return "", "", "", false
		}
		if start, isStart := tok.(xml.StartElement); isStart {
			root = start
			break
		}
	}
	// A missing namespace is tolerated, but a foreign one is rejected.
	if root.Name.Local != "svg" || (root.Name.Space != "" && root.Name.Space != svgNamespace) {
		return "", "", "", false
	}
	for _, attr := range root.Attr {
		switch attr.Name.Local {
		case "viewBox":
			viewBox = attr.Value
		case "width":
			width = attr.Value
		case "height":
			height = attr.Value
		}
	}
	return viewBox, width, height, true
}
