package main

import (
	"testing"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/prettyprint"
)

func TestShortenType(t *testing.T) {
	c := func(src, tgt string) {
		out := prettyprint.ShortenType(src)
		if out != tgt {
			t.Errorf("for %q expected %q got %q", src, tgt, out)
		} else {
			t.Logf("for %q go %q (ok)", src, out)
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
