package main

import (
	"testing"
)

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

func TestAutowrap(t *testing.T) {
	c := func(src, src1 string, ncols int, tgt string) {
		if o := string(autowrappend([]rune(src), []rune(src1), ncols)); o != tgt {
			t.Errorf("for %q+%q (%d) expected %q got %q", src, src1, ncols, tgt, o)
		}
	}

	c("", "", 10, "")
	c("something\n", "blah", 10, "something\nblah")
	c("something\nb", "lah", 10, "something\nblah")
	c("something\nsomething", "blah", 10, "something\nsomethingb\nlah")
	c("something\nsomething1111", "blah", 10, "something\nsomething1111\nblah")
	c("something\nsomething1111", "", 10, "something\nsomething1111")
}
