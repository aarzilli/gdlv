package main

import (
	"fmt"
	"regexp"
	"strconv"
	"unicode"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
)

// ScopedExpr represents an expression to be evaluated in a specified scope.
type ScopedExpr struct {
	Kind     ScopeExprKind
	Gid      int            // goroutine id (-1 for current goroutine)
	Fid      int            // frame id (-1 for current goroutine)
	Foff     int            // frame offset (will search for this specified frame offset or return an error otherwise)
	Fre      *regexp.Regexp // frame regular expression (will search for a frame in a function matching this regular expression)
	EvalExpr string         // expression to evaluate
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
			return ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: -1, EvalExpr: in}
		} else {
			in = in[i:]
			break
		}
	}

	if len(in) < 2 {
		return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "not long enough"}
	}

	in = in[1:]

	r := ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: -1}
	first := true
	gseen, fseen := false, false

	for {
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

		case ' ':
			if first {
				return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "nothing after @"}
			}
			in = in[1:]
			r.EvalExpr = in
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

	var gid, frame int

	switch se.Kind {
	case InvalidScopeExpr:
		return &api.Variable{Name: expr, Unreadable: "syntax error: " + se.EvalExpr}

	case NormalScopeExpr:
		gid = se.Gid
		if gid < 0 {
			gid = curGid
		}
		frame = se.Fid
		if frame < 0 {
			frame = curFrame
		}

	case FrameOffsetScopeExpr:
		gid = se.Gid
		if gid < 0 {
			gid = curGid
		}
		frame = findFrameOffset(gid, int64(se.Foff), nil)

	case FrameRegexScopeExpr:
		gid = se.Gid
		if gid < 0 {
			gid = curGid
		}
		frame = findFrameOffset(gid, 0, se.Fre)

	default:
		panic("unknown kind")
	}

	if frame < 0 {
		return &api.Variable{Name: expr, Unreadable: "could not find specified frame"}
	}

	v, err := client.EvalVariable(api.EvalScope{gid, frame}, se.EvalExpr, cfg)
	if err != nil {
		return &api.Variable{Name: expr, Unreadable: err.Error()}
	}
	return v
}

func findFrameOffset(gid int, frameOffset int64, rx *regexp.Regexp) (frame int) {
	frames, err := client.Stacktrace(gid, 100, nil)
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
