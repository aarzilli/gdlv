package prettyprint

import (
	"io"
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"unicode"
	
	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
)

const (
	// strings longer than this will cause slices, arrays and structs to be printed on multiple lines when newlines is enabled
	maxShortStringLen = 7
	// string used for one indentation level (when printing on multiple lines)
	indentString = "\t"
)

type ppFlags uint8

const (
	ppTop ppFlags = 1 << iota
	ppNewlines
	ppIncludeType
	ppFullTypes
)

func (flags ppFlags) top() bool {
	return flags&ppTop != 0
}

func (flags ppFlags) newlines() bool {
	return flags&ppNewlines != 0
}

func (flags ppFlags) includeType() bool {
	return flags&ppIncludeType != 0
}

func (flags ppFlags) fullTypes() bool {
	return flags&ppFullTypes != 0
}

func (flags ppFlags) negateIncludeType() ppFlags {
	if flags.includeType() {
		return flags & ^ppIncludeType
	}
	return flags | ppIncludeType
}

func (flags ppFlags) clearTop() ppFlags {
	return flags & ^ppTop
}

func (flags ppFlags) clearNewlines() ppFlags {
	return flags & ^ppNewlines
}

func (flags ppFlags) clearIncludeType() ppFlags {
	return flags & ^ppIncludeType
}

func Singleline(v *api.Variable, includeType, fullTypes bool) string {
	var buf bytes.Buffer
	flags := ppTop
	if includeType {
		flags |= ppIncludeType
	}
	if fullTypes {
		flags |= ppFullTypes
	}
	writeTo(v, &buf, flags, "")
	return buf.String()
}

func Multiline(v *api.Variable, indent string) string {
	var buf bytes.Buffer
	writeTo(v, &buf, ppTop|ppNewlines|ppIncludeType, indent)
	return buf.String()
}

func writeTo(v *api.Variable, buf io.Writer, flags ppFlags, indent string) {
	if v.Unreadable != "" {
		fmt.Fprintf(buf, "(unreadable %s)", v.Unreadable)
		return
	}

	if !flags.top() && v.Addr == 0 && v.Value == "" {
		if flags.includeType() && v.Type != "void" {
			fmt.Fprintf(buf, "%s nil", getDisplayType(v, flags.fullTypes()))
		} else {
			fmt.Fprint(buf, "nil")
		}
		return
	}

	switch v.Kind {
	case reflect.Slice:
		writeSliceTo(v, buf, flags, indent)
	case reflect.Array:
		writeArrayTo(v, buf, flags, indent)
	case reflect.Ptr:
		if v.Type == "" {
			fmt.Fprint(buf, "nil")
		} else if len(v.Children) < 1 {
			fmt.Fprintf(buf, "(%s)(noaddr?)", v.Type)
		} else if v.Children[0].OnlyAddr && v.Children[0].Addr != 0 {
			fmt.Fprintf(buf, "(%s)(0x%x)", v.Type, v.Children[0].Addr)
		} else {
			fmt.Fprint(buf, "*")
			writeTo(&v.Children[0], buf, flags.clearTop(), indent)
		}
	case reflect.UnsafePointer:
		fmt.Fprintf(buf, "unsafe.Pointer(0x%x)", v.Children[0].Addr)
	case reflect.String:
		writeStringTo(v, buf)
	case reflect.Chan:
		if flags.newlines() {
			writeStructTo(v, buf, flags, indent)
		} else {
			if len(v.Children) == 0 {
				fmt.Fprintf(buf, "%s nil", v.Type)
			} else {
				fmt.Fprintf(buf, "%s %s/%s", v.Type, v.Children[0].Value, v.Children[1].Value)
			}
		}
	case reflect.Struct:
		writeStructTo(v, buf, flags, indent)
	case reflect.Interface:
		if v.Addr == 0 {
			// an escaped interface variable that points to nil, this shouldn't
			// happen in normal code but can happen if the variable is out of scope.
			fmt.Fprintf(buf, "nil")
			return
		}
		if flags.includeType() {
			if v.Children[0].Kind == reflect.Invalid {
				fmt.Fprintf(buf, "%s ", getDisplayType(v, flags.fullTypes()))
				if v.Children[0].Addr == 0 {
					fmt.Fprint(buf, "nil")
					return
				}
			} else {
				fmt.Fprintf(buf, "%s(%s) ", getDisplayType(v, flags.fullTypes()), getDisplayType(&v.Children[0], flags.fullTypes()))
			}
		}
		data := v.Children[0]
		if data.Kind == reflect.Ptr {
			if len(data.Children) == 0 {
				fmt.Fprint(buf, "...")
			} else if data.Children[0].Addr == 0 {
				fmt.Fprint(buf, "nil")
			} else if data.Children[0].OnlyAddr {
				fmt.Fprintf(buf, "0x%x", v.Children[0].Addr)
			} else {
				writeTo(&v.Children[0], buf, flags.clearTop().negateIncludeType(), indent)
			}
		} else if data.OnlyAddr {
			fmt.Fprintf(buf, "*(*%q)(0x%x)", v.Type, v.Addr)
		} else {
			writeTo(&v.Children[0], buf, flags.clearTop().negateIncludeType(), indent)
		}
	case reflect.Map:
		writeMapTo(v, buf, flags, indent)
	case reflect.Func:
		if v.Value == "" {
			fmt.Fprint(buf, "nil")
		} else {
			fmt.Fprintf(buf, "%s", v.Value)
		}
	case reflect.Complex64, reflect.Complex128:
		fmt.Fprintf(buf, "(%s + %si)", v.Children[0].Value, v.Children[1].Value)
	default:
		if v.Value != "" {
			buf.Write([]byte(v.Value))
		} else {
			fmt.Fprintf(buf, "(unknown %s)", v.Kind)
		}
	}
}

