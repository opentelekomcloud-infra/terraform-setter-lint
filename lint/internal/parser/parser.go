package parser

import (
	"go/ast"
	"go/token"
	"log"
	"path/filepath"

	"github.com/hashicorp/go-multierror"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/core"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/generators"
	"golang.org/x/tools/go/packages"
)

// PackageParser single package
type PackageParser struct {
	fSet       *token.FileSet
	pkg        *packages.Package
	scopeCache map[string]*core.Scope
}

const schemaImportPath = "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

func NewParser(pkg *packages.Package, set *token.FileSet, scopeCache map[string]*core.Scope) *PackageParser {
	p := &PackageParser{
		pkg:        pkg,
		fSet:       set,
		scopeCache: scopeCache,
	}
	return p
}

func (p PackageParser) ParseGenerator(lit *ast.CompositeLit, genName string) (*generators.Generator, error) {
	gen, err := generators.NewGenerator(genName, p.fSet, p.pkg, p.scopeCache)
	if err != nil {
		return nil, err
	}
	err = gen.LoadSchema(lit)
	if err != nil {
		return nil, err
	}
	return gen, nil
}

func getSchemaImportName(file *ast.File) string {
	for _, imp := range file.Imports {
		val, _ := core.UnwrapString(imp.Path)
		if val != schemaImportPath {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		} else {
			return filepath.Base(val) // set alias to module name
		}
	}
	return ""
}

func (p PackageParser) GeneratorFns() map[string]*ast.Object {
	gens := map[string]*ast.Object{}
	files := p.pkg.Syntax
	for _, f := range files {
		for name, obj := range f.Scope.Objects {
			schemaImportName := getSchemaImportName(f)
			if schemaImportName == "" {
				continue // no `schema` import found, skip the file
			}
			if !isGeneratorFn(obj, schemaImportName) {
				continue
			}
			gens[name] = obj
		}
	}
	return gens
}

func isGeneratorFn(obj *ast.Object, schemaImportName string) bool {
	fn, ok := obj.Decl.(*ast.FuncDecl)
	if !ok {
		return false
	}
	if fn.Type.Results.NumFields() != 1 {
		return false
	}
	res := fn.Type.Results.List[0]
	ptr, ok := res.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	sel, ok := ptr.X.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	return sel.X.(*ast.Ident).Name == schemaImportName && sel.Sel.Name == "Resource"
}

func (p PackageParser) Validate() error {
	generatorFns := p.GeneratorFns()
	if l := len(generatorFns); l != 0 {
		log.Printf("found %d generator(s) in package %s", l, p.pkg.ID)
	}
	mErr := &multierror.Error{}
	for name, fnObj := range generatorFns {
		ast.Inspect(fnObj.Decl.(*ast.FuncDecl), func(node ast.Node) bool {
			lit, ok := node.(*ast.CompositeLit)
			if !ok {
				return true
			}
			gen, err := p.ParseGenerator(lit, name)
			if err != nil {
				log.Println(err)
				return false
			}
			mErr = multierror.Append(mErr, gen.ValidateSetters())
			return false
		})
	}
	return mErr.ErrorOrNil()
}
