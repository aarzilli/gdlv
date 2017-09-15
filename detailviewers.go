package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"math"
	"reflect"
	"strconv"
	"strings"
	"sync"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/rect"

	"github.com/derekparker/delve/service/api"
)

type formatterFn func(*Variable)

var varFormat = map[uintptr]formatterFn{}

type stringViewerMode int

const (
	viewString stringViewerMode = iota
	viewByteArray
	viewRuneArray
)

type stringViewer struct {
	v          *Variable
	mode       stringViewerMode
	numberMode numberMode
	ed         nucular.TextEditor
	mu         sync.Mutex
}

func newStringViewer(mw nucular.MasterWindow, v *Variable) {
	sv := &stringViewer{v: v}
	switch v.Type {
	case "string":
		sv.mode = viewString
	case "[]uint8":
		sv.mode = viewByteArray
	case "[]int32":
		sv.mode = viewRuneArray
	}
	sv.ed.Flags = nucular.EditReadOnly | nucular.EditMultiline | nucular.EditSelectable | nucular.EditClipboard
	sv.setupView()
	mw.PopupOpen("Viewing string: "+v.Name, popupFlags|nucular.WindowScalable, rect.Rect{100, 100, 550, 400}, true, sv.Update)
}

func (sv *stringViewer) Update(w *nucular.Window) {
	sv.mu.Lock()
	defer sv.mu.Unlock()
	w.Row(20).Dynamic(1)
	w.Label(sv.v.Name, "LC")

	w.Row(20).Static(100, 80, 80, 80)
	w.Label("View as:", "LC")
	newmode := sv.mode
	if w.OptionText("string", newmode == viewString) {
		newmode = viewString
	}
	if w.OptionText("[]byte", newmode == viewByteArray) {
		newmode = viewByteArray
	}
	if w.OptionText("[]rune", newmode == viewRuneArray) {
		newmode = viewRuneArray
	}
	if newmode != sv.mode {
		sv.mode = newmode
		sv.setupView()
	}

	switch sv.mode {
	case viewString:
		// nothing to choose
	case viewByteArray, viewRuneArray:
		numberMode := sv.numberMode
		w.Row(20).Static(120, 120, 120)
		if w.OptionText("Decimal", numberMode == decMode) {
			numberMode = decMode
		}
		if w.OptionText("Hexadecimal", numberMode == hexMode) {
			numberMode = hexMode
		}
		if w.OptionText("Octal", numberMode == octMode) {
			numberMode = octMode
		}
		if numberMode != sv.numberMode {
			sv.numberMode = numberMode
			sv.setupView()
		}
	}

	w.Row(0).Dynamic(1)
	sv.ed.Edit(w)

	w.Row(20).Static(0, 100, 100)
	l := int64(sv.len())
	w.Label(fmt.Sprintf("Loaded %d/%d", l, sv.v.Len), "LC")
	if sv.v.Len != l {
		if w.ButtonText("Load more") {
			sv.loadMore()
		}
	} else {
		w.Spacing(1)
	}
	if w.ButtonText("OK") {
		w.Close()
	}
}

func (sv *stringViewer) len() int {
	switch sv.v.Kind {
	case reflect.String:
		return len(sv.v.Value)
	case reflect.Array, reflect.Slice:
		return len(sv.v.Children)
	default:
		return 0
	}
}

