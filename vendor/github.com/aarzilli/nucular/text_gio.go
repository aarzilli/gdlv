// +build nucular_gio

package nucular

import (
	"image"
	"image/color"
	"unsafe"

	"github.com/aarzilli/nucular/command"
	"github.com/aarzilli/nucular/font"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"

	ifont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"gioui.org/font/opentype"
	gioclip "gioui.org/op/clip"
	"gioui.org/text"
	"gioui.org/unit"
)

type fontFace struct {
	fnt     *opentype.Font
	shaper  *text.Shaper
	size    int
	fsize   fixed.Int26_6
	metrics ifont.Metrics
}

func fontFace2fontFace(f *font.Face) *fontFace {
	return (*fontFace)(unsafe.Pointer(f))
}

func (face *fontFace) layout(str string, width int) *text.Layout {
	if width < 0 {
		width = 1e6
	}
	return face.shaper.Layout(face, text.Font{}, str, text.LayoutOptions{MaxWidth: width})
}

func (face *fontFace) shape(txtstr text.String) gioclip.Op {
	return face.shaper.Shape(face, text.Font{}, txtstr)
}

func (face *fontFace) Px(v unit.Value) int {
	return face.size
}

func ChangeFontWidthCache(size int) {
}

func FontWidth(f font.Face, str string) int {
	txt := fontFace2fontFace(&f).layout(str, -1)
	maxw := 0
	for i := range txt.Lines {
		if w := txt.Lines[i].Width.Ceil(); w > maxw {
			maxw = w
		}
	}
	return maxw
}

func glyphAdvance(f font.Face, ch rune) int {
	txt := fontFace2fontFace(&f).layout(string(ch), 1e6)
	return txt.Lines[0].Width.Ceil()
}

func measureRunes(f font.Face, runes []rune) int {
	text := fontFace2fontFace(&f).layout(string(runes), 1e6)
	w := fixed.Int26_6(0)
	for i := range text.Lines {
		w += text.Lines[i].Width
	}
	return w.Ceil()
}

///////////////////////////////////////////////////////////////////////////////////
// TEXT WIDGETS
///////////////////////////////////////////////////////////////////////////////////

const (
	tabSizeInSpaces = 8
)

type textWidget struct {
	Padding    image.Point
	Background color.RGBA
	Text       color.RGBA
}

func widgetText(o *command.Buffer, b rect.Rect, str string, t *textWidget, a label.Align, f font.Face) {
	b.H = max(b.H, 2*t.Padding.Y)
	lblrect := rect.Rect{X: 0, W: 0, Y: b.Y + t.Padding.Y, H: b.H - 2*t.Padding.Y}

	/* align in x-axis */
	switch a[0] {
	case 'L':
		lblrect.X = b.X + t.Padding.X
		lblrect.W = max(0, b.W-2*t.Padding.X)
	case 'C':
		text_width := FontWidth(f, str)
		text_width += (2.0 * t.Padding.X)
		lblrect.W = max(1, 2*t.Padding.X+text_width)
		lblrect.X = (b.X + t.Padding.X + ((b.W-2*t.Padding.X)-lblrect.W)/2)
		lblrect.X = max(b.X+t.Padding.X, lblrect.X)
		lblrect.W = min(b.X+b.W, lblrect.X+lblrect.W)
		if lblrect.W >= lblrect.X {
			lblrect.W -= lblrect.X
		}
	case 'R':
		text_width := FontWidth(f, str)
		text_width += (2.0 * t.Padding.X)
		lblrect.X = max(b.X+t.Padding.X, (b.X+b.W)-(2*t.Padding.X+text_width))
		lblrect.W = text_width + 2*t.Padding.X
	default:
		panic("unsupported alignment")
	}

	/* align in y-axis */
	if len(a) >= 2 {
		switch a[1] {
		case 'C':
			lblrect.Y = b.Y + b.H/2.0 - FontHeight(f)/2.0
		case 'B':
			lblrect.Y = b.Y + b.H - FontHeight(f)
		}
	}
	if lblrect.H < FontHeight(f)*2 {
		lblrect.H = FontHeight(f) * 2
	}

	o.DrawText(lblrect, str, f, t.Text)
}

func widgetTextWrap(o *command.Buffer, b rect.Rect, str []rune, t *textWidget, f font.Face) {
	var text textWidget

	text.Padding = image.Point{0, 0}
	text.Background = t.Background
	text.Text = t.Text

	b.W = max(b.W, 2*t.Padding.X)
	b.H = max(b.H, 2*t.Padding.Y)
	b.H = b.H - 2*t.Padding.Y

	var line rect.Rect
	line.X = b.X + t.Padding.X
	line.Y = b.Y + t.Padding.Y
	line.W = b.W - 2*t.Padding.X
	line.H = 2*t.Padding.Y + FontHeight(f)

	lines := fontFace2fontFace(&f).layout(string(str), line.W)

	for _, txtline := range lines.Lines {
		if line.Y+line.H >= (b.Y + b.H) {
			break
		}
		widgetText(o, line, txtline.Text.String, &text, "LC", f)
		line.Y += FontHeight(f) + 2*t.Padding.Y
	}
}
