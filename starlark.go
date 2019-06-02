package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"

	"go.starlark.net/starlark"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/dlvclient/service/rpc2"
	"github.com/aarzilli/gdlv/internal/starbind"
)

var StarlarkEnv = starbind.New(starlarkContext{})

type starlarkContext struct{}

func (s starlarkContext) Client() *rpc2.RPCClient {
	return client
}

func (s starlarkContext) RegisterCallback(name, helpMsg string, fn func(args string) (starlark.Value, error)) {
	cmdfn := func(out io.Writer, args string) error {
		defer refreshState(refreshToFrameZero, clearStop, nil)
		v, err := fn(args)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "%v\n", v)
		return nil
	}

	found := false
	for i := range cmds.cmds {
		cmd := &cmds.cmds[i]
		for _, alias := range cmd.aliases {
			if alias == name {
				cmd.cmdFn = cmdfn
				cmd.helpMsg = helpMsg
				found = true
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		newcmd := command{
			aliases: []string{name},
			helpMsg: helpMsg,
			cmdFn:   cmdfn,
		}
		cmds.cmds = append(cmds.cmds, newcmd)
	}
}

func (s starlarkContext) CallCommand(cmdstr string) error {
	defer wnd.Changed()
	out := editorWriter{&scrollbackEditor, true}
	cmdstr, args := parseCommand(cmdstr)
	return cmds.Call(cmdstr, args, &out)
}

func (s starlarkContext) Scope() api.EvalScope {
	return currentEvalScope()
}

func (s starlarkContext) LoadConfig() api.LoadConfig {
	return getVariableLoadConfig()
}

const defaultInitFile = `
def command_find_array(arr, pred):
	"""Calls pred for each element of the array or slice 'arr' returns the index of
the first element for which pred returns true.

	
	find_array <arr>, <pred>
	
Example use:
	
	find_array "s2", lambda x: x.A == 5
"""
	arrv = eval(None, arr).Variable
	for i in range(0, arrv.Len):
		v = arrv.Value[i]
		if pred(v):
			print("found", i)
			return

	print("not found")

def Ll(var_name, next_field_name, max_depth):
	v = eval(None, var_name).Variable.Value
	r = []
	for i in range(0, max_depth):
		r.append(v)
		if v[0] == None:
			break
		v = v[next_field_name]
	return r

def command_linked_list(args):
	"""Prints the contents of a linked list.
	
	linked_list <var_name> <next_field_name> <max_depth>

Prints up to max_depth elements of the linked list variable 'var_name' using 'next_field_name' as the name of the link field.
"""
	var_name, next_field_name, max_depth = args.split(" ")
	max_depth = int(max_depth)
	next_name = var_name
	v = Ll(var_name, next_field_name, max_depth)
	for i in range(len(v)):
		print(str(i)+":", v)
`

func executeInit() {
	scrollbackOut := editorWriter{&scrollbackEditor, true}
	initPath := configLoc() + ".star"
	fmt.Fprintf(&scrollbackOut, "Loading init file %q...", initPath)
	fh, err := os.Open(initPath)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(&scrollbackOut, "could not read init file: %v", initPath, err)
			return
		}

		err := ioutil.WriteFile(initPath, []byte(defaultInitFile), 0660)
		if err != nil {
			fmt.Fprintf(&scrollbackOut, "could not create init file: %q: %v\n", initPath, err)
			return
		}

		fh, err = os.Open(initPath)
		if err != nil {
			fmt.Fprintf(&scrollbackOut, "could not read init file: %v\n", initPath, err)
			return
		}
	}
	fh.Close()

	_, err = StarlarkEnv.Execute(&scrollbackOut, initPath, nil, "main", nil)
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "\n%v\n", err)
		return
	}
	fmt.Fprintf(&scrollbackOut, "done\n")
}
