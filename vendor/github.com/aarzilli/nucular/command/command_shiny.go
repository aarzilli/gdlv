// +build !nucular_gio

package command

import (
	"github.com/aarzilli/nucular/rect"
	"image"
)

type Image struct {
	Img *image.RGBA
}

func (b *Buffer) DrawImage(r rect.Rect, img *image.RGBA) {
	if !r.Intersect(&b.Clip) {
		return
	}

	var cmd Command
	cmd.Kind = ImageCmd
	cmd.Rect = r
	cmd.Image.Img = img
	b.Commands = append(b.Commands, cmd)
}
