package proc

import (
	"debug/dwarf"
	"errors"
	"fmt"

	"github.com/derekparker/delve/pkg/dwarf/frame"
	"github.com/derekparker/delve/pkg/dwarf/op"
)

// This code is partly adaped from runtime.gentraceback in
// $GOROOT/src/runtime/traceback.go

const (
	runtimeStackBarrier = "runtime.stackBarrier"
	crosscall2          = "crosscall2"
)

// NoReturnAddr is returned when return address
// could not be found during stack trace.
type NoReturnAddr struct {
	Fn string
}

func (nra NoReturnAddr) Error() string {
	return fmt.Sprintf("could not find return address for %s", nra.Fn)
}

// Stackframe represents a frame in a system stack.
type Stackframe struct {
	// Address the function above this one on the call stack will return to.
	Current Location
	// Address of the call instruction for the function above on the call stack.
	Call Location
	// Start address of the stack frame.
	CFA int64
	// Value of DWARF register "frame base"
	hasfb bool
	fb    int64
	// Value of frame pointer for this frame.
	bp uint64
	// Hardware registers for the thread executing this frame (nil for parked goroutines)
	regs Registers
	// High address of the stack.
	stackHi uint64
	// Description of the stack frame.
	FDE *frame.FrameDescriptionEntry
	// Return address for this stack frame (as read from the stack frame itself).
	Ret uint64
	// Address to the memory location containing the return address
	addrret uint64
	// Err is set if an error occoured during stacktrace
	Err error
	// SystemStack is true if this frame belongs to a system stack.
	SystemStack bool
}

// FrameOffset returns the address of the stack frame, absolute for system
// stack frames or as an offset from stackhi for goroutine stacks (a
// negative value).
func (frame *Stackframe) FrameOffset() int64 {
	if frame.SystemStack {
		return frame.CFA
	}
	return frame.CFA - int64(frame.stackHi)
}

// FramePointerOffset returns the value of the frame pointer, absolute for
// system stack frames or as an offset from stackhi for goroutine stacks (a
// negative value).
func (frame *Stackframe) FramePointerOffset() int64 {
	if frame.SystemStack {
		return int64(frame.bp)
	}
	return int64(frame.bp) - int64(frame.stackHi)
}

// frameBase returns the value of the DWARF frame base register for this frame.
func (frame *Stackframe) frameBase(bi *BinaryInfo) {
	if frame.Current.Fn == nil {
		return
	}
	rdr := bi.dwarf.Reader()
	rdr.Seek(frame.Current.Fn.offset)
	e, err := rdr.Next()
	if err != nil {
		return
	}
	frame.fb, _, _, _ = bi.Location(e, dwarf.AttrFrameBase, frame.Current.PC, op.DwarfRegisters{frame.CFA, 0, 0, func(i int) []byte { return GetDwarfRegister(frame.regs, i) }})
}

func (frame *Stackframe) DwarfRegisters(bi *BinaryInfo) op.DwarfRegisters {
	if !frame.hasfb {
		frame.hasfb = true
		frame.frameBase(bi)
	}
	return op.DwarfRegisters{frame.CFA, frame.fb, 0, func(i int) []byte { return GetDwarfRegister(frame.regs, i) }}
}

// ThreadStacktrace returns the stack trace for thread.
// Note the locations in the array are return addresses not call addresses.
func ThreadStacktrace(thread Thread, depth int) ([]Stackframe, error) {
	g, _ := GetG(thread)
	if g == nil {
		regs, err := thread.Registers(true)
		if err != nil {
			return nil, err
		}
		it := &stackIterator{bi: thread.BinInfo(), mem: thread, pc: regs.PC(), sp: regs.SP(), bp: regs.BP(), regs: regs, stackhi: 0, stkbar: nil, stkbarPos: -1, systemstack: true}
		return it.stacktrace(depth)
	}
	return g.Stacktrace(depth)
}

