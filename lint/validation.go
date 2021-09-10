package lint

import (
	"go/ast"
	"go/token"
	"log"

	"github.com/hashicorp/go-multierror"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/parser"
	"golang.org/x/tools/go/packages"
)

// Validate searches for all resource and validate their setters
func Validate(path string) error {
	fSet := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedDeps |
			packages.NeedImports |
			packages.NeedSyntax,
		Fset: fSet,
		Dir:  path,
	}
	log.Println("Start validating packages at", path)
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return err
	}
	var mErr *multierror.Error
	for _, pkg := range pkgs {
		mErr = multierror.Append(mErr, validatePackage(pkg, fSet))
	}
	return mErr.ErrorOrNil()
}

func validatePackage(pkg *packages.Package, fSet *token.FileSet) error {
	p := parser.NewParser(pkg, fSet) // we need this state to use types and imports later
	fnNames := p.GeneratorNames()
	if fnNames == nil {
		return nil
	}
	mErr := &multierror.Error{}
	for _, name := range fnNames {
		fnObj := p.FindFnObject(name)
		ast.Inspect(fnObj.Decl.(*ast.FuncDecl), func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			if t, ok := lit.Type.(*ast.SelectorExpr); !ok || t.Sel.Name != "Resource" {
				return true
			}
			gen, err := p.ParseGenerator(lit, name)
			if err != nil {
				log.Println(err)
			}
			mErr = multierror.Append(mErr, gen.ValidateSetters())
			return false
		})
	}
	return mErr.ErrorOrNil()
}