func writeStringTo(v *api.Variable, buf io.Writer) {
	s := v.Value
	if len(s) != int(v.Len) {
		s = fmt.Sprintf("%s...+%d more", s, int(v.Len)-len(s))
	}
	fmt.Fprintf(buf, "%q", s)
}

func writeSliceTo(v *api.Variable, buf io.Writer, flags ppFlags, indent string) {
	if flags.includeType() {
		fmt.Fprintf(buf, "%s len: %d, cap: %d, ", getDisplayType(v, flags.fullTypes()), v.Len, v.Cap)
	}
	if v.Base == 0 && len(v.Children) == 0 {
		fmt.Fprintf(buf, "nil")
		return
	}
	writeSliceOrArrayTo(v, buf, flags, indent)
}

func writeArrayTo(v *api.Variable, buf io.Writer, flags ppFlags, indent string) {
	if flags.includeType() {
		fmt.Fprintf(buf, "%s ", getDisplayType(v, flags.fullTypes()))
	}
	writeSliceOrArrayTo(v, buf, flags, indent)
}

func writeStructTo(v *api.Variable, buf io.Writer, flags ppFlags, indent string) {
	if int(v.Len) != len(v.Children) && len(v.Children) == 0 {
		fmt.Fprintf(buf, "(*%s)(0x%x)", v.Type, v.Addr)
		return
	}

	if flags.includeType() {
		fmt.Fprintf(buf, "%s ", getDisplayType(v, flags.fullTypes()))
	}

	nl := shouldNewlineStruct(v, flags.newlines())

	fmt.Fprint(buf, "{")

	for i := range v.Children {
		if nl {
			fmt.Fprintf(buf, "\n%s%s", indent, indentString)
		}
		fmt.Fprintf(buf, "%s: ", v.Children[i].Name)
		writeTo(&v.Children[i], buf, flags.clearTop()|ppIncludeType, indent+indentString)
		if i != len(v.Children)-1 || nl {
			fmt.Fprint(buf, ",")
			if !nl {
				fmt.Fprint(buf, " ")
			}
		}
	}

	if len(v.Children) != int(v.Len) {
		if nl {
			fmt.Fprintf(buf, "\n%s%s", indent, indentString)
		} else {
			fmt.Fprint(buf, ",")
		}
		fmt.Fprintf(buf, "...+%d more", int(v.Len)-len(v.Children))
	}

	fmt.Fprint(buf, "}")
}