func (g *G) stackIterator() (*stackIterator, error) {
	stkbar, err := g.stkbar()
	if err != nil {
		return nil, err
	}
	var g0_sched_sp uint64
	g0var, _ := g.variable.fieldVariable("m").structMember("g0")
	if g0var != nil {
		g0, _ := g0var.parseG()
		if g0 != nil {
			g0_sched_sp = g0.SP
		}
	}

	pc := g.PC
	sp := g.SP
	bp := g.BP
	var mem MemoryReadWriter = g.variable.mem

	var regs Registers
	if g.Thread != nil {
		regs, err = g.Thread.Registers(true)
		if err != nil {
			return nil, err
		}
		mem = g.Thread
		pc = regs.PC()
		sp = regs.SP()
		bp = regs.BP()
	}
	return &stackIterator{bi: g.variable.bi, mem: mem, pc: pc, sp: sp, bp: bp, regs: regs, stackhi: g.stackhi, stkbar: stkbar, stkbarPos: g.stkbarPos, systemstack: g.SystemStack, g0_sched_sp: g0_sched_sp, g: g}, nil
}

// Stacktrace returns the stack trace for a goroutine.
// Note the locations in the array are return addresses not call addresses.
func (g *G) Stacktrace(depth int) ([]Stackframe, error) {
	it, err := g.stackIterator()
	if err != nil {
		return nil, err
	}
	return it.stacktrace(depth)
}

// NullAddrError is an error for a null address.
type NullAddrError struct{}

func (n NullAddrError) Error() string {
	return "NULL address"
}

// stackIterator holds information
// required to iterate and walk the program
// stack.
type stackIterator struct {
	pc, sp, bp uint64
	regs       Registers
	top        bool
	atend      bool
	frame      Stackframe
	bi         *BinaryInfo
	mem        MemoryReadWriter
	err        error

	stackhi        uint64
	systemstack    bool
	stackBarrierPC uint64
	stkbar         []savedLR
	stkbarPos      int
	crosscall2fn   *Function

	g           *G     // the goroutine being stacktraced, nil if we are stacktracing a goroutine-less thread
	g0_sched_sp uint64 // value of g0.sched.sp (see comments around its use)

	initialized bool
}

type savedLR struct {
	ptr uint64
	val uint64
}

func (it *stackIterator) initialize() {
	it.initialized = true
	it.top = true

	it.crosscall2fn = it.bi.LookupFunc[crosscall2]

	stackBarrierFunc := it.bi.LookupFunc[runtimeStackBarrier] // stack barriers were removed in Go 1.9
	var stackBarrierPC uint64
	if stackBarrierFunc == nil || it.stkbar == nil {
		return
	}
	stackBarrierPC = stackBarrierFunc.Entry
	fn := it.bi.PCToFunc(it.pc)
	if fn != nil && fn.Name == runtimeStackBarrier {
		// We caught the goroutine as it's executing the stack barrier, we must
		// determine whether or not g.stackPos has already been incremented or not.
		if len(it.stkbar) > 0 && it.stkbar[it.stkbarPos].ptr < it.sp {
			// runtime.stackBarrier has not incremented stkbarPos.
		} else if it.stkbarPos > 0 && it.stkbar[it.stkbarPos-1].ptr < it.sp {
			// runtime.stackBarrier has incremented stkbarPos.
			it.stkbarPos--
		} else {
			it.err = fmt.Errorf("failed to unwind through stackBarrier at SP %x", it.sp)
		}
	}
	it.stkbar = it.stkbar[it.stkbarPos:]
	it.stackBarrierPC = stackBarrierPC
}

