package starbind

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"

	"go.starlark.net/resolve"
	"go.starlark.net/starlark"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/dlvclient/service/rpc2"
)

//go:generate go run ../../scripts/gen-starlark-bindings.go go ./starlark_mapping.go
//go:generate go run ../../scripts/gen-starlark-bindings.go doc ../../doc/starlark.md

const (
	dlvCommandBuiltinName        = "dlv_command"
	readFileBuiltinName          = "read_file"
	writeFileBuiltinName         = "write_file"
	commandPrefix                = "command_"
	dlvContextName               = "dlv_context"
	curScopeBuiltinName          = "cur_scope"
	defaultLoadConfigBuiltinName = "default_load_config"
	targetObjectName             = "tgt"
	helpBuiltinName              = "help"
)

func init() {
	resolve.AllowNestedDef = true
	resolve.AllowLambda = true
	resolve.AllowFloat = true
	resolve.AllowSet = true
	resolve.AllowBitwise = true
	resolve.AllowRecursion = true
	resolve.AllowGlobalReassign = true
}

// Context is the context in which starlark scripts are evaluated.
// It contains methods to call API functions, command line commands, etc.
type Context interface {
	Client() *rpc2.RPCClient
	RegisterCallback(name, helpMsg string, cmdfn func(args string) (starlark.Value, error))
	CallCommand(cmdstr string) error
	Scope() api.EvalScope
	LoadConfig() api.LoadConfig
}

// Env is the environment used to evaluate starlark scripts.
type Env struct {
	env       starlark.StringDict
	contextMu sync.Mutex
	cancelfn  context.CancelFunc
	thread    *starlark.Thread

	ctx Context
	out io.Writer
}

// New creates a new starlark binding environment.
func New(ctx Context) *Env {
	env := &Env{}

	env.ctx = ctx

	var doc map[string]string
	env.env, doc = env.starlarkPredeclare()

	builtindoc := func(name, args, descr string) {
		doc[name] = name + args + "\n\n" + name + " " + descr
	}

	env.env[dlvCommandBuiltinName] = starlark.NewBuiltin(dlvCommandBuiltinName, func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if err := isCancelled(thread); err != nil {
			return starlark.None, err
		}
		argstrs := make([]string, len(args))
		for i := range args {
			a, ok := args[i].(starlark.String)
			if !ok {
				return nil, fmt.Errorf("argument of dlv_command is not a string")
			}
			argstrs[i] = string(a)
		}
		err := env.ctx.CallCommand(strings.Join(argstrs, " "))
		if err != nil && strings.Contains(err.Error(), " has exited with status ") {
			return env.interfaceToStarlarkValue(err), nil
		}
		return starlark.None, decorateError(thread, err)
	})
	builtindoc(dlvCommandBuiltinName, "(Command)", "interrupts, continues and steps through the program.")

	env.env[readFileBuiltinName] = starlark.NewBuiltin(readFileBuiltinName, func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 1 {
			return nil, decorateError(thread, fmt.Errorf("wrong number of arguments"))
		}
		path, ok := args[0].(starlark.String)
		if !ok {
			return nil, decorateError(thread, fmt.Errorf("argument of read_file was not a string"))
		}
		buf, err := ioutil.ReadFile(string(path))
		if err != nil {
			return nil, decorateError(thread, err)
		}
		return starlark.String(string(buf)), nil
	})
	builtindoc(readFileBuiltinName, "(Path)", "reads a file.")

	env.env[writeFileBuiltinName] = starlark.NewBuiltin(writeFileBuiltinName, func(thread *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		if len(args) != 2 {
			return nil, decorateError(thread, fmt.Errorf("wrong number of arguments"))
		}
		path, ok := args[0].(starlark.String)
		if !ok {
			return nil, decorateError(thread, fmt.Errorf("first argument of write_file was not a string"))
		}
		err := ioutil.WriteFile(string(path), []byte(args[1].String()), 0640)
		return starlark.None, decorateError(thread, err)
	})
	builtindoc(writeFileBuiltinName, "(Path, Text)", "writes text to the specified file.")

	env.env[curScopeBuiltinName] = starlark.NewBuiltin(curScopeBuiltinName, func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		return env.interfaceToStarlarkValue(env.ctx.Scope()), nil
	})
	builtindoc(curScopeBuiltinName, "()", "returns the current scope.")

	env.env[defaultLoadConfigBuiltinName] = starlark.NewBuiltin(defaultLoadConfigBuiltinName, func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		return env.interfaceToStarlarkValue(env.ctx.LoadConfig()), nil
	})
	builtindoc(defaultLoadConfigBuiltinName, "()", "returns the default load configuration.")

	env.env[targetObjectName] = targetObject{env}

	env.env[helpBuiltinName] = starlark.NewBuiltin(helpBuiltinName, func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		switch len(args) {
		case 0:
			fmt.Fprintln(env.out, "Available builtins:")
			bins := make([]string, 0, len(env.env))
			for name, value := range env.env {
				switch value.(type) {
				case *starlark.Builtin:
					bins = append(bins, name)
				}
			}
			sort.Strings(bins)
			for _, bin := range bins {
				fmt.Fprintf(env.out, "\t%s\n", bin)
			}
			fmt.Fprintf(env.out, "\n\nUse tgt.varname to access the varname variable in the target process (it is equivalent to 'eval(None, \"varname\").Variable').\n")
		case 1:
			switch x := args[0].(type) {
			case *starlark.Builtin:
				if doc[x.Name()] != "" {
					fmt.Fprintf(env.out, "%s\n", doc[x.Name()])
				} else {
					fmt.Fprintf(env.out, "no help for builtin %s\n", x.Name())
				}
			case *starlark.Function:
				fmt.Fprintf(env.out, "user defined function %s\n", x.Name())
				if doc := x.Doc(); doc != "" {
					fmt.Fprintln(env.out, doc)
				}
			default:
				fmt.Fprintf(env.out, "no help for object of type %T\n", args[0])
			}
		default:
			fmt.Fprintln(env.out, "wrong number of arguments ", len(args))
		}
		return starlark.None, nil
	})
	builtindoc(helpBuiltinName, "(Object)", "prints help for Object.")

	return env
}

