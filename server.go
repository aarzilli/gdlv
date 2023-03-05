package main

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/dlvclient/service/rpc2"
	"github.com/go-delve/delve/pkg/goversion"
)

type ServerDescr struct {
	// address of backend server
	connectString string
	stdinChan     chan string
	// stdin stram to the server process
	stdin io.WriteCloser
	// stdout and stderr streams from server process
	stdout, stderr io.ReadCloser
	// server process
	serverProcess *os.Process
	// arguments for 'go' used to build the executable
	buildcmd []string
	// directory where the 'go' command to build the executable should be run
	builddir string
	// executable file (if we did the build)
	exe string
	// last build was successful
	buildok bool
	// arguments to connect to delve
	dlvargs []string
	// inferior was started (no connect or attach), connectTo should advance to runtime.main
	atStart bool
	// connection to delve failed
	connectionFailed bool
	debugid          string
}

var RemoveExecutable bool = true
var BackendServer ServerDescr
var ScheduledBreakpoints []string

func parseArguments() (descr ServerDescr) {
	debugname := func(p string) {
		p = filepath.Base(p)
		if i := strings.LastIndex(p, "."); i >= 0 {
			p = p[:i]
		}
		template := "gdlv-debug"
		if p != "" {
			template = fmt.Sprintf("%s-gdlv-debug", p)
		}
		fh, err := ioutil.TempFile(os.TempDir(), template)
		if err != nil {
			descr.exe = fmt.Sprintf("%s/gdlv-debug", os.TempDir())
			return
		}
		descr.exe = fh.Name()
		fh.Close()
		os.Remove(descr.exe)
	}

	finish := func(atStart bool, args ...string) {
		descr.atStart = atStart
		descr.dlvargs = args
	}

	if os.Getenv("CGO_CFLAGS") == "" {
		os.Setenv("CGO_CFLAGS", "-O0 -g")
	}
	if os.Getenv("GODEBUG") == "" {
		os.Setenv("GODEBUG", fmt.Sprintf("tracebackancestors=%d", NumAncestors))
	}

	opts := parseOptions(os.Args)

	optflags := []string{"-gcflags", "-N -l"}
	ver, _ := goversion.Installed()
	switch {
	case ver.Major < 0 || ver.AfterOrEqual(goversion.GoVersion{1, 10, -1, 0, 0, ""}):
		optflags = []string{"-gcflags", "all=-N -l"}
	case ver.AfterOrEqual(goversion.GoVersion{1, 9, -1, 0, 0, ""}):
		optflags = []string{"-gcflags", "-N -l", "-a"}
	}

	if !opts.defaultBackend {
		RemoveExecutable = false
	}

	switch opts.cmd {
	case "connect":
		if len(opts.cmdArgs) != 1 {
			usage("wrong number of arguments")
		}
		if opts.buildDir != "" {
			usage("can not use -d with 'connect'")
		}
		if opts.tags != "" {
			usage("can not use -tags with 'connect'")
		}
		descr.connectString = opts.cmdArgs[0]

	case "attach":
		if opts.buildDir != "" {
			usage("can not use -d with 'attach'")
		}
		if opts.tags != "" {
			usage("can not use -tags with 'attach'")
		}
		switch len(opts.cmdArgs) {
		case 1:
			finish(false, opts.backend, "--headless", "attach", opts.cmdArgs[0])
		case 2:
			finish(false, opts.backend, "--headless", "attach", opts.cmdArgs[0], opts.cmdArgs[1])
		default:
			usage("wrong number of arguments")
		}

	case "debug":
		dir := opts.buildDir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		debugname(dir)
		descr.builddir = opts.buildDir
		descr.debugid = dir
		descr.buildcmd = []string{"build", "-o", descr.exe}
		if opts.tags != "" {
			descr.buildcmd = append(descr.buildcmd, "-tags", opts.tags)
		}
		descr.buildcmd = append(descr.buildcmd, optflags...)
		args := make([]string, 0, len(opts.cmdArgs)+4)
		args = append(args, opts.redirectArgs()...)
		args = append(args, opts.backend, "--headless", "exec", descr.exe, "--")
		args = append(args, opts.cmdArgs...)
		finish(true, args...)

	case "run":
		if len(opts.cmdArgs) < 1 {
			usage("wrong number of arguments")
		}
		if opts.buildDir != "" {
			usage("can not use -d with 'run'")
		}
		debugname(opts.cmdArgs[0])
		descr.debugid, _ = filepath.Abs(opts.cmdArgs[0])
		descr.buildcmd = []string{"build", "-o", descr.exe}
		if opts.tags != "" {
			descr.buildcmd = append(descr.buildcmd, "-tags", opts.tags)
		}
		descr.buildcmd = append(descr.buildcmd, optflags...)
		descr.buildcmd = append(descr.buildcmd, opts.cmdArgs[0])
		args := make([]string, 0, len(opts.cmdArgs[1:])+4)
		args = append(args, opts.redirectArgs()...)
		args = append(args, opts.backend, "--headless", "exec", descr.exe, "--")
		args = append(args, opts.cmdArgs[1:]...)
		finish(true, args...)

	case "exec":
		if len(opts.cmdArgs) < 1 {
			usage("wrong number of arguments")
		}
		if opts.buildDir != "" {
			usage("can not use -d with 'exec'")
		}
		if opts.tags != "" {
			usage("can not use -tags with 'exec'")
		}
		descr.debugid, _ = filepath.Abs(opts.cmdArgs[0])
		args := make([]string, 0, len(opts.cmdArgs[1:])+5)
		args = append(args, opts.redirectArgs()...)
		args = append(args, opts.backend, "--headless", "exec", opts.cmdArgs[0], "--")
		args = append(args, opts.cmdArgs[1:]...)
		finish(true, args...)

	case "test":
		dir := opts.buildDir
		if dir == "" {
			dir, _ = os.Getwd()
		}
		debugname(dir)
		descr.debugid = dir
		descr.buildcmd = []string{"test"}
		if opts.tags != "" {
			descr.buildcmd = append(descr.buildcmd, "-tags", opts.tags)
		}
		descr.buildcmd = append(descr.buildcmd, optflags...)
		descr.buildcmd = append(descr.buildcmd, "-c", "-o", descr.exe)
		args := make([]string, 0, len(opts.cmdArgs)+4)
		args = append(args, opts.redirectArgs()...)
		args = append(args, opts.backend, "--headless", "exec", descr.exe, "--")
		args = append(args, addTestPrefix(opts.cmdArgs)...)
		finish(true, args...)

	case "core":
		if !opts.defaultBackend {
			usage("invalid backend for 'core' command")
		}
		if len(opts.cmdArgs) < 2 {
			usage("wrong number of arguments")
		}
		if opts.buildDir != "" {
			usage("can not use -d with 'core'")
		}
		if opts.tags != "" {
			usage("can not use -tags with 'core'")
		}
		descr.debugid, _ = filepath.Abs(opts.cmdArgs[0])
		finish(true, "--headless", "core", opts.cmdArgs[0], opts.cmdArgs[1])

	case "replay":
		if !opts.defaultBackend {
			usage("invalid backend for 'replay' command")
		}
		if len(opts.cmdArgs) < 1 {
			usage("wrong number of arguments")
		}
		if opts.buildDir != "" {
			usage("can not use -d with 'replay'")
		}
		if opts.tags != "" {
			usage("can not use -tags with 'replay'")
		}
		descr.debugid = "replay-" + opts.cmdArgs[0]
		finish(true, "--headless", "replay", opts.cmdArgs[0])

	case "version":
		fmt.Fprintf(os.Stderr, "Gdlv Debugger\nVersion: 1.9\n")
		os.Exit(0)

	default:
		usage(fmt.Sprintf("unknown command %q", opts.cmd))
	}

	return
}

