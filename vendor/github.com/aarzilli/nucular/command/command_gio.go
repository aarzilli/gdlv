// +build nucular_gio

package command

import (
	"image"

	"gioui.org/op/paint"
	"github.com/aarzilli/nucular/rect"
)

type Image struct {
	Img paint.ImageOp
}

func (b *Buffer) DrawImage(r rect.Rect, img *image.RGBA) {
	if !r.Intersect(&b.Clip) {
		return
	}

	var cmd Command
	cmd.Kind = ImageCmd
	cmd.Rect = r
	cmd.Image.Img = paint.NewImageOp(img)
	b.Commands = append(b.Commands, cmd)
}
