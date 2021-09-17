package lint

import (
	"go/ast"
	"go/token"
	"log"

	"github.com/hashicorp/go-multierror"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/core"
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
	validr := validator{
		pkgs:     pkgs,
		fset:     fSet,
		pkgCache: map[string]*core.Scope{},
	}
	return validr.validatePackages()
}

// validator is a global validation storage
type validator struct {
	// pkgs - list of checked packages
	pkgs []*packages.Package
	// fset - shared FilSet
	fset *token.FileSet
	// pkgCache - a map for imported package caching
	pkgCache map[string]*core.Scope
}

func (v *validator) validatePackages() error {
	var mErr *multierror.Error
	for _, pkg := range v.pkgs {
		mErr = multierror.Append(mErr, v.validatePackage(pkg))
	}
	return mErr.ErrorOrNil()
}

func (v *validator) validatePackage(pkg *packages.Package) error {
	p := parser.NewParser(pkg, v.fset, v.pkgCache) // we need this state to use types and imports later
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