func (sv *stringViewer) setupView() {
	var bytes []byte
	var runes []rune

	switch sv.v.Type {
	case "string":
		switch sv.mode {
		case viewString:
			sv.ed.Buffer = []rune(sv.v.Value)
		case viewByteArray:
			bytes = []byte(sv.v.Value)
		case viewRuneArray:
			runes = []rune(sv.v.Value)
		}
	case "[]uint8":
		bytes = make([]byte, len(sv.v.Children))
		for i := range sv.v.Children {
			n, _ := strconv.Atoi(sv.v.Children[i].Variable.Value)
			bytes[i] = byte(n)
		}
		switch sv.mode {
		case viewString:
			sv.ed.Buffer = []rune(string(bytes))
		case viewByteArray:
			// nothing to do
		case viewRuneArray:
			runes = []rune(string(bytes))
		}
	case "[]int32":
		runes = make([]rune, len(sv.v.Children))
		for i := range sv.v.Children {
			n, _ := strconv.Atoi(sv.v.Children[i].Variable.Value)
			runes[i] = rune(n)
		}
		switch sv.mode {
		case viewString:
			sv.ed.Buffer = runes
		case viewByteArray:
			bytes = []byte(string(runes))
		case viewRuneArray:
			// nothing to do
		}
	}

	switch sv.mode {
	case viewString:
		// nothing more to do
	case viewByteArray:
		array := make([]int64, len(bytes))
		for i := range bytes {
			array[i] = int64(bytes[i])
		}
		sv.ed.Buffer = []rune(formatArray(array, true, sv.numberMode, true, 1, 16))
	case viewRuneArray:
		array := make([]int64, len(runes))
		for i := range runes {
			array[i] = int64(runes[i])
		}
		sv.ed.Buffer = []rune(formatArray(array, sv.numberMode != decMode, sv.numberMode, false, 2, 10))
	}
}

func formatArray(array []int64, hexaddr bool, mode numberMode, canonical bool, size, stride int) string {
	var fmtstr, emptyfield string
	switch mode {
	case decMode:
		fmtstr = fmt.Sprintf("%%%dd ", size*3)
		emptyfield = fmt.Sprintf("%*s", size*3+1, "")
	case hexMode:
		fmtstr = fmt.Sprintf("%%0%dx ", size*2)
		emptyfield = fmt.Sprintf("%*s", size*2+1, "")
	case octMode:
		fmtstr = fmt.Sprintf("%%0%do ", size*3)
		emptyfield = fmt.Sprintf("%*s", size*3+1, "")
	}

	var addrfmtstr string
	if hexaddr {
		d := hexdigits(uint64(len(array)))
		if d < 2 {
			d = 2
		}
		addrfmtstr = fmt.Sprintf("%%0%dx  ", d)
	} else {
		addrfmtstr = fmt.Sprintf("[%%%dd]  ", digits(len(array)))
	}

	var buf bytes.Buffer
	i := 0
	for i < len(array) {
		fmt.Fprintf(&buf, addrfmtstr, i)
		start := i
		for c := 0; c < stride; i, c = i+1, c+1 {
			if stride%8 == 0 && c%8 == 0 && c != 0 && c != stride-1 {
				fmt.Fprintf(&buf, " ")
			}
			if i < len(array) {
				fmt.Fprintf(&buf, fmtstr, array[i])
			} else {
				fmt.Fprintf(&buf, emptyfield)
			}
		}

		if canonical {
			fmt.Fprintf(&buf, " |")
			for j := start; j < i; j++ {
				if j < len(array) {
					if array[j] >= 0x20 && array[j] <= 0x7e {
						fmt.Fprintf(&buf, "%c", byte(array[j]))
					} else {
						fmt.Fprintf(&buf, ".")
					}
				} else {
					fmt.Fprintf(&buf, " ")
				}
			}
			fmt.Fprintf(&buf, "|\n")
		} else {
			fmt.Fprintf(&buf, "\n")
		}
	}

	return buf.String()
}

func (sv *stringViewer) loadMore() {
	additionalLoadMu.Lock()
	defer additionalLoadMu.Unlock()
	if !additionalLoadRunning {
		additionalLoadRunning = true
		go func() {
			expr := fmt.Sprintf("(*(*%q)(%#x))[%d:]", sv.v.RealType, sv.v.Addr, sv.len())
			lv, err := client.EvalVariable(api.EvalScope{curGid, curFrame}, expr, LongArrayLoadConfig)
			if err != nil {
				out := editorWriter{&scrollbackEditor, true}
				fmt.Fprintf(&out, "Error loading string contents %s: %v\n", expr, err)
			} else {
				switch sv.v.Kind {
				case reflect.String:
					sv.v.Width = 0
					sv.v.Value += lv.Value
				case reflect.Array, reflect.Slice:
					sv.v.Children = append(sv.v.Children, wrapApiVariables(lv.Children, sv.v.Kind, len(sv.v.Children), sv.v.Expression)...)
				}
			}
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
			sv.mu.Lock()
			sv.setupView()
			sv.mu.Unlock()
			wnd.Changed()
		}()
	}
}

