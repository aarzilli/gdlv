// +build darwin

// Package mtl provides access to Apple's Metal API (https://developer.apple.com/documentation/metal).
//
// Package mtl requires macOS version 10.13 or newer.
//
// This package is in very early stages of development.
// The API will change when opportunities for improvement are discovered; it is not yet frozen.
// Less than 20% of the Metal API surface is implemented.
// Current functionality is sufficient to render very basic geometry.
package mtl

import (
	"errors"
	"fmt"
	"unsafe"
)

/*
#cgo LDFLAGS: -framework Metal -framework CoreGraphics -framework Foundation
#include <stdlib.h>
#include <stdbool.h>
#include "mtl.h"
struct Library Go_Device_MakeLibrary(void * device, _GoString_ source) {
	return Device_MakeLibrary(device, _GoStringPtr(source), _GoStringLen(source));
}
*/
import "C"

// FeatureSet defines a specific platform, hardware, and software configuration.
//
// Reference: https://developer.apple.com/documentation/metal/mtlfeatureset.
type FeatureSet uint16

// The device feature sets that define specific platform, hardware, and software configurations.
const (
	MacOSGPUFamily1V1          FeatureSet = 10000 // The GPU family 1, version 1 feature set for macOS.
	MacOSGPUFamily1V2          FeatureSet = 10001 // The GPU family 1, version 2 feature set for macOS.
	MacOSReadWriteTextureTier2 FeatureSet = 10002 // The read-write texture, tier 2 feature set for macOS.
	MacOSGPUFamily1V3          FeatureSet = 10003 // The GPU family 1, version 3 feature set for macOS.
	MacOSGPUFamily1V4          FeatureSet = 10004 // The GPU family 1, version 4 feature set for macOS.
	MacOSGPUFamily2V1          FeatureSet = 10005 // The GPU family 2, version 1 feature set for macOS.
)

// PixelFormat defines data formats that describe the organization
// and characteristics of individual pixels in a texture.
//
// Reference: https://developer.apple.com/documentation/metal/mtlpixelformat.
type PixelFormat uint8

// The data formats that describe the organization and characteristics
// of individual pixels in a texture.
const (
	PixelFormatRGBA8UNorm     PixelFormat = 70 // Ordinary format with four 8-bit normalized unsigned integer components in RGBA order.
	PixelFormatBGRA8UNorm     PixelFormat = 80 // Ordinary format with four 8-bit normalized unsigned integer components in BGRA order.
	PixelFormatBGRA8UNormSRGB PixelFormat = 81 // Ordinary format with four 8-bit normalized unsigned integer components in BGRA order with conversion between sRGB and linear space.
)

// PrimitiveType defines geometric primitive types for drawing commands.
//
// Reference: https://developer.apple.com/documentation/metal/mtlprimitivetype.
type PrimitiveType uint8

// Geometric primitive types for drawing commands.
const (
	PrimitiveTypePoint         PrimitiveType = 0
	PrimitiveTypeLine          PrimitiveType = 1
	PrimitiveTypeLineStrip     PrimitiveType = 2
	PrimitiveTypeTriangle      PrimitiveType = 3
	PrimitiveTypeTriangleStrip PrimitiveType = 4
)

// LoadAction defines actions performed at the start of a rendering pass
// for a render command encoder.
//
// Reference: https://developer.apple.com/documentation/metal/mtlloadaction.
type LoadAction uint8

// Actions performed at the start of a rendering pass for a render command encoder.
const (
	LoadActionDontCare LoadAction = 0
	LoadActionLoad     LoadAction = 1
	LoadActionClear    LoadAction = 2
)

// StoreAction defines actions performed at the end of a rendering pass
// for a render command encoder.
//
// Reference: https://developer.apple.com/documentation/metal/mtlstoreaction.
type StoreAction uint8

// Actions performed at the end of a rendering pass for a render command encoder.
const (
	StoreActionDontCare                   StoreAction = 0
	StoreActionStore                      StoreAction = 1
	StoreActionMultisampleResolve         StoreAction = 2
	StoreActionStoreAndMultisampleResolve StoreAction = 3
	StoreActionUnknown                    StoreAction = 4
	StoreActionCustomSampleDepthStore     StoreAction = 5
)

