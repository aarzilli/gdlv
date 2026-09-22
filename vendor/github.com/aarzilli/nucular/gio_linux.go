//go:build !nucular_shiny && linux

package nucular

import (
	"image"

	"gioui.org/app"

	"github.com/aarzilli/nucular/internal/x11"
)

func (mw *masterWindow) setIcon(icon image.Image) {
	if mw.ve == nil {
		return
	}

	switch ve := mw.ve.(type) {
	case app.X11ViewEvent:
		x11.SetIcon(ve.Display, ve.Window, icon)
	case app.WaylandViewEvent:
		//TODO: implement
	}
}
