package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type delveVersion struct {
	major, minor, patch int
}

func (v delveVersion) AfterOrEqual(major, minor, patch int) bool {
	if v.major > major {
		return true
	}
	if v.major < major {
		return false
	}

	if v.minor > minor {
		return true
	}
	if v.minor < minor {
		return false
	}

	return v.patch >= patch
}

var delveFeatures struct {
	hasRerecord bool
}

func getDelveVersion() (ver delveVersion, ok bool) {
	versionOut, err := exec.Command("dlv", "version").Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not run Delve: %v\n", err)
		os.Exit(1)
	}

	const versionPrefix = "Version: "

	versionSlice := strings.Split(string(versionOut), "\n")
	if len(versionSlice) < 3 || versionSlice[0] != "Delve Debugger" || !strings.HasPrefix(versionSlice[1], versionPrefix) {
		return
	}

	verDots := strings.Split(strings.TrimSpace(versionSlice[1][len(versionPrefix):]), ".")

	if len(verDots) != 3 {
		return
	}

	major, _ := strconv.Atoi(verDots[0])
	minor, err1 := strconv.Atoi(verDots[1])
	patch, err2 := strconv.Atoi(verDots[2])
	ok = (major > 0) && (minor >= 0) && (patch >= 0) && (err1 == nil) && (err2 == nil)
	return delveVersion{major, minor, patch}, ok
}

func checkCompatibility() {
	delveVersion, ok := getDelveVersion()
	if !ok {
		return
	}

	if delveVersion.AfterOrEqual(1, 3, 2) {
		delveFeatures.hasRerecord = true
	}
}