// StorageMode defines defines the memory location and access permissions of a resource.
//
// Reference: https://developer.apple.com/documentation/metal/mtlstoragemode.
type StorageMode uint8

const (
	// StorageModeShared indicates that the resource is stored in system memory
	// accessible to both the CPU and the GPU.
	StorageModeShared StorageMode = 0

	// StorageModeManaged indicates that the resource exists as a synchronized
	// memory pair with one copy stored in system memory accessible to the CPU
	// and another copy stored in video memory accessible to the GPU.
	StorageModeManaged StorageMode = 1

	// StorageModePrivate indicates that the resource is stored in memory
	// only accessible to the GPU. In iOS and tvOS, the resource is stored in
	// system memory. In macOS, the resource is stored in video memory.
	StorageModePrivate StorageMode = 2

	// StorageModeMemoryless indicates that the resource is stored in on-tile memory,
	// without CPU or GPU memory backing. The contents of the on-tile memory are undefined
	// and do not persist; the only way to populate the resource is to render into it.
	// Memoryless resources are limited to temporary render targets (i.e., Textures configured
	// with a TextureDescriptor and used with a RenderPassAttachmentDescriptor).
	StorageModeMemoryless StorageMode = 3
)

// ResourceOptions defines optional arguments used to create
// and influence behavior of buffer and texture objects.
//
// Reference: https://developer.apple.com/documentation/metal/mtlresourceoptions.
type ResourceOptions uint16

const (
	// ResourceCPUCacheModeDefaultCache is the default CPU cache mode for the resource.
	// Guarantees that read and write operations are executed in the expected order.
	ResourceCPUCacheModeDefaultCache ResourceOptions = ResourceOptions(CPUCacheModeDefaultCache) << resourceCPUCacheModeShift

	// ResourceCPUCacheModeWriteCombined is a write-combined CPU cache mode for the resource.
	// Optimized for resources that the CPU will write into, but never read.
	ResourceCPUCacheModeWriteCombined ResourceOptions = ResourceOptions(CPUCacheModeWriteCombined) << resourceCPUCacheModeShift

	// ResourceStorageModeShared indicates that the resource is stored in system memory
	// accessible to both the CPU and the GPU.
	ResourceStorageModeShared ResourceOptions = ResourceOptions(StorageModeShared) << resourceStorageModeShift

	// ResourceStorageModeManaged indicates that the resource exists as a synchronized
	// memory pair with one copy stored in system memory accessible to the CPU
	// and another copy stored in video memory accessible to the GPU.
	ResourceStorageModeManaged ResourceOptions = ResourceOptions(StorageModeManaged) << resourceStorageModeShift

	// ResourceStorageModePrivate indicates that the resource is stored in memory
	// only accessible to the GPU. In iOS and tvOS, the resource is stored
	// in system memory. In macOS, the resource is stored in video memory.
	ResourceStorageModePrivate ResourceOptions = ResourceOptions(StorageModePrivate) << resourceStorageModeShift

	// ResourceStorageModeMemoryless indicates that the resource is stored in on-tile memory,
	// without CPU or GPU memory backing. The contents of the on-tile memory are undefined
	// and do not persist; the only way to populate the resource is to render into it.
	// Memoryless resources are limited to temporary render targets (i.e., Textures configured
	// with a TextureDescriptor and used with a RenderPassAttachmentDescriptor).
	ResourceStorageModeMemoryless ResourceOptions = ResourceOptions(StorageModeMemoryless) << resourceStorageModeShift

	// ResourceHazardTrackingModeUntracked indicates that the command encoder dependencies
	// for this resource are tracked manually with Fence objects. This value is always set
	// for resources sub-allocated from a Heap object and may optionally be specified for
	// non-heap resources.
	ResourceHazardTrackingModeUntracked ResourceOptions = 1 << resourceHazardTrackingModeShift
)

const (
	resourceCPUCacheModeShift       = 0
	resourceStorageModeShift        = 4
	resourceHazardTrackingModeShift = 8
)

// CPUCacheMode is the CPU cache mode that defines the CPU mapping of a resource.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcpucachemode.
type CPUCacheMode uint8

