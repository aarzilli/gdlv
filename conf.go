// Copyright 2016, Gdlv Authors

package main

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"

	"github.com/aarzilli/nucular/rect"
)

const (
	darkTheme  = "Dark theme"
	whiteTheme = "White theme"
	redTheme   = "Red theme"
)

var themes = []string{darkTheme, whiteTheme, redTheme}

type Configuration struct {
	Scaling              float64
	Theme                string
	StopOnNextBreakpoint bool
	DisassemblyFlavour   int
	DefaultStepBehaviour string
	Layouts              map[string]LayoutDescr
	CustomFormatters     map[string]*CustomFormatter
	SavedBounds          map[string]rect.Rect
	MaxArrayValues       int
	MaxStringLen         int
	SubstitutePath       []SubstitutePathRule
	FrozenBreakpoints    map[string][]frozenBreakpoint
	DisabledBreakpoints  map[string][]frozenBreakpoint
}

type LayoutDescr struct {
	Layout      string
	Description string
}

// Describes a rule for substitution of path to source code file.
type SubstitutePathRule struct {
	// Directory path will be substituted if it matches `From`.
	From string
	// Path to which substitution is performed.
	To string
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
	if conf.SavedBounds == nil {
		conf.SavedBounds = make(map[string]rect.Rect)
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
	if BackendServer.debugid != "" {
		if conf.FrozenBreakpoints == nil {
			conf.FrozenBreakpoints = make(map[string][]frozenBreakpoint)
		}
		if conf.DisabledBreakpoints == nil {
			conf.DisabledBreakpoints = make(map[string][]frozenBreakpoint)
		}
		conf.FrozenBreakpoints[BackendServer.debugid] = append(conf.FrozenBreakpoints[BackendServer.debugid][:0], FrozenBreakpoints...)
		conf.DisabledBreakpoints[BackendServer.debugid] = append(conf.DisabledBreakpoints[BackendServer.debugid][:0], DisabledBreakpoints...)
	}
	fh, err := os.Create(configLoc())
	if err != nil {
		return
	}
	defer fh.Close()
	json.NewEncoder(fh).Encode(&conf)
}

func (conf *Configuration) substitutePath(path string) string {
	path = crossPlatformPath(path)
	separator := string(os.PathSeparator)
	for _, r := range conf.SubstitutePath {
		from := crossPlatformPath(r.From)
		to := r.To

		if !strings.HasSuffix(from, separator) {
			from = from + separator
		}
		if !strings.HasSuffix(to, separator) {
			to = to + separator
		}
		if strings.HasPrefix(path, from) {
			return strings.Replace(path, from, to, 1)
		}
	}
	return path
}

func crossPlatformPath(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}
