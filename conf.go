// Copyright 2016, Gdlv Authors

package main

import (
	"encoding/json"
	"os"
	"runtime"
)

type Configuration struct {
	Scaling              float64
	WhiteTheme           bool
	StopOnNextBreakpoint bool
	DisassemblyFlavour   int
	Layouts              map[string]LayoutDescr
	CustomFormatters     map[string]*CustomFormatter
}

type LayoutDescr struct {
	Layout      string
	Description string
}

var conf Configuration

func adjustConfiguration() {
	if conf.Scaling < 0.2 {
		conf.Scaling = 1.0
	}
	if conf.Layouts == nil {
		conf.Layouts = map[string]LayoutDescr{}
		conf.Layouts["gs"] = LayoutDescr{"|300_250LC_231GS", "Goroutines and Stacktraces"}
		conf.Layouts["sl"] = LayoutDescr{"|300_250LC_180Sl", "Stacktrace and Locals"}
		conf.Layouts["tr"] = LayoutDescr{"|300_250LC_180Tl", "Threads and Registers"}
	}
	if ld, ok := conf.Layouts["default"]; !ok || ld.Layout == "" {
		conf.Layouts["default"] = LayoutDescr{"|300_250LC_180Sl", "Default layout"}
	}
}

func configLoc() string {
	loc := "$HOME/.config/gdlv"
	if runtime.GOOS == "windows" {
		loc = "$APPDATA/gdlv"
	}
	return os.ExpandEnv(loc)
}

func loadConfiguration() {
	defer adjustConfiguration()
	fh, err := os.Open(configLoc())
	if err != nil {
		return
	}
	defer fh.Close()
	json.NewDecoder(fh).Decode(&conf)
	if conf.CustomFormatters == nil {
		conf.CustomFormatters = make(map[string]*CustomFormatter)
	}
}

func saveConfiguration() {
	fh, err := os.Create(configLoc())
	if err != nil {
		return
	}
	defer fh.Close()
	json.NewEncoder(fh).Encode(&conf)
}
