package main

import (
	"bufio"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/derekparker/delve/service/api"
	"github.com/derekparker/delve/service/rpc2"
)

type ServerDescr struct {
	// address of backend server
	connectString string
	// stdout and stderr streams from server process
	stdout, stderr io.ReadCloser
	// server process
	serverProcess *os.Process
	// arguments for 'go' used to build the executable
	buildcmd []string
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
}

var BackendServer ServerDescr

func parseArguments() (descr ServerDescr) {
	debugname := func() {
		fh, err := ioutil.TempFile(os.TempDir(), "gdlv-debug")
		if err != nil {
			descr.exe = fmt.Sprintf("%s/gdlv-debug", os.TempDir())
		}
		defer fh.Close()
		descr.exe = fh.Name()
	}

	finish := func(atStart bool, args ...string) {
		descr.atStart = atStart
		descr.dlvargs = args
	}

	if len(os.Args) < 2 {
		usage()
	}

	switch os.Args[1] {
	case "connect":
		if len(os.Args) != 3 {
		}
		descr.connectString = os.Args[2]
		return
	case "attach":
		switch len(os.Args) {
		case 3:
			finish(false, "--headless", "attach", os.Args[2])
		case 4:
			finish(false, "--headless", "attach", os.Args[2], os.Args[3])
		default:
			usage()
		}
	case "debug":
		debugname()
		descr.buildcmd = []string{"build", "-gcflags", "-N -l", "-o", descr.exe}
		args := make([]string, 0, len(os.Args[2:])+3)
		args = append(args, "--headless", "exec", descr.exe, "--")
		args = append(args, os.Args[2:]...)
		finish(true, args...)
	case "run":
		debugname()
		descr.buildcmd = []string{"build", "-gcflags", "-N -l", "-o", descr.exe, os.Args[2]}
		args := make([]string, 0, len(os.Args[3:])+3)
		args = append(args, "--headless", "exec", descr.exe, "--")
		args = append(args, os.Args[3:]...)
		finish(true, args...)
	case "exec":
		if len(os.Args) < 3 {
			usage()
		}
		args := make([]string, 0, len(os.Args[3:])+4)
		args = append(args, "--headless", "exec", os.Args[2], "--")
		args = append(args, os.Args[3:]...)
		finish(true, args...)
	case "test":
		debugname()
		descr.buildcmd = []string{"test", "-gcflags", "-N -l", "-c", "-o", descr.exe}
		args := make([]string, 0, len(os.Args[2:])+3)
		args = append(args, "--headless", "exec", descr.exe, "--")
		for _, arg := range os.Args[2:] {
			switch arg {
			case "-bench", "-benchtime", "-count", "-cover", "-covermode", "-coverpkg", "-cpu", "-parallel", "-run", "-short", "-timeout", "-v":
				fallthrough
			case "-benchmem", "-blockprofile", "-blockprofilerate", "-coverprofile", "-cpuprofile", "-memprofile", "-memprofilerate", "-mutexprofile", "-mutexprofilefraction", "-outputdir", "-trace":
				args = append(args, "-test."+arg[1:])
			default:
				args = append(args, arg)
			}
		}
		finish(true, args...)
	default:
		usage()
	}

	return
}

func parseListenString(listenstr string) string {
	var scrollbackOut = editorWriter{&scrollbackEditor, false}

	const prefix = "API server listening at: "
	if !strings.HasPrefix(listenstr, prefix) {
		mu.Lock()
		fmt.Fprintf(&scrollbackOut, "Could not parse connection string: %q\n", listenstr)
		mu.Unlock()
		return ""
	}

	return listenstr[len(prefix):]
}

func (s *ServerDescr) Start() {
	if s.connectString != "" {
		s.connectTo()
		return
	}

	s.Rebuild()
}

