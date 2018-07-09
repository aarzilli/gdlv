package main

import (
	"fmt"
	"image/color"
	"strconv"

	nstyle "github.com/aarzilli/nucular/style"
)

func hexc(s string) color.RGBA {
	defer func() {
		ierr := recover()
		if ierr != nil {
			panic(fmt.Errorf("called with %q: %v", s, ierr))
		}
	}()
	r, _ := strconv.ParseInt(s[:2], 16, 8)
	g, _ := strconv.ParseInt(s[2:4], 16, 8)
	b, _ := strconv.ParseInt(s[4:6], 16, 8)

	return color.RGBA{uint8(r), uint8(g), uint8(b), 255}
}

var redThemeTable = nstyle.ColorTable{
	ColorText:                  color.RGBA{190, 190, 190, 255},
	ColorWindow:                color.RGBA{30, 33, 40, 255},
	ColorHeader:                color.RGBA{181, 45, 69, 255},
	ColorBorder:                color.RGBA{51, 55, 67, 255},
	ColorButton:                color.RGBA{181, 45, 69, 255},
	ColorButtonHover:           color.RGBA{190, 50, 70, 255},
	ColorButtonActive:          color.RGBA{195, 55, 75, 255},
	ColorToggle:                color.RGBA{51, 55, 67, 255},
	ColorToggleHover:           color.RGBA{45, 60, 60, 255},
	ColorToggleCursor:          color.RGBA{181, 45, 69, 255},
	ColorSelect:                color.RGBA{51, 55, 67, 255},
	ColorSelectActive:          color.RGBA{181, 45, 69, 255},
	ColorSlider:                color.RGBA{51, 55, 67, 255},
	ColorSliderCursor:          color.RGBA{181, 45, 69, 255},
	ColorSliderCursorHover:     color.RGBA{186, 50, 74, 255},
	ColorSliderCursorActive:    color.RGBA{191, 55, 79, 255},
	ColorProperty:              color.RGBA{51, 55, 67, 255},
	ColorEdit:                  color.RGBA{51, 55, 67, 225},
	ColorEditCursor:            color.RGBA{190, 190, 190, 255},
	ColorCombo:                 color.RGBA{51, 55, 67, 255},
	ColorChart:                 color.RGBA{51, 55, 67, 255},
	ColorChartColor:            color.RGBA{170, 40, 60, 255},
	ColorChartColorHighlight:   color.RGBA{255, 0, 0, 255},
	ColorScrollbar:             color.RGBA{30, 33, 40, 255},
	ColorScrollbarCursor:       color.RGBA{64, 84, 95, 255},
	ColorScrollbarCursorHover:  color.RGBA{70, 90, 100, 255},
	ColorScrollbarCursorActive: color.RGBA{75, 95, 105, 255},
	ColorTabHeader:             color.RGBA{181, 45, 69, 255},
}
