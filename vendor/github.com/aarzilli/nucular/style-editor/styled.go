package styled

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"io"
	"math"
	"reflect"

	"golang.org/x/mobile/event/mouse"

	"github.com/aarzilli/nucular"
	lbl "github.com/aarzilli/nucular/label"
	"github.com/aarzilli/nucular/rect"
	nstyle "github.com/aarzilli/nucular/style"
)

type styleEditor struct {
	style  *nstyle.Style
	saveFn func(string)

	colorPickers map[*color.RGBA]*colorPicker
}

func EditStyle(w nucular.MasterWindow, flags nucular.WindowFlags, saveFn func(string)) {
	w.PopupOpen("Style Editor", nucular.WindowTitle|nucular.WindowBorder|nucular.WindowMovable|nucular.WindowScalable|flags, rect.Rect{0, 0, 400, 600}, true, StyleEditor(w.Style(), saveFn))
}

func StyleEditor(style *nstyle.Style, saveFn func(string)) nucular.UpdateFn {
	se := &styleEditor{style: style, saveFn: saveFn}
	return se.Update
}

func (se *styleEditor) Update(w *nucular.Window) {
	if se.style == nil {
		se.style = w.Master().Style()
	}
	val := reflect.ValueOf(se.style.Unscaled()).Elem()
	w.MenubarBegin()
	w.Row(25).Static(0, 100)
	w.Spacing(1)
	if w.ButtonText("Save") && se.saveFn != nil {
		var buf bytes.Buffer
		se.serialize(&buf, "style", val)
		se.saveFn(buf.String())
	}
	w.MenubarEnd()
	if se.editStruct(w, val) {
		se.style.Scale(se.style.Scaling)
	}
}

func (se *styleEditor) editStruct(w *nucular.Window, val reflect.Value) bool {
	changed := false
	for i := 0; i < val.NumField(); i++ {
		field := val.Type().Field(i)
		if field.PkgPath != "" {
			continue
		}
		fieldval := val.Field(i)
		fieldiface := fieldval.Addr().Interface()
		switch fieldptr := fieldiface.(type) {
		case *nstyle.Text, *nstyle.Button, *nstyle.Toggle, *nstyle.Selectable, *nstyle.Slider, *nstyle.Progress, *nstyle.Property, *nstyle.Edit, *nstyle.Scrollbar, *nstyle.Tab, *nstyle.Combo, *nstyle.Window, *nstyle.WindowHeader:
			if w.TreePush(nucular.TreeTab, field.Name, false) {
				if se.editStruct(w, fieldval) {
					changed = true
				}
				w.TreePop()
			}
		case *color.RGBA:
			if se.editColor(w, field.Name, fieldptr) {
				changed = true
			}
		case *int:
			w.Row(20).Dynamic(1)
			if w.PropertyInt(field.Name, -10000, fieldptr, 10000, 1, 1) {
				changed = true
			}
		case *uint16:
			w.Row(20).Dynamic(1)
			v := int(*fieldptr)
			if w.PropertyInt(field.Name, -10000, &v, 10000, 1, 1) {
				*fieldptr = uint16(v)
				changed = true
			}
		case *uint32:
			w.Row(20).Dynamic(1)
			v := int(*fieldptr)
			if w.PropertyInt(field.Name, -10000, &v, 10000, 1, 1) {
				*fieldptr = uint32(v)
				changed = true
			}
		case *lbl.SymbolType:
			w.Row(20).Dynamic(1)
			v := int(*fieldptr)
			if w.PropertyInt(field.Name, -10000, &v, 10000, 1, 1) {
				*fieldptr = lbl.SymbolType(v)
				changed = true
			}
		case *nstyle.HeaderAlign:
			w.Row(20).Dynamic(1)
			v := int(*fieldptr)
			if w.PropertyInt(field.Name, -10000, &v, 10000, 1, 1) {
				*fieldptr = nstyle.HeaderAlign(v)
				changed = true
			}
		case *image.Point:
			w.Row(20).Static(200, 0)
			w.Label(field.Name, "LC")
			if w.PropertyInt("X:", -10000, &fieldptr.X, 10000, 1, 1) {
				changed = true
			}
			w.Spacing(1)
			if w.PropertyInt("Y:", -10000, &fieldptr.Y, 10000, 1, 1) {
				changed = true
			}
		case *nstyle.Item:
			if fieldptr.Type == nstyle.ItemColor {
				if se.editColor(w, field.Name, &fieldptr.Data.Color) {
					changed = true
				}
			}
		}
	}

	return changed
}

