// SPDX-License-Identifier: Unlicense OR BSD-3-Clause

package shaping

import (
	"github.com/go-text/typesetting/di"
	"github.com/go-text/typesetting/font"
	"golang.org/x/image/math/fixed"
)

// Glyph describes the attributes of a single glyph from a single
// font face in a shaped output.
type Glyph struct {
	// Width is the width of the glyph content,
	// expressed as a distance from the [XBearing],
	// typically positive
	Width fixed.Int26_6
	// Height is the height of the glyph content,
	// expressed as a distance from the [YBearing],
	// typically negative
	Height fixed.Int26_6
	// XBearing is the distance between the dot (with offset applied) and
	// the glyph content, typically positive for horizontal text;
	// often negative for vertical text.
	XBearing fixed.Int26_6
	// YBearing is the distance between the dot (with offset applied) and
	// the top of the glyph content, typically positive
	YBearing fixed.Int26_6
	// XAdvance is the distance between the current dot (without offset applied) and the next dot.
	// It is typically positive for horizontal text, and always zero for vertical text.
	XAdvance fixed.Int26_6
	// YAdvance is the distance between the current dot (without offset applied) and the next dot.
	// It is typically negative for vertical text, and always zero for horizontal text.
	YAdvance fixed.Int26_6

	// Offsets to be applied to the dot before actually drawing
	// the glyph.
	// For vertical text, YOffset is typically used to position the glyph
	// below the horizontal line at the dot
	XOffset, YOffset fixed.Int26_6

	// ClusterIndex is the lowest rune index of all runes shaped into
	// this glyph cluster. All glyphs sharing the same cluster value
	// are part of the same cluster and will have identical RuneCount
	// and GlyphCount fields.
	ClusterIndex int
	// RuneCount is the number of input runes shaped into this output
	// glyph cluster.
	RuneCount int
	// GlyphCount is the number of glyphs in this output glyph cluster.
	GlyphCount int
	GlyphID    font.GID
	Mask       uint32

	// startLetterSpacing and endLetterSpacing are set when letter spacing is applied,
	// measuring the whitespace added on one side (half of the user provided letter spacing)
	// The line wrapper will ignore [endLetterSpacing] when deciding where to break,
	// and will trim [startLetterSpacing] at the start of the lines
	startLetterSpacing, endLetterSpacing fixed.Int26_6
}

// LeftSideBearing returns the distance from the glyph's X origin to
// its leftmost edge. This value can be negative if the glyph extends
// across the origin.
func (g Glyph) LeftSideBearing() fixed.Int26_6 {
	return g.XBearing
}

// RightSideBearing returns the distance from the glyph's right edge to
// the edge of the glyph's advance. This value can be negative if the glyph's
// right edge is after the end of its advance.
func (g Glyph) RightSideBearing() fixed.Int26_6 {
	return g.XAdvance - g.Width - g.XBearing
}

// Bounds describes the minor-axis bounds of a line of text. In a LTR or RTL
// layout, it describes the vertical axis. In a BTT or TTB layout, it describes
// the horizontal.
//
// For horizontal text:
//
//   - Ascent      GLYPH
//     |             GLYPH
//     |             GLYPH
//     |             GLYPH
//     |             GLYPH
//   - Baseline    GLYPH
//     |             GLYPH
//     |             GLYPH
//     |             GLYPH
//   - Descent     GLYPH
//     |
//   - Gap
//
// For vertical text:
//
//	Descent ------- Baseline --------------- Ascent --- Gap
//		|				| 						|		|
//		GLYPH		  GLYPH					GLYPH
//			GLYPH GLYPH		GLYPH GLYPH GLYPH
type Bounds struct {
	// Ascent is the maximum ascent away from the baseline. This value is typically
	// positive in coordiate systems that grow up.
	Ascent fixed.Int26_6
	// Descent is the maximum descent away from the baseline. This value is typically
	// negative in coordinate systems that grow up.
	Descent fixed.Int26_6
	// Gap is the height of empty pixels between lines. This value is typically positive
	// in coordinate systems that grow up.
	Gap fixed.Int26_6
}

