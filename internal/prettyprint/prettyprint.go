package prettyprint

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"

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

// SimpleFormat describes how a variable should be formatted
type SimpleFormat struct {
	HexdumpString bool   // hexdump strings when printing
	IntFormat     string // formatting directive for integers
	FloatFormat   string // formatting directive for floats
}

type context struct {
	buf  io.Writer
	sfmt *SimpleFormat
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
	writeTo(v, &context{&buf, nil}, flags, "")
	return buf.String()
}

func Multiline(v *api.Variable, indent string, sfmt *SimpleFormat) string {
	var buf bytes.Buffer
	writeTo(v, &context{&buf, sfmt}, ppTop|ppNewlines|ppIncludeType, indent)
	return buf.String()
}

func writeTo(v *api.Variable, ctx *context, flags ppFlags, indent string) {
	if v.Unreadable != "" {
		fmt.Fprintf(ctx.buf, "(unreadable %s)", v.Unreadable)
		return
	}

	if !flags.top() && v.Addr == 0 && v.Value == "" {
		if flags.includeType() && v.Type != "void" {
			fmt.Fprintf(ctx.buf, "%s nil", getDisplayType(v, flags.fullTypes()))
		} else {
			fmt.Fprint(ctx.buf, "nil")
		}
		return
	}

	switch v.Kind {
	case reflect.Slice:
		writeSliceTo(v, ctx, flags, indent)
	case reflect.Array:
		writeArrayTo(v, ctx, flags, indent)
	case reflect.Ptr:
		if v.Type == "" {
			fmt.Fprint(ctx.buf, "nil")
		} else if len(v.Children) < 1 {
			fmt.Fprintf(ctx.buf, "(%s)(noaddr?)", v.Type)
		} else if v.Children[0].OnlyAddr && v.Children[0].Addr != 0 {
			fmt.Fprintf(ctx.buf, "(%s)(0x%x)", v.Type, v.Children[0].Addr)
		} else {
			fmt.Fprint(ctx.buf, "*")
			writeTo(&v.Children[0], ctx, flags.clearTop(), indent)
		}
	case reflect.UnsafePointer:
		fmt.Fprintf(ctx.buf, "unsafe.Pointer(0x%x)", v.Children[0].Addr)
	case reflect.String:
		if !flags.top() || ctx.sfmt == nil || !ctx.sfmt.HexdumpString {
			writeStringTo(v, ctx)
		} else {
			ctx.buf.Write([]byte(ctx.sfmt.Apply(v)))
		}
	case reflect.Chan:
		if flags.newlines() {
			writeStructTo(v, ctx, flags, indent)
		} else {
			if len(v.Children) == 0 {
				fmt.Fprintf(ctx.buf, "%s nil", v.Type)
			} else {
				fmt.Fprintf(ctx.buf, "%s %s/%s", v.Type, v.Children[0].Value, v.Children[1].Value)
			}
		}
	case reflect.Struct:
		writeStructTo(v, ctx, flags, indent)
	case reflect.Interface:
		if v.Addr == 0 {
			// an escaped interface variable that points to nil, this shouldn't
			// happen in normal code but can happen if the variable is out of scope.
			fmt.Fprintf(ctx.buf, "nil")
			return
		}
		if flags.includeType() {
			if v.Children[0].Kind == reflect.Invalid {
				fmt.Fprintf(ctx.buf, "%s ", getDisplayType(v, flags.fullTypes()))
				if v.Children[0].Addr == 0 {
					fmt.Fprint(ctx.buf, "nil")
					return
				}
			} else {
				fmt.Fprintf(ctx.buf, "%s(%s) ", getDisplayType(v, flags.fullTypes()), getDisplayType(&v.Children[0], flags.fullTypes()))
			}
		}
		data := v.Children[0]
		if data.Kind == reflect.Ptr {
			if len(data.Children) == 0 {
				fmt.Fprint(ctx.buf, "...")
			} else if data.Children[0].Addr == 0 {
				fmt.Fprint(ctx.buf, "nil")
			} else if data.Children[0].OnlyAddr {
				fmt.Fprintf(ctx.buf, "0x%x", v.Children[0].Addr)
			} else {
				writeTo(&v.Children[0], ctx, flags.clearTop().negateIncludeType(), indent)
			}
		} else if data.OnlyAddr {
			fmt.Fprintf(ctx.buf, "*(*%q)(0x%x)", v.Type, v.Addr)
		} else {
			writeTo(&v.Children[0], ctx, flags.clearTop().negateIncludeType(), indent)
		}
	case reflect.Map:
		writeMapTo(v, ctx, flags, indent)
	case reflect.Func:
		if v.Value == "" {
			fmt.Fprint(ctx.buf, "nil")
		} else {
			fmt.Fprintf(ctx.buf, "%s", v.Value)
		}
	case reflect.Complex64, reflect.Complex128:
		fmt.Fprintf(ctx.buf, "(%s + %si)", v.Children[0].Value, v.Children[1].Value)
	default:
		if v.Value != "" {
			if ctx.sfmt != nil {
				ctx.buf.Write([]byte(ctx.sfmt.Apply(v)))
			} else {
				ctx.buf.Write([]byte(v.Value))
			}
		} else {
			fmt.Fprintf(ctx.buf, "(unknown %s)", v.Kind)
		}
	}
}

