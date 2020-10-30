package rpc2

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/rpc"
	"net/rpc/jsonrpc"
	"sync"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
)

// Client is a RPC service.Client.
type RPCClient struct {
	addr   string
	client *rpc.Client

	mu sync.Mutex

	running, recording bool

	retValLoadCfg *api.LoadConfig

	recordedCache *bool
}

// NewClient creates a new RPCClient.
func NewClient(addr string, logFile io.Writer) (*RPCClient, error) {
	netclient, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	var rwc io.ReadWriteCloser = netclient
	if logFile != nil {
		rwc = &LogClient{netclient, logFile}
	}
	client := jsonrpc.NewClient(rwc)
	c := &RPCClient{addr: addr, client: client}
	c.call("SetApiVersion", api.SetAPIVersionIn{2}, &api.SetAPIVersionOut{})
	return c, nil
}

func (c *RPCClient) Running() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running || c.recording
}

var errRunning = errors.New("running")

func (c *RPCClient) call(method string, args, reply interface{}) error {
	argsAsCmd := func() api.DebuggerCommand {
		cmd, ok := args.(api.DebuggerCommand)
		if !ok {
			pcmd := args.(*api.DebuggerCommand)
			cmd = *pcmd
		}
		return cmd
	}
	switch method {
	case "Command":
		cmd := argsAsCmd()
		switch cmd.Name {
		case api.SwitchThread, api.SwitchGoroutine, api.Halt:
			// those don't start the process
		default:
			c.mu.Lock()
			c.running = true
			c.mu.Unlock()
			defer func() {
				c.mu.Lock()
				c.running = false
				c.mu.Unlock()
			}()
		}
	case "Restart":
		c.mu.Lock()
		c.running = true
		c.mu.Unlock()
		defer func() {
			c.mu.Lock()
			c.running = false
			c.mu.Unlock()
		}()
	}

	return c.client.Call("RPCServer."+method, args, reply)
}

func (c *RPCClient) WaitForRecordingDone() {
	c.mu.Lock()
	c.recording = true
	c.mu.Unlock()
	c.GetState()
	c.mu.Lock()
	c.recording = false
	c.mu.Unlock()
}

type ProcessExitedError struct {
	pid, exitStatus int
}

func (err *ProcessExitedError) Error() string {
	return fmt.Sprintf("Process %d has exited with status %d", err.pid, err.exitStatus)
}

// exitedToError returns an error if out.State says that the process exited.
func (c *RPCClient) exitedToError(out *CommandOut, err error) (*api.DebuggerState, error) {
	if err != nil {
		return nil, err
	}
	if out.State.Exited {
		return nil, &ProcessExitedError{c.ProcessPid(), out.State.ExitStatus}
	}
	return &out.State, nil
}

// Recorded returns true if the debugger target is a recording.
func (c *RPCClient) Recorded() bool {
	if c.recordedCache != nil {
		return *c.recordedCache
	}
	out := new(RecordedOut)
	c.call("Recorded", RecordedIn{}, out)
	c.recordedCache = &out.Recorded
	return out.Recorded
}
