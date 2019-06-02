// Copyright 2016, Gdlv Authors

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var fullpathCompl []string
var pathCompl []string
var funcCompl []string

func completeLocationSetup() {
	fullpathCompl = []string{}
	pathCompl = []string{}
	funcCompl = []string{}
	for _, source := range sourcesPanel.slice {
		fullpathCompl = append(fullpathCompl, source)
		for _, seg := range strings.Split(source, "/") {
			pathCompl = append(pathCompl, seg)
		}
	}

	for _, name := range funcsPanel.slice {
		funcCompl = append(funcCompl, name)
		for _, seg0 := range strings.Split(name, "/") {
			funcCompl = append(funcCompl, seg0)
			for _, seg := range strings.Split(seg0, ".") {
				funcCompl = append(funcCompl, seg)
			}
		}
	}
}

func completeAny() {
	buf := commandLineEditor.Buffer
	if len(buf) == commandLineEditor.Cursor {
		completeCommand()
	}
	for i := range buf {
		if buf[i] == ' ' {
			if commandLineEditor.Cursor <= i {
				return
			}
			cmdstr := string(buf[:i])
			for _, v := range cmds.cmds {
				if v.match(cmdstr) {
					if v.complete != nil {
						v.complete()
					}
					return
				}
			}
		}
	}
}

func completeLocation() {
	word := lastWord([]rune{' ', ':'})
	if len(word) > 0 && word[0] == '/' {
		completeWord(word, fullpathCompl)
		return
	}
	word = lastWord([]rune{' ', ':', '/'})
	if len(word) > 0 {
		completeWord(word, pathCompl, funcCompl)
	}
}

func lastWord(seps []rune) string {
	buf := commandLineEditor.Buffer
	start := commandLineEditor.Cursor
	if start >= len(buf) {
		start--
	}
	for i := start; i > 0; i-- {
		for _, sep := range seps {
			if buf[i] == sep {
				return string(buf[i+1:])
			}
		}
	}
	return ""
}

func completeWord(word string, completionLists ...[]string) {
	cm := completeMachine{word: word}
	for _, completionList := range completionLists {
		for _, compl := range completionList {
			cm.add(compl)
		}
	}
	cm.finish()
}

func completeVariable() {
	word := lastWord([]rune{' '})
	cm := completeMachine{word: word}
	func() {
		localsPanel.asyncLoad.mu.Lock()
		defer localsPanel.asyncLoad.mu.Unlock()

		if !localsPanel.asyncLoad.loaded {
			return
		}

		for i := range localsPanel.locals {
			cm.add(localsPanel.locals[i].Name)
		}
	}()

	func() {
		globalsPanel.asyncLoad.mu.Lock()
		defer globalsPanel.asyncLoad.mu.Unlock()

		if !globalsPanel.asyncLoad.loaded {
			return
		}

		for i := range globalsPanel.globals {
			cm.add(globalsPanel.globals[i].Name)
		}
	}()

	cm.finish()
}

func completeCommand() {
	if cmds == nil || len(commandLineEditor.Buffer) == 0 {
		return
	}
	cm := completeMachine{word: string(commandLineEditor.Buffer)}
	for _, cmd := range cmds.cmds {
		for _, alias := range cmd.aliases {
			cm.add(alias)
		}
	}
	cm.finish()
}

func completeWindow() {
	if cmds == nil || len(commandLineEditor.Buffer) == 0 {
		return
	}
	cm := completeMachine{word: lastWord([]rune{' '})}
	for _, w := range infoModes {
		cm.add(strings.ToLower(w))
	}
	cm.finish()
}

func completeFilesystem() {
	word := expandTilde(lastWord([]rune{' '}))
	dir := filepath.Dir(word)
	dh, err := os.Open(dir)
	if err != nil {
		return
	}
	fis, err := dh.Readdir(-1)
	dh.Close()
	if err != nil {
		return
	}
	compl := make([]string, 0, len(fis))
	for _, fi := range fis {
		switch n := fi.Name(); n {
		case ".", "..":
		default:
			n := filepath.Join(dir, n)
			if fi.IsDir() {
				n = n + "/"
			}
			compl = append(compl, n)
		}
	}
	completeWord(word, compl)
}

func expandTilde(path string) string {
	if len(path) < 2 {
		return path
	}
	if path[0] == '~' && path[1] == '/' {
		homedir := os.Getenv("HOME")
		if homedir != "" {
			return homedir + path[1:]
		}
	}
	return path
}

type completeMachine struct {
	word   string
	compls []string
}

func (cm *completeMachine) add(compl string) {
	if strings.HasPrefix(compl, cm.word) {
		cm.compls = append(cm.compls, compl)
	}
}

func (cm *completeMachine) finish() {
	cm.compls = dedup(cm.compls)
	switch len(cm.compls) {
	case 0:
		return
	case 1:
		commandLineEditor.Text([]rune(cm.compls[0][len(cm.word):]))
	default:
		compl := commonPrefix(cm.compls)
		commandLineEditor.Text([]rune(compl[len(cm.word):]))
		out := editorWriter{&scrollbackEditor, false}
		more := ""
		if len(cm.compls) > 5 {
			more = "..."
			cm.compls = cm.compls[:5]
		}
		fmt.Fprintf(&out, "Completions: %s%s\n", strings.Join(cm.compls, ", "), more)
	}
}

func dedup(v []string) []string {
	if len(v) == 0 {
		return v
	}
	sort.Strings(v)
	dst := 0
	var prev *string = nil
	for src := 0; src < len(v); src++ {
		if (prev == nil) || (v[src] != *prev) {
			v[dst] = v[src]
			dst++
		}
		prev = &v[dst-1]
	}
	return v[:dst]
}

func commonPrefix(in []string) string {
	if len(in) <= 0 {
		return ""
	}
	r := in[0]
	for _, x := range in {
		r = commonPrefix2(r, x)
		if r == "" {
			break
		}
	}
	return r
}

func commonPrefix2(a, b string) string {
	l := len(a)
	if l > len(b) {
		l = len(b)
	}
	for i := 0; i < l; i++ {
		if a[i] != b[i] {
			return a[:i]
		}
	}
	return a[:l]
}