func writeStringTo(v *api.Variable, ctx *context) {
	s := v.Value
	if len(s) != int(v.Len) {
		s = fmt.Sprintf("%s...+%d more", s, int(v.Len)-len(s))
	}
	fmt.Fprintf(ctx.buf, "%q", s)
}

func writeSliceTo(v *api.Variable, ctx *context, flags ppFlags, indent string) {
	if flags.includeType() {
		fmt.Fprintf(ctx.buf, "%s len: %d, cap: %d, ", getDisplayType(v, flags.fullTypes()), v.Len, v.Cap)
	}
	if v.Base == 0 && len(v.Children) == 0 {
		fmt.Fprintf(ctx.buf, "nil")
		return
	}
	writeSliceOrArrayTo(v, ctx, flags, indent)
}

func writeArrayTo(v *api.Variable, ctx *context, flags ppFlags, indent string) {
	if flags.includeType() {
		fmt.Fprintf(ctx.buf, "%s ", getDisplayType(v, flags.fullTypes()))
	}
	writeSliceOrArrayTo(v, ctx, flags, indent)
}

func writeStructTo(v *api.Variable, ctx *context, flags ppFlags, indent string) {
	if int(v.Len) != len(v.Children) && len(v.Children) == 0 {
		fmt.Fprintf(ctx.buf, "(*%s)(0x%x)", v.Type, v.Addr)
		return
	}

	if flags.includeType() {
		fmt.Fprintf(ctx.buf, "%s ", getDisplayType(v, flags.fullTypes()))
	}

	nl := shouldNewlineStruct(v, flags.newlines())

	fmt.Fprint(ctx.buf, "{")

	for i := range v.Children {
		if nl {
			fmt.Fprintf(ctx.buf, "\n%s%s", indent, indentString)
		}
		fmt.Fprintf(ctx.buf, "%s: ", v.Children[i].Name)
		writeTo(&v.Children[i], ctx, flags.clearTop()|ppIncludeType, indent+indentString)
		if i != len(v.Children)-1 || nl {
			fmt.Fprint(ctx.buf, ",")
			if !nl {
				fmt.Fprint(ctx.buf, " ")
			}
		}
	}

	if len(v.Children) != int(v.Len) {
		if nl {
			fmt.Fprintf(ctx.buf, "\n%s%s", indent, indentString)
		} else {
			fmt.Fprint(ctx.buf, ",")
		}
		fmt.Fprintf(ctx.buf, "...+%d more", int(v.Len)-len(v.Children))
	}

	fmt.Fprint(ctx.buf, "}")
}

