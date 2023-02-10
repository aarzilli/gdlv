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
	"github.com/aarzilli/gdlv/internal/prettyprint"
	"github.com/aarzilli/gdlv/internal/starbind"
)

// ScopedExpr represents an expression to be evaluated in a specified scope.
type ScopedExpr struct {
	Kind   ScopeExprKind
	Gid    int            // goroutine id (-1 for current goroutine)
	Fid    int            // frame id (-1 for current goroutine)
	Foff   int            // frame offset (will search for this specified frame offset or return an error otherwise)
	Fre    *regexp.Regexp // frame regular expression (will search for a frame in a function matching this regular expression)
	Frestr string

	DeferredCall int // deferred call index

	EvalExpr string // expression to evaluate

	MaxStringLen       int // maximum string length if > 0
	MaxArrayValues     int // maximum array values if > 0
	MaxVariableRecurse int // maximum variable recursion if > 0
	Fmt                prettyprint.SimpleFormat
}

type ScopeExprKind uint8

const (
	NormalScopeExpr      ScopeExprKind = iota // use Gid and Fid
	FrameOffsetScopeExpr                      // use Foff instead of Fid
	FrameRegexScopeExpr                       // use Fre instead of Fid
	InvalidScopeExpr
)

func ParseScopedExpr(in string) ScopedExpr {
	r := ScopedExpr{Kind: NormalScopeExpr, Gid: -1, Fid: -1, DeferredCall: -1}
	first := true

	for len(in) > 0 {
		for i, ch := range in {
			if unicode.IsSpace(ch) {
				continue
			}
			if ch != '@' && ch != '%' {
				r.EvalExpr = strings.TrimSpace(in)
				return r
			} else {
				in = in[i:]
				break
			}
		}

		if len(in) < 2 {
			return ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "not long enough"}
		}

		switch in[0] {
		case '@':
			if first {
				r.DeferredCall = 0
			}
			first = false
			in = parseScopedExprScope(in, &r)
		case '%':
			in = parseScopedExprLoad(in, &r)
		}
		if r.Kind == InvalidScopeExpr {
			return r
		}
	}
	return r
}

func parseScopedExprScope(in string, r *ScopedExpr) string {
	in = in[1:]

	first := true
	var gseen, fseen, dseen bool

	for {
		if len(in) == 0 {
			*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "no expression"}
			return ""
		}
		switch in[0] {
		case 'g':
			in = in[1:]
			if gseen || len(in) == 0 {
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "no argument for 'g'"}
				return ""
			}
			gseen = true
			var ok bool
			in, r.Gid, ok = scopeReadNumber(in)
			if !ok {
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid argument for 'g'"}
				return ""
			}
			if r.Gid < 0 {
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid (negative) argument for 'g'"}
				return ""
			}

		case 'f':
			in = in[1:]
			if fseen || len(in) == 0 {
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "no argument for 'f'"}
				return ""
			}
			fseen = true
			if in[0] == '/' {
				var s string
				s, in = scopeReadDelim('/', in[1:])
				r.Frestr = s
				var err error
				r.Fre, err = regexp.Compile(s)
				if err != nil {
					*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: fmt.Sprintf("could not compile regexp: %v", err)}
					return ""
				}
				r.Kind = FrameRegexScopeExpr
			} else {
				var ok bool
				in, r.Fid, ok = scopeReadNumber(in)
				if !ok {
					*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid argument for 'f'"}
					return ""
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
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "no argument for 'd'"}
				return ""
			}
			dseen = true
			var ok bool
			in, r.DeferredCall, ok = scopeReadNumber(in)
			if !ok {
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid argument for 'd'"}
				return ""
			}
			if r.DeferredCall < 0 {
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "invalid (negative) argument for 'd'"}
				return ""
			}

		case ' ':
			if first {
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: "nothing after @"}
				return ""
			}
			in = in[1:]
			return strings.TrimSpace(in)

		default:
			*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: fmt.Sprintf("unknown character %c", in[0])}
			return ""
		}
		first = false
	}
}

func parseScopedExprLoad(in string, r *ScopedExpr) string {
	for i := range in {
		parseNum := func() int {
			s := in[:i+1]
			s = s[1:]
			s = s[:len(s)-1]
			if s == "" {
				return 0
			}
			n, err := strconv.Atoi(s)
			if err != nil {
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: fmt.Sprintf("invalid formatter %q", in[1:])}
			}
			return n

		}
		if (in[i] < '0' || in[i] > '9') && in[i] != '%' && in[i] != '#' && in[i] != '+' && in[i] != '.' {
			switch in[i] {
			case 's':
				s := in[:i+1]
				s = s[1:]
				if s[0] == '#' {
					r.Fmt.HexdumpString = true
					s = s[1:]
				}
				s = s[:len(s)-1]
				if s != "" {
					n, err := strconv.Atoi(s)
					if err != nil {
						*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: fmt.Sprintf("invalid formatter %q", in[1:])}
					}
					r.MaxStringLen = n
				}
				return in[i+1:]
			case 'a':
				r.MaxArrayValues = parseNum()
				return in[i+1:]
			case 'v':
				r.MaxVariableRecurse = parseNum()
				return in[i+1:]
			case 'x', 'X', 'o', 'O', 'd':
				r.Fmt.IntFormat = in[:i+1]
				return in[i+1:]
			case 'e', 'f', 'g', 'E', 'F', 'G':
				r.Fmt.FloatFormat = in[:i+1]
				return in[i+1:]
			default:
				*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: fmt.Sprintf("unknown format string character '%c'", in[i])}
				return ""
			}
		}
	}
	*r = ScopedExpr{Kind: InvalidScopeExpr, EvalExpr: fmt.Sprintf("non-termianted format string %q", in)}
	return ""
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

