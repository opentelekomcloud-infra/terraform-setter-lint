package core

import (
	"fmt"
	"go/ast"
	"strings"

	"golang.org/x/tools/go/packages"
)

func UnwrapString(lit *ast.BasicLit) (string, error) {
	return strings.Trim(lit.Value, `"'`), nil
}

type Scope struct {
	Package     *packages.Package
	Objects     map[string]*ast.Object
	FuncDecls   map[string]*ast.FuncDecl
	FuncTypes   map[string]*FuncType
	StructDecls map[string]*ast.GenDecl
	StructTypes map[string]*StructType
}

func MethodName(receiver, fnc string) string {
	if receiver == "" {
		return fnc
	}
	return fmt.Sprintf("%s.%s", receiver, fnc)
}