func (se *styleEditor) serialize(wr io.Writer, prefix string, val reflect.Value) {
	for i := 0; i < val.NumField(); i++ {
		field := val.Type().Field(i)
		if field.PkgPath != "" {
			continue
		}
		fieldval := val.Field(i)
		fieldiface := fieldval.Addr().Interface()
		switch fieldptr := fieldiface.(type) {
		case *nstyle.Text, *nstyle.Button, *nstyle.Toggle, *nstyle.Selectable, *nstyle.Slider, *nstyle.Progress, *nstyle.Property, *nstyle.Edit, *nstyle.Scrollbar, *nstyle.Tab, *nstyle.Combo, *nstyle.Window, *nstyle.WindowHeader:
			se.serialize(wr, fmt.Sprintf("%s.%s", prefix, field.Name), fieldval)
		case *color.RGBA:
			fmt.Fprintf(wr, "%s.%s = color.RGBA{ %d, %d, %d, %d }\n", prefix, field.Name, fieldptr.R, fieldptr.G, fieldptr.B, fieldptr.A)

		case *int:
			fmt.Fprintf(wr, "%s.%s = %d\n", prefix, field.Name, *fieldptr)
		case *uint16:
			fmt.Fprintf(wr, "%s.%s = %d\n", prefix, field.Name, *fieldptr)
		case *uint32:
			fmt.Fprintf(wr, "%s.%s = %d\n", prefix, field.Name, *fieldptr)
		case *bool:
			fmt.Fprintf(wr, "%s.%s = %v\n", prefix, field.Name, *fieldptr)
		case *lbl.SymbolType:
			fmt.Fprintf(wr, "%s.%s = lbl.SymbolType(%d)\n", prefix, field.Name, *fieldptr)
		case *nstyle.HeaderAlign:
			fmt.Fprintf(wr, "%s.%s = nstyle.HeaderAlign(%d)\n", prefix, field.Name, *fieldptr)
		case *image.Point:
			fmt.Fprintf(wr, "%s.%s = image.Point{ %d, %d }\n", prefix, field.Name, fieldptr.X, fieldptr.Y)
		case *nstyle.Item:
			c := fieldptr.Data.Color
			fmt.Fprintf(wr, "%s.%s.Type = nstyle.ItemColor\n", prefix, field.Name)
			fmt.Fprintf(wr, "%s.%s.Data.Color = color.RGBA{ %d, %d, %d, %d }\n", prefix, field.Name, c.R, c.G, c.B, c.A)
		default:
			switch field.Name {
			case "DrawBegin", "DrawEnd", "Draw":
			default:
				fmt.Fprintf(wr, "// not serializing %s.%s of type %T\n", prefix, field.Name, fieldiface)
			}
		}
	}
}

func (se *styleEditor) editColor(w *nucular.Window, name string, pc *color.RGBA) bool {
	if se.colorPickers == nil {
		se.colorPickers = make(map[*color.RGBA]*colorPicker)
	}
	cp := se.colorPickers[pc]
	if cp == nil {
		cp = newColorPicker(pc)
		se.colorPickers[pc] = cp
	}

	w.Row(20).Dynamic(3)
	w.Label(name, "LC")
	changed := false
	if cp.Edit(w) {
		changed = true
	}
	if cp.Picker(w) {
		changed = true
	}
	return changed
}