// Execute executes a script. Path is the name of the file to execute and
// source is the source code to execute.
// Source can be either a []byte, a string or a io.Reader. If source is nil
// Execute will execute the file specified by 'path'.
// After the file is executed if a function named mainFnName exists it will be called, passing args to it.
func (env *Env) Execute(out io.Writer, path string, source interface{}, mainFnName string, args []interface{}, v *api.Variable) (starlark.Value, error) {
	defer func() {
		err := recover()
		if err == nil {
			return
		}
		fmt.Printf("panic executing starlark script: %v\n", err)
		for i := 0; ; i++ {
			pc, file, line, ok := runtime.Caller(i)
			if !ok {
				break
			}
			fname := "<unknown>"
			fn := runtime.FuncForPC(pc)
			if fn != nil {
				fname = fn.Name()
			}
			fmt.Printf("%s\n\tin %s:%d\n", fname, file, line)
		}
	}()

	env.out = out
	thread := env.newThread()

	envenv := env.env
	if v != nil {
		envenv = starlark.StringDict{}
		for k, v := range env.env {
			envenv[k] = v
		}
		envenv["x"], _ = env.variableValueToStarlarkValue(v, true)
	}

	if mainFnName == "<expr>" {
		return starlark.Eval(thread, path, source, envenv)
	}

	globals, err := starlark.ExecFile(thread, path, source, envenv)
	if err != nil {
		return starlark.None, err
	}

	err = env.saveGlobals(globals)
	if err != nil {
		return starlark.None, err
	}

	return env.callMain(thread, globals, mainFnName, args)
}

func (env *Env) saveGlobals(globals starlark.StringDict) error {
	for name, val := range globals {
		switch {
		case strings.HasPrefix(name, commandPrefix):
			err := env.createCallback(name, val)
			if err != nil {
				return err
			}
		case name[0] >= 'A' && name[0] <= 'Z':
			env.env[name] = val
		}
	}
	return nil
}

// Cancel cancels the execution of a currently running script or function.
func (env *Env) Cancel() {
	if env == nil {
		return
	}
	env.contextMu.Lock()
	if env.cancelfn != nil {
		env.cancelfn()
		env.cancelfn = nil
	}
	if env.thread != nil {
		env.thread.Cancel("user interrupt")
	}
	env.contextMu.Unlock()
}

