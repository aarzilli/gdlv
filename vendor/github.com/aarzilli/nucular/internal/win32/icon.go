//go:build windows

package win32

import (
	"image"
	"image/draw"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

const (
	wmSetIcon = 0x0080
	iconBig   = 1

	biRGB        = 0
	dibRGBColors = 0
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")

	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
	procSendMessage        = user32.NewProc("SendMessageW")

	procCreateBitmap     = gdi32.NewProc("CreateBitmap")
	procCreateDIBSection = gdi32.NewProc("CreateDIBSection")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
)

type bitmapInfoHeader struct {
	size          uint32
	width, height int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
}

type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]uint32
}

type iconInfo struct {
	fIcon    int32
	xHotspot uint32
	yHotspot uint32
	hbmMask  syscall.Handle
	hbmColor syscall.Handle
}

// Icon currently set on a window. It must be kept alive until it is replaced,
// WM_SETICON does not take ownership of it.
var icons struct {
	sync.Mutex
	m map[uintptr]syscall.Handle
}

// SetIcon sets the icon of window hwnd. The icon must be square.
func SetIcon(hwnd uintptr, icon image.Image) {
	bounds := icon.Bounds()
	if hwnd == 0 || bounds.Dx() <= 0 || bounds.Dx() != bounds.Dy() {
		return
	}

	h := createIcon(icon)
	if h == 0 {
		return
	}
	procSendMessage.Call(hwnd, wmSetIcon, iconBig, uintptr(h))

	icons.Lock()
	defer icons.Unlock()
	if old := icons.m[hwnd]; old != 0 {
		procDestroyIcon.Call(uintptr(old))
	}
	if icons.m == nil {
		icons.m = make(map[uintptr]syscall.Handle)
	}
	icons.m[hwnd] = h
}

// createIcon returns a HICON with the contents of icon, or 0 if it could not
// be created.
func createIcon(icon image.Image) syscall.Handle {
	size := icon.Bounds().Dx()

	img, _ := icon.(*image.RGBA)
	if img == nil || img.Bounds() != image.Rect(0, 0, size, size) {
		img = image.NewRGBA(image.Rect(0, 0, size, size))
		draw.Draw(img, img.Bounds(), icon, icon.Bounds().Min, draw.Src)
	}

	var bi bitmapInfo
	bi.header.size = uint32(unsafe.Sizeof(bi.header))
	bi.header.width = int32(size)
	bi.header.height = -int32(size) // negative means top-down
	bi.header.planes = 1
	bi.header.bitCount = 32
	bi.header.compression = biRGB

	var bits unsafe.Pointer
	hbmColor, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	runtime.KeepAlive(&bi)
	if hbmColor == 0 {
		return 0
	}

	// image.RGBA is premultiplied RGBA, the DIB wants premultiplied BGRA
	pix := unsafe.Slice((*byte)(bits), len(img.Pix))
	for i := 0; i < len(pix); i += 4 {
		pix[i+0] = img.Pix[i+2]
		pix[i+1] = img.Pix[i+1]
		pix[i+2] = img.Pix[i+0]
		pix[i+3] = img.Pix[i+3]
	}

	// the alpha channel of hbmColor determines the shape of the icon but a
	// mask bitmap must be supplied anyway, its rows are word aligned
	mask := make([]byte, ((size+15)/16*2)*size)
	hbmMask, _, _ := procCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, uintptr(unsafe.Pointer(&mask[0])))
	runtime.KeepAlive(mask)
	if hbmMask == 0 {
		procDeleteObject.Call(hbmColor)
		return 0
	}

	ii := iconInfo{
		fIcon:    1,
		hbmMask:  syscall.Handle(hbmMask),
		hbmColor: syscall.Handle(hbmColor),
	}
	// CreateIconIndirect copies the bitmaps
	h, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	runtime.KeepAlive(&ii)
	procDeleteObject.Call(hbmMask)
	procDeleteObject.Call(hbmColor)

	return syscall.Handle(h)
}