type colorPicker struct {
	h, s, v int
	r, g, b uint8
	a       int

	ed nucular.TextEditor

	dst *color.RGBA

	hslider, sslider, vslider [256]color.RGBA
	himg, simg, vimg          *image.RGBA
}

func newColorPicker(pc *color.RGBA) *colorPicker {
	cp := &colorPicker{}
	cp.dst = pc

	cp.setRGB()

	cp.ed.Flags = nucular.EditField | nucular.EditSigEnter | nucular.EditSelectable | nucular.EditClipboard
	cp.setBuffer()

	return cp
}

func (cp *colorPicker) setBuffer() {
	cp.ed.Buffer = []rune(fmt.Sprintf("%02x%02x%02x%02x", cp.dst.R, cp.dst.G, cp.dst.B, cp.dst.A))
}

func (cp *colorPicker) setRGB() {
	r, g, b, a := cp.dst.RGBA()

	cp.r = uint8(float64(r) / float64(a) * 255)
	cp.g = uint8(float64(g) / float64(a) * 255)
	cp.b = uint8(float64(b) / float64(a) * 255)
	cp.a = int(a >> 8)

	cp.h, cp.s, cp.v = rgb2hsv(cp.r, cp.g, cp.b)
	cp.recalcSliders()
}

func (cp *colorPicker) Edit(w *nucular.Window) bool {
	a := cp.ed.Edit(w)
	if a&nucular.EditCommitted != 0 {
		fmt.Sscanf(string(cp.ed.Buffer), "%2x%2x%2x%2x", &cp.dst.R, &cp.dst.G, &cp.dst.B, &cp.dst.A)
		cp.setRGB()
		return true
	}
	return false
}

func (cp *colorPicker) Picker(w *nucular.Window) bool {
	if w := w.Combo(lbl.C(color.RGBA{cp.r, cp.g, cp.b, 0xff}), 1000, nil); w != nil {
		w.Row(20).Static(20, 0)
		w.Label("H:", "LC")
		changed := false
		if colorProgress(w, &cp.h, &cp.hslider, &cp.himg) {
			changed = true
		}
		w.Label("S:", "LC")
		if colorProgress(w, &cp.s, &cp.sslider, &cp.simg) {
			changed = true
		}
		w.Label("V:", "LC")
		if colorProgress(w, &cp.v, &cp.vslider, &cp.vimg) {
			changed = true
		}
		w.Label("A:", "LC")
		if w.SliderInt(0, &cp.a, 255, 1) {
			changed = true
		}

		if changed {
			cp.r, cp.g, cp.b = hsv2rgb(cp.h, cp.s, cp.v)

			n := color.NRGBA{cp.r, cp.g, cp.b, uint8(cp.a)}
			sr, sg, sb, sa := n.RGBA()

			cp.dst.R = uint8(sr >> 8)
			cp.dst.G = uint8(sg >> 8)
			cp.dst.B = uint8(sb >> 8)
			cp.dst.A = uint8(sa >> 8)

			cp.setBuffer()
			cp.recalcSliders()
			return true
		}
	}
	return false
}

func (cp *colorPicker) recalcSliders() {
	makeSlider(&cp.hslider, func(h int) (r, g, b uint8) {
		return hsv2rgb(h, cp.s, cp.v)
	})
	makeSlider(&cp.sslider, func(s int) (r, g, b uint8) {
		return hsv2rgb(cp.h, s, cp.v)
	})
	makeSlider(&cp.vslider, func(v int) (r, g, b uint8) {
		return hsv2rgb(cp.h, cp.s, v)
	})
}

func makeSlider(slider *[256]color.RGBA, fn func(x int) (r, g, b uint8)) {
	for x := 0; x < 256; x++ {
		r, g, b := fn(x)
		slider[x] = color.RGBA{r, g, b, 0xff}
	}
}

