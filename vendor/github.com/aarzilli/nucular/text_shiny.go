// +build !nucular_gio

package nucular

import (
	"image"
	"image/color"
	"strings"
	"unsafe"

	"github.com/aarzilli/nucular/command"
	"github.com/aarzilli/nucular/font"
	"github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"

	ifont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/hashicorp/golang-lru"
)

// tracks github.com/aarzilli/nucular/font.Face
type fontFace struct {
	face ifont.Face
}

func fontFace2fontFace(f *font.Face) *fontFace {
	return (*fontFace)(unsafe.Pointer(f))
}

func textClamp(f font.Face, text []rune, space int) []rune {
	text_width := 0
	fc := fontFace2fontFace(&f).face
	for i, ch := range text {
		_, _, _, xwfixed, _ := fc.Glyph(fixed.P(0, 0), ch)
		xw := xwfixed.Ceil()
		if text_width+xw >= space {
			return text[:i]
		}
		text_width += xw
	}
	return text
}

var fontWidthCache *lru.Cache
var fontWidthCacheSize int

func init() {
	fontWidthCacheSize = 256
	fontWidthCache, _ = lru.New(256)
}

func ChangeFontWidthCache(size int) {
	if size > fontWidthCacheSize {
		fontWidthCacheSize = size
		fontWidthCache, _ = lru.New(fontWidthCacheSize)
	}
}

type fontWidthCacheKey struct {
	f      font.Face
	string string
}

func FontWidth(f font.Face, str string) int {
	maxw := 0
	for {
		newline := strings.Index(str, "\n")
		line := str
		if newline >= 0 {
			line = str[:newline]
		}

		k := fontWidthCacheKey{f, line}

		var w int
		if val, ok := fontWidthCache.Get(k); ok {
			w = val.(int)
		} else {
			d := ifont.Drawer{Face: fontFace2fontFace(&f).face}
			w = d.MeasureString(line).Ceil()
			fontWidthCache.Add(k, w)
		}

		if w > maxw {
			maxw = w
		}

		if newline >= 0 {
			str = str[newline+1:]
		} else {
			break
		}
	}
	return maxw
}

func glyphAdvance(f font.Face, ch rune) int {
	a, _ := fontFace2fontFace(&f).face.GlyphAdvance(ch)
	return a.Ceil()
}

func measureRunes(f font.Face, runes []rune) int {
	var advance fixed.Int26_6
	prevC := rune(-1)
	fc := fontFace2fontFace(&f).face
	for _, c := range runes {
		if prevC >= 0 {
			advance += fc.Kern(prevC, c)
		}
		a, ok := fc.GlyphAdvance(c)
		if !ok {
			// TODO: is falling back on the U+FFFD glyph the responsibility of
			// the Drawer or the Face?
			// TODO: set prevC = '\ufffd'?
			continue
		}
		advance += a
		prevC = c
	}
	return advance.Ceil()
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
	var done int = 0
	var line rect.Rect
	var text textWidget

	text.Padding = image.Point{0, 0}
	text.Background = t.Background
	text.Text = t.Text

	b.W = max(b.W, 2*t.Padding.X)
	b.H = max(b.H, 2*t.Padding.Y)
	b.H = b.H - 2*t.Padding.Y

	line.X = b.X + t.Padding.X
	line.Y = b.Y + t.Padding.Y
	line.W = b.W - 2*t.Padding.X
	line.H = 2*t.Padding.Y + FontHeight(f)

	fitting := textClamp(f, str, line.W)
	for done < len(str) {
		if len(fitting) == 0 || line.Y+line.H >= (b.Y+b.H) {
			break
		}
		widgetText(o, line, string(fitting), &text, "LC", f)
		done += len(fitting)
		line.Y += FontHeight(f) + 2*t.Padding.Y
		fitting = textClamp(f, str[done:], line.W)
	}
}