const (
	// CPUCacheModeDefaultCache is the default CPU cache mode for the resource.
	// Guarantees that read and write operations are executed in the expected order.
	CPUCacheModeDefaultCache CPUCacheMode = 0

	// CPUCacheModeWriteCombined is a write-combined CPU cache mode for the resource.
	// Optimized for resources that the CPU will write into, but never read.
	CPUCacheModeWriteCombined CPUCacheMode = 1
)

// Resource represents a memory allocation for storing specialized data
// that is accessible to the GPU.
//
// Reference: https://developer.apple.com/documentation/metal/mtlresource.
type Resource interface {
	// resource returns the underlying id<MTLResource> pointer.
	resource() unsafe.Pointer
}

// RenderPipelineDescriptor configures new RenderPipelineState objects.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrenderpipelinedescriptor.
type RenderPipelineDescriptor struct {
	// VertexFunction is a programmable function that processes individual vertices in a rendering pass.
	VertexFunction Function

	// FragmentFunction is a programmable function that processes individual fragments in a rendering pass.
	FragmentFunction Function

	// ColorAttachments is an array of attachments that store color data.
	ColorAttachments [1]RenderPipelineColorAttachmentDescriptor
}

// RenderPipelineColorAttachmentDescriptor describes a color render target that specifies
// the color configuration and color operations associated with a render pipeline.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrenderpipelinecolorattachmentdescriptor.
type RenderPipelineColorAttachmentDescriptor struct {
	// PixelFormat is the pixel format of the color attachment's texture.
	PixelFormat PixelFormat
}

// RenderPassDescriptor describes a group of render targets that serve as
// the output destination for pixels generated by a render pass.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrenderpassdescriptor.
type RenderPassDescriptor struct {
	// ColorAttachments is array of state information for attachments that store color data.
	ColorAttachments [1]RenderPassColorAttachmentDescriptor
}

// RenderPassColorAttachmentDescriptor describes a color render target that serves
// as the output destination for color pixels generated by a render pass.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrenderpasscolorattachmentdescriptor.
type RenderPassColorAttachmentDescriptor struct {
	RenderPassAttachmentDescriptor
	ClearColor ClearColor
}

// RenderPassAttachmentDescriptor describes a render target that serves
// as the output destination for pixels generated by a render pass.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrenderpassattachmentdescriptor.
type RenderPassAttachmentDescriptor struct {
	LoadAction  LoadAction
	StoreAction StoreAction
	Texture     Texture
}

// ClearColor is an RGBA value used for a color pixel.
//
// Reference: https://developer.apple.com/documentation/metal/mtlclearcolor.
type ClearColor struct {
	Red, Green, Blue, Alpha float64
}

// TextureDescriptor configures new Texture objects.
//
// Reference: https://developer.apple.com/documentation/metal/mtltexturedescriptor.
type TextureDescriptor struct {
	PixelFormat PixelFormat
	Width       int
	Height      int
	StorageMode StorageMode
}

// Device is abstract representation of the GPU that
// serves as the primary interface for a Metal app.
//
// Reference: https://developer.apple.com/documentation/metal/mtldevice.
type Device struct {
	device unsafe.Pointer

	// Headless indicates whether a device is configured as headless.
	Headless bool

	// LowPower indicates whether a device is low-power.
	LowPower bool

	// Removable determines whether or not a GPU is removable.
	Removable bool

	// RegistryID is the registry ID value for the device.
	RegistryID uint64

	// Name is the name of the device.
	Name string
}

// CreateSystemDefaultDevice returns the preferred system default Metal device.
//
// Reference: https://developer.apple.com/documentation/metal/1433401-mtlcreatesystemdefaultdevice.
func CreateSystemDefaultDevice() (Device, error) {
	d := C.CreateSystemDefaultDevice()
	if d.Device == nil {
		return Device{}, errors.New("Metal is not supported on this system")
	}

	return Device{
		device:     d.Device,
		Headless:   bool(d.Headless),
		LowPower:   bool(d.LowPower),
		Removable:  bool(d.Removable),
		RegistryID: uint64(d.RegistryID),
		Name:       C.GoString(d.Name),
	}, nil
}

