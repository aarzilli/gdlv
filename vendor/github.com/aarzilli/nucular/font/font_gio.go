//go:build (darwin && !nucular_shiny) || nucular_gio
// +build darwin,!nucular_shiny nucular_gio

package font

import (
	"crypto/md5"
	"strings"
	"sync"

	"gioui.org/font/opentype"
	"gioui.org/io/system"
	"gioui.org/text"

	"golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

type Face struct {
	fnt     opentype.Face
	shaper  *text.Shaper
	fsize   fixed.Int26_6
	metrics font.Metrics
}

var fontsMu sync.Mutex
var fontsMap = map[[md5.Size]byte]opentype.Face{}

func NewFace(ttf []byte, size int) (Face, error) {
	key := md5.Sum(ttf)
	fontsMu.Lock()
	defer fontsMu.Unlock()

	fnt, ok := fontsMap[key]
	if !ok {
		var err error
		fnt, err = opentype.Parse(ttf)
		if err != nil {
			return Face{}, err
		}
	}

	shaper := text.NewShaper([]text.FontFace{{text.Font{}, fnt}})

	face := Face{fnt, shaper, fixed.I(size), font.Metrics{}}
	face.shaper.Layout(text.Parameters{
		Font:     text.Font{},
		PxPerEm:  face.fsize,
		MinWidth: 0,
		MaxWidth: 1e6,
		Locale:   system.Locale{},
	}, strings.NewReader("metrics"))
	g, _ := face.shaper.NextGlyph()
	face.metrics.Ascent = g.Ascent
	face.metrics.Descent = g.Descent
	face.metrics.Height = face.metrics.Ascent + face.metrics.Descent
	return face, nil
}

func (face Face) Metrics() font.Metrics {
	return face.metrics
}
