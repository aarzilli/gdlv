package main

import (
	"regexp"
	"testing"

	"github.com/aarzilli/gdlv/internal/prettyprint"
)

type scopeTestCase struct {
	input string
	ScopedExpr
}

func TestScopeSpecs(t *testing.T) {
	for _, tc := range []scopeTestCase{
		{"expr", ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: -1, DeferredCall: -1, EvalExpr: "expr"}},
		{"@g12", ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid argument for 'g'"}},
		{"@g12 expr", ScopedExpr{Kind: NormalScopeExpr, Gid: 12, Fid: -1, DeferredCall: 0, EvalExpr: "expr"}},
		{"@g0xa1 expr", ScopedExpr{Kind: NormalScopeExpr, Gid: 0xa1, Fid: -1, DeferredCall: 0, EvalExpr: "expr"}},
		{"@f1 expr", ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: 1, DeferredCall: 0, EvalExpr: "expr"}},
		{"@f0x1 expr", ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: 1, DeferredCall: 0, EvalExpr: "expr"}},
		{"@f-54 expr", ScopedExpr{Kind: FrameOffsetScopeExpr, Gid: -1, Foff: -54, DeferredCall: 0, EvalExpr: "expr"}},
		{"@f-0x32 expr", ScopedExpr{Kind: FrameOffsetScopeExpr, Gid: -1, Foff: -0x32, DeferredCall: 0, EvalExpr: "expr"}},
		{"@f/fnname/ expr", ScopedExpr{Kind: FrameRegexScopeExpr, Gid: -1, Fre: regexp.MustCompile("fnname"), DeferredCall: 0, EvalExpr: "expr"}},
		{"@f/some expr/ expr", ScopedExpr{Kind: FrameRegexScopeExpr, Gid: -1, Fre: regexp.MustCompile("some expr"), DeferredCall: 0, EvalExpr: "expr"}},
		{"@g12f/some expr/ expr", ScopedExpr{Kind: FrameRegexScopeExpr, Gid: 12, Fre: regexp.MustCompile("some expr"), DeferredCall: 0, EvalExpr: "expr"}},
		{"@f/some expr/g12 expr", ScopedExpr{Kind: FrameRegexScopeExpr, Gid: 12, Fre: regexp.MustCompile("some expr"), DeferredCall: 0, EvalExpr: "expr"}},
		{"    some expr", ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: -1, DeferredCall: -1, EvalExpr: "some expr"}},
		{"@gf some expr", ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid argument for 'g'"}},
		{"@m some expr", ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "unknown character m"}},
	} {
		se := ParseScopedExpr(tc.input)
		if se.Kind != tc.Kind {
			t.Fatalf("error parsing %q, expected\n%#v\ngot %#v", tc.input, tc.ScopedExpr, se)
		}

		switch se.Kind {
		case InvalidScopeExpr:
			if se.EvalExpr != tc.EvalExpr {
				t.Fatalf("error parsing %q, expected\n%#v\ngot\n%#v", tc.input, tc.ScopedExpr, se)
			}
		case NormalScopeExpr:
			if se.Gid != tc.Gid || se.Fid != tc.Fid || se.EvalExpr != tc.EvalExpr || se.DeferredCall != tc.DeferredCall {
				t.Fatalf("error parsing %q, expected\n%#v\ngot\n%#v", tc.input, tc.ScopedExpr, se)
			}
		case FrameOffsetScopeExpr:
			if se.Gid != tc.Gid || se.Foff != tc.Foff || se.EvalExpr != tc.EvalExpr || se.DeferredCall != tc.DeferredCall {
				t.Fatalf("error parsing %q, expected\n%#v\ngot\n%#v", tc.input, tc.ScopedExpr, se)
			}
		case FrameRegexScopeExpr:
			if se.Gid != tc.Gid || se.Fre.String() != tc.Fre.String() || se.EvalExpr != tc.EvalExpr || se.DeferredCall != tc.DeferredCall {
				t.Fatalf("error parsing %q, expected\n%#v\ngot\n%#v", tc.input, tc.ScopedExpr, se)
			}
		}
	}
}

func TestFormatSpecs(t *testing.T) {
	for _, tc := range []scopeTestCase{
		{"expr", ScopedExpr{Kind: NormalScopeExpr, EvalExpr: "expr"}},
		{"%100s expr", ScopedExpr{Kind: NormalScopeExpr, EvalExpr: "expr", MaxStringLen: 100}},
		{"%#100s expr", ScopedExpr{Kind: NormalScopeExpr, EvalExpr: "expr", MaxStringLen: 100, Fmt: prettyprint.SimpleFormat{HexdumpString: true}}},
		{"%x%0.2f expr", ScopedExpr{Kind: NormalScopeExpr, EvalExpr: "expr", Fmt: prettyprint.SimpleFormat{IntFormat: "%x", FloatFormat: "%0.2f"}}},
		{"@g12 %x expr", ScopedExpr{Kind: NormalScopeExpr, EvalExpr: "expr", Fmt: prettyprint.SimpleFormat{IntFormat: "%x"}}},
		{"%s expr", ScopedExpr{Kind: NormalScopeExpr, EvalExpr: "expr"}},
	} {
		se := ParseScopedExpr(tc.input)
		if se.Kind != tc.Kind {
			t.Fatalf("error parsing %q, expected\n%#v\ngot\n%#v", tc.input, tc.ScopedExpr, se)
		}
		switch se.Kind {
		case InvalidScopeExpr:
			if se.EvalExpr != tc.EvalExpr {
				t.Fatalf("error parsing %q, expected\n%#v\ngot\n%#v", tc.input, tc.ScopedExpr, se)
			}
		default:
			if se.MaxStringLen != tc.MaxStringLen || se.Fmt != tc.Fmt || se.EvalExpr != tc.EvalExpr {
				t.Fatalf("error parsing %q, expected\n%#v\ngot\n%#v\n", tc.input, tc.ScopedExpr, se)
			}
		}
	}
}

func TestToString(t *testing.T) {
	for _, tc := range [][2]string{
		{"expr", "expr"},
		{"@g12 expr", "@g12 expr"},
		{"@g0xa1 expr", "@g161 expr"},
		{"@f1 expr", "@f0x1 expr"},
		{"@f0x1 expr", "@f0x1 expr"},
		{"@f-54 expr", "@f-0x36 expr"},
		{"@f-0x32 expr", "@f-0x32 expr"},
		{"@f/fnname/ expr", "@f/fnname/ expr"},
		{"@f/some expr/ expr", "@f/some expr/ expr"},
		{"@g12f/some expr/ expr", "@g12f/some expr/ expr"},
		{"@f/some expr/g12 expr", "@g12f/some expr/ expr"},
		{"@d3 expr", "@d3 expr"},
		{"    some expr", "some expr"},
		{"%100s expr", "%100s expr"},
		{"%#100s expr", "%#100s expr"},
		{"%x%0.2f expr", "%x%0.2f expr"},
		{"@g12 %x expr", "@g12 %x expr"},
		{"%s expr", "expr"},
	} {
		se := ParseScopedExpr(tc[0])
		if se.String() != tc[1] {
			t.Errorf("mismatch for %q got %q expected %q\n", tc[0], se.String(), tc[1])
		}
	}
}
