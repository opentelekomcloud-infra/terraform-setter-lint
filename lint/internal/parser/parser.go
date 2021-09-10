package parser

import (
	"fmt"
	"go/ast"
	"go/token"
	"log"
	"path/filepath"
	"strings"

	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/generators"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/set"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/utils"
	"golang.org/x/tools/go/packages"
)

type Parser struct {
	fSet *token.FileSet
	pkg  *packages.Package
	// map of file to import name
	schemaImportNames map[token.Pos]string
}

const schemaImportPath = "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

func NewParser(pkg *packages.Package, set *token.FileSet) *Parser {
	p := &Parser{
		pkg:  pkg,
		fSet: set,
	}
	p.schemaImportNames = p.findImportNames(schemaImportPath)
	return p
}

var usedFnNames = set.SetFromSlice([]string{
	"CreateContext",
	"Create",
	"ReadContext",
	"Read",
	"UpdateContext",
	"Update",
})

func (p Parser) ParseGenerator(lit *ast.CompositeLit, genName string) (*generators.Generator, error) {
	gen := &generators.Generator{FSet: p.fSet, Name: genName}
	for _, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		k, ok := kv.Key.(*ast.Ident)
		if !ok {
			log.Printf("failed to parse field key")
			continue
		}
		if usedFnNames.Contains(k.Name) {
			ident, ok := kv.Value.(*ast.Ident)
			if !ok {
				log.Printf("can't parse %s field", k.Name)
				continue
			}
			if ident.Obj == nil {
				continue
			}
			fnDecl, ok := ident.Obj.Decl.(*ast.FuncDecl)
			if !ok {
				log.Printf("%s is not a function declaration", ident.Obj.Name)
				continue
			}
			gen.OperatingFns = append(gen.OperatingFns, fnDecl)
			continue
		}
		if k.Name == "Schema" {
			cmp, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				return nil, fmt.Errorf("can't find schema definition in a `Schema` field")
			}
			sch, err := p.schemaDeclToMap(cmp)
			if err != nil {
				return nil, fmt.Errorf("error constructing resource schema: %w", err)
			}
			gen.Schema = sch
		}
	}
	return gen, nil
}

func (p Parser) schemaDeclToMap(schemaDecl *ast.CompositeLit) (map[string]generators.Field, error) {
	result := map[string]generators.Field{}
	for i, el := range schemaDecl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			return nil, fmt.Errorf("the element #%d is not a key-value element", i)
		}
		key := strings.Trim(kv.Key.(*ast.BasicLit).Value, `"`)
		val, err := p.parseSchemaField(kv.Value)
		if err != nil {
			return nil, err
		}
		if val == nil {
			log.Printf("can't process field `%s`", key)
			continue
		}
		result[key] = *val
	}
	return result, nil
}

func parseComposite(lit *ast.CompositeLit) (*generators.Field, error) {
	f := &generators.Field{}
	for i, el := range lit.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			return nil, fmt.Errorf("error processing element #%d of a composite", i)
		}
		name := kv.Key.(*ast.Ident).Name
		if name == "Type" {
			val, ok := kv.Value.(*ast.SelectorExpr)
			if !ok {
				return nil, fmt.Errorf("invalid `Type` field of %s", name)
			}
			f.Type = val.Sel.String()
		}
	}
	return f, nil
}

func (p Parser) parseImportedFn(expr *ast.SelectorExpr) (*generators.Field, error) {
	imps := p.pkg.Imports
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
			fnDcl := fl.Scope.Lookup(fnName)
			if fnDcl == nil {
				continue
			}
			dcl, ok := fnDcl.Decl.(*ast.FuncDecl)
			if !ok {
				return nil, fmt.Errorf("invalid function declaration for %s", fnName)
			}
			return parseFnDeclaration(dcl)
		}
		break
	}
	return nil, nil
}

func parseFnDeclaration(decl *ast.FuncDecl) (*generators.Field, error) {
	for _, stmt := range decl.Body.List {
		ret, ok := stmt.(*ast.ReturnStmt)
		if !ok {
			continue
		}
		if len(ret.Results) != 1 {
			return nil, fmt.Errorf("number of returns is more than 1")
		}
		result := ret.Results[0]
		var cmp *ast.CompositeLit
		switch r := result.(type) {
		case *ast.UnaryExpr:
			// value defined in the return
			cmp = r.X.(*ast.CompositeLit)
		case *ast.Ident:
			// if function returns some variable
			// find the declaration
			ass, ok := r.Obj.Decl.(*ast.AssignStmt)
			if !ok {
				return nil, fmt.Errorf("unknown kind of var assignment")
			}
			// check if we can find the value
			if len(ass.Rhs) != 1 {
				return nil, fmt.Errorf("too complex assignment :(")
			}
			// get the value and hope it's a unary expression now
			val := ass.Rhs[0].(*ast.UnaryExpr)
			// do the same as in the previous case
			cmp = val.X.(*ast.CompositeLit)
		default:
			return nil, fmt.Errorf("unknown kind of return: %v", r)
		}
		return parseComposite(cmp)
	}
	return nil, nil
}

func (p Parser) parseCall(call *ast.CallExpr) (*generators.Field, error) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// in package
		return parseFnDeclaration(fn.Obj.Decl.(*ast.FuncDecl))
	case *ast.SelectorExpr:
		// imported
		return p.parseImportedFn(fn)
	}
	return nil, nil
}

func (p Parser) parseSchemaField(expr ast.Expr) (*generators.Field, error) {
	switch v := expr.(type) {
	case *ast.CompositeLit:
		return parseComposite(v)
	case *ast.CallExpr:
		return p.parseCall(v)
	}
	return nil, fmt.Errorf("invalid field %+v", expr)
}

func (p Parser) FindFnObject(name string) *ast.Object {
	for _, fl := range p.pkg.Syntax {
		for _, obj := range fl.Scope.Objects {
			if obj.Name == name {
				return obj
			}
		}
	}
	return nil
}

func (p Parser) findImportNames(iPath string) map[token.Pos]string {
	imports := make(map[token.Pos]string)
	for _, fl := range p.pkg.Syntax {
		pos := fl.Pos()
		for _, imp := range fl.Imports {
			val, _ := utils.UnwrapString(imp.Path)
			if val != iPath {
				continue
			}
			if imp.Name != nil {
				imports[pos] = imp.Name.Name
			} else {
				imports[pos] = filepath.Base(val) // set alias to module name
			}
		}
	}
	return imports
}

func (p Parser) GeneratorNames() (names []string) {
	files := p.pkg.Syntax
	for _, f := range files {
		for name, obj := range f.Scope.Objects {
			if !p.isGeneratorFn(obj, f.Pos()) {
				continue
			}
			names = append(names, name)
		}
	}

	return
}

func (p Parser) isGeneratorFn(obj *ast.Object, filePos token.Pos) bool {
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
	return sel.X.(*ast.Ident).Name == p.schemaImportNames[filePos] && sel.Sel.Name == "Resource"
}
