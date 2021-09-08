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
)

type Field struct {
	ResourceName string
	Type         string
	ReadOnly     bool
}

type Generator struct {
	FSet         *token.FileSet
	Name         string
	Schema       map[string]Field
	OperatingFns []*ast.FuncDecl
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

type ValidationError struct {
	pos    token.Position
	field  string
	reason error
}

func simplifyPath(src string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return src
	}
	res, err := filepath.Rel(cwd, src)
	if err != nil {
		return src
	}
	return res
}

func (v ValidationError) Error() string {
	position := v.pos
	position.Filename = simplifyPath(position.Filename)
	return fmt.Sprintf(
		"%s - broken setter for field `%s`: %s",
		position.String(), v.field, v.reason,
	)
}

func (g Generator) validateKey(key string) error {
	_, ok := g.Schema[key]
	if !ok {
		return fmt.Errorf("field missing in the schema defined in `%s`", g.Name)
	}
	return nil
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
				err := g.validateKey(key)
				if err != nil {
					mErr = multierror.Append(mErr, ValidationError{
						pos:    g.FSet.Position(call.Pos()),
						field:  key,
						reason: err,
					})
				}
				return false
			}
			return true
		})
	}
	return mErr.ErrorOrNil()
}
