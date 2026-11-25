//go:build linux

package wayland

import (
	"image"
	"unsafe"

	"golang.org/x/image/draw"
)

//go:generate sh -c "wayland-scanner client-header $(pkg-config --variable=pkgdatadir wayland-protocols)/staging/xdg-toplevel-icon/xdg-toplevel-icon-v1.xml xdg-toplevel-icon-v1-client-protocol.h"
//go:generate sh -c "wayland-scanner private-code $(pkg-config --variable=pkgdatadir wayland-protocols)/staging/xdg-toplevel-icon/xdg-toplevel-icon-v1.xml xdg-toplevel-icon-v1-protocol.c"

/*
#cgo pkg-config: wayland-client

#include "wlicon.h"
*/
import "C"

func SetIcon(display, surface, toplevel unsafe.Pointer, icon image.Image) {
	if display == nil || surface == nil || toplevel == nil {
		return
	}
	dpy := (*C.struct_wl_display)(display)

	var sizes [16]C.int32_t
	n := int(C.nucular_wl_icon_sizes(dpy, &sizes[0], C.int(len(sizes))))
	if n < 0 {
		return
	}

	size := pickSize(sizes[:n], icon.Bounds())
	if size <= 0 {
		return
	}

	pixels := iconPixels(icon, size)

	C.nucular_wl_icon_set(dpy, (*C.struct_xdg_toplevel)(toplevel), (*C.struct_wl_surface)(surface), C.int32_t(size), unsafe.Pointer(&pixels[0]))
}

// pickSize returns the size of the square icon to send to the compositor:
// the smallest size preferred by the compositor that fits the icon, or the
// size of the icon itself if the compositor has no preference.
func pickSize(sizes []C.int32_t, bounds image.Rectangle) int {
	size := bounds.Dx()
	if bounds.Dy() > size {
		size = bounds.Dy()
	}

	best := 0
	for _, sz := range sizes {
		sz := int(sz)
		if sz <= 0 {
			continue
		}
		switch {
		case best == 0:
			best = sz
		case best < size: // any bigger size is an improvement
			if sz > best {
				best = sz
			}
		case sz >= size && sz < best:
			best = sz
		}
	}
	if best != 0 {
		return best
	}
	return size
}

// iconPixels scales icon to a size×size square, preserving its aspect ratio,
// and converts it to the ARGB8888 format expected by wl_shm (i.e. 32bit
// little endian 0xAARRGGBB with premultiplied alpha).
func iconPixels(icon image.Image, size int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	bounds := icon.Bounds()
	w, h := bounds.Dx()*size, bounds.Dy()*size
	if bounds.Dx() > bounds.Dy() {
		w, h = w/bounds.Dx(), h/bounds.Dx()
	} else {
		w, h = w/bounds.Dy(), h/bounds.Dy()
	}
	r := image.Rect(0, 0, w, h).Add(image.Pt((size-w)/2, (size-h)/2))

	draw.BiLinear.Scale(img, r, icon, bounds, draw.Src, nil)

	// image.RGBA is premultiplied RGBA, swap the red and blue channels
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+2] = img.Pix[i+2], img.Pix[i]
	}

	return img.Pix
}