// LineThickness returns the thickness of a line of text described by b,
// that is its height for horizontal text, its width for vertical text.
func (b Bounds) LineThickness() fixed.Int26_6 {
	return b.Ascent - b.Descent + b.Gap
}

// Output describes the dimensions and content of shaped text.
type Output struct {
	// Advance is the distance the Dot has advanced.
	// It is typically positive for horizontal text, negative for vertical.
	Advance fixed.Int26_6
	// Size is copied from the shaping.Input.Size that produced this Output.
	Size fixed.Int26_6
	// Glyphs are the shaped output text.
	Glyphs []Glyph
	// LineBounds describes the font's suggested line bounding dimensions. The
	// dimensions described should contain any glyphs from the given font.
	LineBounds Bounds
	// GlyphBounds describes a tight bounding box on the specific glyphs contained
	// within this output. The dimensions may not be sufficient to contain all
	// glyphs within the chosen font.
	//
	// Its [Gap] field is always zero.
	GlyphBounds Bounds

	// Direction is the direction used to shape the text,
	// as provided in the Input.
	Direction di.Direction

	// Runes describes the runes this output represents from the input text.
	Runes Range

	// Face is the font face that this output is rendered in. This is needed in
	// the output in order to render each run in a multi-font sequence in the
	// correct font.
	Face *font.Face

	// VisualIndex is the visual position of this run within its containing line where
	// 0 indicates the leftmost run and increasing values move to the right. This is
	// useful for sorting the runs for drawing purposes.
	VisualIndex int32
}

// ToFontUnit converts a metrics (typically found in [Glyph] fields)
// to unscaled font units.
func (o *Output) ToFontUnit(v fixed.Int26_6) float32 {
	return float32(v) / float32(o.Size) * float32(o.Face.Upem())
}

// FromFontUnit converts an unscaled font value to the current [Size]
func (o *Output) FromFontUnit(v float32) fixed.Int26_6 {
	return fixed.Int26_6(v * float32(o.Size) / float32(o.Face.Upem()))
}

// RecomputeAdvance updates only the Advance field based on the current
// contents of the Glyphs field. It is faster than RecalculateAll(),
// and can be used to speed up line wrapping logic.
func (o *Output) RecomputeAdvance() {
	advance := fixed.Int26_6(0)
	if o.Direction.IsVertical() {
		for _, g := range o.Glyphs {
			advance += g.YAdvance
		}
	} else { // horizontal
		for _, g := range o.Glyphs {
			advance += g.XAdvance
		}
	}
	o.Advance = advance
}

// advanceSpaceAware adjust the value in [Advance]
// if a white space character ends the run.
// Any end letter spacing (on the last glyph) is also removed
// The paragraphDir is the text direction of the overall paragraph containing o.
// If the paragraphDir is different then o's Direction, this method has no effect
// because the trailing space in this run will always be internal to the paragraph.
//
// TODO: should we take into account multiple spaces ?
func (o *Output) advanceSpaceAware(paragraphDir di.Direction) fixed.Int26_6 {
	L := len(o.Glyphs)
	if L == 0 || paragraphDir != o.Direction {
		return o.Advance
	}

	// adjust the last to account for spaces
	var lastG Glyph
	if o.Direction.Progression() == di.FromTopLeft {
		lastG = o.Glyphs[L-1]
	} else {
		lastG = o.Glyphs[0]
	}
	if o.Direction.IsVertical() {
		if lastG.Height == 0 {
			return o.Advance - lastG.YAdvance
		}
	} else { // horizontal
		if lastG.Width == 0 {
			return o.Advance - lastG.XAdvance
		}
	}
	return o.Advance - lastG.endLetterSpacing
}