func (opts *commandLineOptions) redirectArgs() []string {
	r := []string{}
	names := []string{"stdin", "stdout", "stderr"}
	for i := range opts.redirects {
		if opts.redirects[i] != "" {
			r = append(r, "-r", names[i]+":"+opts.redirects[i])
		}
	}
	return r
}

const apiServerPrefix = "API server listening at: "

func parseListenString(listenstr string) string {
	var scrollbackOut = editorWriter{true}

	if !strings.HasPrefix(listenstr, apiServerPrefix) {
		fmt.Fprintf(&scrollbackOut, "Could not parse connection string: %q\n", listenstr)
		return ""
	}

	return listenstr[len(apiServerPrefix):]
}

func (s *ServerDescr) Start() {
	if s.connectString != "" {
		s.connectTo()
		return
	}

	s.Rebuild()
}

func (descr *ServerDescr) stdoutProcess(lenient bool) {
	var scrollbackOut = editorWriter{true}

	bucket := 0
	t0 := time.Now()
	first := true

	copyToScrollback := func(text []byte) {
		wnd.Lock()
		if silenced {
			wnd.Unlock()
			return
		}
		wnd.Unlock()
		now := time.Now()
		if now.Sub(t0) > 500*time.Millisecond {
			t0 = now
			bucket = 0
		}
		bucket += len(text)
		if bucket > scrollbackLowMark {
			wnd.Lock()
			silenced = true
			wnd.Unlock()
			fmt.Fprintf(&scrollbackOut, "too much output in 500ms (%d), output silenced\n", bucket)
			wnd.Changed()
			bucket = 0
			return
		}
		scrollbackOut.Write(text)
		wnd.Changed()
	}

	buf := make([]byte, 4*1024)
	for {
		n, err := descr.stdout.Read(buf)

		text := buf[:n]

		if first {
			if nl := strings.Index(string(text), "\n"); nl >= 0 {
				line := string(text)[:nl]
				text = text[nl+1:]
				if !lenient || strings.HasPrefix(line, apiServerPrefix) {
					descr.connectString = parseListenString(line)
					descr.connectTo()
					first = false
				}
				copyToScrollback(text)
			}
		}
		copyToScrollback(text)

		if err != nil {
			fmt.Fprintf(&scrollbackOut, "Error reading stdout: %v\n", err)
			break
		}
	}
	if first {
		descr.connectionFailed = true
		fmt.Fprintf(&scrollbackOut, "connection failed\n")
	}
}

