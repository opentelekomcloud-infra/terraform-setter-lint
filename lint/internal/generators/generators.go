package generators

import (
	"fmt"
	"go/ast"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/hashicorp/go-multierror"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/core"
	"golang.org/x/tools/go/packages"
)

type Field struct {
	Type string
}

type StructFields map[string]core.Type

// Generator is representation of a single generator function
type Generator struct {
	FSet         *token.FileSet
	Pkg          *packages.Package
	Name         string
	Schema       map[string]Field
	OperatingFns []*ast.FuncDecl

	scopeCache map[string]*core.Scope // scopes of any imported library, populated lazily
}

func NewGenerator(name string, fset *token.FileSet, pkg *packages.Package, sharedScopes map[string]*core.Scope) (*Generator, error) {
	gen := &Generator{
		FSet:       fset,
		Pkg:        pkg,
		Name:       name,
		scopeCache: sharedScopes,
	}
	pkgScope, ok := sharedScopes[pkg.ID] // should be populated in parser
	if !ok {
		var err error
		pkgScope, err = gen.packageScope(pkg)
		if err != nil {
			return nil, err
		}
		sharedScopes[pkg.ID] = pkgScope
	}
	return gen, nil
}

func (g Generator) functionScope(baseScope map[string]*ast.Object, pkg *packages.Package) map[string]*core.FuncType {
	res := make(map[string]*core.FuncType)
	for _, obj := range baseScope {
		if fnDec, ok := obj.Decl.(*ast.FuncDecl); ok {
			types, err := g.getFunctionTypes(fnDec, pkg)
			if err != nil {
				log.Printf("error creating Generator: %s", err)
			}

			recType, err := g.getFuncReceiverName(fnDec)
			if err != nil {
				continue
			}
			key := core.MethodName(recType, fnDec.Name.Name)
			res[key] = types
		}
	}
	return res
}

func getDName(fn *ast.FuncDecl) string {
	params := fn.Type.Params
	var dataFld *ast.Field
	switch params.NumFields() {
	case 3:
		dataFld = params.List[1] // for new methods
	case 2:
		dataFld = params.List[0] // for methods w/o context
	default:
		log.Printf("function %s is broken", fn.Name.Name)
		return "" // it's strange, but just ignore it
	}
	if len(dataFld.Names) == 0 {
		log.Printf("function has anonymous schema.ResourceData argument")
		return "" // nothing to do in such a function
	}
	return dataFld.Names[0].Name
}

func isDSetSelector(expr ast.Expr, dName string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	idnt, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	if idnt.Name != dName || sel.Sel.Name != "Set" {
		return false
	}
	return true
}

// simplifyPath - simplify absolute path if possible
func simplifyPath(src string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return src
	}
	res, err := filepath.Rel(cwd, src)
	if err != nil {
		return src
	}
	if len(src) < len(res) {
		return src
	}
	return res
}

func (g Generator) getKey(key string) (*Field, error) {
	fld, ok := g.Schema[key]
	if !ok {
		return nil, fmt.Errorf("field missing in the schema defined in `%s`", g.Name)
	}
	return &fld, nil
}

func (g Generator) ValidateSetters() error {
	mErr := &multierror.Error{}
	for _, fn := range g.OperatingFns {
		// first - get *schema.ResourceData argument name
		dName := getDName(fn)
		// seconds - go through function body finding `d.Set` calls
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			if call, ok := node.(*ast.CallExpr); ok {
				if !isDSetSelector(call.Fun, dName) {
					return true // go on
				}
				// d.Set always has two arguments
				if len(call.Args) != 2 {
					log.Print("d.Set call has invalid argument number")
					return false
				}
				keyExpr, ok := call.Args[0].(*ast.BasicLit)
				if !ok {
					// expected to find a string literal as a key, ignoring this setter
					return false
				}
				key := strings.Trim(keyExpr.Value, `"`)

				// position for error messages
				pos := g.FSet.Position(call.Pos())
				pos.Column = 0 // no need for such details
				pos.Filename = simplifyPath(pos.Filename)

				fld, err := g.getKey(key)
				if err != nil {
					mErr = multierror.Append(mErr, fmt.Errorf(
						"%s - broken setter for field `%s`: %w", pos.String(), key, err,
					))
					return false
				}
				typ, err := g.getExpType(call.Args[1], g.Pkg)
				if err != nil {
					mErr = multierror.Append(mErr, fmt.Errorf(
						"%s - error getting `%s` value type: %w", pos.String(), key, err,
					))
					return false
				}
				expected := typeMapping[fld.Type]
				if typ == nil {
					return false
				}
				if !typ.Matches(expected) {
					mErr = multierror.Append(mErr, fmt.Errorf(
						"%s - field `%s` has invalid type `%s`, expected `%s`",
						pos.String(), key, typ.String(), expected,
					))
				}
				return false
			}
			return true
		})
	}
	return mErr.ErrorOrNil()
}
