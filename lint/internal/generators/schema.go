package generators

import (
	"fmt"
	"go/ast"

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
		key := kv.Key.(*ast.Ident)
		if usedFnNames.Contains(key.Name) {
			ident := kv.Value.(*ast.Ident)
			if ident.Obj == nil {
				continue
			}
			fnDecl := ident.Obj.Decl.(*ast.FuncDecl)
			g.OperatingFns = append(g.OperatingFns, fnDecl)
			continue
		}
		if key.Name == "Schema" {
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
		key, _ := core.UnwrapString(kv.Key.(*ast.BasicLit))
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
		return g.parseFieldGenCall(v)
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

func (g Generator) parseImportedFieldGenFn(expr *ast.SelectorExpr) (*Field, error) {
	pkgName := expr.X.(*ast.Ident).Name
	absImport := g.absoluteImport(pkgName, expr, g.Pkg)
	if absImport == "" {
		return nil, fmt.Errorf("can't find import with name `%s` in generator", pkgName)
	}
	imp, err := importByName(g.Pkg, absImport)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve imported function: %w", err)
	}
	scope, err := g.getCachedScope(imp)
	if err != nil {
		return nil, err
	}
	fnName := expr.Sel.Name
	fnDecl, ok := scope.FuncDecls[fnName]
	if !ok {
		return nil, fmt.Errorf("can't find function with name `%s` in package `%s`", fnName, pkgName)
	}
	return parseFnDeclaration(fnDecl)
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

func (g Generator) parseFieldGenCall(call *ast.CallExpr) (*Field, error) {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		// in package
		return parseFnDeclaration(fn.Obj.Decl.(*ast.FuncDecl))
	case *ast.SelectorExpr:
		// imported
		return g.parseImportedFieldGenFn(fn)
	}
	return nil, fmt.Errorf("error parsing generator field function call: unknown type of function")
}
