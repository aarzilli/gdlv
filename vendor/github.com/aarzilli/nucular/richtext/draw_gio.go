// +build darwin,!nucular_shiny windows,!nucular_shiny nucular_gio

package richtext

import (
	"unsafe"

	"github.com/aarzilli/nucular/font"

	ifont "golang.org/x/image/font"

	"golang.org/x/image/math/fixed"

	"gioui.org/font/opentype"
	"gioui.org/text"
	"gioui.org/unit"
)

type fontFace struct {
	fnt     *opentype.Font
	shaper  *text.Cache
	size    int
	fsize   fixed.Int26_6
	metrics ifont.Metrics
}

func fontFace2fontFace(f *font.Face) *fontFace {
	return (*fontFace)(unsafe.Pointer(f))
}

func (face *fontFace) layout(str string, width int) []text.Line {
	if width < 0 {
		width = 1e6
	}
	return face.shaper.LayoutString(text.Font{}, fixed.I(face.size), width, str)
}

func (face *fontFace) Px(v unit.Value) int {
	return face.size
}

func (rtxt *RichText) calcAdvances(partial int) {
	if rtxt.adv != nil && partial == 0 {
		rtxt.adv = rtxt.adv[:0]
	}
	pos := int32(0)
	var siter styleIterator
	siter.Init(rtxt)
	for _, chunk := range rtxt.chunks[partial:] {
		// Note chunk is a copy of the element in the slice so we can modify it with impunity
		for chunk.len() > 0 {
			len := chunk.len()
			if siter.styleSel.E < pos+len {
				len = siter.styleSel.E - pos
			}

			if chunk.b != nil {
				panic("not implemented")
			}

			txt := fontFace2fontFace(&siter.styleSel.Face).layout(chunk.s[:len], 1e6)
			for _, line := range txt {
				for i := range line.Layout.Advances {
					rtxt.adv = append(rtxt.adv, line.Layout.Advances[i])
				}
			}

			siter.AdvanceTo(pos + len)
			pos += len
			chunk.s = chunk.s[len:]
		}
	}
}