func evalScopedExpr(expr string, cfg api.LoadConfig, customFormatters bool) (*Variable, *prettyprint.SimpleFormat) {
	unreadable := func(err string) (*Variable, *prettyprint.SimpleFormat) {
		return &Variable{Variable: &api.Variable{Name: expr, Unreadable: err}}, nil
	}

	se := ParseScopedExpr(expr)

	var gid, frame, deferredCall int

	gid = se.Gid
	if gid < 0 {
		gid = curGid
	}

	switch se.Kind {
	case InvalidScopeExpr:
		return unreadable("syntax error: " + se.EvalExpr)

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
		return unreadable("could not find specified frame")
	}

	if se.MaxStringLen > 0 {
		cfg.MaxStringLen = se.MaxStringLen
	}
	if se.MaxArrayValues > 0 {
		cfg.MaxArrayValues = se.MaxArrayValues
	}
	if se.MaxVariableRecurse > 0 {
		cfg.MaxVariableRecurse = se.MaxVariableRecurse
	}

	if len(se.EvalExpr) == 0 || se.EvalExpr[0] != '$' {
		v, err := client.EvalVariable(api.EvalScope{gid, frame, deferredCall}, se.EvalExpr, cfg)
		if err != nil {
			return unreadable(err.Error())
		}
		return wrapApiVariable("", v, v.Name, v.Name, customFormatters, &se.Fmt, 0), &se.Fmt
	}

	sv, err := StarlarkEnv.Execute(&editorWriter{true}, "<expr>", strings.TrimLeft(se.EvalExpr[1:], " "), "<expr>", nil, nil)
	if err != nil {
		return unreadable(err.Error())
	}

	v := convertStarlarkToVariable(expr, sv)
	return wrapApiVariable("", v, v.Name, v.Name, customFormatters, &se.Fmt, 0), &se.Fmt
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
	case starlark.String:
		return &api.Variable{Name: expr, Kind: reflect.String, Type: "string", RealType: "string", Len: int64(len(string(sv))), Value: string(sv)}
	default:
		s := sv.String()
		return &api.Variable{Name: expr, Kind: reflect.String, Type: "string", RealType: "string", Len: int64(len(s)), Value: s}
	}
}

func findFrameOffset(gid int, frameOffset int64, rx *regexp.Regexp) (frame int) {
	frames, err := client.Stacktrace(gid, 100, 0, nil)
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

func (se *ScopedExpr) String() string {
	var buf strings.Builder

	started := false
	start := func() {
		if started {
			return
		}
		buf.WriteByte('@')
		started = true
	}

	if se.Gid > 0 {
		start()
		fmt.Fprintf(&buf, "g%d", se.Gid)
	}

	switch se.Kind {
	case NormalScopeExpr:
		if se.Fid >= 0 {
			start()
			fmt.Fprintf(&buf, "f%#x", se.Fid)
		}
	case FrameOffsetScopeExpr:
		start()
		fmt.Fprintf(&buf, "f%#x", se.Foff)
	case FrameRegexScopeExpr:
		start()
		fmt.Fprintf(&buf, "f/%s/", escapeSlash(se.Frestr))

	}

	if se.DeferredCall > 0 {
		start()
		fmt.Fprintf(&buf, "d%d", se.DeferredCall)
	}

	if started {
		buf.WriteByte(' ')
	}

	oldlen := buf.Len()

	if se.MaxStringLen > 0 || se.Fmt.HexdumpString {
		buf.WriteByte('%')
		if se.Fmt.HexdumpString {
			buf.WriteByte('#')
		}
		if se.MaxStringLen > 0 {
			fmt.Fprintf(&buf, "%d", se.MaxStringLen)
		}
		buf.WriteByte('s')
	}

	buf.WriteString(se.Fmt.IntFormat)
	buf.WriteString(se.Fmt.FloatFormat)

	if buf.Len() != oldlen {
		buf.WriteByte(' ')
	}

	buf.WriteString(se.EvalExpr)
	return buf.String()
}

func escapeSlash(s string) string {
	found := false
	for i := range s {
		if s[i] == '/' {
			found = true
			break
		}
	}
	if !found {
		return s
	}

	var b []byte
	for i := range s {
		if s[i] == '/' || s[i] == '\\' {
			b = append(b, '\\')
		}
		b = append(b, s[i])
	}
	return string(b)
}
