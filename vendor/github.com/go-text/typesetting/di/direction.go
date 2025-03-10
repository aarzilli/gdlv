package di

import (
	"github.com/go-text/typesetting/harfbuzz"
)

// Direction indicates the layout direction of a piece of text.
type Direction uint8

const (
	// DirectionLTR is for Left-to-Right text.
	DirectionLTR Direction = iota
	// DirectionRTL is for Right-to-Left text.
	DirectionRTL
	// DirectionTTB is for Top-to-Bottom text.
	DirectionTTB
	// DirectionBTT is for Bottom-to-Top text.
	DirectionBTT
)

const (
	progression Direction = 1 << iota
	// axisVertical is the bit for the axis, 0 for horizontal, 1 for vertical
	axisVertical

	// If this flag is set, the orientation is chosen
	// using the [verticalSideways] flag.
	// Otherwise, the segmenter will resolve the orientation based
	// on unicode properties
	verticalOrientationSet
	// verticalSideways is set for 'sideways', unset for 'upright'
	// It implies BVerticalOrientationSet is set
	verticalSideways
)

// IsVertical returns whether d is laid out on a vertical
// axis. If the return value is false, d is on the horizontal
// axis.
func (d Direction) IsVertical() bool { return d&axisVertical != 0 }

// Axis returns the layout axis for d.
func (d Direction) Axis() Axis {
	if d.IsVertical() {
		return Vertical
	}
	return Horizontal
}

// SwitchAxis switches from horizontal to vertical (and vice versa), preserving
// the progression.
func (d Direction) SwitchAxis() Direction { return d ^ axisVertical }

// Progression returns the text layout progression for d.
func (d Direction) Progression() Progression {
	if d&progression == 0 {
		return FromTopLeft
	}
	return TowardTopLeft
}

// SetProgression sets the progression, preserving the others bits.
func (d *Direction) SetProgression(p Progression) {
	if p == FromTopLeft {
		*d &= ^progression
	} else {
		*d |= progression
	}
}

// Axis indicates the axis of layout for a piece of text.
type Axis bool

const (
	Horizontal Axis = false
	Vertical   Axis = true
)

// Progression indicates how text is read within its Axis relative
// to the top left corner.
type Progression bool

const (
	// FromTopLeft indicates text in which a reader starts reading
	// at the top left corner of the text and moves away from it.
	// DirectionLTR and DirectionTTB are examples of FromTopLeft
	// Progression.
	FromTopLeft Progression = false
	// TowardTopLeft indicates text in which a reader starts reading
	// at the opposite end of the text's Axis from the top left corner
	// and moves towards it. DirectionRTL and DirectionBTT are examples
	// of TowardTopLeft progression.
	TowardTopLeft Progression = true
)

// HasVerticalOrientation returns true if the direction has set up
// an orientation for vertical text (typically using [SetSideways] or [SetUpright])
func (d Direction) HasVerticalOrientation() bool { return d&verticalOrientationSet != 0 }

// IsSideways returns true if the direction is vertical with a 'sideways'
// orientation.
//
// When shaping vertical text, 'sideways' means that the glyphs are rotated
// by 90°, clock-wise. This flag should be used by renderers to properly
// rotate the glyphs when drawing.
func (d Direction) IsSideways() bool { return d.IsVertical() && d&verticalSideways != 0 }

// SetSideways makes d vertical with 'sideways' or 'upright' orientation, preserving only the
// progression.
func (d *Direction) SetSideways(sideways bool) {
	*d |= axisVertical | verticalOrientationSet
	if sideways {
		*d |= verticalSideways
	} else {
		*d &= ^verticalSideways
	}
}

// Harfbuzz returns the equivalent direction used by harfbuzz.
func (d Direction) Harfbuzz() harfbuzz.Direction {
	switch d & (progression | axisVertical) {
	case DirectionRTL:
		return harfbuzz.RightToLeft
	case DirectionBTT:
		return harfbuzz.BottomToTop
	case DirectionTTB:
		return harfbuzz.TopToBottom
	default:
		return harfbuzz.LeftToRight
	}
}