// CopyAllDevices returns all Metal devices in the system.
//
// Reference: https://developer.apple.com/documentation/metal/1433367-mtlcopyalldevices.
func CopyAllDevices() []Device {
	d := C.CopyAllDevices()
	defer C.free(unsafe.Pointer(d.Devices))

	ds := make([]Device, d.Length)
	for i := 0; i < len(ds); i++ {
		d := (*C.struct_Device)(unsafe.Pointer(uintptr(unsafe.Pointer(d.Devices)) + uintptr(i)*C.sizeof_struct_Device))

		ds[i].device = d.Device
		ds[i].Headless = bool(d.Headless)
		ds[i].LowPower = bool(d.LowPower)
		ds[i].Removable = bool(d.Removable)
		ds[i].RegistryID = uint64(d.RegistryID)
		ds[i].Name = C.GoString(d.Name)
	}
	return ds
}

// Device returns the underlying id<MTLDevice> pointer.
func (d Device) Device() unsafe.Pointer { return d.device }

// SupportsFeatureSet reports whether device d supports feature set fs.
//
// Reference: https://developer.apple.com/documentation/metal/mtldevice/1433418-supportsfeatureset.
func (d Device) SupportsFeatureSet(fs FeatureSet) bool {
	return bool(C.Device_SupportsFeatureSet(d.device, C.uint16_t(fs)))
}

// MakeCommandQueue creates a serial command submission queue.
//
// Reference: https://developer.apple.com/documentation/metal/mtldevice/1433388-makecommandqueue.
func (d Device) MakeCommandQueue() CommandQueue {
	return CommandQueue{C.Device_MakeCommandQueue(d.device)}
}

// MakeLibrary creates a new library that contains
// the functions stored in the specified source string.
//
// Reference: https://developer.apple.com/documentation/metal/mtldevice/1433431-makelibrary.
func (d Device) MakeLibrary(source string, opt CompileOptions) (Library, error) {
	l := C.Go_Device_MakeLibrary(d.device, source) // TODO: opt.
	if l.Library == nil {
		return Library{}, errors.New(C.GoString(l.Error))
	}

	return Library{l.Library}, nil
}

// MakeRenderPipelineState creates a render pipeline state object.
//
// Reference: https://developer.apple.com/documentation/metal/mtldevice/1433369-makerenderpipelinestate.
func (d Device) MakeRenderPipelineState(rpd RenderPipelineDescriptor) (RenderPipelineState, error) {
	descriptor := C.struct_RenderPipelineDescriptor{
		VertexFunction:              rpd.VertexFunction.function,
		FragmentFunction:            rpd.FragmentFunction.function,
		ColorAttachment0PixelFormat: C.uint16_t(rpd.ColorAttachments[0].PixelFormat),
	}
	rps := C.Device_MakeRenderPipelineState(d.device, descriptor)
	if rps.RenderPipelineState == nil {
		return RenderPipelineState{}, errors.New(C.GoString(rps.Error))
	}

	return RenderPipelineState{rps.RenderPipelineState}, nil
}

// MakeBuffer allocates a new buffer of a given length
// and initializes its contents by copying existing data into it.
//
// Reference: https://developer.apple.com/documentation/metal/mtldevice/1433429-makebuffer.
func (d Device) MakeBuffer(bytes unsafe.Pointer, length uintptr, opt ResourceOptions) Buffer {
	return Buffer{C.Device_MakeBuffer(d.device, bytes, C.size_t(length), C.uint16_t(opt))}
}

// MakeTexture creates a texture object with privately owned storage
// that contains texture state.
//
// Reference: https://developer.apple.com/documentation/metal/mtldevice/1433425-maketexture.
func (d Device) MakeTexture(td TextureDescriptor) Texture {
	descriptor := C.struct_TextureDescriptor{
		PixelFormat: C.uint16_t(td.PixelFormat),
		Width:       C.uint_t(td.Width),
		Height:      C.uint_t(td.Height),
		StorageMode: C.uint8_t(td.StorageMode),
	}
	return Texture{
		texture: C.Device_MakeTexture(d.device, descriptor),
		Width:   td.Width,  // TODO: Fetch dimensions of actually created texture.
		Height:  td.Height, // TODO: Fetch dimensions of actually created texture.
	}
}