type intArrayViewer struct {
	v          *Variable
	displayLen int
	mode       numberMode
	ed         nucular.TextEditor
	mu         sync.Mutex
}

func newIntArrayViewer(mw nucular.MasterWindow, v *Variable) {
	av := &intArrayViewer{v: v}
	av.mode = decMode
	av.ed.Flags = nucular.EditReadOnly | nucular.EditMultiline | nucular.EditSelectable | nucular.EditClipboard
	av.setupView()
	mw.PopupOpen("Viewing array: "+v.Name, popupFlags|nucular.WindowScalable, rect.Rect{100, 100, 550, 400}, true, av.Update)
}

func (av *intArrayViewer) Update(w *nucular.Window) {
	av.mu.Lock()
	defer av.mu.Unlock()

	if av.displayLen != len(av.v.Children) {
		av.setupView()
	}

	w.Row(20).Static(100, 120, 120, 120)
	w.Label("View as:", "LC")
	mode := av.mode
	if w.OptionText("Decimal", mode == decMode) {
		mode = decMode
	}
	if w.OptionText("Hexadecimal", mode == hexMode) {
		mode = hexMode
	}
	if w.OptionText("Octal", mode == octMode) {
		mode = octMode
	}
	if mode != av.mode {
		av.mode = mode
		av.setupView()
	}

	w.Row(0).Dynamic(1)
	av.ed.Edit(w)

	w.Row(20).Static(0, 100, 100)
	w.Label(fmt.Sprintf("Loaded %d/%d", len(av.v.Children), av.v.Len), "LC")
	if av.v.Len != int64(len(av.v.Children)) {
		if w.ButtonText("Load more") {
			loadMoreArrayOrSlice(av.v)
		}
	} else {
		w.Spacing(1)
	}
	if w.ButtonText("OK") {
		w.Close()
	}
}

func (av *intArrayViewer) setupView() {
	array := make([]int64, len(av.v.Children))
	max := int64(0)
	for i := range av.v.Children {
		array[i], _ = strconv.ParseInt(av.v.Children[i].Variable.Value, 10, 64)
		x := array[i]
		if x < 0 {
			x = -x
		}
		if x > max {
			max = x
		}
	}

	if max < 1 {
		max = 1
	}

	size := int(math.Ceil((math.Log(float64(max)) / math.Log(2)) / 8))
	av.ed.Buffer = []rune(formatArray(array, av.mode != decMode, av.mode, false, size, 10))
}

type floatViewer struct {
	v  *Variable
	ed nucular.TextEditor
}

func newFloatViewer(w *nucular.Window, v *Variable) {
	vw := &floatViewer{v: v}
	vw.ed.Flags = nucular.EditSelectable | nucular.EditClipboard | nucular.EditSigEnter
	vw.ed.Buffer = []rune(v.FloatFmt)
	w.Master().PopupOpen(fmt.Sprintf("Format %s", v.Name), dynamicPopupFlags, rect.Rect{20, 100, 480, 500}, true, vw.Update)
}

func (vw *floatViewer) Update(w *nucular.Window) {
	w.Row(30).Static(100, 0)
	w.Label("Value:", "LC")
	w.Label(vw.v.Value, "LC")
	w.Label("Format:", "LC")
	if ev := vw.ed.Edit(w); ev&nucular.EditCommitted != 0 {
		w.Close()
	}
	if newfmt := string(vw.ed.Buffer); newfmt != vw.v.FloatFmt {
		vw.v.FloatFmt = newfmt
		f := floatFormatter(vw.v.FloatFmt)
		varFormat[vw.v.Addr] = f
		f(vw.v)
		vw.v.Width = 0
	}
	w.Row(30).Static(0, 100)
	w.Spacing(1)
	if w.ButtonText("Done") {
		w.Close()
	}
}

