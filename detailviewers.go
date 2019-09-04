package main

import (
	"bytes"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"sync"

	"go.starlark.net/starlark"

	"github.com/aarzilli/nucular"
	"github.com/aarzilli/nucular/rect"

	"github.com/aarzilli/gdlv/internal/dlvclient/service/api"
)

type formatterFn func(*Variable)

var varFormat = map[uintptr]formatterFn{}

type detailViewer struct {
	asyncLoad asyncLoad

	len int

	exprEd nucular.TextEditor

	loaded  string
	loadErr error

	v *Variable

	stringMode stringViewerMode
	numberMode numberMode
	ed         nucular.TextEditor

	mu sync.Mutex
}

type stringViewerMode int

const (
	viewString stringViewerMode = iota
	viewByteArray
	viewRuneArray
)

func newDetailViewer(mw nucular.MasterWindow, expr string) {
	r := &detailViewer{}

	r.asyncLoad.load = r.load
	r.ed.Flags = nucular.EditReadOnly | nucular.EditMultiline | nucular.EditSelectable | nucular.EditClipboard

	r.exprEd.Flags = nucular.EditSelectable | nucular.EditClipboard | nucular.EditSigEnter
	r.exprEd.Buffer = []rune(expr)
	r.len = 64

	mw.PopupOpen("Details", popupFlags|nucular.WindowNonmodal|nucular.WindowScalable|nucular.WindowClosable, rect.Rect{100, 100, 550, 400}, true, r.Update)
}

func (dv *detailViewer) load(p *asyncLoad) {
	expr := string(dv.exprEd.Buffer)
	dv.v = nil
	dv.loadErr = nil
	v, err := client.EvalVariable(currentEvalScope(), expr, api.LoadConfig{false, 0, dv.len, dv.len, -1})
	if err != nil {
		dv.loadErr = err
		if p != nil {
			p.done(nil)
		}
		return
	}

	dv.v = wrapApiVariable(v, v.Name, v.Name, true, 0)

	switch dv.v.Type {
	case "string":
		dv.stringMode = viewString
	case "[]uint8":
		dv.stringMode = viewByteArray
	case "[]int32":
		dv.stringMode = viewRuneArray
	}

	dv.loaded = fmt.Sprintf("%s (loaded: %d/%d)", expr, dv.length(), dv.v.Len)
	dv.setupView()

	if p != nil {
		p.done(nil)
	}
}

