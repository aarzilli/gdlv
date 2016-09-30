package command

import (
	"image"
	"image/color"

	"github.com/aarzilli/nucular/rect"

	"golang.org/x/image/font"
)

// CommandBuffer is a list of drawing directives.
type Buffer struct {
	UseClipping bool
	Clip        rect.Rect
	Commands    []Command
}

var nk_null_rect = rect.Rect{-8192.0, -8192.0, 16384.0, 16384.0}

func (buffer *Buffer) Reset() {
	buffer.UseClipping = true
	buffer.Clip = nk_null_rect
	buffer.Commands = buffer.Commands[:0]
}

// Represents one drawing directive.
type Command struct {
	rect.Rect
	Kind CommandKind
	Scissor Scissor
	Line Line
	RectFilled RectFilled
	TriangleFilled TriangleFilled
	CircleFilled CircleFilled
	Image Image
	Text Text
}

type CommandKind uint8
const (
	ScissorCmd CommandKind = iota
	LineCmd
	RectFilledCmd
	TriangleFilledCmd
	CircleFilledCmd
	ImageCmd
	TextCmd
)

type Scissor struct {
	rect.Rect
}

type Line struct {
	LineThickness uint16
	Begin         image.Point
	End           image.Point
	Color         color.RGBA
}

type RectFilled struct {
	//rect.Rect
	Rounding uint16
	Color    color.RGBA
}

type TriangleFilled struct {
	A     image.Point
	B     image.Point
	C     image.Point
	Color color.RGBA
}

type CircleFilled struct {
	//rect.Rect
	Color color.RGBA
}

type Image struct {
	//rect.Rect
	Img *image.RGBA
}

type Text struct {
	//rect.Rect
	Face       font.Face
	Foreground color.RGBA
	String     string
}

func (b *Buffer) PushScissor(r rect.Rect) {
	b.Clip = r
	
	var cmd Command
	cmd.Kind = ScissorCmd
	cmd.Rect = r

	b.Commands = append(b.Commands, cmd)
}

func (b *Buffer) StrokeLine(p0, p1 image.Point, line_thickness int, c color.RGBA) {
	var cmd Command
	cmd.Kind = LineCmd
	cmd.Line.LineThickness = uint16(line_thickness)
	cmd.Line.Begin = p0
	cmd.Line.End = p1
	cmd.Line.Color = c
	b.Commands = append(b.Commands, cmd)
}

func (b *Buffer) FillRect(rect rect.Rect, rounding uint16, c color.RGBA) {
	if c.A == 0 {
		return
	}
	if b.UseClipping {
		if !rect.Intersect(&b.Clip) {
			return
		}
	}

	var cmd Command
	cmd.Kind = RectFilledCmd
	cmd.RectFilled.Rounding = rounding
	cmd.Rect = rect
	cmd.RectFilled.Color = c
	b.Commands = append(b.Commands, cmd)
}

func (b *Buffer) FillCircle(r rect.Rect, c color.RGBA) {
	if c.A == 0 {
		return
	}
	if b.UseClipping {
		if !r.Intersect(&b.Clip) {
			return
		}
	}
	
	var cmd Command
	cmd.Kind = CircleFilledCmd
	cmd.Rect = r
	cmd.CircleFilled.Color = c
	b.Commands = append(b.Commands, cmd)
}

func (b *Buffer) FillTriangle(p0, p1, p2 image.Point, c color.RGBA) {
	if c.A == 0 {
		return
	}
	if b.UseClipping {
		if !b.Clip.Contains(p0) || !b.Clip.Contains(p1) || !b.Clip.Contains(p2) {
			return
		}
	}

	var cmd Command
	cmd.Kind = TriangleFilledCmd
	cmd.TriangleFilled.A = p0
	cmd.TriangleFilled.B = p1
	cmd.TriangleFilled.C = p2
	cmd.TriangleFilled.Color = c
	b.Commands = append(b.Commands, cmd)
}

func (b *Buffer) DrawImage(r rect.Rect, img *image.RGBA) {
	if b.UseClipping {
		if !r.Intersect(&b.Clip) {
			return
		}
	}

	var cmd Command
	cmd.Kind = ImageCmd
	cmd.Rect = r
	cmd.Image.Img = img
	b.Commands = append(b.Commands, cmd)
}

func (b *Buffer) DrawText(r rect.Rect, str string, face font.Face, fg color.RGBA) {
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
	
	var cmd Command
	cmd.Kind = TextCmd
	cmd.Rect = r
	cmd.Text.Foreground = fg
	cmd.Text.Face = face
	cmd.Text.String = str
	b.Commands = append(b.Commands, cmd)
}
