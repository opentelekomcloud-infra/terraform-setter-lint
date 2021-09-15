package core

import (
	"fmt"
	"go/ast"
	"go/token"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/packages"
)

func UnwrapString(lit *ast.BasicLit) (string, error) {
	if lit.Kind != token.STRING {
		return "", fmt.Errorf("value is not a string")
	}
	return strings.Trim(lit.Value, `"'`), nil
}

type Scope struct {
	Package     *packages.Package
	Objects     map[string]*ast.Object
	FuncDecls   map[string]*ast.FuncDecl
	FuncTypes   map[string]*FuncType
	StructDecls map[string]*ast.GenDecl
}

func MethodName(receiver, fnc string) string {
	if receiver == "" {
		return fnc
	}
	if filepath.Ext(fnc) != "" {
		return fnc // already bind to package
	}
	return fmt.Sprintf("%s.%s", receiver, fnc)
}

func ResolveImportedDeclaration(expr *ast.SelectorExpr, pkg *packages.Package) (*ast.Object, *packages.Package, error) {
	imps := pkg.Imports
	pkgName := expr.X.(*ast.Ident).Name
	fnName := expr.Sel.Name

	for k, imp := range imps {
		name := imp.Name
		if name == "" {
			name = filepath.Base(k) // hope this always works
		}
		if name != pkgName {
			continue
		}
		for _, fl := range imp.Syntax {
			dcl := fl.Scope.Lookup(fnName)
			if dcl == nil {
				continue
			}
			return dcl, imp, nil
		}
	}
	return nil, nil, fmt.Errorf("failed to resolve imported declaration %s.%s", pkgName, fnName)
}

type FunctionReference struct {
	Package *packages.Package
	Decl    *ast.FuncDecl
}

func ResolveImportedFunction(expr *ast.SelectorExpr, pkg *packages.Package) (*FunctionReference, error) {
	fnDcl, srcPkg, err := ResolveImportedDeclaration(expr, pkg)
	if err != nil {
		return nil, err
	}
	dcl, ok := fnDcl.Decl.(*ast.FuncDecl)
	if !ok {
		return nil, fmt.Errorf("invalid function declaration for %s", fnDcl.Name)
	}
	return &FunctionReference{
		Package: srcPkg,
		Decl:    dcl,
	}, nil
}