// Next points the iterator to the next stack frame.
func (it *stackIterator) Next() bool {
	if !it.initialized {
		it.initialize()
	}
	if it.err != nil || it.atend {
		return false
	}
	it.frame, it.err = it.frameInfo(it.pc, it.sp, it.bp, it.top)
	if it.err != nil {
		if _, nofde := it.err.(*frame.NoFDEForPCError); nofde && !it.top {
			it.frame = Stackframe{Current: Location{PC: it.pc, File: "?", Line: -1}, Call: Location{PC: it.pc, File: "?", Line: -1}, CFA: 0, Ret: 0}
			it.atend = true
			it.err = nil
			return true
		}
		return false
	}

	if it.frame.Ret <= 0 {
		it.atend = true
		return true
	}

	if it.stkbar != nil && it.frame.Ret == it.stackBarrierPC && it.frame.addrret == it.stkbar[0].ptr {
		// Skip stack barrier frames
		it.frame.Ret = it.stkbar[0].val
		it.stkbar = it.stkbar[1:]
	}

	if it.switchStack() {
		return true
	}

	it.top = false
	it.pc = it.frame.Ret
	it.sp = uint64(it.frame.CFA)
	if it.bp <= uint64(it.frame.CFA) {
		// some functions in the runtime do not update BP, only follow BP if it's
		// part of the current frame.
		it.bp, _ = readUintRaw(it.mem, uintptr(it.bp), int64(it.bi.Arch.PtrSize()))
	}
	return true
}

// switchStack will use the current frame to determine if it's time to
// switch between the system stack and the goroutine stack or vice versa.
func (it *stackIterator) switchStack() bool {
	if it.frame.Current.Fn == nil {
		return false
	}
	switch it.frame.Current.Fn.Name {
	case "runtime.asmcgocall":
		if it.top || !it.systemstack {
			return false
		}

		// switch from system stack to goroutine stack

		off, _ := readIntRaw(it.mem, uintptr(it.sp+0x28), int64(it.bi.Arch.PtrSize())) // reads "offset of SP from StackHi" from where runtime.asmcgocall saved it
		oldsp := it.sp
		it.sp = uint64(int64(it.stackhi) - off)

		// runtime.asmcgocall can also be called from inside the system stack,
		// in that case no stack switch actually happens
		if it.sp == oldsp {
			return false
		}
		it.systemstack = false

		// advances to the next frame in the call stack
		it.frame.addrret = uint64(int64(it.sp) + int64(it.bi.Arch.PtrSize()))
		it.frame.Ret, _ = readUintRaw(it.mem, uintptr(it.frame.addrret), int64(it.bi.Arch.PtrSize()))
		it.pc = it.frame.Ret

		it.top = false
		return true

	case "runtime.mstart", "runtime.sigtramp":
		if it.top || !it.systemstack || it.g == nil {
			return false
		}

		// Calls to runtime.systemstack will switch to the systemstack then:
		// 1. alter the goroutine stack so that it looks like systemstack_switch
		//    was called
		// 2. alter the system stack so that it looks like the bottom-most frame
		//    belongs to runtime.mstart
		// If we find a runtime.mstart frame on the system stack of a goroutine
		// parked on runtime.systemstack_switch we assume runtime.systemstack was
		// called and continue tracing from the parked position.

		if fn := it.bi.PCToFunc(it.g.PC); fn == nil || fn.Name != "runtime.systemstack_switch" {
			return false
		}
		it.systemstack = false
		it.pc = it.g.PC
		it.sp = it.g.SP
		it.bp = it.g.BP
		it.top = false
		return true

	case "runtime.cgocallback_gofunc":
		// For a detailed description of how this works read the long comment at
		// the start of $GOROOT/src/runtime/cgocall.go and the source code of
		// runtime.cgocallback_gofunc in $GOROOT/src/runtime/asm_amd64.s
		//
		// When a C functions calls back into go it will eventually call into
		// runtime.cgocallback_gofunc which is the function that does the stack
		// switch from the system stack back into the goroutine stack
		// Since we are going backwards on the stack here we see the transition
		// as goroutine stack -> system stack.

		if it.top || it.systemstack {
			return false
		}

		if it.g0_sched_sp <= 0 {
			return false
		}
		// entering the system stack
		it.sp = it.g0_sched_sp
		// reads the previous value of g0.sched.sp that runtime.cgocallback_gofunc saved on the stack
		it.g0_sched_sp, _ = readUintRaw(it.mem, uintptr(it.sp), int64(it.bi.Arch.PtrSize()))
		frameOnSystemStack, _ := it.frameInfo(it.pc, it.sp, it.bp, false)
		it.pc = frameOnSystemStack.Ret
		it.sp = uint64(frameOnSystemStack.CFA)
		if it.bp <= uint64(frameOnSystemStack.CFA) {
			// some functions in the runtime do not update BP, only follow BP if it's
			// part of the current frame.
			it.bp, _ = readUintRaw(it.mem, uintptr(it.bp), int64(it.bi.Arch.PtrSize()))
		}

		it.systemstack = true
		it.top = false
		return true

	case "runtime.goexit", "runtime.rt0_go", "runtime.mcall":
		// Look for "top of stack" functions.
		it.atend = true
		return true

	default:
		return false
	}
}

