package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"sort"
	"strings"

	"github.com/derekparker/delve/service/api"
)

type stmtsInLocVisitor struct {
	r   []*ast.CallExpr
	pkg string

	fset   *token.FileSet
	loc    api.Location
	isType map[string]bool
}

type stmtsInLocVisitorSkip struct {
	w *stmtsInLocVisitor
}

type callKind uint8

const (
	typeCall callKind = iota
	builtinCall
	funCall
)

func kindOfCall(pkg string, fun ast.Node, isType map[string]bool) callKind {
	switch fun1 := fun.(type) {
	case (*ast.ParenExpr):
		return kindOfCall(pkg, fun1.X, isType)
	case (*ast.StarExpr):
		return kindOfCall(pkg, fun1.X, isType)

	case (*ast.ArrayType):
		return typeCall
	case (*ast.FuncType):
		return typeCall
	case (*ast.InterfaceType):
		return typeCall
	case (*ast.StructType):
		return typeCall
	case (*ast.MapType):
		return typeCall

	case (*ast.SelectorExpr):
		lident, ok := fun1.X.(*ast.Ident)
		if !ok {
			return funCall
		}

		if isType[fmt.Sprintf("%s.%s", lident.Name, fun1.Sel.Name)] {
			return typeCall
		}
		return funCall

	case (*ast.Ident):
		switch fun1.Name {
		case "append", "cap", "close", "complex", "copy", "delete", "imag", "len", "make", "new", "panic", "print", "println", "real", "recover":
			return builtinCall
		case "byte", "rune":
			return typeCall
		}

		if isType[pkg+"."+fun1.Name] || isType[fun1.Name] {
			return typeCall
		}
		return funCall

	default:
		return funCall
	}
}

func (w *stmtsInLocVisitor) Visit(n ast.Node) (wret ast.Visitor) {
	if n == nil {
		return w
	}

	switch sn := n.(type) {
	case (*ast.DeferStmt):
		return &stmtsInLocVisitorSkip{w}
	case (*ast.GoStmt):
		return &stmtsInLocVisitorSkip{w}
	case (*ast.File):
		w.pkg = sn.Name.Name
	case (*ast.CallExpr):
		start := w.fset.Position(n.End())
		ok := start.Filename == w.loc.File
		ok = ok && start.Line == w.loc.Line
		ok = ok && kindOfCall(w.pkg, sn.Fun, w.isType) == funCall

		if ok {
			w.r = append(w.r, sn)
		}
	default:
		//TODO: calculate the pos of the first character in w.loc.File:w.loc.Line and check that is between n.Pos() and n.End()
	}
	return w
}

func (w *stmtsInLocVisitorSkip) Visit(n ast.Node) (wret ast.Visitor) {
	return w.w
}

// Reorder a list of calls so that a function call appears after all its arguments are evaluated
func reorderCalls(calls []*ast.CallExpr) {
	for i := 0; i < len(calls); {
		end := calls[i].End()
		argstart := i + 1
		argend := argstart
		for ; argend < len(calls); argend++ {
			if calls[argend].Pos() >= end {
				break
			}
		}

		if argend > argstart {
			reorderCalls(calls[argstart:argend])
			curcall := calls[i]
			copy(calls[i:argend-1], calls[argstart:argend])
			calls[argend-1] = curcall
		}

		i = argend
	}
}

func stmtsInLoc(loc api.Location, n ast.Node, fset *token.FileSet, isType map[string]bool) []*ast.CallExpr {
	visitor := stmtsInLocVisitor{fset: fset, loc: loc, isType: isType}
	ast.Walk(&visitor, n)
	reorderCalls(visitor.r)
	return visitor.r
}

type stepIntoCall struct {
	Name string
	Inst api.AsmInstruction
	X    *ast.CallExpr
	fset *token.FileSet
}

func (sic *stepIntoCall) ColInterval() (int, int) {
	return sic.fset.Position(sic.X.Fun.Pos()).Column, sic.fset.Position(sic.X.Fun.End()).Column
}

func (sic *stepIntoCall) Filename() string {
	return sic.fset.Position(sic.X.Fun.Pos()).Filename
}

func (sic *stepIntoCall) Line() int {
	return sic.fset.Position(sic.X.Fun.Pos()).Line
}

func (sic *stepIntoCall) ExprString() string {
	var buf bytes.Buffer
	format.Node(&buf, sic.fset, sic.X)
	return buf.String()
}

func isExportedRuntime(name string) bool {
	const n = len("runtime.")
	return len(name) > n && name[:n] == "runtime." && 'A' <= name[n] && name[n] <= 'Z'
}

func isVisibleCall(inst api.AsmInstruction) bool {
	if !strings.HasPrefix(inst.Text, "call ") {
		return false
	}
	if inst.DestLoc != nil && inst.DestLoc.Function != nil {
		fn := inst.DestLoc.Function
		if strings.HasPrefix(fn.Name, "runtime.") && !isExportedRuntime(fn.Name) {
			return false
		}
	}
	return true
}