func writeMapTo(v *api.Variable, ctx *context, flags ppFlags, indent string) {
	if flags.includeType() {
		fmt.Fprintf(ctx.buf, "%s ", getDisplayType(v, flags.fullTypes()))
	}
	if v.Base == 0 && len(v.Children) == 0 {
		fmt.Fprintf(ctx.buf, "nil")
		return
	}

	nl := flags.newlines() && (len(v.Children) > 0)

	fmt.Fprint(ctx.buf, "[")

	for i := 0; i < len(v.Children); i += 2 {
		key := &v.Children[i]
		value := &v.Children[i+1]

		if nl {
			fmt.Fprintf(ctx.buf, "\n%s%s", indent, indentString)
		}

		if value == nil {
			fmt.Fprintf(ctx.buf, "%s: ", key.Name)
			writeTo(key, ctx, flags.clearTop().clearIncludeType(), indent+indentString)
		} else {
			writeTo(key, ctx, flags.clearTop().clearNewlines().clearIncludeType(), indent+indentString)
			fmt.Fprint(ctx.buf, ": ")
			writeTo(value, ctx, flags.clearTop().clearIncludeType(), indent+indentString)
		}
		if i != len(v.Children)-1 || nl {
			fmt.Fprint(ctx.buf, ", ")
		}
	}

	if len(v.Children)/2 != int(v.Len) {
		if len(v.Children) != 0 {
			if nl {
				fmt.Fprintf(ctx.buf, "\n%s%s", indent, indentString)
			} else {
				fmt.Fprint(ctx.buf, ",")
			}
			fmt.Fprintf(ctx.buf, "...+%d more", int(v.Len)-(len(v.Children)/2))
		} else {
			fmt.Fprint(ctx.buf, "...")
		}
	}

	if nl {
		fmt.Fprintf(ctx.buf, "\n%s", indent)
	}
	fmt.Fprint(ctx.buf, "]")
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

func writeSliceOrArrayTo(v *api.Variable, ctx *context, flags ppFlags, indent string) {
	nl := shouldNewlineArray(v, flags.newlines())
	fmt.Fprint(ctx.buf, "[")

	for i := range v.Children {
		if nl {
			fmt.Fprintf(ctx.buf, "\n%s%s", indent, indentString)
		}
		writeTo(&v.Children[i], ctx, (flags & ^ppTop) & ^ppIncludeType, indent+indentString)
		if i != len(v.Children)-1 || nl {
			fmt.Fprint(ctx.buf, ",")
		}
	}

	if len(v.Children) != int(v.Len) {
		if len(v.Children) != 0 {
			if nl {
				fmt.Fprintf(ctx.buf, "\n%s%s", indent, indentString)
			} else {
				fmt.Fprint(ctx.buf, ",")
			}
			fmt.Fprintf(ctx.buf, "...+%d more", int(v.Len)-len(v.Children))
		} else {
			fmt.Fprint(ctx.buf, "...")
		}
	}

	if nl {
		fmt.Fprintf(ctx.buf, "\n%s", indent)
	}

	fmt.Fprint(ctx.buf, "]")
}

func getDisplayType(v *api.Variable, fullTypes bool) string {
	if fullTypes {
		return v.Type
	}
	return ShortenType(v.Type)
}

func (sfmt *SimpleFormat) Apply(v *api.Variable) string {
	if v.Unreadable != "" || v.Value == "" {
		return v.Value
	}
	switch v.Kind {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if sfmt.IntFormat == "" {
			return v.Value
		}
		n, _ := strconv.ParseInt(v.Value, 10, 64)
		return fmt.Sprintf(sfmt.IntFormat, n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if sfmt.IntFormat == "" {
			return v.Value
		}
		n, _ := strconv.ParseUint(v.Value, 10, 64)
		return fmt.Sprintf(sfmt.IntFormat, n)
	case reflect.Float32, reflect.Float64:
		if sfmt.FloatFormat == "" {
			return v.Value
		}
		x, _ := strconv.ParseFloat(v.Value, 64)
		return fmt.Sprintf(sfmt.FloatFormat, x)
	case reflect.Complex64, reflect.Complex128:
		if sfmt.FloatFormat == "" {
			return v.Value
		}
		real, _ := strconv.ParseFloat(v.Children[0].Value, 64)
		imag, _ := strconv.ParseFloat(v.Children[1].Value, 64)
		return fmt.Sprintf(sfmt.FloatFormat, complex(real, imag))
	case reflect.String:
		if !sfmt.HexdumpString {
			return v.Value
		}
		var buf strings.Builder
		d := Hexdigits(uint64(len(v.Value)))
		for i := 0; i < len(v.Value); i += 16 {
			fmt.Fprintf(&buf, "%[1]*[2]x | ", d, i)
			for c := 0; c < 16; c++ {
				if c == 8 {
					buf.WriteByte(' ')
				}
				if i+c < len(v.Value) {
					fmt.Fprintf(&buf, "%02x ", v.Value[i+c])
				} else {
					fmt.Fprintf(&buf, "   ")
				}
			}
			fmt.Fprintf(&buf, "|")
			for c := 0; c < 16; c++ {
				if i+c < len(v.Value) {
					ch := v.Value[i+c]
					if ch >= 0x20 && ch <= 0x7e {
						fmt.Fprintf(&buf, "%c", byte(ch))
					} else {
						fmt.Fprintf(&buf, ".")
					}
				} else {
					fmt.Fprintf(&buf, " ")
				}
			}
			fmt.Fprintf(&buf, "|\n")
		}
		return buf.String()
	default:
		return v.Value
	}
}

func Hexdigits(n uint64) int {
	if n <= 0 {
		return 1
	}
	return int(math.Floor(math.Log10(float64(n))/math.Log10(16))) + 1
}