var intFormatter = map[numberMode]formatterFn{
	decMode: func(v *Variable) {
		v.IntMode = decMode
		v.Value = v.Variable.Value
	},
	hexMode: func(v *Variable) {
		v.IntMode = hexMode
		n, _ := strconv.ParseInt(v.Variable.Value, 10, 64)
		v.Value = fmt.Sprintf("%#x", n)
	},
	octMode: func(v *Variable) {
		v.IntMode = octMode
		n, _ := strconv.ParseInt(v.Variable.Value, 10, 64)
		v.Value = fmt.Sprintf("%#o", n)
	},
}

var uintFormatter = map[numberMode]formatterFn{
	decMode: func(v *Variable) {
		v.IntMode = decMode
		v.Value = v.Variable.Value
	},
	hexMode: func(v *Variable) {
		v.IntMode = hexMode
		n, _ := strconv.ParseUint(v.Variable.Value, 10, 64)
		v.Value = fmt.Sprintf("%#x", n)
	},
	octMode: func(v *Variable) {
		v.IntMode = octMode
		n, _ := strconv.ParseUint(v.Variable.Value, 10, 64)
		v.Value = fmt.Sprintf("%#o", n)
	},
}

func floatFormatter(format string) formatterFn {
	return func(v *Variable) {
		v.FloatFmt = format
		if format == "" {
			v.Value = v.Variable.Value
			return
		}
		f, _ := strconv.ParseFloat(v.Variable.Value, 64)
		v.Value = fmt.Sprintf(format, f)
	}
}

func formatLocation2(loc api.Location) string {
	name := "(nil)"
	if loc.Function != nil {
		name = loc.Function.Name
	}
	return fmt.Sprintf("%s\nat %s:%d", name, ShortenFilePath(loc.File), loc.Line)
}

type customFmtMaker struct {
	v      *Variable
	fmtEd  nucular.TextEditor
	argEd  nucular.TextEditor
	errstr string
}

func viewCustomFormatterMaker(w *nucular.Window, v *Variable, fmtstr string, argstr []string) {
	vw := &customFmtMaker{v: v}
	vw.fmtEd.Flags = nucular.EditSelectable | nucular.EditClipboard
	vw.argEd.Flags = nucular.EditSelectable | nucular.EditClipboard | nucular.EditMultiline
	vw.fmtEd.Buffer = []rune(fmtstr)
	vw.argEd.Buffer = []rune(strings.Join(argstr, "\n"))
	w.Master().PopupOpen(fmt.Sprintf("Format %s", v.Type), dynamicPopupFlags, rect.Rect{20, 100, 480, 500}, true, vw.Update)
}

func (vw *customFmtMaker) Update(w *nucular.Window) {
	w.Row(30).Static(0)
	w.Label(fmt.Sprintf("Format string for all variables x of type %s", vw.v.Type), "LC")

	w.Row(30).Static(100, 0)

	w.Label("Format String: ", "LC")
	vw.fmtEd.Edit(w)

	w.Row(30).Static(0)
	w.Label("Arguments (use x for the variable name):", "LC")

	w.RowScaled(nucular.FontHeight(w.Master().Style().Font) * 7).Dynamic(1)
	vw.argEd.Edit(w)

	if vw.errstr != "" {
		w.Row(30).Static(0)
		w.Label(vw.errstr, "LC")
	}

	w.Row(30).Static(0, 80, 80)
	w.Spacing(1)
	if w.ButtonText("Cancel") {
		w.Close()
	}

	if w.ButtonText("OK") {
		var err error
		conf.CustomFormatters[vw.v.Type], err = newCustomFormatter(string(vw.fmtEd.Buffer), string(vw.argEd.Buffer))
		if err == nil {
			saveConfiguration()
			go refreshState(refreshToSameFrame, clearFrameSwitch, nil)
			w.Close()
		} else {
			vw.errstr = fmt.Sprintf("Error: %s", err.Error())
		}
	}

}

