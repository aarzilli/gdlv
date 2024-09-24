package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func usage(s string) {
	if s != "" {
		fmt.Fprint(os.Stderr, s)
	}
	fmt.Fprintf(os.Stderr, `Usage: rerungo <subcommand>...

Rerungo is used to circumvent the caching strategies of cmd/go while making changes or debugging cmd/compile or cmd/go.
First create a log.txt file like this:

$ rm <executable>
$ go clean -cache
$ go build -x -work > log.txt 2>&1

Then rerungo can be used to rerun specific calls to cmd/compile or the call to cmd/link with the same arguments used for the full build.

The following commands are available:

rerungo help			This help
rerungo list			Lists packages that can be rebuilt
rerungo compile <str>		Recompiles the package matching the specified substring (it must match a single package)
rerungo link			Reruns the linker step
rerungo gdlv compile <str>	Debugs the compiler invocation for the package matching the specified substring, using gdlv
rerungo gdlv link		Debugs the linker step
rerungo rr:gdlv ...		Same as gdlv but uses the rr backend

`)
	os.Exit(1)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func nargs(n int) {
	if len(os.Args) != n {
		usage("Wrong number of arguments\n\n")
	}
}

func main() {
	if len(os.Args) < 2 {
		usage("Wrong number of arguments\n\n")
	}

	dorr := false

	switch os.Args[1] {
	case "help":
		usage("")
	default:
		usage("Wrong subcommand\n\n")

	case "list":
		nargs(2)
		listCmd()

	case "compile":
		nargs(3)
		compileCmd(os.Args[2])

	case "link":
		nargs(2)
		linkCmd()

	case "rr:gdlv":
		dorr = true
		fallthrough
	case "gdlv":
		if len(os.Args) < 3 {
			usage("Wrong number of arguments\n\n")
		}

		switch os.Args[2] {
		default:
			usage("Wrong subcommand\n\n")
		case "compile":
			nargs(4)
			dbgcompileCmd(dorr, os.Args[3])
		case "link":
			nargs(3)
			dbglinkCmd(dorr)
		}
	}
}

func fatalf(fmtstr string, args ...any) {
	fmt.Fprintf(os.Stderr, fmtstr, args...)
	os.Exit(1)
}

type compileCall struct {
	pkg string
	cmd string
}

var Workdir string // work directory
var CompileCalls []compileCall
var LinkCmd string

func loadlog() {
	fh, err := os.Open("log.txt")
	if err != nil {
		usage(fmt.Sprintf("Could not open log.txt: %v", err))
	}
	defer fh.Close()

	s := bufio.NewScanner(fh)
	for s.Scan() {
		line := s.Text()
		if Workdir == "" {
			const workprefix = "WORK="
			if !strings.HasPrefix(line, workprefix) {
				fatalf("Malformed log.txt, first line does not contain WORK directory\n")
			}
			Workdir = line[len(workprefix):]
			continue
		}

		cmd, rest, _ := strings.Cut(line, " ")
		if strings.HasPrefix(cmd, "GOROOT=") {
			cmd, rest, _ = strings.Cut(rest, " ")
		}
		if strings.HasSuffix(cmd, "/compile") {
			const parg = " -p "
			if p := strings.Index(rest, parg); p >= 0 {
				pkg, _, _ := strings.Cut(rest[p+len(parg):], " ")
				CompileCalls = append(CompileCalls, compileCall{pkg, line})
			}
			continue
		}

		if strings.HasSuffix(cmd, "/link") {
			LinkCmd = line
			continue
		}

	}
	if err := s.Err(); err != nil {
		usage(fmt.Sprintf("Could not read log.txt: %v", err))
	}
}

func getcompile(substr string) string {
	v := []string{}
	for _, cc := range CompileCalls {
		if strings.Contains(cc.pkg, substr) {
			v = append(v, cc.cmd)
		}
	}
	switch len(v) {
	case 0:
		fatalf("no match for %q\n", substr)
	case 1:
		return v[0]
	default:
		fatalf("too many matches for %q\n", substr)
	}
	return ""
}

func listCmd() {
	loadlog()
	for _, cc := range CompileCalls {
		fmt.Printf("rerungo compile %s\n", cc.pkg)
	}
	if LinkCmd != "" {
		fmt.Printf("rerungo link\n")
	}
}

func compileCmd(substr string) {
	loadlog()
	ccstr := getcompile(substr)
	execute(ccstr)
}

func linkCmd() {
	loadlog()
	if LinkCmd == "" {
		fatalf("no link command found in log.txt\n")
	}
	execute(LinkCmd)
}

func dbgcompileCmd(dorr bool, substr string) {
	loadlog()
	debug := "debug"
	if dorr {
		debug = "rr:debug"
	}
	ccstr := getcompile(substr)
	execute(replacegdlv(debug, ccstr))
}

func dbglinkCmd(dorr bool) {
	loadlog()
	if LinkCmd == "" {
		fatalf("no link command found in log.txt\n")
	}
	debug := "debug"
	if dorr {
		debug = "rr:debug"
	}
	execute(replacegdlv(debug, LinkCmd))
}

func replacegdlv(debug, ccstr string) string {
	cmd, rest, _ := strings.Cut(ccstr, " ")
	if strings.HasPrefix(cmd, "GOROOT=") {
		key, value, _ := strings.Cut(cmd, "=")
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			value = value[1 : len(value)-1]
		}
		os.Setenv(key, value)
		cmd, rest, _ = strings.Cut(rest, " ")
	}
	idx := strings.LastIndex(cmd, "/pkg/")
	goroot := cmd[:idx]
	cmd = cmd[idx:]
	var path string
	switch {
	case strings.Contains(cmd, "/compile"):
		path = filepath.Join(goroot, "src", "cmd", "compile")
	case strings.Contains(cmd, "/link"):
		path = filepath.Join(goroot, "src", "cmd", "link")
	default:
		fatalf("could not figure out what this string is calling to create the gdlv command: %q\n", ccstr)
	}
	return "gdlv -d " + path + " " + debug + " " + rest
}

const dummy = false

func execute(cmdstr string) {
	if dummy {
		fmt.Printf("%s\n", cmdstr)
		return
	}
	cmd := exec.Command("/bin/bash", "-c", "--", cmdstr)
	cmd.Env = append(os.Environ(), "WORK="+Workdir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
}