func (descr *ServerDescr) stderrProcess() {
	var scrollbackOut = editorWriter{true}
	_, err := io.Copy(&scrollbackOut, descr.stderr)
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Error reading stderr: %v\n", err)
	}
}

func (descr *ServerDescr) stdinProcess() {
	var scrollbackOut = editorWriter{true}
	for line := range descr.stdinChan {
		scrollbackOut.Write([]byte(line))
		descr.stdin.Write([]byte(line))
	}
	descr.stdinChan = nil
	descr.stdin.Close()
}

func (descr *ServerDescr) Rebuild() {
	sw := &editorWriter{true}
	descr.buildok = true
	if descr.buildcmd != nil {
		fmt.Fprintf(sw, "Compiling...")
		cmd := exec.Command("go", descr.buildcmd...)
		cmd.Dir = descr.builddir
		out, err := cmd.CombinedOutput()
		fmt.Fprintf(sw, "done\n")
		s := string(out)
		if err != nil {
			descr.buildok = false
			s += fmt.Sprintf("\n%v\n", err)
		}
		io.WriteString(sw, s)
	}
	if descr.serverProcess == nil && descr.buildok {
		lenient := false
		for _, arg := range descr.dlvargs {
			if arg == "--backend=rr" {
				lenient = true
			}
		}
		cmd := exec.Command("dlv", descr.dlvargs...)
		descr.stdinChan = make(chan string, 10)
		descr.stdin, _ = cmd.StdinPipe()
		descr.stdout, _ = cmd.StdoutPipe()
		descr.stderr, _ = cmd.StderrPipe()
		err := cmd.Start()
		if err != nil {
			io.WriteString(sw, fmt.Sprintf("Could not start delve: %v\n", err))
		}
		descr.serverProcess = cmd.Process
		go descr.stdinProcess()
		go descr.stdoutProcess(lenient)
		go descr.stderrProcess()
	}
}

func (descr *ServerDescr) StaleExecutable() bool {
	if descr.buildcmd == nil {
		return false
	}
	for _, source := range sourcesPanel.slice {
		fi, err := os.Stat(source)
		if err != nil {
			continue
		}
		if fi.ModTime().After(lastModExe) {
			return true
		}
	}
	return false
}

