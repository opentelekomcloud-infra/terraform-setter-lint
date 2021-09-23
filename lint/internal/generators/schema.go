package generators

import (
	"fmt"
	"go/ast"
	"log"
	"strings"

	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/core"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/set"
)

var usedFnNames = set.StringSetFromSlice([]string{
	"CreateContext",
	"Create",
	"ReadContext",
	"Read",
	"UpdateContext",
	"Update",
})

func (g *Generator) LoadSchema(lit *ast.CompositeLit) error {
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
			g.OperatingFns = append(g.OperatingFns, fnDecl)
			continue
		}
		if k.Name == "Schema" {
			cmp, ok := kv.Value.(*ast.CompositeLit)
			if !ok {
				return fmt.Errorf("can't find schema definition in a `Schema` field")
			}
			sch, err := g.schemaDeclToMap(cmp)
			if err != nil {
				return fmt.Errorf("error constructing resource schema: %w", err)
			}
			g.Schema = sch
		}
	}
	return nil
}

func (g Generator) schemaDeclToMap(schemaDecl *ast.CompositeLit) (map[string]*Field, error) {
	result := map[string]*Field{}
	for i, el := range schemaDecl.Elts {
		kv, ok := el.(*ast.KeyValueExpr)
		if !ok {
			return nil, fmt.Errorf("the element #%d is not a key-value element", i)
		}
		key := strings.Trim(kv.Key.(*ast.BasicLit).Value, `"`)
		val, err := g.parseSchemaField(kv.Value)
		if err != nil {
			return nil, err
		}
		result[key] = val
	}
	return result, nil
}

func (g Generator) parseSchemaField(expr ast.Expr) (*Field, error) {
	switch v := expr.(type) {
	case *ast.CompositeLit:
		return parseComposite(v)
	case *ast.CallExpr:
		return g.parseCall(v)
	}
	return nil, fmt.Errorf("invalid field %+v", expr)
}

func parseComposite(lit *ast.CompositeLit) (*Field, error) {
	f := &Field{}
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

func (g Generator) parseImportedFn(expr *ast.SelectorExpr) (*Field, error) {
	dcl, err := core.ResolveImportedFunction(expr, g.Pkg)
	if err != nil {
		return nil, err
	}
	return parseFnDeclaration(dcl.Decl)
}

func parseFnDeclaration(decl *ast.FuncDecl) (*Field, error) {
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

func (g Generator) parseCall(call *ast.CallExpr) (*Field, error) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// in package
		return parseFnDeclaration(fn.Obj.Decl.(*ast.FuncDecl))
	case *ast.SelectorExpr:
		// imported
		return g.parseImportedFn(fn)
	}
	return nil, nil
}