func (env *Env) newThread() *starlark.Thread {
	thread := &starlark.Thread{
		Print: func(thread *starlark.Thread, msg string) {
			if err := isCancelled(thread); err != nil {
				panic("cancelled")
			}
			fmt.Fprintln(env.out, msg)
		},
	}
	env.contextMu.Lock()
	var ctx context.Context
	env.thread = thread
	ctx, env.cancelfn = context.WithCancel(context.Background())
	env.contextMu.Unlock()
	thread.SetLocal(dlvContextName, ctx)
	return thread
}

func (env *Env) createCallback(name string, val starlark.Value) error {
	fnval, ok := val.(*starlark.Function)
	if !ok {
		return nil
	}

	name = name[len(commandPrefix):]

	helpMsg := fnval.Doc()
	if helpMsg == "" {
		helpMsg = "user defined"
	}

	if fnval.NumParams() == 1 {
		if p0, _ := fnval.Param(0); p0 == "args" {
			env.ctx.RegisterCallback(name, helpMsg, func(args string) (starlark.Value, error) {
				return starlark.Call(env.newThread(), fnval, starlark.Tuple{starlark.String(args)}, nil)
			})
			return nil
		}
	}

	env.ctx.RegisterCallback(name, helpMsg, func(args string) (starlark.Value, error) {
		thread := env.newThread()
		argval, err := starlark.Eval(thread, "<input>", "("+args+")", env.env)
		if err != nil {
			return starlark.None, err
		}
		argtuple, ok := argval.(starlark.Tuple)
		if !ok {
			argtuple = starlark.Tuple{argval}
		}
		return starlark.Call(thread, fnval, argtuple, nil)
	})
	return nil
}

// callMain calls the main function in globals, if one was defined.
func (env *Env) callMain(thread *starlark.Thread, globals starlark.StringDict, mainFnName string, args []interface{}) (starlark.Value, error) {
	if mainFnName == "" {
		return starlark.None, nil
	}
	mainval := globals[mainFnName]
	if mainval == nil {
		return starlark.None, nil
	}
	mainfn, ok := mainval.(*starlark.Function)
	if !ok {
		return starlark.None, fmt.Errorf("%s is not a function", mainFnName)
	}
	if mainfn.NumParams() != len(args) {
		return starlark.None, fmt.Errorf("wrong number of arguments for %s", mainFnName)
	}
	argtuple := make(starlark.Tuple, len(args))
	for i := range args {
		argtuple[i] = env.interfaceToStarlarkValue(args[i])
	}
	return starlark.Call(thread, mainfn, argtuple, nil)
}

type argument struct {
	name         string
	defaultValue defaultValue
}

type defaultValue uint8

const (
	defaultNone = iota
	defaultScope
	defaultLoadConfig
)

func isCancelled(thread *starlark.Thread) error {
	if ctx, ok := thread.Local(dlvContextName).(context.Context); ok {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	return nil
}

func decorateError(thread *starlark.Thread, err error) error {
	if err == nil {
		return nil
	}
	pos := thread.CallFrame(1).Pos
	if pos.Col > 0 {
		return fmt.Errorf("%s:%d:%d: %v", pos.Filename(), pos.Line, pos.Col, err)
	}
	return fmt.Errorf("%s:%d: %v", pos.Filename(), pos.Line, err)
}

var _ starlark.HasAttrs = targetObject{}

type targetObject struct {
	env *Env
}

func (targetObject) Freeze() {
}

func (targetObject) Hash() (uint32, error) {
	return 0, errors.New("not hashable")
}

func (targetObject) String() string {
	return "<target variables>"
}

func (targetObject) Truth() starlark.Bool {
	return true
}

func (targetObject) Type() string {
	return "<target variables>"
}

func (tgt targetObject) AttrNames() []string {
	return nil
}

func (tgt targetObject) Attr(name string) (starlark.Value, error) {
	env := tgt.env
	v, err := env.ctx.Client().EvalVariable(env.ctx.Scope(), name, env.ctx.LoadConfig())
	if err != nil {
		return starlark.None, fmt.Errorf("could not find variable %q: %v", name, err)
	}
	return structAsStarlarkValue{reflect.ValueOf(v).Elem(), env}, nil
}