func (descr *ServerDescr) connectTo() {
	var scrollbackOut = editorWriter{true}

	if descr.connectString == "" {
		return
	}

	wnd.Lock()
	var err error
	client, err = rpc2.NewClient(descr.connectString, LogOutputRpc)
	if err != nil {
		client = nil
		wnd.Unlock()
		fmt.Fprintf(&scrollbackOut, "Could not connect: %v\n", err)
		return
	}

	if client.IsMulticlient() {
		state, _ := client.GetStateNonBlocking()
		if state != nil && state.Running {
			_, err := client.Halt()
			if err != nil {
				fmt.Fprintf(&scrollbackOut, "could not halt: %v\n", err)
			}
		}
	}

	client.SetReturnValuesLoadConfig(&LongLoadConfig)
	wnd.Unlock()
	if client == nil {
		fmt.Fprintf(&scrollbackOut, "Could not connect\n")
	}

	go func() {
		if state, _ := client.GetStateNonBlocking(); state != nil && state.Recording {
			client.WaitForRecordingDone()
		}

		restoreFrozenBreakpoints(&scrollbackOut)

		finishRestart(&scrollbackOut, descr.atStart)

		state, err := client.GetState()
		if err == nil && state == nil {
			wnd.Lock()
			client = nil
			wnd.Unlock()
			fmt.Fprintf(&scrollbackOut, "Could not get state, old version of delve?\n")
		}

		refreshState(refreshToFrameZero, clearStop, state)
	}()
}

func continueToRuntimeMain() {
	startupfn := conf.StartupFunc
	if startupfn == "" {
		startupfn = "main.main"
	}
	bp, err := client.CreateBreakpoint(&api.Breakpoint{FunctionName: startupfn, Line: -1})
	if err != nil {
		if !strings.HasPrefix(err.Error(), "Breakpoint exists at ") {
			return
		}
	}
	defer client.ClearBreakpoint(bp.ID)

	ch := client.Continue()
	for range ch {
	}
}

func finishRestart(out io.Writer, contToMain bool) {
	loadProgramInfo(out)

	if len(ScheduledBreakpoints) > 0 {
		refreshState(refreshToFrameZero, clearStop, nil)
		for _, scheduledBp := range ScheduledBreakpoints {
			tracepoint := scheduledBp[0] == 'T'
			setBreakpoint(out, tracepoint, scheduledBp[1:])
		}
		ScheduledBreakpoints = ScheduledBreakpoints[:0]
	}

	if contToMain {
		continueToRuntimeMain()
	}
}

func loadProgramInfo(out io.Writer) {
	fmt.Fprintf(out, "Loading program info...")

	var err error
	funcsPanel.slice, err = client.ListFunctions("")
	if err != nil {
		fmt.Fprintf(out, "Could not list functions: %v\n", err)
	}

	sourcesPanel.slice, err = client.ListSources("")
	if err != nil {
		fmt.Fprintf(out, "Could not list sources: %v\n", err)
	}

	typesPanel.slice, err = client.ListTypes("")
	if err != nil {
		fmt.Fprintf(out, "Could not list types: %v\n", err)
	}

	lastModExe = client.LastModified()

	funcsPanel.id++
	typesPanel.id++
	sourcesPanel.id++

	completeLocationSetup()

	fmt.Fprintf(out, "done\n")
}

var closeOnce sync.Once

func (descr *ServerDescr) Close() {
	closeOnce.Do(func() {
		if descr.exe != "" && RemoveExecutable {
			os.Remove(descr.exe)
		}
	})
}

var testArguments = []string{"-bench", "-benchtime", "-count", "-cover", "-covermode", "-coverpkg", "-cpu", "-parallel", "-run", "-short", "-timeout", "-v", "-benchmem", "-blockprofile", "-blockprofilerate", "-coverprofile", "-cpuprofile", "-memprofile", "-memprofilerate", "-mutexprofile", "-mutexprofilefraction", "-outputdir", "-trace"}

func addTestPrefix(inputArgs []string) []string {
	if inputArgs == nil {
		return nil
	}
	args := make([]string, 0, len(inputArgs))
argloop:
	for _, arg := range inputArgs {
		added := false
		for _, testarg := range testArguments {
			if arg == testarg || strings.HasPrefix(arg, testarg+"=") {
				args = append(args, "-test."+arg[1:])
				added = true
				continue argloop
			}
		}
		if !added {
			args = append(args, arg)
		}
	}
	return args
}
