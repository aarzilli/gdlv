package main

import (
	"testing"
)

func TestShortenType(t *testing.T) {
	c := func(src, tgt string) {
		out := shortenType(src)
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
