package rpc2

import (
	"time"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
)

type ProcessPidIn struct {
}

type ProcessPidOut struct {
	Pid int
}

type LastModifiedIn struct {
}

type LastModifiedOut struct {
	Time time.Time
}

type DetachIn struct {
	Kill bool
}

type DetachOut struct {
}

type RestartIn struct {
	// Position to restart from, if it starts with 'c' it's a checkpoint ID,
	// otherwise it's an event number. Only valid for recorded targets.
	Position string

	// ResetArgs tell whether NewArgs should take effect.
	ResetArgs bool
	// NewArgs are arguments to launch a new process.  They replace only the
	// argv[1] and later. Argv[0] cannot be changed.
	NewArgs []string

	// When Rerecord is set the target will be rerecorded
	Rerecord bool
}

type RestartOut struct {
	DiscardedBreakpoints []api.DiscardedBreakpoint
}

type StateIn struct {
	// If NonBlocking is true State will return immediately even if the target process is running.
	NonBlocking bool
}

type StateOut struct {
	State *api.DebuggerState
}

type CommandOut struct {
	State api.DebuggerState
}

type GetBreakpointIn struct {
	Id   int
	Name string
}

type GetBreakpointOut struct {
	Breakpoint api.Breakpoint
}

type StacktraceIn struct {
	Id     int
	Depth  int
	Full   bool
	Defers bool // read deferred functions
	Opts   api.StacktraceOptions
	Cfg    *api.LoadConfig
}

type StacktraceOut struct {
	Locations []api.Stackframe
}

type ListBreakpointsIn struct {
}

type ListBreakpointsOut struct {
	Breakpoints []*api.Breakpoint
}

type CreateBreakpointIn struct {
	Breakpoint api.Breakpoint
}

type CreateBreakpointOut struct {
	Breakpoint api.Breakpoint
}

type ClearBreakpointIn struct {
	Id   int
	Name string
}

type ClearBreakpointOut struct {
	Breakpoint *api.Breakpoint
}

type AmendBreakpointIn struct {
	Breakpoint api.Breakpoint
}

type AmendBreakpointOut struct {
}

type CancelNextIn struct {
}

type CancelNextOut struct {
}

type ListThreadsIn struct {
}

type ListThreadsOut struct {
	Threads []*api.Thread
}

type GetThreadIn struct {
	Id int
}

type GetThreadOut struct {
	Thread *api.Thread
}

type ListPackageVarsIn struct {
	Filter string
	Cfg    api.LoadConfig
}

type ListPackageVarsOut struct {
	Variables []api.Variable
}

type ListRegistersIn struct {
	ThreadID  int
	IncludeFp bool
}

type ListRegistersOut struct {
	Registers string
	Regs      api.Registers
}

type ListLocalVarsIn struct {
	Scope api.EvalScope
	Cfg   api.LoadConfig
}

type ListLocalVarsOut struct {
	Variables []api.Variable
}

type ListFunctionArgsIn struct {
	Scope api.EvalScope
	Cfg   api.LoadConfig
}

type ListFunctionArgsOut struct {
	Args []api.Variable
}

type EvalIn struct {
	Scope api.EvalScope
	Expr  string
	Cfg   *api.LoadConfig
}

type EvalOut struct {
	Variable *api.Variable
}

type SetIn struct {
	Scope  api.EvalScope
	Symbol string
	Value  string
}

type SetOut struct {
}

type ListSourcesIn struct {
	Filter string
}

type ListSourcesOut struct {
	Sources []string
}

type ListFunctionsIn struct {
	Filter string
}

type ListFunctionsOut struct {
	Funcs []string
}

type ListTypesIn struct {
	Filter string
}

type ListTypesOut struct {
	Types []string
}

type ListGoroutinesIn struct {
	Start int
	Count int
}

type ListGoroutinesOut struct {
	Goroutines []*api.Goroutine
	Nextg      int
}

type AttachedToExistingProcessIn struct {
}

type AttachedToExistingProcessOut struct {
	Answer bool
}

type FindLocationIn struct {
	Scope api.EvalScope
	Loc   string
}

type FindLocationOut struct {
	Locations []api.Location
}

type DisassembleIn struct {
	Scope          api.EvalScope
	StartPC, EndPC uint64
	Flavour        api.AssemblyFlavour
}

type DisassembleOut struct {
	Disassemble api.AsmInstructions
}

type RecordedIn struct {
}

type RecordedOut struct {
	Recorded       bool
	TraceDirectory string
}

type CheckpointIn struct {
	Where string
}

type CheckpointOut struct {
	ID int
}

type ListCheckpointsIn struct {
}

type ListCheckpointsOut struct {
	Checkpoints []api.Checkpoint
}

type ClearCheckpointIn struct {
	ID int
}

type ClearCheckpointOut struct {
}

type AncestorsIn struct {
	GoroutineID  int
	NumAncestors int
	Depth        int
}

type AncestorsOut struct {
	Ancestors []api.Ancestor
}

// FunctionReturnLocationsIn holds arguments for the
// FunctionReturnLocationsRPC call. It holds the name of
// the function for which all return locations should be
// given.
type FunctionReturnLocationsIn struct {
	// FnName is the name of the function for which all
	// return locations should be given.
	FnName string
}

// FunctionReturnLocationsOut holds the result of the FunctionReturnLocations
// RPC call. It provides the list of addresses that the given function returns,
// for example with a `RET` instruction or `CALL runtime.deferreturn`.
type FunctionReturnLocationsOut struct {
	// Addrs is the list of all locations where the given function returns.
	Addrs []uint64
}

type IsMulticlientIn struct {
}

type IsMulticlientOut struct {
	// IsMulticlient returns true if the headless instance was started with --accept-multiclient
	IsMulticlient bool
}

// ListDynamicLibrariesIn holds the arguments of ListDynamicLibraries
type ListDynamicLibrariesIn struct {
}

// ListDynamicLibrariesOut holds the return values of ListDynamicLibraries
type ListDynamicLibrariesOut struct {
	List []api.Image
}

type StopRecordingIn struct {
}

type StopRecordingOut struct {
}