// CompileOptions specifies optional compilation settings for
// the graphics or compute functions within a library.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcompileoptions.
type CompileOptions struct {
	// TODO.
}

// Drawable is a displayable resource that can be rendered or written to.
//
// Reference: https://developer.apple.com/documentation/metal/mtldrawable.
type Drawable interface {
	// Drawable returns the underlying id<MTLDrawable> pointer.
	Drawable() unsafe.Pointer
}

// CommandQueue is a queue that organizes the order
// in which command buffers are executed by the GPU.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandqueue.
type CommandQueue struct {
	commandQueue unsafe.Pointer
}

// MakeCommandBuffer creates a command buffer.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandqueue/1508686-makecommandbuffer.
func (cq CommandQueue) MakeCommandBuffer() CommandBuffer {
	return CommandBuffer{C.CommandQueue_MakeCommandBuffer(cq.commandQueue)}
}

// CommandBuffer is a container that stores encoded commands
// that are committed to and executed by the GPU.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandbuffer.
type CommandBuffer struct {
	commandBuffer unsafe.Pointer
}

// PresentDrawable registers a drawable presentation to occur as soon as possible.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandbuffer/1443029-presentdrawable.
func (cb CommandBuffer) PresentDrawable(d Drawable) {
	C.CommandBuffer_PresentDrawable(cb.commandBuffer, d.Drawable())
}

// Commit commits this command buffer for execution as soon as possible.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandbuffer/1443003-commit.
func (cb CommandBuffer) Commit() {
	C.CommandBuffer_Commit(cb.commandBuffer)
}

// WaitUntilCompleted waits for the execution of this command buffer to complete.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandbuffer/1443039-waituntilcompleted.
func (cb CommandBuffer) WaitUntilCompleted() {
	C.CommandBuffer_WaitUntilCompleted(cb.commandBuffer)
}

// MakeRenderCommandEncoder creates an encoder object that can
// encode graphics rendering commands into this command buffer.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandbuffer/1442999-makerendercommandencoder.
func (cb CommandBuffer) MakeRenderCommandEncoder(rpd RenderPassDescriptor) RenderCommandEncoder {
	descriptor := C.struct_RenderPassDescriptor{
		ColorAttachment0LoadAction:  C.uint8_t(rpd.ColorAttachments[0].LoadAction),
		ColorAttachment0StoreAction: C.uint8_t(rpd.ColorAttachments[0].StoreAction),
		ColorAttachment0ClearColor: C.struct_ClearColor{
			Red:   C.double(rpd.ColorAttachments[0].ClearColor.Red),
			Green: C.double(rpd.ColorAttachments[0].ClearColor.Green),
			Blue:  C.double(rpd.ColorAttachments[0].ClearColor.Blue),
			Alpha: C.double(rpd.ColorAttachments[0].ClearColor.Alpha),
		},
		ColorAttachment0Texture: rpd.ColorAttachments[0].Texture.texture,
	}
	return RenderCommandEncoder{CommandEncoder{C.CommandBuffer_MakeRenderCommandEncoder(cb.commandBuffer, descriptor)}}
}

// MakeBlitCommandEncoder creates an encoder object that can encode
// memory operation (blit) commands into this command buffer.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandbuffer/1443001-makeblitcommandencoder.
func (cb CommandBuffer) MakeBlitCommandEncoder() BlitCommandEncoder {
	return BlitCommandEncoder{CommandEncoder{C.CommandBuffer_MakeBlitCommandEncoder(cb.commandBuffer)}}
}

// CommandEncoder is an encoder that writes sequential GPU commands
// into a command buffer.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandencoder.
type CommandEncoder struct {
	commandEncoder unsafe.Pointer
}

// EndEncoding declares that all command generation from this encoder is completed.
//
// Reference: https://developer.apple.com/documentation/metal/mtlcommandencoder/1458038-endencoding.
func (ce CommandEncoder) EndEncoding() {
	C.CommandEncoder_EndEncoding(ce.commandEncoder)
}

// RenderCommandEncoder is an encoder that specifies graphics-rendering commands
// and executes graphics functions.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder.
type RenderCommandEncoder struct {
	CommandEncoder
}