type CustomFormatter struct {
	Fmtstr  string
	Argstr  []string
	argexpr []ast.Expr
}

func newCustomFormatter(fmtstr string, argstr string) (*CustomFormatter, error) {
	r := &CustomFormatter{Fmtstr: fmtstr}

	v := strings.Split(argstr, "\n")

	r.Argstr = make([]string, 0, len(v))

	for i := range v {
		v[i] = strings.TrimSpace(v[i])
		if v[i] == "" {
			continue
		}
		r.Argstr = append(r.Argstr, v[i])
	}

	err := r.parseArgs()
	if err != nil {
		return nil, err
	}

	return r, nil
}

func (c *CustomFormatter) parseArgs() error {
	c.argexpr = make([]ast.Expr, len(c.Argstr))
	for i := range c.Argstr {
		if err := c.parseArg(i); err != nil {
			return err
		}
	}
	return nil
}

type CustomFormatterWalker struct {
	replace string
	err     error
}

func (c *CustomFormatter) parseArg(i int) error {
	var err error
	c.argexpr[i], err = parser.ParseExpr(c.Argstr[i])
	if err != nil {
		return fmt.Errorf("argument %d: %s", i, err.Error())
	}

	var cfw CustomFormatterWalker
	ast.Walk(&cfw, c.argexpr[i])
	if cfw.err != nil {
		return fmt.Errorf("argument %d: %s", i, cfw.err.Error())
	}

	return nil
}

func (cfw *CustomFormatterWalker) Visit(n ast.Node) ast.Visitor {
	if cfw.err != nil {
		return nil
	}
	switch n := n.(type) {
	case *ast.SelectorExpr:
		ast.Walk(cfw, n.X)
		return nil
	case *ast.Ident:
		if cfw.replace != "" {
			n.Name = cfw.replace
		} else {
			if n.Name != "x" {
				cfw.err = fmt.Errorf("unexpected identifier %q (use x)", n.Name)
			}
		}
		return nil
	default:
		return cfw
	}
}

func (c *CustomFormatter) Format(v *Variable) {
	if c.argexpr == nil {
		c.parseArgs()
	}

	vars := make([]*api.Variable, len(c.argexpr))
	errors := make([]error, len(c.argexpr))

	var cfw CustomFormatterWalker
	cfw.replace = fmt.Sprintf("(*(*%q)(%#x))", v.Type, v.Addr)

	for i := range c.argexpr {
		if _, isident := c.argexpr[i].(*ast.Ident); isident {
			vars[i] = v.Variable
		} else {
			ast.Walk(&cfw, c.argexpr[i])

			var buf bytes.Buffer
			printer.Fprint(&buf, token.NewFileSet(), c.argexpr[i])
			expr := buf.String()

			vars[i], errors[i] = client.EvalVariable(api.EvalScope{curGid, curFrame}, expr, LongLoadConfig)
		}
	}

	args := make([]interface{}, 0, len(vars))

	for i, v := range vars {
		if v == nil {
			args = append(args, errors[i])
			continue
		}
		switch v.Kind {
		case reflect.Bool:
			args = append(args, v.Value == "true")
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			n, _ := strconv.ParseInt(v.Value, 0, 64)
			args = append(args, n)
		case reflect.Uintptr, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			n, _ := strconv.ParseUint(v.Value, 0, 64)
			args = append(args, n)
		case reflect.Float32, reflect.Float64:
			n, _ := strconv.ParseFloat(v.Value, 64)
			args = append(args, n)
		case reflect.String:
			args = append(args, v.Value)
		default:
			args = append(args, v.SinglelineString())
		}
	}

	v.Value = fmt.Sprintf(c.Fmtstr, args...)
}
