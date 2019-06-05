package main

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"go.starlark.net/starlark"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
	"github.com/aarzilli/gdlv/internal/starbind"
)

// ScopedExpr represents an expression to be evaluated in a specified scope.
type ScopedExpr struct {
	Kind ScopeExprKind
	Gid  int            // goroutine id (-1 for current goroutine)
	Fid  int            // frame id (-1 for current goroutine)
	Foff int            // frame offset (will search for this specified frame offset or return an error otherwise)
	Fre  *regexp.Regexp // frame regular expression (will search for a frame in a function matching this regular expression)

	DeferredCall int // deferred call index

	EvalExpr string // expression to evaluate
}

type ScopeExprKind uint8

const (
	NormalScopeExpr      ScopeExprKind = iota // use Gid and Fid
	FrameOffsetScopeExpr                      // use Foff instead of Fid
	FrameRegexScopeExpr                       // use Fre instead of Fid
	InvalidScopeExpr
)

func ParseScopedExpr(in string) ScopedExpr {
	for i, ch := range in {
		if unicode.IsSpace(ch) {
			continue
		}
		if ch != '@' {
			return ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: -1, DeferredCall: -1, EvalExpr: strings.TrimSpace(in)}
		} else {
			in = in[i:]
			break
		}
	}

	if len(in) < 2 {
		return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "not long enough"}
	}

	in = in[1:]

	r := ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: -1, DeferredCall: 0}
	first := true
	var gseen, fseen, dseen bool

	for {
		if len(in) == 0 {
			return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "no expression"}
		}
		switch in[0] {
		case 'g':
			in = in[1:]
			if gseen || len(in) == 0 {
				return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "no argument for 'g'"}
			}
			gseen = true
			var ok bool
			in, r.Gid, ok = scopeReadNumber(in)
			if !ok {
				return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid argument for 'g'"}
			}
			if r.Gid < 0 {
				return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid (negative) argument for 'g'"}
			}

		case 'f':
			in = in[1:]
			if fseen || len(in) == 0 {
				return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "no argument for 'f'"}
			}
			fseen = true
			if in[0] == '/' {
				var s string
				s, in = scopeReadDelim('/', in[1:])
				var err error
				r.Fre, err = regexp.Compile(s)
				if err != nil {
					return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: fmt.Sprintf("could not compile regexp: %v", err)}
				}
				r.Kind = FrameRegexScopeExpr
			} else {
				var ok bool
				in, r.Fid, ok = scopeReadNumber(in)
				if !ok {
					return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid argument for 'f'"}
				}
				if r.Fid < 0 {
					r.Foff = r.Fid
					r.Fid = -1
					r.Kind = FrameOffsetScopeExpr
				}
			}

		case 'd':
			in = in[1:]
			if dseen || len(in) == 0 {
				return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "no argument for 'd'"}
			}
			dseen = true
			var ok bool
			in, r.DeferredCall, ok = scopeReadNumber(in)
			if !ok {
				return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid argument for 'd'"}
			}
			if r.DeferredCall < 0 {
				return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid (negative) argument for 'd'"}
			}

		case ' ':
			if first {
				return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "nothing after @"}
			}
			in = in[1:]
			r.EvalExpr = strings.TrimSpace(in)
			return r

		default:
			return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: fmt.Sprintf("unknown character %c", in[0])}
		}
		first = false
	}
}

func scopeReadNumber(in string) (rest string, n int, ok bool) {
	cur := 0
	if len(in[cur:]) == 0 {
		return in, 0, false
	}

	if in[cur] == '+' || in[cur] == '-' {
		cur++
		if len(in[cur:]) == 0 {
			return in, 0, false
		}
	}

	allowhex := false

	if in[cur] == '0' {
		if cur+1 < len(in) && in[cur+1] == 'x' {
			cur += 2
			allowhex = true
		}
	}

	if len(in[cur:]) == 0 {
		return in, 0, false
	}

	isnum := func(ch rune) bool {
		return ch >= '0' && ch <= '9'
	}
	ishex := func(ch rune) bool {
		return isnum(ch) || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
	}

	for i, ch := range in[cur:] {
		if allowhex {
			if !ishex(ch) && !isnum(ch) {
				cur += i
				break
			}
		} else {
			if !isnum(ch) {
				cur += i
				break
			}
		}
	}

	n64, err := strconv.ParseInt(in[:cur], 0, 64)
	if err != nil {
		return in, 0, false
	}

	return in[cur:], int(n64), true
}

