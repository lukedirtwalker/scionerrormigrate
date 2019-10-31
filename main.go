// Copyright 2019 Lukas Vogel <lukedirtwalker@gmail.com>
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	"os"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

var (
	dirFlag = flag.String("dir", "", "Directory to use as root for inspecting packages.")
)

func main() {
	flag.Parse()

	// Many tools pass their command-line arguments (after any flags)
	// uninterpreted to packages.Load so that it can interpret them
	// according to the conventions of the underlying build system.
	cfg := &packages.Config{
		Mode: packages.NeedFiles | packages.NeedCompiledGoFiles | packages.NeedImports | packages.NeedTypes | packages.NeedTypesSizes | packages.NeedSyntax | packages.NeedTypesInfo | packages.NeedDeps,
		Dir:  *dirFlag,
	}
	pkgs, err := packages.Load(cfg, flag.Args()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load: %v\n", err)
		os.Exit(1)
	}
	if packages.PrintErrors(pkgs) > 0 {
		os.Exit(1)
	}

	// Print the names of the source files
	// for each package listed on the command line.
	for _, pkg := range pkgs {
		for _, file := range pkg.Syntax {
			fileModified := false
			addImport := false
			astutil.Apply(file, nil, func(cursor *astutil.Cursor) bool {
				cont, modified, importReq := handleNewError(cursor)
				fileModified = fileModified || modified
				addImport = addImport || importReq
				return cont
			})
			if fileModified {
				if addImport {
					astutil.AddImport(pkg.Fset, file, "github.com/scionproto/scion/go/lib/serrors")
				}
				if !astutil.UsesImport(file, "github.com/scionproto/scion/go/lib/common") {
					astutil.DeleteImport(pkg.Fset, file, "github.com/scionproto/scion/go/lib/common")
				}
			}
			tFile := pkg.Fset.File(file.Pos())
			fHandle, err := os.OpenFile(tFile.Name(), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0755)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Failed to open file for writing: %s", tFile.Name())
				os.Exit(1)
			}
			if err := format.Node(fHandle, pkg.Fset, file); err != nil {
				fmt.Println("Failed to write file")
			}
			fHandle.Close()
		}
	}
}

func handleNewError(cursor *astutil.Cursor) (bool, bool, bool) {
	n := cursor.Node()
	ce, ok := n.(*ast.CallExpr)
	if !ok {
		return true, false, false
	}
	se, ok := ce.Fun.(*ast.SelectorExpr)
	if !ok {
		return true, false, false
	}
	pkg, ok := se.X.(*ast.Ident)
	if !ok || pkg.Name != "common" {
		return true, false, false
	}
	if se.Sel.Name != "NewBasicError" {
		return true, false, false
	}
	errIdent, ok := ce.Args[1].(*ast.Ident)
	if !ok {
		return true, false, false
	}
	_, stringMsg := ce.Args[0].(*ast.BasicLit)
	if errIdent.Name == "nil" {
		newArgs := []ast.Expr{ce.Args[0]}
		if len(ce.Args) > 2 {
			newArgs = append(newArgs, ce.Args[2:]...)
		}
		replaceIdent := "New"
		if !stringMsg {
			if _, ok := ce.Args[0].(*ast.CompositeLit); ok {
				fmt.Printf("composite lit:\n")
			}
			replaceIdent = "WithCtx"
			if len(ce.Args) <= 2 {
				cursor.Replace(ce.Args[0])
				return true, true, false
			}
		}
		cursor.Replace(&ast.CallExpr{
			Fun: &ast.SelectorExpr{
				X: &ast.Ident{
					Name: "serrors",
				},
				Sel: &ast.Ident{
					Name: replaceIdent,
				},
			},
			Args: newArgs,
		})
		return true, true, true
	}
	// This is always serrors.WrapStr or Wrap depending on the first arg
	replaceIdent := "WrapStr"
	if !stringMsg {
		replaceIdent = "Wrap"
	}
	// TODO check if first Arg is refering to a const string
	cursor.Replace(&ast.CallExpr{
		Fun: &ast.SelectorExpr{
			X: &ast.Ident{
				Name: "serrors",
			},
			Sel: &ast.Ident{
				Name: replaceIdent,
			},
		},
		Args: ce.Args,
	})
	return true, true, true
}