func (dv *detailViewer) setupView() {
	if dv.loadErr != nil {
		return
	}
	switch dv.v.Type {
	case "string":
		switch dv.stringMode {
		case viewString:
			dv.ed.Buffer = []rune(dv.v.Value)
		case viewByteArray:
			dv.viewStringAsByteArray([]byte(dv.v.Value))
		case viewRuneArray:
			dv.viewStringAsRuneArray([]rune(dv.v.Value))
		}
		return

	case "[]uint8":
		bytes := make([]byte, len(dv.v.Children))
		for i := range dv.v.Children {
			n, _ := strconv.Atoi(dv.v.Children[i].Variable.Value)
			bytes[i] = byte(n)
		}
		switch dv.stringMode {
		case viewString:
			dv.ed.Buffer = []rune(string(bytes))
		case viewByteArray:
			dv.viewStringAsByteArray(bytes)
		case viewRuneArray:
			dv.viewStringAsRuneArray([]rune(string(bytes)))
		}
		return

	case "[]int32":
		runes := make([]rune, len(dv.v.Children))
		for i := range dv.v.Children {
			n, _ := strconv.Atoi(dv.v.Children[i].Variable.Value)
			runes[i] = rune(n)
		}
		switch dv.stringMode {
		case viewString:
			dv.ed.Buffer = runes
		case viewByteArray:
			dv.viewStringAsByteArray([]byte(string(runes)))
		case viewRuneArray:
			dv.viewStringAsRuneArray(runes)
		}
		return

	case "[]int", "[]int8", "[]int16", "[]int64", "[]uint", "[]uint16", "[]uint32", "[]uint64":
		array := make([]int64, len(dv.v.Children))
		max := int64(0)
		for i := range dv.v.Children {
			array[i], _ = strconv.ParseInt(dv.v.Children[i].Variable.Value, 10, 64)
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
		dv.ed.Buffer = []rune(formatArray(array, dv.numberMode != decMode, dv.numberMode, false, size, 10))

	default:
		dv.ed.Buffer = []rune(fmt.Sprintf("unsupported type %s", dv.v.Type))
	}
}

func (dv *detailViewer) viewStringAsByteArray(bytes []byte) {
	array := make([]int64, len(bytes))
	for i := range bytes {
		array[i] = int64(bytes[i])
	}
	dv.ed.Buffer = []rune(formatArray(array, true, dv.numberMode, true, 1, 16))
}

func (dv *detailViewer) viewStringAsRuneArray(runes []rune) {
	array := make([]int64, len(runes))
	for i := range runes {
		array[i] = int64(runes[i])
	}
	dv.ed.Buffer = []rune(formatArray(array, dv.numberMode != decMode, dv.numberMode, false, 2, 10))
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

func (dv *detailViewer) Update(container *nucular.Window) {
	w := dv.asyncLoad.showRequest(container)
	if w == nil {
		return
	}

	w.Row(30).Static(100, 0, 80, 150)
	w.Label("Expression: ", "LC")
	active := dv.exprEd.Edit(w)
	if active&nucular.EditCommitted != 0 {
		dv.load(nil)
	}
	if w.ButtonText("Set") {
		dv.load(nil)
	}
	if dv.v != nil {
		if w.PropertyInt("Length:", 1, &dv.len, int(dv.v.Len), 16, 16) {
			dv.load(nil)
		}
	} else {
		w.Spacing(1)
	}

	if dv.loadErr != nil {
		w.Row(30).Dynamic(1)
		w.Label(dv.loadErr.Error(), "LC")
		return
	}
	if dv.v.Unreadable != "" {
		w.Row(30).Dynamic(1)
		w.Label(fmt.Sprintf("Unreadable %s", dv.v.Unreadable), "LC")
		return
	}

	w.Row(30).Static(100, 0)
	w.Label("Showing: ", "LC")
	w.Label(dv.loaded, "LC")

	switch dv.v.Type {
	case "string", "[]uint8", "[]int32":
		dv.stringUpdate(w)
	case "[]int", "[]int8", "[]int16", "[]int64", "[]uint", "[]uint16", "[]uint32", "[]uint64":
		dv.intArrayUpdate(w)
	default:
		w.Row(30).Dynamic(1)
		w.Label(fmt.Sprintf("Unsupported type %s", dv.v.Type), "LC")
	}
}

func (dv *detailViewer) stringUpdate(w *nucular.Window) {
	dv.mu.Lock()
	defer dv.mu.Unlock()

	w.Row(20).Static(100, 100, 20, 100)
	w.Label("View as:", "LC")
	newmode := stringViewerMode(w.ComboSimple([]string{"string", "[]byte", "[]rune"}, int(dv.stringMode), 20))
	if newmode != dv.stringMode {
		dv.stringMode = newmode
		dv.setupView()
	}

	w.Spacing(1)

	switch dv.stringMode {
	case viewString:
		// nothing to choose
		w.Spacing(1)
	case viewByteArray, viewRuneArray:
		numberMode := numberMode(w.ComboSimple([]string{"Decimal", "Hexadecimal", "Octal"}, int(dv.numberMode), 20))
		if numberMode != dv.numberMode {
			dv.numberMode = numberMode
			dv.setupView()
		}
	}

	w.Row(0).Dynamic(1)
	dv.ed.Edit(w)
}

func (dv *detailViewer) length() int {
	switch dv.v.Kind {
	case reflect.String:
		return len(dv.v.Value)
	case reflect.Array, reflect.Slice:
		return len(dv.v.Children)
	default:
		return 0
	}
}

func (dv *detailViewer) loadMore() {
	additionalLoadMu.Lock()
	defer additionalLoadMu.Unlock()
	if !additionalLoadRunning {
		additionalLoadRunning = true
		go func() {
			expr := fmt.Sprintf("(*(*%q)(%#x))[%d:]", dv.v.RealType, dv.v.Addr, dv.length())
			lv, err := client.EvalVariable(currentEvalScope(), expr, LongArrayLoadConfig)
			if err != nil {
				out := editorWriter{&scrollbackEditor, true}
				fmt.Fprintf(&out, "Error loading string contents %s: %v\n", expr, err)
			} else {
				switch dv.v.Kind {
				case reflect.String:
					dv.v.Width = 0
					dv.v.Value += lv.Value
				case reflect.Array, reflect.Slice:
					dv.v.Children = append(dv.v.Children, wrapApiVariables(lv.Children, dv.v.Kind, len(dv.v.Children), dv.v.Expression, true, 0)...)
				}
			}
			additionalLoadMu.Lock()
			additionalLoadRunning = false
			additionalLoadMu.Unlock()
			dv.mu.Lock()
			dv.setupView()
			dv.mu.Unlock()
			wnd.Changed()
		}()
	}
}

func (dv *detailViewer) intArrayUpdate(w *nucular.Window) {
	if dv.len != len(dv.v.Children) {
		dv.setupView()
	}

	w.Row(20).Static(100, 120, 120, 120)
	w.Label("View as:", "LC")
	mode := dv.numberMode
	if w.OptionText("Decimal", mode == decMode) {
		mode = decMode
	}
	if w.OptionText("Hexadecimal", mode == hexMode) {
		mode = hexMode
	}
	if w.OptionText("Octal", mode == octMode) {
		mode = octMode
	}
	if mode != dv.numberMode {
		dv.numberMode = mode
		dv.setupView()
	}

	w.Row(0).Dynamic(1)
	dv.ed.Edit(w)
}

type floatViewer struct {
	v  *Variable
	ed nucular.TextEditor
}

func newFloatViewer(w *nucular.Window, v *Variable) {
	vw := &floatViewer{v: v}
	vw.ed.Flags = nucular.EditSelectable | nucular.EditClipboard | nucular.EditSigEnter
	vw.ed.Buffer = []rune(v.FloatFmt)
	w.Master().PopupOpen(fmt.Sprintf("Format %s", v.Name), dynamicPopupFlags|nucular.WindowClosable, rect.Rect{20, 100, 480, 500}, true, vw.Update)
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
	return fmt.Sprintf("%s\nat %s:%d", loc.Function.Name(), ShortenFilePath(loc.File), loc.Line)
}

type customFmtMaker struct {
	v  *Variable
	ed nucular.TextEditor
}

func viewCustomFormatterMaker(w *nucular.Window, v *Variable, fmtstr string, argstr []string) {
	vw := &customFmtMaker{v: v}
	vw.ed.Flags = nucular.EditSelectable | nucular.EditClipboard | nucular.EditMultiline
	vw.ed.Buffer = []rune(fmtstr)
	w.Master().PopupOpen(fmt.Sprintf("Format %s", v.Type), popupFlags|nucular.WindowScalable, rect.Rect{20, 100, 480, 500}, true, vw.Update)
}

func (vw *customFmtMaker) Update(w *nucular.Window) {
	w.Row(30).Static(0)
	w.Label(fmt.Sprintf("Format string for all variables x of type %s", vw.v.Type), "LC")

	w.Row(30).Dynamic(1)
	w.Label("Starlark script (current variable is bound to 'x'):", "LC")

	w.LayoutReserveRow(30, 1)
	w.RowScaled(0).Dynamic(1)
	vw.ed.Edit(w)

	w.Row(30).Static(0, 80, 80)
	w.Spacing(1)
	if w.ButtonText("Cancel") {
		w.Close()
	}

	if w.ButtonText("OK") {
		conf.CustomFormatters[vw.v.Type] = newCustomFormatter(string(vw.ed.Buffer))
		saveConfiguration()
		go refreshState(refreshToSameFrame, clearFrameSwitch, nil)
		w.Close()
	}
}

type CustomFormatter struct {
	Fmtstr     string
	Argstr     []string
	IsStarlark bool
}

func newCustomFormatter(fmtstr string) *CustomFormatter {
	return &CustomFormatter{Fmtstr: fmtstr, IsStarlark: true}
}

func (c *CustomFormatter) Format(v *Variable) {
	sv, err := StarlarkEnv.Execute(&editorWriter{&scrollbackEditor, true}, "<expr>", c.Fmtstr, "<expr>", nil, v.Variable)
	if err != nil {
		v.Value = fmt.Sprintf("custom formatter error: %v", err)
		return
	}
	switch sv := sv.(type) {
	case starlark.String:
		v.Value = string(sv)
	default:
		v.Value = sv.String()
	}
}
