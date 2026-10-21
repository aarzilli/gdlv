package main

import (
	"io"
	"math"
	"testing"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/prettyprint"
)

func TestShortenType(t *testing.T) {
	c := func(src, tgt string) {
		t.Helper()
		out := prettyprint.ShortenType(src)
		if out != tgt {
			t.Errorf("for %q expected %q got %q", src, tgt, out)
		} else {
			t.Logf("for %q got %q (ok)", src, out)
		}
	}

	c("long/package/path/pkg.A", "pkg.A")
	c("[]long/package/path/pkg.A", "[]pkg.A")
	c("map[long/package/path/pkg.A]long/package/path/pkg.B", "map[pkg.A]pkg.B")
	c("map[long/package/path/pkg.A]interface {}", "map[pkg.A]interface {}")
	c("map[long/package/path/pkg.A]interface{}", "map[pkg.A]interface{}")
	c("map[long/package/path/pkg.A]struct {}", "map[pkg.A]struct {}")
	c("map[long/package/path/pkg.A]struct{}", "map[pkg.A]struct{}")
	c("map[long/package/path/pkg.A]map[long/package/path/pkg.B]long/package/path/pkg.C", "map[pkg.A]map[pkg.B]pkg.C")
	c("map[long/package/path/pkg.A][]long/package/path/pkg.B", "map[pkg.A][]pkg.B")
	c("map[uint64]*github.com/aarzilli/dwarf5/dwarf.typeUnit", "map[uint64]*dwarf.typeUnit")
	c("uint8", "uint8")
	c("encoding/binary", "encoding/binary")
	c("*github.com/go-delve/delve/pkg/proc.Target", "*proc.Target")
	c("long/package/path/pkg.Parametric[long/package/path/pkg.A, map[long/package/path/pkg.B]long/package/path/pkg.A]", "pkg.Parametric[pkg.A, map[pkg.B]pkg.A]")
	c("[]long/package/path/pkg.Parametric[long/package/path/pkg.A]", "[]pkg.Parametric[pkg.A]")
	c("[24]long/package/path/pkg.A", "[24]pkg.A")
	c("chan func()", "chan func()")
}

func TestCurrentColumn(t *testing.T) {
	c := func(src string, n int) {
		if o := currentColumn([]rune(src)); o != n {
			t.Errorf("for %q expected %d got %d", src, n, o)
		}
	}

	c("", 0)
	c("blah", 4)
	c("something\nblah", 4)
	c("something\nsomething else\nb", 1)
	c("something\nsomething else\nblah", 4)
}

func TestWrapInstruction(t *testing.T) {
	c := func(src, tgtop, tgtargs string) {
		out := wrapInstructions([]api.AsmInstruction{{Text: src}}, 0)
		if out[0].op != tgtop || out[0].args != tgtargs {
			t.Errorf("for %q expected op=%q arg=%q got op=%q arg=%q", src, tgtop, tgtargs, out[0].op, out[0].args)
		} else {
			t.Logf("for %q got op=%q arg=%q", src, out[0].op, out[0].args)
		}
	}

	c("blah", "blah", "")
	c("rep blah", "rep blah", "")
	c("blah arg1, arg2", "blah", "arg1, arg2")
	c("rep blah arg1, arg2", "rep blah", "arg1, arg2")
	c("rep lock blah arg1, arg2", "rep lock blah", "arg1, arg2")
	c("rex.w blah", "rex.w blah", "")
	c("rex.w blah arg1", "rex.w blah", "arg1")
}

func TestDisplayScalingValidation(t *testing.T) {
	for _, tc := range []struct {
		scaling float64
		valid   bool
	}{
		{0.2, true},
		{1.0, true},
		{4.0, true},
		{0.19, false},
		{4.01, false},
		{10.0, false},
		{math.NaN(), false},
		{math.Inf(1), false},
		{math.Inf(-1), false},
	} {
		if got := validDisplayScaling(tc.scaling); got != tc.valid {
			t.Errorf("validDisplayScaling(%v) = %v, want %v", tc.scaling, got, tc.valid)
		}
	}
}

func TestConfigCommandRejectsUnsupportedDisplayScaling(t *testing.T) {
	previous := conf.Scaling
	conf.Scaling = 1.25
	defer func() { conf.Scaling = previous }()

	if err := configCommand(io.Discard, "zoom 10"); err == nil {
		t.Fatal("config zoom 10 should be rejected")
	}
	if conf.Scaling != 1.25 {
		t.Fatalf("rejected zoom changed scaling to %v", conf.Scaling)
	}
}

func TestAdjustConfigurationNormalizesUnsupportedDisplayScaling(t *testing.T) {
	previous := conf
	defer func() { conf = previous }()

	conf = Configuration{Scaling: 10}
	adjustConfiguration()
	if conf.Scaling != 1.0 {
		t.Fatalf("unsupported persisted zoom normalized to %v, want 1.0", conf.Scaling)
	}
}

func TestNormalizeDisplayScaling(t *testing.T) {
	for _, tc := range []struct {
		input, want float64
	}{
		{0.2, 0.2},
		{1.0, 1.0},
		{4.0, 4.0},
		{0.1, 1.0},
		{10.0, 1.0},
		{math.NaN(), 1.0},
		{math.Inf(1), 1.0},
	} {
		if got := normalizedDisplayScaling(tc.input); got != tc.want {
			t.Errorf("normalizedDisplayScaling(%v) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestSteppedDisplayScalingStaysInRange(t *testing.T) {
	for _, tc := range []struct {
		scaling, delta, want float64
	}{
		{1.0, 0.1, 1.1},
		{1.0, -0.1, 0.9},
		{4.0, 0.1, 4.0},
		{0.2, -0.1, 0.2},
		{3.95, 0.1, 4.0},
	} {
		if got := steppedDisplayScaling(tc.scaling, tc.delta); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("steppedDisplayScaling(%v, %v) = %v, want %v", tc.scaling, tc.delta, got, tc.want)
		}
	}
}