func scopeReadDelim(delim rune, in string) (out, rest string) {
	const escape = '\\'
	state := 0
	r := []rune{}
	for i, ch := range in {
		switch state {
		case 0: // not escaped
			switch ch {
			case escape:
				state = 1
			case delim:
				return string(r), in[i+1:]
			default:
				r = append(r, ch)
			}
		case 1: // escaped
			switch ch {
			case delim:
				r = append(r, ch)
			default:
				r = append(r, escape)
				r = append(r, ch)
			}
			state = 0
		}
	}
	return string(r), ""
}

func exprIsScoped(expr string) bool {
	se := ParseScopedExpr(expr)
	switch se.Kind {
	case InvalidScopeExpr:
		return true
	case NormalScopeExpr:
		return se.Gid >= 0 || se.Fid >= 0
	default:
		return true
	}
}

func evalScopedExpr(expr string, cfg api.LoadConfig) *api.Variable {
	se := ParseScopedExpr(expr)

	var gid, frame, deferredCall int

	gid = se.Gid
	if gid < 0 {
		gid = curGid
	}

	switch se.Kind {
	case InvalidScopeExpr:
		return &api.Variable{Name: expr, Unreadable: "syntax error: " + se.EvalExpr}

	case NormalScopeExpr:
		frame = se.Fid
		if frame < 0 {
			frame = curFrame
		}

	case FrameOffsetScopeExpr:
		frame = findFrameOffset(gid, int64(se.Foff), nil)

	case FrameRegexScopeExpr:
		frame = findFrameOffset(gid, 0, se.Fre)

	default:
		panic("unknown kind")
	}

	deferredCall = se.DeferredCall
	if deferredCall < 0 {
		deferredCall = curDeferredCall
	}

	if frame < 0 {
		return &api.Variable{Name: expr, Unreadable: "could not find specified frame"}
	}

	if len(se.EvalExpr) == 0 || se.EvalExpr[0] != '$' {
		v, err := client.EvalVariable(api.EvalScope{gid, frame, deferredCall}, se.EvalExpr, cfg)
		if err != nil {
			return &api.Variable{Name: expr, Unreadable: err.Error()}
		}
		return v
	}

	sv, err := StarlarkEnv.Execute(&editorWriter{&scrollbackEditor, true}, "<expr>", strings.TrimLeft(se.EvalExpr[1:], " "), "<expr>", nil)
	if err != nil {
		return &api.Variable{Name: expr, Unreadable: err.Error()}
	}

	return convertStarlarkToVariable(expr, sv)
}

func convertStarlarkToVariable(expr string, sv starlark.Value) *api.Variable {
	switch sv := sv.(type) {
	case *starlark.List:
		r := &api.Variable{
			Name: expr,
			Type: "list", RealType: "list",
			Kind: reflect.Slice,
			Len:  int64(sv.Len()), Cap: int64(sv.Len()),
		}

		for i := 0; i < sv.Len(); i++ {
			r.Children = append(r.Children, *convertStarlarkToVariable("", sv.Index(i)))
		}
		return r

	case starbind.WrappedVariable:
		return sv.UnwrapVariable()
	default:
		s := sv.String()
		return &api.Variable{Name: expr, Kind: reflect.String, Type: "string", RealType: "string", Len: int64(len(s)), Value: s}
	}
}

func findFrameOffset(gid int, frameOffset int64, rx *regexp.Regexp) (frame int) {
	frames, err := client.Stacktrace(gid, 100, false, nil)
	if err != nil {
		return -1
	}

	for i := range frames {
		if rx != nil {
			if rx.FindStringIndex(frames[i].Function.Name()) != nil {
				return i
			}
		} else if frames[i].FrameOffset == frameOffset {
			return i
		}
	}
	return -1
}
