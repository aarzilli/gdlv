package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

func findGdlvDir() string {
	for _, dir := range []string{".", ".."} {
		_, err := os.Stat(filepath.Join(dir, "internal/dlvclient/service/api/types.go"))
		if err == nil {
			return dir
		}
	}
	fmt.Fprintf(os.Stderr, "wrong gdlv directory\n")
	os.Exit(1)
	return ""
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

var (
	delveDir string
	gdlvDir  string
)

func copyChanged(dstpath, srcpath string, edit func(*token.FileSet, *ast.File)) {
	var fset token.FileSet
	f, err := parser.ParseFile(&fset, filepath.Join(delveDir, srcpath), nil, parser.ParseComments)
	must(err)

	edit(&fset, f)

	fh, err := os.Create(filepath.Join(gdlvDir, dstpath))
	must(err)
	format.Node(fh, &fset, f)
	must(fh.Close())
}

func syncTypes() {
	copyChanged("internal/dlvclient/service/api/types.go", "service/api/types.go", func(_ *token.FileSet, f *ast.File) {
		oldDecls := f.Decls
		f.Decls = f.Decls[:0]

		for _, decl := range oldDecls {
			gendecl, isgendecl := decl.(*ast.GenDecl)

			if isgendecl && gendecl.Tok == token.IMPORT {
				oldSpecs := gendecl.Specs
				gendecl.Specs = gendecl.Specs[:0]
				for _, spec := range oldSpecs {
					spec := spec.(*ast.ImportSpec)
					s, _ := strconv.Unquote(spec.Path.Value)
					if !strings.HasPrefix(s, "github.com/go-delve") {
						gendecl.Specs = append(gendecl.Specs, spec)
					}
				}
			}

			if isgendecl && gendecl.Tok == token.CONST {
				if gendecl.Specs[0].(*ast.ValueSpec).Names[0].Name == "GNUFlavour" {
					gendecl.Specs[0].(*ast.ValueSpec).Values[0].(*ast.CallExpr).Args[0] = &ast.Ident{Name: "iota"}
					gendecl.Specs[1].(*ast.ValueSpec).Values = nil
					gendecl.Specs[2].(*ast.ValueSpec).Values = nil
				}
			}

			f.Decls = append(f.Decls, decl)
		}
	})
}

func syncServer() {
	copyChanged("internal/dlvclient/service/rpc2/server.go", "service/rpc2/server.go", func(_ *token.FileSet, f *ast.File) {
		oldDecls := f.Decls
		f.Decls = f.Decls[:0]
		for _, decl := range oldDecls {
			removeComments := func() {
				for i, cgrp := range f.Comments {
					if cgrp == nil {
						continue
					}
					if cgrp.End() == decl.Pos()-1 || (cgrp.Pos() >= decl.Pos() && cgrp.Pos() < decl.End()) {
						f.Comments[i] = nil
					}
				}
			}
			if _, isfunc := decl.(*ast.FuncDecl); isfunc {
				removeComments()
				continue
			}

			gendecl, isgendecl := decl.(*ast.GenDecl)

			if isgendecl && gendecl.Tok == token.IMPORT {
				oldSpecs := gendecl.Specs
				gendecl.Specs = gendecl.Specs[:0]
				for _, spec := range oldSpecs {
					spec := spec.(*ast.ImportSpec)
					switch s, _ := strconv.Unquote(spec.Path.Value); s {
					case "time":
						gendecl.Specs = append(gendecl.Specs, spec)
					case "github.com/go-delve/delve/service/api":
						spec.Path.Value = `"github.com/aarzilli/gdlv/internal/dlvclient/service/api"`
						gendecl.Specs = append(gendecl.Specs, spec)
					}
				}
			}

			if isgendecl && gendecl.Tok == token.TYPE && len(gendecl.Specs) == 1 {
				spec := gendecl.Specs[0].(*ast.TypeSpec)
				if spec.Name.Name == "RPCServer" {
					removeComments()
					continue
				}
			}

			f.Decls = append(f.Decls, decl)
		}

		oldComments := f.Comments
		f.Comments = f.Comments[:0]
		for _, cmt := range oldComments {
			if cmt != nil {
				f.Comments = append(f.Comments, cmt)
			}
		}

		f.Imports = nil
	})
}

func nodeString(fset *token.FileSet, n ast.Node) string {
	buf := new(bytes.Buffer)
	must(format.Node(buf, fset, n))
	return buf.String()
}

func syncClient() {
	copyChanged("internal/dlvclient/service/rpc2/client.go", "service/rpc2/client.go", func(fset *token.FileSet, f *ast.File) {
		oldDecls := f.Decls
		f.Decls = f.Decls[:0]
		for _, decl := range oldDecls {
			removeComments := func() {
				for i, cgrp := range f.Comments {
					if cgrp == nil {
						continue
					}
					if cgrp.End() == decl.Pos()-1 || (cgrp.Pos() >= decl.Pos() && cgrp.Pos() < decl.End()) {
						f.Comments[i] = nil
					}
				}
			}

			gendecl, isgendecl := decl.(*ast.GenDecl)
			funcdecl, isfuncdecl := decl.(*ast.FuncDecl)

			if isgendecl && gendecl.Tok == token.IMPORT {
				oldSpecs := gendecl.Specs
				gendecl.Specs = gendecl.Specs[:0]
				for _, spec := range oldSpecs {
					spec := spec.(*ast.ImportSpec)
					s, _ := strconv.Unquote(spec.Path.Value)
					if !strings.HasPrefix(s, "github.com/go-delve") && s != "log" && s != "net" && s != "net/rpc" && s != "net/rpc/jsonrpc" {
						gendecl.Specs = append(gendecl.Specs, spec)
					} else if s == "github.com/go-delve/delve/service/api" {
						spec.Path.Value = `"github.com/aarzilli/gdlv/internal/dlvclient/service/api"`
						gendecl.Specs = append(gendecl.Specs, spec)
					}
				}
			}

			if isgendecl && gendecl.Tok == token.TYPE && len(gendecl.Specs) == 1 {
				spec := gendecl.Specs[0].(*ast.TypeSpec)
				if spec.Name.Name == "RPCClient" {
					removeComments()
					continue
				}
			}

			s := nodeString(fset, decl)
			if strings.Contains(s, "var _ service.Client = &RPCClient{}") {
				removeComments()
				continue
			}

			if isfuncdecl {
				switch funcdecl.Name.Name {
				case "NewClient", "newFromRPCClient", "NewClientFromConn", "call", "Recorded":
					removeComments()
					continue
				case "Next", "ReverseNext", "Step", "ReverseStep", "StepOut", "ReverseStepOut", "StepInstruction", "ReverseStepInstruction", "Call":
					for _, stmt := range funcdecl.Body.List {
						ret, isret := stmt.(*ast.ReturnStmt)
						if !isret {
							continue
						}
						results := ret.Results
						results[0].(*ast.UnaryExpr).X = results[0].(*ast.UnaryExpr).X.(*ast.SelectorExpr).X
						ret.Results = []ast.Expr{
							&ast.CallExpr{
								Fun: &ast.SelectorExpr{
									X:   &ast.Ident{Name: "c"},
									Sel: &ast.Ident{Name: "exitedToError"},
								},
								Args: results,
							},
						}
					}
				}
			}

			f.Decls = append(f.Decls, decl)
		}

		oldComments := f.Comments
		f.Comments = f.Comments[:0]
		for _, cmt := range oldComments {
			if cmt != nil {
				f.Comments = append(f.Comments, cmt)
			}
		}
	})
}

func syncStarlark() {
	copyChanged("internal/starbind/starlark_mapping.go", "pkg/terminal/starbind/starlark_mapping.go", func(fset *token.FileSet, f *ast.File) {
		oldDecls := f.Decls
		f.Decls = f.Decls[:0]
		for _, decl := range oldDecls {
			gendecl, isgendecl := decl.(*ast.GenDecl)

			if isgendecl && gendecl.Tok == token.IMPORT {
				oldSpecs := gendecl.Specs
				gendecl.Specs = gendecl.Specs[:0]
				for _, spec := range oldSpecs {
					spec := spec.(*ast.ImportSpec)
					s, _ := strconv.Unquote(spec.Path.Value)
					if !strings.HasPrefix(s, "github.com/go-delve") {
						gendecl.Specs = append(gendecl.Specs, spec)
					} else if s == "github.com/go-delve/delve/service/api" {
						spec.Path.Value = `"github.com/aarzilli/gdlv/internal/dlvclient/service/api"`
						gendecl.Specs = append(gendecl.Specs, spec)
					} else if s == "github.com/go-delve/delve/service/rpc2" {
						spec.Path.Value = `"github.com/aarzilli/gdlv/internal/dlvclient/service/rpc2"`
						gendecl.Specs = append(gendecl.Specs, spec)
					}
				}
			}

			f.Decls = append(f.Decls, decl)
		}
	})
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintf(os.Stderr, "usage: go run _scripts/syncapi.go <delve directory>\n")
		os.Exit(1)
	}
	delveDir = os.Args[1]
	if _, err := os.Stat(filepath.Join(delveDir, "service/api/types.go")); err != nil {
		fmt.Fprintf(os.Stderr, "wrong delve directory\n")
		os.Exit(1)
	}
	gdlvDir = findGdlvDir()
	fmt.Printf("Sync %s -> %s\n", delveDir, gdlvDir)

	syncTypes()
	syncServer()
	syncClient()
	syncStarlark()
}
