//go:build !nucular_shiny && !linux && !windows

package nucular

import (
	"image"
)

func (mw *masterWindow) setIcon(icon image.Image) {
	// not implemented
}
