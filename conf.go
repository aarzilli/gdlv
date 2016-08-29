// Copyright 2016, Alessandro Arzilli

package main

import (
	"encoding/json"
	"os"
)

type Configuration struct {
	Scaling    float64
	WhiteTheme bool
	Layouts    map[string]LayoutDescr
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

func loadConfiguration() {
	defer adjustConfiguration()
	fh, err := os.Open(os.ExpandEnv("$HOME/.config/gdlv"))
	if err != nil {
		return
	}
	defer fh.Close()
	json.NewDecoder(fh).Decode(&conf)
}

func saveConfiguration() {
	fh, err := os.Create(os.ExpandEnv("$HOME/.config/gdlv"))
	if err != nil {
		return
	}
	defer fh.Close()
	json.NewEncoder(fh).Encode(&conf)
}
