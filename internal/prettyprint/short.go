package prettyprint

import (
	"strings"
	"unicode"
)

func ShortenType(typ string) string {
	out, ok := shortenTypeEx(typ)
	if !ok {
		return typ
	}
	return out
}

func shortenTypeEx(typ string) (string, bool) {
	switch {
	case strings.HasPrefix(typ, "["):
		for i := range typ {
			if typ[i] == ']' {
				sub, ok := shortenTypeEx(typ[i+1:])
				return typ[:i+1] + sub, ok
			}
		}
		return "", false
	case strings.HasPrefix(typ, "*"):
		sub, ok := shortenTypeEx(typ[1:])
		return "*" + sub, ok
	case strings.HasPrefix(typ, "map["):
		depth := 1
		for i := 4; i < len(typ); i++ {
			switch typ[i] {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					key, keyok := shortenTypeEx(typ[4:i])
					val, valok := shortenTypeEx(typ[i+1:])
					return "map[" + key + "]" + val, keyok && valok
				}
			}
		}
		return "", false
	case typ == "interface {}" || typ == "interface{}":
		return typ, true
	case typ == "struct {}" || typ == "struct{}":
		return typ, true
	default:
		if containsAnonymousType(typ) {
			return "", false
		}

		if lbrk := strings.Index(typ, "["); lbrk >= 0 {
			if typ[len(typ)-1] != ']' {
				return "", false
			}
			typ0, ok := shortenTypeEx(typ[:lbrk])
			if !ok {
				return "", false
			}
			args := strings.Split(typ[lbrk+1:len(typ)-1], ",")
			for i := range args {
				var ok bool
				args[i], ok = shortenTypeEx(strings.TrimSpace(args[i]))
				if !ok {
					return "", false
				}
			}
			return typ0 + "[" + strings.Join(args, ", ") + "]", true
		}

		slashnum := 0
		slash := -1
		for i, ch := range typ {
			if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' && ch != '.' && ch != '/' && ch != '@' && ch != '%' && ch != '-' {
				return "", false
			}
			if ch == '/' {
				slash = i
				slashnum++
			}
		}
		if slashnum <= 1 || slash < 0 {
			return typ, true
		}
		return typ[slash+1:], true
	}
}

func containsAnonymousType(typ string) bool {
	for _, thing := range []string{"interface {", "interface{", "struct {", "struct{", "func (", "func("} {
		idx := strings.Index(typ, thing)
		if idx >= 0 && idx+len(thing) < len(typ) {
			ch := typ[idx+len(thing)]
			if ch != '}' && ch != ')' {
				return true
			}
		}
	}
	return false
}

func ShortenFunctionName(fnname string) string {
	pkgname := packageName(fnname)
	lastSlash := strings.LastIndex(pkgname, "/")
	if lastSlash >= 0 {
		return fnname[lastSlash+1:]
	}
	return fnname
}

func instRange(fnname string) [2]int {
	d := len(fnname)
	inst := [2]int{d, d}
	if strings.HasPrefix(fnname, "type..") {
		return inst
	}
	inst[0] = strings.Index(fnname, "[")
	if inst[0] < 0 {
		inst[0] = d
		return inst
	}
	inst[1] = strings.LastIndex(fnname, "]")
	if inst[1] < 0 {
		inst[0] = d
		inst[1] = d
		return inst
	}
	return inst
}

func packageName(name string) string {
	pathend := strings.LastIndex(name, "/")
	if pathend < 0 {
		pathend = 0
	}

	if i := strings.Index(name[pathend:], "."); i != -1 {
		return name[:pathend+i]
	}
	return ""
}