// SetRenderPipelineState sets the current render pipeline state object.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1515811-setrenderpipelinestate.
func (rce RenderCommandEncoder) SetRenderPipelineState(rps RenderPipelineState) {
	C.RenderCommandEncoder_SetRenderPipelineState(rce.commandEncoder, rps.renderPipelineState)
}

// SetVertexBuffer sets a buffer for the vertex shader function at an index
// in the buffer argument table with an offset that specifies the start of the data.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1515829-setvertexbuffer.
func (rce RenderCommandEncoder) SetVertexBuffer(buf Buffer, offset, index int) {
	C.RenderCommandEncoder_SetVertexBuffer(rce.commandEncoder, buf.buffer, C.uint_t(offset), C.uint_t(index))
}

// SetVertexBytes sets a block of data for the vertex function.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1515846-setvertexbytes.
func (rce RenderCommandEncoder) SetVertexBytes(bytes unsafe.Pointer, length uintptr, index int) {
	C.RenderCommandEncoder_SetVertexBytes(rce.commandEncoder, bytes, C.size_t(length), C.uint_t(index))
}

// DrawPrimitives renders one instance of primitives using vertex data
// in contiguous array elements.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrendercommandencoder/1516326-drawprimitives.
func (rce RenderCommandEncoder) DrawPrimitives(typ PrimitiveType, vertexStart, vertexCount int) {
	C.RenderCommandEncoder_DrawPrimitives(rce.commandEncoder, C.uint8_t(typ), C.uint_t(vertexStart), C.uint_t(vertexCount))
}

// BlitCommandEncoder is an encoder that specifies resource copy
// and resource synchronization commands.
//
// Reference: https://developer.apple.com/documentation/metal/mtlblitcommandencoder.
type BlitCommandEncoder struct {
	CommandEncoder
}

// CopyFromTexture encodes a command to copy image data from a slice of
// a source texture into a slice of a destination texture.
//
// Reference: https://developer.apple.com/documentation/metal/mtlblitcommandencoder/1400754-copyfromtexture.
func (bce BlitCommandEncoder) CopyFromTexture(
	src Texture, srcSlice, srcLevel int, srcOrigin Origin, srcSize Size,
	dst Texture, dstSlice, dstLevel int, dstOrigin Origin,
) {
	C.BlitCommandEncoder_CopyFromTexture(
		bce.commandEncoder,
		src.texture, C.uint_t(srcSlice), C.uint_t(srcLevel),
		C.struct_Origin{
			X: C.uint_t(srcOrigin.X),
			Y: C.uint_t(srcOrigin.Y),
			Z: C.uint_t(srcOrigin.Z),
		},
		C.struct_Size{
			Width:  C.uint_t(srcSize.Width),
			Height: C.uint_t(srcSize.Height),
			Depth:  C.uint_t(srcSize.Depth),
		},
		dst.texture, C.uint_t(dstSlice), C.uint_t(dstLevel),
		C.struct_Origin{
			X: C.uint_t(dstOrigin.X),
			Y: C.uint_t(dstOrigin.Y),
			Z: C.uint_t(dstOrigin.Z),
		},
	)
}

// Synchronize flushes any copy of the specified resource from its corresponding
// Device caches and, if needed, invalidates any CPU caches.
//
// Reference: https://developer.apple.com/documentation/metal/mtlblitcommandencoder/1400775-synchronize.
func (bce BlitCommandEncoder) Synchronize(resource Resource) {
	C.BlitCommandEncoder_Synchronize(bce.commandEncoder, resource.resource())
}

// Library is a collection of compiled graphics or compute functions.
//
// Reference: https://developer.apple.com/documentation/metal/mtllibrary.
type Library struct {
	library unsafe.Pointer
}

// MakeFunction returns a pre-compiled, non-specialized function.
//
// Reference: https://developer.apple.com/documentation/metal/mtllibrary/1515524-makefunction.
func (l Library) MakeFunction(name string) (Function, error) {
	f := C.Library_MakeFunction(l.library, C.CString(name))
	if f == nil {
		return Function{}, fmt.Errorf("function %q not found", name)
	}

	return Function{f}, nil
}