func (descr *ServerDescr) stdoutProcess() {
	var scrollbackOut = editorWriter{&scrollbackEditor, true}

	bucket := 0
	t0 := time.Now()
	first := true
	scan := bufio.NewScanner(descr.stdout)
	for scan.Scan() {
		if first {
			descr.connectString = parseListenString(scan.Text())
			descr.connectTo()
			first = false
		} else {
			mu.Lock()
			if silenced {
				mu.Unlock()
				continue
			}
			mu.Unlock()
			now := time.Now()
			if now.Sub(t0) > 500*time.Millisecond {
				t0 = now
				bucket = 0
			}
			bucket += len(scan.Text())
			if bucket > scrollbackLowMark {
				mu.Lock()
				silenced = true
				mu.Unlock()
				fmt.Fprintf(&scrollbackOut, "too much output in 500ms (%d), output silenced\n", bucket)
				bucket = 0
				continue
			}
			fmt.Fprintln(&scrollbackOut, scan.Text())
		}
	}
	if err := scan.Err(); err != nil {
		fmt.Fprintf(&scrollbackOut, "Error reading stdout: %v\n", err)
	}
	if first {
		descr.connectionFailed = true
		fmt.Fprintf(&scrollbackOut, "connection failed\n")
	}
}

func (descr *ServerDescr) stderrProcess() {
	var scrollbackOut = editorWriter{&scrollbackEditor, true}
	_, err := io.Copy(&scrollbackOut, descr.stderr)
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Error reading stderr: %v\n", err)
	}
}

func (descr *ServerDescr) Rebuild() {
	sw := &editorWriter{&scrollbackEditor, true}
	descr.buildok = true
	if descr.buildcmd != nil {
		fmt.Fprintf(sw, "Compiling...")
		out, err := exec.Command("go", descr.buildcmd...).CombinedOutput()
		fmt.Fprintf(sw, "done\n")
		s := string(out)
		if err != nil {
			descr.buildok = false
			s += fmt.Sprintf("\n%v\n", err)
		}
		io.WriteString(sw, s)
	}
	if descr.serverProcess == nil && descr.buildok {
		cmd := exec.Command("dlv", descr.dlvargs...)
		descr.stdout, _ = cmd.StdoutPipe()
		descr.stderr, _ = cmd.StderrPipe()
		err := cmd.Start()
		if err != nil {
			io.WriteString(sw, fmt.Sprintf("Could not start delve: %v\n", err))
		}
		descr.serverProcess = cmd.Process
		go descr.stdoutProcess()
		go descr.stderrProcess()
	}
}

func (descr *ServerDescr) connectTo() {
	var scrollbackOut = editorWriter{&scrollbackEditor, true}

	if descr.connectString == "" {
		return
	}

	mu.Lock()
	running = true
	client = rpc2.NewClient(descr.connectString)
	mu.Unlock()
	if client == nil {
		fmt.Fprintf(&scrollbackOut, "Could not connect\n")
	}

	fmt.Fprintf(&scrollbackOut, "Loading program info...")

	var err error
	funcsPanel.slice, err = client.ListFunctions("")
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not list functions: %v\n", err)
	}

	sourcesPanel.slice, err = client.ListSources("")
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not list sources: %v\n", err)
	}

	typesPanel.slice, err = client.ListTypes("")
	if err != nil {
		fmt.Fprintf(&scrollbackOut, "Could not list types: %v\n", err)
	}

	funcsPanel.id++
	typesPanel.id++
	sourcesPanel.id++

	completeLocationSetup()

	fmt.Fprintf(&scrollbackOut, "done\n")

	if descr.atStart {
		continueToRuntimeMain()
	}

	mu.Lock()
	running = false
	mu.Unlock()

	state, err := client.GetState()
	if err == nil && state == nil {
		mu.Lock()
		client = nil
		mu.Unlock()
		fmt.Fprintf(&scrollbackOut, "Could not get state, old version of delve?\n")
	}

	refreshState(refreshToFrameZero, clearStop, state)
}

func continueToRuntimeMain() {
	bp, err := client.CreateBreakpoint(&api.Breakpoint{FunctionName: "runtime.main", Line: -1})
	if err != nil {
		return
	}
	defer client.ClearBreakpoint(bp.ID)

	ch := client.Continue()
	for range ch {
	}
}

func (descr *ServerDescr) Close() {
	if descr.serverProcess != nil {
		descr.serverProcess.Signal(os.Interrupt)
	}
	if descr.exe != "" {
		os.Remove(descr.exe)
	}
}