func colorProgress(w *nucular.Window, x *int, slider *[256]color.RGBA, pimg **image.RGBA) bool {
	const maxval = 255

	state := w.CustomState()

	if state == nstyle.WidgetStateActive {
		if !w.Input().Mouse.Down(mouse.ButtonLeft) {
			state = nstyle.WidgetStateInactive
		}
	}

	bounds, out := w.Custom(state)
	if out == nil {
		return false
	}

	value := *x
	if state == nstyle.WidgetStateActive {
		ratio := float64(w.Input().Mouse.Pos.X-bounds.X) / float64(bounds.W)
		if ratio < 0 {
			ratio = 0
		}
		value = int(maxval * ratio)
		if value < 0 {
			value = 0
		}
		if value > maxval {
			value = maxval
		}
	}

	style := w.Master().Style()
	//bg := &style.Progress.Normal.Data.Color
	cursor := style.Checkbox.CursorNormal.Data.Color

	var r image.Rectangle
	{
		zbounds := bounds
		zbounds.X = 0
		zbounds.Y = 0
		r = zbounds.Rectangle()
	}

	if *pimg == nil {
		*pimg = image.NewRGBA(r)
	}

	if (*pimg).Bounds() != r {
		*pimg = image.NewRGBA(r)
	}

	r = (*pimg).Bounds()

	for x := r.Min.X; x < r.Max.X; x++ {
		c := slider[int(float64(x-r.Min.X)/float64(r.Max.X-r.Min.X)*maxval)]
		col := image.Rect(x, r.Min.X, x+1, r.Max.Y)
		col = col.Intersect(r)
		draw.Draw(*pimg, col, image.NewUniform(c), image.Point{}, draw.Src)
	}

	out.DrawImage(bounds, *pimg)

	cursorRect := bounds
	cursorRect.W = cursorRect.H
	cursorRect.X = int((float64(value)/255)*float64(bounds.W)) + bounds.X - cursorRect.W/2

	oldclip := out.Clip
	out.PushScissor(bounds)
	out.FillCircle(cursorRect, cursor)
	out.PushScissor(oldclip)

	if value != *x {
		*x = value
		return true
	}

	return false
}

func hsv2rgb(inh, ins, inv int) (outr, outg, outb uint8) {
	h, s, v := float64(inh)/255, float64(ins)/255, float64(inv)/255
	i := math.Floor(float64(h) * 6)
	f := float64(h)*6 - i
	p := float64(v) * (1 - float64(s))
	q := float64(v) * (1 - f*float64(s))
	t := float64(v) * (1 - (1-f)*float64(s))

	var r, g, b float64

	switch int(i) % 6 {
	case 0:
		r = float64(v)
		g = t
		b = p
	case 1:
		r = q
		g = float64(v)
		b = p
	case 2:
		r = p
		g = float64(v)
		b = t
	case 3:
		r = p
		g = q
		b = float64(v)
	case 4:
		r = t
		g = p
		b = float64(v)
	case 5:
		r = float64(v)
		g = p
		b = q
	}

	return uint8(r * 255), uint8(g * 255), uint8(b * 255)
}

func rgb2hsv(inr, ing, inb uint8) (int, int, int) {
	r := float64(inr) / 255
	g := float64(ing) / 255
	b := float64(inb) / 255

	max := math.Max(math.Max(r, g), b)
	min := math.Min(math.Min(r, g), b)
	var h, s float64
	v := max

	var d = max - min
	if max == 0 {
		s = 0
	} else {
		s = d / max
	}

	if max == min {
		h = 0 // achromatic
	} else {
		switch max {
		case r:
			coeff := float64(0)
			if g < b {
				coeff = 6
			}
			h = (g-b)/d + coeff
		case g:
			h = (b-r)/d + 2
		case b:
			h = (r-g)/d + 4
		}

		h /= 6
	}

	return int(h * 255), int(s * 255), int(v * 255)
}