// Texture is a memory allocation for storing formatted
// image data that is accessible to the GPU.
//
// Reference: https://developer.apple.com/documentation/metal/mtltexture.
type Texture struct {
	texture unsafe.Pointer

	// TODO: Change these fields into methods.

	// Width is the width of the texture image for the base level mipmap, in pixels.
	Width int

	// Height is the height of the texture image for the base level mipmap, in pixels.
	Height int
}

// NewTexture returns a Texture that wraps an existing id<MTLTexture> pointer.
func NewTexture(texture unsafe.Pointer) Texture {
	return Texture{texture: texture}
}

// resource implements the Resource interface.
func (t Texture) resource() unsafe.Pointer { return t.texture }

// ReplaceRegion copies a block of pixels into a section of texture slice 0.
//
// Reference: https://developer.apple.com/documentation/metal/mtltexture/1515464-replaceregion.
func (t Texture) ReplaceRegion(region Region, level int, pixelBytes *byte, bytesPerRow uintptr) {
	r := C.struct_Region{
		Origin: C.struct_Origin{
			X: C.uint_t(region.Origin.X),
			Y: C.uint_t(region.Origin.Y),
			Z: C.uint_t(region.Origin.Z),
		},
		Size: C.struct_Size{
			Width:  C.uint_t(region.Size.Width),
			Height: C.uint_t(region.Size.Height),
			Depth:  C.uint_t(region.Size.Depth),
		},
	}
	C.Texture_ReplaceRegion(t.texture, r, C.uint_t(level), unsafe.Pointer(pixelBytes), C.size_t(bytesPerRow))
}

// GetBytes copies a block of pixels from the storage allocation of texture
// slice zero into system memory at a specified address.
//
// Reference: https://developer.apple.com/documentation/metal/mtltexture/1515751-getbytes.
func (t Texture) GetBytes(pixelBytes *byte, bytesPerRow uintptr, region Region, level int) {
	r := C.struct_Region{
		Origin: C.struct_Origin{
			X: C.uint_t(region.Origin.X),
			Y: C.uint_t(region.Origin.Y),
			Z: C.uint_t(region.Origin.Z),
		},
		Size: C.struct_Size{
			Width:  C.uint_t(region.Size.Width),
			Height: C.uint_t(region.Size.Height),
			Depth:  C.uint_t(region.Size.Depth),
		},
	}
	C.Texture_GetBytes(t.texture, unsafe.Pointer(pixelBytes), C.size_t(bytesPerRow), r, C.uint_t(level))
}

// Buffer is a memory allocation for storing unformatted data
// that is accessible to the GPU.
//
// Reference: https://developer.apple.com/documentation/metal/mtlbuffer.
type Buffer struct {
	buffer unsafe.Pointer
}

// Function represents a programmable graphics or compute function executed by the GPU.
//
// Reference: https://developer.apple.com/documentation/metal/mtlfunction.
type Function struct {
	function unsafe.Pointer
}

// RenderPipelineState contains the graphics functions
// and configuration state used in a render pass.
//
// Reference: https://developer.apple.com/documentation/metal/mtlrenderpipelinestate.
type RenderPipelineState struct {
	renderPipelineState unsafe.Pointer
}

// Region is a rectangular block of pixels in an image or texture,
// defined by its upper-left corner and its size.
//
// Reference: https://developer.apple.com/documentation/metal/mtlregion.
type Region struct {
	Origin Origin // The location of the upper-left corner of the block.
	Size   Size   // The size of the block.
}

// Origin represents the location of a pixel in an image or texture relative
// to the upper-left corner, whose coordinates are (0, 0).
//
// Reference: https://developer.apple.com/documentation/metal/mtlorigin.
type Origin struct{ X, Y, Z int }

// Size represents the set of dimensions that declare the size of an object,
// such as an image, texture, threadgroup, or grid.
//
// Reference: https://developer.apple.com/documentation/metal/mtlsize.
type Size struct{ Width, Height, Depth int }

// RegionMake2D returns a 2D, rectangular region for image or texture data.
//
// Reference: https://developer.apple.com/documentation/metal/1515675-mtlregionmake2d.
func RegionMake2D(x, y, width, height int) Region {
	return Region{
		Origin: Origin{x, y, 0},
		Size:   Size{width, height, 1},
	}
}
