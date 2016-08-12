package command

import (
	"image"
	"image/color"
	
	"github.com/aarzilli/nucular/types"
)

// CommandBuffer is a list of drawing directives.
type Buffer struct {
	UseClipping bool
	Clip        types.Rect
	Commands    []Command
}

var nk_null_rect = types.Rect{-8192.0, -8192.0, 16384.0, 16384.0}

func (buffer *Buffer) Reset() {
	buffer.UseClipping = true
	buffer.Clip = nk_null_rect
	buffer.Commands = []Command{}
}

// Represents one drawing directive.
type Command interface {
	command()
}

type Scissor struct {
	types.Rect
}

func (c *Scissor) command() {}

type Line struct {
	LineThickness uint16
	Begin         image.Point
	End           image.Point
	Color         color.RGBA
}

func (c *Line) command() {}

type RectFilled struct {
	types.Rect
	Rounding uint16
	Color    color.RGBA
}

func (c *RectFilled) command() {}

type TriangleFilled struct {
	A     image.Point
	B     image.Point
	C     image.Point
	Color color.RGBA
}

func (c *TriangleFilled) command() {}

type CircleFilled struct {
	types.Rect
	Color color.RGBA
}

func (c *CircleFilled) command() {}

type Image struct {
	types.Rect
	Img *image.RGBA
}

func (c *Image) command() {}

type Text struct {
	types.Rect
	Font       *types.Face
	Foreground color.RGBA
	String     string
}

func (c *Text) command() {}

func (b *Buffer) PushScissor(r types.Rect) {
	cmd := &Scissor{}

	b.Clip = r

	b.Commands = append(b.Commands, cmd)

	cmd.Rect = r
}

func (b *Buffer) StrokeLine(p0, p1 image.Point, line_thickness int, c color.RGBA) {
	cmd := &Line{}
	b.Commands = append(b.Commands, cmd)
	cmd.LineThickness = uint16(line_thickness)
	cmd.Begin = p0
	cmd.End = p1
	cmd.Color = c
}

func (b *Buffer) FillRect(rect types.Rect, rounding uint16, c color.RGBA) {
	cmd := &RectFilled{}
	if c.A == 0 {
		return
	}
	if b.UseClipping {
		if !rect.Intersect(&b.Clip) {
			return
		}
	}

	b.Commands = append(b.Commands, cmd)
	cmd.Rounding = rounding
	cmd.Rect = rect
	cmd.Color = c
}

func (b *Buffer) FillCircle(r types.Rect, c color.RGBA) {
	cmd := &CircleFilled{}
	if c.A == 0 {
		return
	}
	if b.UseClipping {
		if !r.Intersect(&b.Clip) {
			return
		}
	}

	b.Commands = append(b.Commands, cmd)
	cmd.Rect = r
	cmd.Color = c
}

func (b *Buffer) FillTriangle(p0, p1, p2 image.Point, c color.RGBA) {
	cmd := &TriangleFilled{}
	if c.A == 0 {
		return
	}
	if b.UseClipping {
		if !b.Clip.Contains(p0) || !b.Clip.Contains(p1) || !b.Clip.Contains(p2) {
			return
		}
	}

	b.Commands = append(b.Commands, cmd)
	cmd.A = p0
	cmd.B = p1
	cmd.C = p2
	cmd.Color = c
}

func (b *Buffer) DrawImage(r types.Rect, img *image.RGBA) {
	cmd := &Image{}
	if b.UseClipping {
		if !r.Intersect(&b.Clip) {
			return
		}
	}

	b.Commands = append(b.Commands, cmd)
	cmd.Rect = r
	cmd.Img = img
}

func (b *Buffer) DrawText(r types.Rect, str string, font *types.Face, fg color.RGBA) {
	cmd := &Text{}

	if len(str) == 0 || (fg.A == 0) {
		return
	}
	if b.UseClipping {
		if !r.Intersect(&b.Clip) {
			return
		}
	}

	if len(str) == 0 {
		return
	}
	b.Commands = append(b.Commands, cmd)
	cmd.Rect = r
	cmd.Foreground = fg
	cmd.Font = font
	cmd.String = str
}
