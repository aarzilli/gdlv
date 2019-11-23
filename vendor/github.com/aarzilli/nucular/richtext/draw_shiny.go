// +build !darwin,!nucular_gio nucular_shiny

package richtext

import (
	"unsafe"

	"unicode/utf8"

	"github.com/aarzilli/nucular/font"

	ifont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// tracks github.com/aarzilli/nucular/font.Face
type fontFace struct {
	face ifont.Face
}

func fontFace2fontFace(f *font.Face) *fontFace {
	return (*fontFace)(unsafe.Pointer(f))
}

func (rtxt *RichText) calcAdvances(partial int) {
	if rtxt.adv != nil && partial == 0 {
		rtxt.adv = rtxt.adv[:0]
	}
	prevch := rune(-1)
	advance := fixed.I(0)
	var siter styleIterator
	siter.Init(rtxt)
	for _, chunk := range rtxt.chunks[partial:] {
		// Note chunk is a copy of the element in the slice so we can modify it with impunity
		for chunk.len() > 0 {
			var ch rune
			var rsz int
			if chunk.b != nil {
				ch, rsz = utf8.DecodeRune(chunk.b)
				chunk.b = chunk.b[rsz:]
			} else {
				ch, rsz = utf8.DecodeRuneInString(chunk.s)
				chunk.s = chunk.s[rsz:]
			}

			styleSel := siter.styleSel

			if len(rtxt.adv) > 0 {
				kern := fontFace2fontFace(&styleSel.Face).face.Kern(prevch, ch)
				rtxt.adv[len(rtxt.adv)-1] += kern
				advance += kern
			}

			switch ch {
			case '\t':
				tabszf, _ := fontFace2fontFace(&styleSel.Face).face.GlyphAdvance(' ')
				tabszf *= 8
				tabsz := tabszf.Ceil()
				a := fixed.I(int((float64(advance.Ceil()+tabsz)/float64(tabsz))*float64(tabsz)) - advance.Ceil())
				rtxt.adv = append(rtxt.adv, a)
				advance += a
			case '\n':
				rtxt.adv = append(rtxt.adv, 0)
				advance = 0
			default:
				a, _ := fontFace2fontFace(&styleSel.Face).face.GlyphAdvance(ch)
				rtxt.adv = append(rtxt.adv, a)
				advance += a
			}

			prevch = ch
			if siter.AdvanceRune(rsz) {
				prevch = rune(-1)
			}
		}
	}
}
