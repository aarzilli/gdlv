//go:build (darwin && !nucular_shiny) || nucular_gio
// +build darwin,!nucular_shiny nucular_gio

package richtext

import (
	"unsafe"

	"github.com/aarzilli/nucular/font"

	ifont "golang.org/x/image/font"

	"golang.org/x/image/math/fixed"

	"gioui.org/font/opentype"
	"gioui.org/io/system"
	"gioui.org/text"
)

type fontFace struct {
	fnt     opentype.Face
	shaper  *text.Shaper
	fsize   fixed.Int26_6
	metrics ifont.Metrics
}

func fontFace2fontFace(f *font.Face) *fontFace {
	return (*fontFace)(unsafe.Pointer(f))
}

func (face *fontFace) layout(str string, width int) []text.Glyph {
	if width < 0 {
		width = 1e6
	}
	face.shaper.LayoutString(text.Parameters{
		Font:     text.Font{},
		PxPerEm:  face.fsize,
		MinWidth: 0,
		MaxWidth: width,
		Locale:   system.Locale{}}, str)
	gs := []text.Glyph{}
	x := fixed.I(0)
	for {
		g, ok := face.shaper.NextGlyph()
		if !ok {
			break
		}
		g.X = x
		g.Advance = fixed.I(g.Advance.Ceil())
		x += g.Advance
		gs = append(gs, g)
	}
	return gs
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
			l := chunk.len()
			if siter.styleSel.E < pos+l {
				l = siter.styleSel.E - pos
			}

			if chunk.b != nil {
				panic("not implemented")
			}

			advLenBefore := len(rtxt.adv)

			txt := fontFace2fontFace(&siter.styleSel.Face).layout(chunk.s[:l], 1e6)
			for _, glyph := range txt {
				if glyph.Runes == 0 {
					continue
				}
				for n := 0; n < int(glyph.Runes)-1; n++ {
					rtxt.adv = append(rtxt.adv, 0)
				}
				rtxt.adv = append(rtxt.adv, glyph.Advance)
			}

			if len([]rune(chunk.s[:l])) != len(rtxt.adv)-advLenBefore {
				panic("internal error")
			}

			siter.AdvanceTo(pos + l)
			pos += l
			chunk.s = chunk.s[l:]
		}
	}
}