// Frame returns the frame the iterator is pointing at.
func (it *stackIterator) Frame() Stackframe {
	if it.err != nil {
		panic(it.err)
	}
	return it.frame
}

// Err returns the error encountered during stack iteration.
func (it *stackIterator) Err() error {
	return it.err
}

func (it *stackIterator) frameInfo(pc, sp, bp uint64, top bool) (Stackframe, error) {
	f, l, fn := it.bi.PCToLine(pc)
	curloc := Location{PC: pc, File: f, Line: l, Fn: fn}
	var fde *frame.FrameDescriptionEntry
	var err error
	if curloc.Fn == nil || curloc.Fn.cu.isgo {
		// The C compiler emits debug_frame instructions that we don't know how to
		// interpret correctly.
		fde, err = it.bi.frameEntries.FDEForPC(pc)
	}
	if fde == nil {
		if bp == 0 {
			return Stackframe{}, err
		}
		// When no FDE is available attempt to use BP instead
		retaddr := uintptr(int(bp) + it.bi.Arch.PtrSize())
		cfa := int64(retaddr) + int64(it.bi.Arch.PtrSize())
		return it.newStackframe(curloc, cfa, bp, retaddr, nil, top)
	}

	spoffset, retoffset := fde.ReturnAddressOffset(pc)

	if it.crosscall2fn != nil {
		if pc >= it.crosscall2fn.Entry && pc < it.crosscall2fn.End && spoffset == 8 && !it.top {
			// the frame descriptor entry of crosscall2 is wrong, see: https://github.com/golang/go/issues/21569
			switch it.bi.GOOS {
			case "windows":
				spoffset += 0x118
			default:
				spoffset += 0x58
			}
		}
	}

	cfa := int64(sp) + spoffset

	retaddr := uintptr(cfa + retoffset)
	return it.newStackframe(curloc, cfa, bp, retaddr, fde, top)
}

func (it *stackIterator) newStackframe(curloc Location, cfa int64, bp uint64, retaddr uintptr, fde *frame.FrameDescriptionEntry, top bool) (Stackframe, error) {
	if retaddr == 0 {
		return Stackframe{}, NullAddrError{}
	}
	ret, err := readUintRaw(it.mem, retaddr, int64(it.bi.Arch.PtrSize()))
	if err != nil {
		it.err = err
	}
	r := Stackframe{Current: curloc, CFA: cfa, regs: it.regs, bp: bp, FDE: fde, Ret: ret, addrret: uint64(retaddr), stackHi: it.stackhi, SystemStack: it.systemstack}
	if !top {
		fnname := ""
		if r.Current.Fn != nil {
			fnname = r.Current.Fn.Name
		}
		switch fnname {
		case "runtime.mstart", "runtime.systemstack_switch":
			// these frames are inserted by runtime.systemstack and there is no CALL
			// instruction to look for at pc - 1
			r.Call = r.Current
		default:
			r.Call.File, r.Call.Line, r.Call.Fn = it.bi.PCToLine(curloc.PC - 1)
			r.Call.PC = r.Current.PC
		}
	} else {
		r.Call = r.Current
	}
	return r, nil
}

func (it *stackIterator) stacktrace(depth int) ([]Stackframe, error) {
	if depth < 0 {
		return nil, errors.New("negative maximum stack depth")
	}
	frames := make([]Stackframe, 0, depth+1)
	for it.Next() {
		frames = append(frames, it.Frame())
		if len(frames) >= depth+1 {
			break
		}
	}
	if err := it.Err(); err != nil {
		if len(frames) == 0 {
			return nil, err
		}
		frames = append(frames, Stackframe{Err: err})
	}
	return frames, nil
}
