package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var LogOutputNice, LogOutputRpc io.WriteCloser

func replacepid(in string) string {
	return strings.Replace(in, "%p", strconv.Itoa(os.Getpid()), -1)
}

func logf(fmtstr string, args ...interface{}) {
	if LogOutputNice == nil {
		return
	}
	if len(fmtstr) == 0 || fmtstr[len(fmtstr)-1] != '\n' {
		fmtstr = fmtstr + "\n"
	}
	LogOutputNice.Write([]byte(time.Now().Format(time.RFC3339)))
	LogOutputNice.Write([]byte(" "))
	fmt.Fprintf(LogOutputNice, fmtstr, args...)
}

func setupLog() {
	tmpdir := getLogArchivesDir()
	if tmpdir == "" {
		return
	}

	cleanLogArchives(tmpdir)

	tmpdir = filepath.Join(tmpdir, fmt.Sprintf("%s-%d", time.Now().Format("20060102150405"), os.Getpid()))

	err := os.Mkdir(tmpdir, 0770)
	if err != nil {
		return
	}

	fhnice, err1 := os.Create(filepath.Join(tmpdir, "nice.log"))
	if err1 != nil {
		return
	}
	fhrpc, err2 := os.Create(filepath.Join(tmpdir, "rpc.log"))
	if err2 != nil {
		fhnice.Close()
		return
	}

	LogOutputNice = fhnice
	LogOutputRpc = fhrpc
}

func getLogArchivesDir() string {
	tmpdir := os.TempDir()
	if tmpdir == "" {
		return ""
	}
	tmpdir = filepath.Join(tmpdir, "gdlv-archive")
	err := os.Mkdir(tmpdir, 0770)
	if err != nil {
		if !os.IsExist(err) {
			return ""
		}
	}
	return tmpdir
}

func cleanLogArchives(tmpdir string) {
	dh, err := os.Open(tmpdir)
	if err != nil {
		return
	}
	fis, err := dh.Readdir(-1)
	if err != nil {
		return
	}
	for _, fi := range fis {
		fields := strings.SplitN(fi.Name(), "-", 2)
		if len(fields) != 2 {
			continue
		}
		when, err := time.Parse("20060102150405", fields[0])
		if err != nil {
			continue
		}
		if time.Since(when) > 240*time.Hour {
			// removing old archive
			os.RemoveAll(filepath.Join(tmpdir, fi.Name()))
		}
	}
}