func isClosure(name string) bool {
	const strFunc = "func"
	name = removePath(name)
	v := strings.Split(name, ".")
	if len(v) == 0 {
		return false
	}
	s := v[len(v)-1]
	if !strings.HasPrefix(s, strFunc) {
		return false
	}
	for i := len(strFunc); i < len(s); i++ {
		ch := s[i]
		if ch < '0' || ch > '9' {
			return false
		}
	}
	return true
}

func removePath(name string) string {
	if d := strings.LastIndex(name, "/"); d >= 0 {
		name = name[d+1:]
	}
	return name
}

func selectorSequence(x ast.Expr) bool {
	for {
		switch x1 := x.(type) {
		case (*ast.SelectorExpr):
			x = x1.X
		case (*ast.Ident):
			return true
		default:
			return false
		}
	}
}

func stepIntoList(loc api.Location) []stepIntoCall {
	var fset token.FileSet
	n, err := parser.ParseFile(&fset, loc.File, nil, 0)
	if err != nil {
		return nil
	}

	text, err := client.DisassemblePC(api.EvalScope{curGid, curFrame}, loc.PC, api.IntelFlavour)
	if err != nil {
		return nil
	}

	isType := map[string]bool{}
	for _, n := range typesPanel.slice {
		isType[n] = true
	}
	isFunc := map[string]bool{}
	for _, n := range funcsPanel.slice {
		isFunc[n] = true
	}

	callInstrs := []api.AsmInstruction{}

	for _, inst := range text {
		if inst.Loc.File != loc.File || inst.Loc.Line != loc.Line {
			continue
		}
		if isVisibleCall(inst) {
			callInstrs = append(callInstrs, inst)
		}
	}

	if len(callInstrs) <= 1 {
		return nil
	}

	callExprs := stmtsInLoc(loc, n, &fset, isType)

	if len(callInstrs) != len(callExprs) {
		return nil
	}

	var v []stepIntoCall

	for i := range callInstrs {
		inst := callInstrs[i]
		x := callExprs[i]

		var fun string
		switch x1 := x.Fun.(type) {
		case (*ast.SelectorExpr):
			if id, ok := x1.X.(*ast.Ident); ok {
				fun = fmt.Sprintf("%s.%s", id.Name, x1.Sel.Name)
			}
		case (*ast.Ident):
			fun = x1.Name
		}

		if fun != "" && isFunc[fun] && (inst.DestLoc == nil || inst.DestLoc.Function == nil || (!isClosure(inst.DestLoc.Function.Name) && removePath(inst.DestLoc.Function.Name) != fun)) {
			return nil
		}

		sic := stepIntoCall{Inst: inst, X: x, fset: &fset}

		switch {
		case fun != "":
			sic.Name = fun
		case selectorSequence(x.Fun):
			var buf bytes.Buffer
			format.Node(&buf, &fset, x.Fun)
			sic.Name = buf.String()
		case inst.DestLoc != nil && inst.DestLoc.Function != nil:
			sic.Name = removePath(inst.DestLoc.Function.Name)
		default:
			sic.Name = fmt.Sprintf("call%d", i)
		}

		// check that name is unique
		for j := range v {
			if v[j].Name == sic.Name {
				sic.Name += fmt.Sprintf("#%d", i)
				break
			}
		}

		v = append(v, sic)
	}

	return v
}

type stepIntoInfo struct {
	Lineno, Colno int
	Filename      string
	Valid         bool
	Calls         []stepIntoCall
	Call          stepIntoCall
	Msg           string
}

type sortStepIntoCallsByColWidth []stepIntoCall

func (v sortStepIntoCallsByColWidth) Len() int          { return len(v) }
func (v sortStepIntoCallsByColWidth) Swap(i int, j int) { v[i], v[j] = v[j], v[i] }
func (v sortStepIntoCallsByColWidth) Less(i int, j int) bool {
	ia, ib := v[i].ColInterval()
	ja, jb := v[j].ColInterval()
	return (ib - ia) < (jb - ja)
}

func (sii *stepIntoInfo) Config(filename string, lineno, colno int) bool {
	if sii.Colno == colno && sii.Lineno == lineno && sii.Filename == filename {
		return sii.Valid
	}

	sii.Colno = colno
	sii.Lineno = lineno
	sii.Filename = filename
	sii.Valid = false

	state, err := client.GetState()
	if err != nil || state.CurrentThread == nil {
		return false
	}

	sics := stepIntoList(api.Location{File: filename, Line: lineno, PC: state.CurrentThread.PC})
	sort.Sort(sortStepIntoCallsByColWidth(sics))
	listingPanel.stepIntoInfo.Calls = sics

	for _, sic := range sii.Calls {
		a, b := sic.ColInterval()
		if colno >= a && colno < b && sic.Inst.Loc.PC >= state.CurrentThread.PC {
			sii.Valid = true
			sii.Call = sic
			expr := sic.ExprString()
			if len(expr) > 20 {
				expr = expr[:20] + "..."
			}
			sii.Msg = fmt.Sprintf("Step into %s", expr)
			return true
		}
	}
	return false
}
