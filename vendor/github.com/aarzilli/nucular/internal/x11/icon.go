//go:build linux

package x11

import (
	"image"
	"unsafe"
)

/*
#cgo LDFLAGS: -lX11

#include <stdlib.h>
#include <X11/Xlib.h>
#include <X11/Xatom.h>

// XA_CARDINAL is a macro, wrap it in a function so that cgo can use it, a
// static variable would not work: cgo can not link to static variables.
static Atom cardinalAtom() { return XA_CARDINAL; }
*/
import "C"

// SetIcon sets the _NET_WM_ICON property of window w to icon.
func SetIcon(dpy unsafe.Pointer, w uintptr, icon image.Image) {
	bounds := icon.Bounds()
	if bounds.Dx() <= 0 || bounds.Dy() <= 0 {
		return
	}

	data := make([]C.long, 0, 2+bounds.Dx()*bounds.Dy())
	data = append(data, C.long(bounds.Dx()), C.long(bounds.Dy()))

	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, a := icon.At(x, y).RGBA()
			data = append(data, C.long(a/256)<<24|C.long(r/256)<<16|C.long(g/256)<<8|C.long(b/256))
		}
	}

	name := C.CString("_NET_WM_ICON")
	defer C.free(unsafe.Pointer(name))

	d := (*C.Display)(dpy)
	prop := C.XInternAtom(d, name, C.False)
	if prop == C.None {
		return
	}

	C.XChangeProperty(d, C.Window(w), prop, C.cardinalAtom(), 32, C.PropModeReplace, (*C.uchar)(unsafe.Pointer(&data[0])), C.int(len(data)))
	C.XFlush(d)
}
