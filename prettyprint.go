package main

import (
	"bytes"
	"fmt"
	"io"
	"reflect"
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

// SinglelineString returns a representation of v on a single line.
func (v *Variable) SinglelineString(includeType, fullTypes bool) string {
	var buf bytes.Buffer
	flags := ppTop
	if includeType {
		flags |= ppIncludeType
	}
	if fullTypes {
		flags |= ppFullTypes
	}
	v.writeTo(&buf, flags, "")
	return buf.String()
}

// MultilineString returns a representation of v on multiple lines.
func (v *Variable) MultilineString(indent string) string {
	var buf bytes.Buffer
	v.writeTo(&buf, ppTop|ppNewlines|ppIncludeType, indent)
	return buf.String()
}

func (v *Variable) writeTo(buf io.Writer, flags ppFlags, indent string) {
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
		v.writeSliceTo(buf, flags, indent)
	case reflect.Array:
		v.writeArrayTo(buf, flags, indent)
	case reflect.Ptr:
		if v.Type == "" {
			fmt.Fprint(buf, "nil")
		} else if v.Children[0].OnlyAddr && v.Children[0].Addr != 0 {
			fmt.Fprintf(buf, "(%s)(0x%x)", v.Type, v.Children[0].Addr)
		} else {
			fmt.Fprint(buf, "*")
			v.Children[0].writeTo(buf, flags.clearTop(), indent)
		}
	case reflect.UnsafePointer:
		fmt.Fprintf(buf, "unsafe.Pointer(0x%x)", v.Children[0].Addr)
	case reflect.String:
		v.writeStringTo(buf)
	case reflect.Chan:
		if flags.newlines() {
			v.writeStructTo(buf, flags, indent)
		} else {
			if len(v.Children) == 0 {
				fmt.Fprintf(buf, "%s nil", v.Type)
			} else {
				fmt.Fprintf(buf, "%s %s/%s", v.Type, v.Children[0].Value, v.Children[1].Value)
			}
		}
	case reflect.Struct:
		v.writeStructTo(buf, flags, indent)
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
				fmt.Fprintf(buf, "%s(%s) ", getDisplayType(v, flags.fullTypes()), getDisplayType(v.Children[0], flags.fullTypes()))
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
				v.Children[0].writeTo(buf, flags.clearTop().negateIncludeType(), indent)
			}
		} else if data.OnlyAddr {
			fmt.Fprintf(buf, "*(*%q)(0x%x)", v.Type, v.Addr)
		} else {
			v.Children[0].writeTo(buf, flags.clearTop().negateIncludeType(), indent)
		}
	case reflect.Map:
		v.writeMapTo(buf, flags, indent)
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

func (v *Variable) writeStringTo(buf io.Writer) {
	s := v.Value
	if len(s) != int(v.Len) {
		s = fmt.Sprintf("%s...+%d more", s, int(v.Len)-len(s))
	}
	fmt.Fprintf(buf, "%q", s)
}

func (v *Variable) writeSliceTo(buf io.Writer, flags ppFlags, indent string) {
	if flags.includeType() {
		fmt.Fprintf(buf, "%s len: %d, cap: %d, ", getDisplayType(v, flags.fullTypes()), v.Len, v.Cap)
	}
	if v.Base == 0 && len(v.Children) == 0 {
		fmt.Fprintf(buf, "nil")
		return
	}
	v.writeSliceOrArrayTo(buf, flags, indent)
}

func (v *Variable) writeArrayTo(buf io.Writer, flags ppFlags, indent string) {
	if flags.includeType() {
		fmt.Fprintf(buf, "%s ", getDisplayType(v, flags.fullTypes()))
	}
	v.writeSliceOrArrayTo(buf, flags, indent)
}

func (v *Variable) writeStructTo(buf io.Writer, flags ppFlags, indent string) {
	if int(v.Len) != len(v.Children) && len(v.Children) == 0 {
		fmt.Fprintf(buf, "(*%s)(0x%x)", v.Type, v.Addr)
		return
	}

	if flags.includeType() {
		fmt.Fprintf(buf, "%s ", getDisplayType(v, flags.fullTypes()))
	}

	nl := v.shouldNewlineStruct(flags.newlines())

	fmt.Fprint(buf, "{")

	for i := range v.Children {
		if nl {
			fmt.Fprintf(buf, "\n%s%s", indent, indentString)
		}
		fmt.Fprintf(buf, "%s: ", v.Children[i].Name)
		v.Children[i].writeTo(buf, flags.clearTop()|ppIncludeType, indent+indentString)
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

func (v *Variable) writeMapTo(buf io.Writer, flags ppFlags, indent string) {
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
		key := v.Children[i]
		value := v.Children[i+1]

		if nl {
			fmt.Fprintf(buf, "\n%s%s", indent, indentString)
		}

		if value == nil {
			fmt.Fprintf(buf, "%s: ", key.DisplayName[1:len(key.DisplayName)-1])
			key.writeTo(buf, flags.clearTop().clearIncludeType(), indent+indentString)
		} else {
			key.writeTo(buf, flags.clearTop().clearNewlines().clearIncludeType(), indent+indentString)
			fmt.Fprint(buf, ": ")
			value.writeTo(buf, flags.clearTop().clearIncludeType(), indent+indentString)
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

func (v *Variable) shouldNewlineArray(newlines bool) bool {
	if !newlines || len(v.Children) == 0 {
		return false
	}

	kind, hasptr := v.Children[0].recursiveKind()

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

func (v *Variable) recursiveKind() (reflect.Kind, bool) {
	hasptr := false
	var kind reflect.Kind
	for {
		kind = v.Kind
		if kind == reflect.Ptr {
			hasptr = true
			v = v.Children[0]
		} else {
			break
		}
	}
	return kind, hasptr
}

func (v *Variable) shouldNewlineStruct(newlines bool) bool {
	if !newlines || len(v.Children) == 0 {
		return false
	}

	for i := range v.Children {
		kind, hasptr := v.Children[i].recursiveKind()

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

func (v *Variable) writeSliceOrArrayTo(buf io.Writer, flags ppFlags, indent string) {
	nl := v.shouldNewlineArray(flags.newlines())
	fmt.Fprint(buf, "[")

	for i := range v.Children {
		if nl {
			fmt.Fprintf(buf, "\n%s%s", indent, indentString)
		}
		v.Children[i].writeTo(buf, (flags & ^ppTop) & ^ppIncludeType, indent+indentString)
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
