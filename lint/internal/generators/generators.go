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
	Schema       map[string]*Field
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
	_, ok := sharedScopes[pkg.ID] // should be populated in parser
	if !ok {
		var err error
		pkgScope, err := gen.packageScope(pkg)
		if err != nil {
			return nil, err
		}
		sharedScopes[pkg.ID] = pkgScope
	}
	return gen, nil
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
	return fld, nil
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
				mErr = multierror.Append(mErr, g.validateSetter(call))
				return false
			}
			return true
		})
	}
	return mErr.ErrorOrNil()
}

func (g Generator) validateSetter(call *ast.CallExpr) error {
	keyExpr, ok := call.Args[0].(*ast.BasicLit)
	if !ok {
		// expected to find a string literal as a key, ignoring this setter
		return nil
	}
	key := strings.Trim(keyExpr.Value, `"`)

	// position for error messages
	pos := g.FSet.Position(call.Pos())
	pos.Column = 0 // no need for such details
	pos.Filename = simplifyPath(pos.Filename)

	fld, err := g.getKey(key)
	if err != nil {
		return fmt.Errorf(
			"%s - broken setter for field `%s`: %w", pos.String(), key, err,
		)
	}
	typ, err := g.getExpType(call.Args[1], g.Pkg)
	if err != nil {
		return fmt.Errorf(
			"%s - error getting `%s` value type: %w", pos.String(), key, err,
		)
	}
	if typ == nil {
		return fmt.Errorf("%s - can't determine expression type for field `%s`", pos.String(), key)
	}
	expected := typeMapping[fld.Type]
	if !g.extendedMatch(typ, expected) {
		return fmt.Errorf(
			"%s - field `%s` has invalid type `%s`, expected `%s`",
			pos.String(), key, typ.String(), expected,
		)
	}
	return nil
}

func (g Generator) extendedMatch(typ core.Type, expected string) bool {
	base := typ.Matches(expected)
	if base {
		return true
	}

	pkgID := typ.Package()
	if pkgID == "" {
		return base
	}
	scope := g.scopeCache[pkgID]
	internalType, err := g.resolveLocalType(typ.Name(), scope.Package)
	if err != nil {
		log.Printf("error resolving local type")
		return base
	}
	return internalType.Matches(expected)
}
