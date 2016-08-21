package nucular

import (
	"image"

	nstyle "github.com/aarzilli/nucular/style"

	"golang.org/x/image/font"
)

func (mw *MasterWindow) Style() (style *nstyle.Style, scaling float64) {
	return &mw.ctx.Style, mw.ctx.Scaling
}

func (mw *MasterWindow) SetStyle(style *nstyle.Style, ff font.Face, scaling float64) {
	mw.ctx.Style = *style

	if ff == nil {
		ff = DefaultFont(12, scaling)
	}
	mw.ctx.Style.Font = ff
	mw.ctx.Scaling = scaling

	scale := func(x *int) {
		*x = int(float64(*x) * scaling)
	}

	scaleu := func(x *uint16) {
		*x = uint16(float64(*x) * scaling)
	}

	scalept := func(p *image.Point) {
		if scaling == 1.0 {
			return
		}
		scale(&p.X)
		scale(&p.Y)
	}

	scalebtn := func(button *nstyle.Button) {
		scalept(&button.Padding)
		scalept(&button.ImagePadding)
		scalept(&button.TouchPadding)
		scale(&button.Border)
		scaleu(&button.Rounding)
	}

	z := &mw.ctx.Style

	scalept(&z.Text.Padding)

	scalebtn(&z.Button)
	scalebtn(&z.ContextualButton)
	scalebtn(&z.MenuButton)

	scalept(&z.Checkbox.Padding)
	scalept(&z.Checkbox.TouchPadding)

	scalept(&z.Option.Padding)
	scalept(&z.Option.TouchPadding)

	scalept(&z.Selectable.Padding)
	scalept(&z.Selectable.TouchPadding)
	scaleu(&z.Selectable.Rounding)

	scalept(&z.Slider.CursorSize)
	scalept(&z.Slider.Padding)
	scalept(&z.Slider.Spacing)
	scaleu(&z.Slider.Rounding)
	scale(&z.Slider.BarHeight)

	scalebtn(&z.Slider.IncButton)

	scalept(&z.Progress.Padding)
	scaleu(&z.Progress.Rounding)

	scalept(&z.Scrollh.Padding)
	scale(&z.Scrollh.Border)
	scaleu(&z.Scrollh.Rounding)

	scalebtn(&z.Scrollh.IncButton)
	scalebtn(&z.Scrollh.DecButton)
	scalebtn(&z.Scrollv.IncButton)
	scalebtn(&z.Scrollv.DecButton)

	scaleedit := func(edit *nstyle.Edit) {
		scale(&edit.RowPadding)
		scalept(&edit.Padding)
		scalept(&edit.ScrollbarSize)
		scale(&edit.CursorSize)
		scale(&edit.Border)
		scaleu(&edit.Rounding)
	}

	scaleedit(&z.Edit)

	scalept(&z.Property.Padding)
	scale(&z.Property.Border)
	scaleu(&z.Property.Rounding)

	scalebtn(&z.Property.IncButton)
	scalebtn(&z.Property.DecButton)

	scaleedit(&z.Property.Edit)

	scalept(&z.Combo.ContentPadding)
	scalept(&z.Combo.ButtonPadding)
	scalept(&z.Combo.Spacing)
	scale(&z.Combo.Border)
	scaleu(&z.Combo.Rounding)

	scalebtn(&z.Combo.Button)

	scale(&z.Tab.Border)
	scaleu(&z.Tab.Rounding)
	scalept(&z.Tab.Padding)
	scalept(&z.Tab.Spacing)

	scalebtn(&z.Tab.TabButton)
	scalebtn(&z.Tab.NodeButton)

	scalewin := func(win *nstyle.Window) {
		scalept(&win.Header.Padding)
		scalept(&win.Header.Spacing)
		scalept(&win.Header.LabelPadding)
		scalebtn(&win.Header.CloseButton)
		scalebtn(&win.Header.MinimizeButton)
		scalept(&win.FooterPadding)
		scaleu(&win.Rounding)
		scalept(&win.ScalerSize)
		scalept(&win.Padding)
		scalept(&win.Spacing)
		scalept(&win.ScrollbarSize)
		scalept(&win.MinSize)
		scale(&win.Border)
	}

	scalewin(&z.NormalWindow)
	scalewin(&z.MenuWindow)
	scalewin(&z.TooltipWindow)
	scalewin(&z.ComboWindow)
	scalewin(&z.ContextualWindow)
	scalewin(&z.GroupWindow)
}