// RecalculateAll updates the all other fields of the Output
// to match the current contents of the Glyphs field.
// This method will fail with UnimplementedDirectionError if the Output
// direction is unimplemented.
func (o *Output) RecalculateAll() {
	var (
		advance fixed.Int26_6
		ascent  fixed.Int26_6
		descent fixed.Int26_6
	)

	if o.Direction.IsVertical() {
		for i := range o.Glyphs {
			g := &o.Glyphs[i]
			advance += g.YAdvance
			depth := g.XOffset + g.XBearing // start of the glyph
			if depth < descent {
				descent = depth
			}
			height := depth + g.Width // end of the glyph
			if height > ascent {
				ascent = height
			}
		}
	} else { // horizontal
		for i := range o.Glyphs {
			g := &o.Glyphs[i]
			advance += g.XAdvance
			height := g.YBearing + g.YOffset
			if height > ascent {
				ascent = height
			}
			depth := height + g.Height
			if depth < descent {
				descent = depth
			}
		}
	}
	o.Advance = advance
	o.GlyphBounds = Bounds{
		Ascent:  ascent,
		Descent: descent,
	}
}

// Assuming [Glyphs] comes from an horizontal shaping,
// applies a 90°, clockwise rotation to the whole slice of glyphs,
// to create 'sideways' vertical text.
//
// The [Direction] field is updated by switching the axis to vertical
// and the orientation to "sideways".
//
// [RecalculateAll] should be called afterwards to update [Avance] and [GlyphBounds].
func (out *Output) sideways() {
	for i, g := range out.Glyphs {
		// switch height and width
		out.Glyphs[i].Width = -g.Height // height is negative
		out.Glyphs[i].Height = -g.Width
		// compute the bearings
		out.Glyphs[i].XBearing = g.YBearing + g.Height
		out.Glyphs[i].YBearing = g.Width
		// switch advance direction
		out.Glyphs[i].XAdvance = 0
		out.Glyphs[i].YAdvance = -g.XAdvance // YAdvance is negative
		// apply a rotation around the dot, and position the glyph
		// below the dot
		out.Glyphs[i].XOffset = g.YOffset
		out.Glyphs[i].YOffset = -(g.XOffset + g.XBearing + g.Width)
	}

	// adjust direction
	out.Direction.SetSideways(true)
}

// properly update [GlyphBounds]
func (out *Output) moveCrossAxis(d fixed.Int26_6) {
	if out.Direction.IsVertical() {
		for i := range out.Glyphs {
			out.Glyphs[i].XOffset += d
		}
	} else {
		for i := range out.Glyphs {
			out.Glyphs[i].YOffset += d
		}
	}
	out.GlyphBounds.Ascent += d
	out.GlyphBounds.Descent += d
}

// AdjustBaselines aligns runs with different baselines.
//
// For vertical text, it centralizes 'sideways' runs, so
// that text with mixed 'upright' and
// 'sideways' orientation is better aligned.
//
// This is currently a no-op for horizontal text.
//
// Note that this method only update cross-axis metrics,
// so that the advance is preserved. As such, it is valid
// to call this method after line wrapping, for instance.
func (l Line) AdjustBaselines() {
	if len(l) == 0 {
		return
	}
	firstRun := l[0]

	if firstRun.Direction.Axis() == di.Horizontal {
		return
	}

	// Centralize sideways runs, to better align
	// with upright ones, which are usually visually centered.
	// We want to shift all the runs by the same amount, to
	// avoid breaking alignment of similar runs (consider "A あ is a pretty char.")
	var sidewaysBounds Bounds
	for _, run := range l {
		if !run.Direction.IsSideways() {
			continue
		}
		if a := run.GlyphBounds.Ascent; a > sidewaysBounds.Ascent {
			sidewaysBounds.Ascent = a
		}
		if d := run.GlyphBounds.Descent; d < sidewaysBounds.Descent {
			sidewaysBounds.Descent = d
		}
	}
	// Place the middle of sideways run at the baseline (the zero)
	middle := sidewaysBounds.Descent + sidewaysBounds.LineThickness()/2
	for i := range l {
		if !l[i].Direction.IsSideways() {
			continue
		}
		l[i].moveCrossAxis(-middle)
	}
}
