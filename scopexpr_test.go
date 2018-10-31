package main

import (
	"regexp"
	"testing"
)

type scopeTestCase struct {
	input string
	ScopedExpr
}

func TestScopeSpecs(t *testing.T) {
	for _, tc := range []scopeTestCase{
		{"@g12 expr", ScopedExpr{Kind: NormalScopeExpr, Gid: 12, Fid: -1, EvalExpr: "expr"}},
		{"@g0xa1 expr", ScopedExpr{Kind: NormalScopeExpr, Gid: 0xa1, Fid: -1, EvalExpr: "expr"}},
		{"@f1 expr", ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: 1, EvalExpr: "expr"}},
		{"@f0x1 expr", ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: 1, EvalExpr: "expr"}},
		{"@f-54 expr", ScopedExpr{Kind: FrameOffsetScopeExpr, Gid: -1, Foff: -54, EvalExpr: "expr"}},
		{"@f-0x32 expr", ScopedExpr{Kind: FrameOffsetScopeExpr, Gid: -1, Foff: -0x32, EvalExpr: "expr"}},
		{"@f/fnname/ expr", ScopedExpr{Kind: FrameRegexScopeExpr, Gid: -1, Fre: regexp.MustCompile("fnname"), EvalExpr: "expr"}},
		{"@f/some expr/ expr", ScopedExpr{Kind: FrameRegexScopeExpr, Gid: -1, Fre: regexp.MustCompile("some expr"), EvalExpr: "expr"}},
		{"@g12f/some expr/ expr", ScopedExpr{Kind: FrameRegexScopeExpr, Gid: 12, Fre: regexp.MustCompile("some expr"), EvalExpr: "expr"}},
		{"@f/some expr/g12 expr", ScopedExpr{Kind: FrameRegexScopeExpr, Gid: 12, Fre: regexp.MustCompile("some expr"), EvalExpr: "expr"}},
		{"    some expr", ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: -1, EvalExpr: "    some expr"}},
		{"@gf some expr", ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid argument for 'g'"}},
		{"@m some expr", ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "unknown character m"}},
	} {
		se := ParseScopedExpr(tc.input)
		if se.Kind != tc.Kind {
			t.Fatalf("error parsing %q, expected %#v got %#v", tc.input, tc.ScopedExpr, se)
		}

		switch se.Kind {
		case InvalidScopeExpr:
			if se.EvalExpr != tc.EvalExpr {
				t.Fatalf("error parsing %q, expected %#v got %#v", tc.input, tc.ScopedExpr, se)
			}
		case NormalScopeExpr:
			if se.Gid != tc.Gid || se.Fid != tc.Fid || se.EvalExpr != tc.EvalExpr {
				t.Fatalf("error parsing %q, expected %#v got %#v", tc.input, tc.ScopedExpr, se)
			}
		case FrameOffsetScopeExpr:
			if se.Gid != tc.Gid || se.Foff != tc.Foff || se.EvalExpr != tc.EvalExpr {
				t.Fatalf("error parsing %q, expected %#v got %#v", tc.input, tc.ScopedExpr, se)
			}
		case FrameRegexScopeExpr:
			if se.Gid != tc.Gid || se.Fre.String() != tc.Fre.String() || se.EvalExpr != tc.EvalExpr {
				t.Fatalf("error parsing %q, expected %#v got %#v", tc.input, tc.ScopedExpr, se)
			}
		}
	}
}
