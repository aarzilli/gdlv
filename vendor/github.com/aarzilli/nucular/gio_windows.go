//go:build !nucular_shiny && windows

package nucular

import (
	"image"

	"gioui.org/app"

	"github.com/aarzilli/nucular/internal/win32"
)

func (mw *masterWindow) setIcon(icon image.Image) {
	if mw.ve == nil {
		mw.delayedIcon = icon
		return
	}

	if ve, ok := mw.ve.(app.Win32ViewEvent); ok {
		// WM_SETICON must be sent by the thread that owns the window, which
		// is gio's event loop thread, not the one running the update function
		mw.w.Run(func() {
			win32.SetIcon(ve.HWND, icon)
		})
	}
}
