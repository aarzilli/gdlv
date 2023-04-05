// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build darwin
// +build darwin

// Package coreanim provides access to Apple's Core Animation API
// (https://developer.apple.com/documentation/quartzcore).
//
// This package is in very early stages of development.
// It's a minimal implementation with scope limited to
// supporting mtldriver.
//
// It was copied from dmitri.shuralyov.com/gpu/mtl/example/movingtriangle/internal/coreanim.
package coreanim

import (
	"errors"
	"unsafe"

	"dmitri.shuralyov.com/gpu/mtl"
)

/*
#cgo LDFLAGS: -framework QuartzCore -framework Foundation
#include <stdbool.h>
#include "coreanim.h"
*/
import "C"

// Layer is an object that manages image-based content and
// allows you to perform animations on that content.
//
// Reference: https://developer.apple.com/documentation/quartzcore/calayer.
type Layer interface {
	// Layer returns the underlying CALayer * pointer.
	Layer() unsafe.Pointer
}

// MetalLayer is a Core Animation Metal layer, a layer that manages a pool of Metal drawables.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametallayer.
type MetalLayer struct {
	metalLayer unsafe.Pointer
}

// MakeMetalLayer creates a new Core Animation Metal layer.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametallayer.
func MakeMetalLayer() MetalLayer {
	return MetalLayer{C.MakeMetalLayer()}
}

// Layer implements the Layer interface.
func (ml MetalLayer) Layer() unsafe.Pointer { return ml.metalLayer }

// PixelFormat returns the pixel format of textures for rendering layer content.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametallayer/1478155-pixelformat.
func (ml MetalLayer) PixelFormat() mtl.PixelFormat {
	return mtl.PixelFormat(C.MetalLayer_PixelFormat(ml.metalLayer))
}

// SetDevice sets the Metal device responsible for the layer's drawable resources.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametallayer/1478163-device.
func (ml MetalLayer) SetDevice(device mtl.Device) {
	C.MetalLayer_SetDevice(ml.metalLayer, device.Device())
}

// SetPixelFormat controls the pixel format of textures for rendering layer content.
//
// The pixel format for a Metal layer must be PixelFormatBGRA8UNorm, PixelFormatBGRA8UNormSRGB,
// PixelFormatRGBA16Float, PixelFormatBGRA10XR, or PixelFormatBGRA10XRSRGB.
// SetPixelFormat panics for other values.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametallayer/1478155-pixelformat.
func (ml MetalLayer) SetPixelFormat(pf mtl.PixelFormat) {
	e := C.MetalLayer_SetPixelFormat(ml.metalLayer, C.uint16_t(pf))
	if e != nil {
		panic(errors.New(C.GoString(e)))
	}
}

// SetMaximumDrawableCount controls the number of Metal drawables in the resource pool
// managed by Core Animation.
//
// It can set to 2 or 3 only. SetMaximumDrawableCount panics for other values.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametallayer/2938720-maximumdrawablecount.
func (ml MetalLayer) SetMaximumDrawableCount(count int) {
	e := C.MetalLayer_SetMaximumDrawableCount(ml.metalLayer, C.uint_t(count))
	if e != nil {
		panic(errors.New(C.GoString(e)))
	}
}

// SetDisplaySyncEnabled controls whether the Metal layer and its drawables
// are synchronized with the display's refresh rate.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametallayer/2887087-displaysyncenabled.
func (ml MetalLayer) SetDisplaySyncEnabled(enabled bool) {
	C.MetalLayer_SetDisplaySyncEnabled(ml.metalLayer, C.bool(enabled))
}

// SetDrawableSize sets the size, in pixels, of textures for rendering layer content.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametallayer/1478174-drawablesize.
func (ml MetalLayer) SetDrawableSize(width, height int) {
	C.MetalLayer_SetDrawableSize(ml.metalLayer, C.double(width), C.double(height))
}

// NextDrawable returns a Metal drawable.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametallayer/1478172-nextdrawable.
func (ml MetalLayer) NextDrawable() (MetalDrawable, error) {
	md := C.MetalLayer_NextDrawable(ml.metalLayer)
	if md == nil {
		return MetalDrawable{}, errors.New("nextDrawable returned nil")
	}

	return MetalDrawable{md}, nil
}

// MetalDrawable is a displayable resource that can be rendered or written to by Metal.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametaldrawable.
type MetalDrawable struct {
	metalDrawable unsafe.Pointer
}

// Drawable implements the mtl.Drawable interface.
func (md MetalDrawable) Drawable() unsafe.Pointer { return md.metalDrawable }

// Texture returns a Metal texture object representing the drawable object's content.
//
// Reference: https://developer.apple.com/documentation/quartzcore/cametaldrawable/1478159-texture.
func (md MetalDrawable) Texture() mtl.Texture {
	return mtl.NewTexture(C.MetalDrawable_Texture(md.metalDrawable))
}