func writeMapTo(v *api.Variable, buf io.Writer, flags ppFlags, indent string) {
	if flags.includeType() {
		fmt.Fprintf(buf, "%s ", getDisplayType(v, flags.fullTypes()))
	}
	if v.Base == 0 && len(v.Children) == 0 {
		fmt.Fprintf(buf, "nil")
		return
	}

	nl := flags.newlines() && (len(v.Children) > 0)

	fmt.Fprint(buf, "[")

	for i := 0; i < len(v.Children); i += 2 {
		key := &v.Children[i]
		value := &v.Children[i+1]

		if nl {
			fmt.Fprintf(buf, "\n%s%s", indent, indentString)
		}

		if value == nil {
			fmt.Fprintf(buf, "%s: ", key.Name)
			writeTo(key, buf, flags.clearTop().clearIncludeType(), indent+indentString)
		} else {
			writeTo(key, buf, flags.clearTop().clearNewlines().clearIncludeType(), indent+indentString)
			fmt.Fprint(buf, ": ")
			writeTo(value, buf, flags.clearTop().clearIncludeType(), indent+indentString)
		}
		if i != len(v.Children)-1 || nl {
			fmt.Fprint(buf, ", ")
		}
	}

	if len(v.Children)/2 != int(v.Len) {
		if len(v.Children) != 0 {
			if nl {
				fmt.Fprintf(buf, "\n%s%s", indent, indentString)
			} else {
				fmt.Fprint(buf, ",")
			}
			fmt.Fprintf(buf, "...+%d more", int(v.Len)-(len(v.Children)/2))
		} else {
			fmt.Fprint(buf, "...")
		}
	}

	if nl {
		fmt.Fprintf(buf, "\n%s", indent)
	}
	fmt.Fprint(buf, "]")
}

func shouldNewlineArray(v *api.Variable, newlines bool) bool {
	if !newlines || len(v.Children) == 0 {
		return false
	}

	kind, hasptr := recursiveKind(&v.Children[0])

	switch kind {
	case reflect.Slice, reflect.Array, reflect.Struct, reflect.Map, reflect.Interface:
		return true
	case reflect.String:
		if hasptr {
			return true
		}
		for i := range v.Children {
			if len(v.Children[i].Value) > maxShortStringLen {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func recursiveKind(v *api.Variable) (reflect.Kind, bool) {
	hasptr := false
	var kind reflect.Kind
	for {
		kind = v.Kind
		if kind == reflect.Ptr {
			hasptr = true
			if len(v.Children) <= 0 {
				return reflect.Ptr, true
			}
			v = &v.Children[0]
		} else {
			break
		}
	}
	return kind, hasptr
}

func shouldNewlineStruct(v *api.Variable, newlines bool) bool {
	if !newlines || len(v.Children) == 0 {
		return false
	}

	for i := range v.Children {
		kind, hasptr := recursiveKind(&v.Children[i])

		switch kind {
		case reflect.Slice, reflect.Array, reflect.Struct, reflect.Map, reflect.Interface:
			return true
		case reflect.String:
			if hasptr {
				return true
			}
			if len(v.Children[i].Value) > maxShortStringLen {
				return true
			}
		}
	}

	return false
}

func writeSliceOrArrayTo(v *api.Variable, buf io.Writer, flags ppFlags, indent string) {
	nl := shouldNewlineArray(v, flags.newlines())
	fmt.Fprint(buf, "[")

	for i := range v.Children {
		if nl {
			fmt.Fprintf(buf, "\n%s%s", indent, indentString)
		}
		writeTo(&v.Children[i], buf, (flags & ^ppTop) & ^ppIncludeType, indent+indentString)
		if i != len(v.Children)-1 || nl {
			fmt.Fprint(buf, ",")
		}
	}

	if len(v.Children) != int(v.Len) {
		if len(v.Children) != 0 {
			if nl {
				fmt.Fprintf(buf, "\n%s%s", indent, indentString)
			} else {
				fmt.Fprint(buf, ",")
			}
			fmt.Fprintf(buf, "...+%d more", int(v.Len)-len(v.Children))
		} else {
			fmt.Fprint(buf, "...")
		}
	}

	if nl {
		fmt.Fprintf(buf, "\n%s", indent)
	}

	fmt.Fprint(buf, "]")
}

func getDisplayType(v *api.Variable, fullTypes bool) string {
	if fullTypes {
		return v.Type
	}
	return ShortenType(v.Type)
}

func ShortenType(typ string) string {
	out, ok := shortenTypeEx(typ)
	if !ok {
		return typ
	}
	return out
}

func shortenTypeEx(typ string) (string, bool) {
	switch {
	case strings.HasPrefix(typ, "[]"):
		sub, ok := shortenTypeEx(typ[2:])
		return "[]" + sub, ok
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
		slashnum := 0
		slash := -1
		for i, ch := range typ {
			if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' && ch != '.' && ch != '/' && ch != '@' && ch != '%' {
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
