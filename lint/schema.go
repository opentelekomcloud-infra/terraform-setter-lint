package lint

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func findPackages(path string) ([]*ast.Package, error) {
	set := token.NewFileSet()

	var packs []*ast.Package
	err := filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if !d.IsDir() {
			return nil
		}
		pMap, err := parser.ParseDir(set, p, nil, 0)
		if err != nil {
			return fmt.Errorf("error parsing a package: %w", err)
		}
		for _, v := range pMap {
			packs = append(packs, v)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return packs, nil
}

// ResourceGenerators finds all resource generating functions
func ResourceGenerators(path string) (map[string]*ast.FuncDecl, error) {
	packs, err := findPackages(path)
	if err != nil {
		return nil, err
	}

	generators := make(map[string]*ast.FuncDecl, 0)
	for _, pack := range packs {
		for p, f := range pack.Files {
			importAlias := schemaImportName(f)
			if importAlias == "" {
				continue // no schema usage found, skip this file
			}
			for _, d := range f.Decls {
				if fn, isFn := d.(*ast.FuncDecl); isFn && isResourceGeneratorFn(fn) {
					generators[p] = fn
					continue // we don't expect more than one resource generator in a file
				}
			}
		}
	}
	return generators, nil
}

// GetGeneratorSchema returns resource base schema from generator function
func GetGeneratorSchema(genFn *ast.FuncDecl) (*ResourceSchema, error) {
	schema := &ResourceSchema{map[string]Field{}}
	fieldsMap, err := searchForMap(genFn.Body)
	if err != nil {
		return nil, err
	}
	if fieldsMap == nil {
		return nil, fmt.Errorf("can't find fields map in %s", genFn.Name.Name)
	}
	for _, v := range fieldsMap.Elts {
		elem := v.(*ast.KeyValueExpr)
		key := elem.Key.(*ast.BasicLit)
		if key.Kind != token.STRING {
			return nil, fmt.Errorf("invalid schema key (not a string): %v", key.Value)
		}
		keyName := strings.Trim(key.Value, `"`)
		field := parseField(elem.Value.(*ast.CompositeLit))
		schema.Fields[keyName] = field
	}
	return schema, nil
}

func parseField(src *ast.CompositeLit) Field {
	fld := Field{ReadOnly: true}
	for _, e := range src.Elts {
		prop := e.(*ast.KeyValueExpr)
		switch prop.Key.(*ast.Ident).Name {
		case "Type":
			fld.Type = prop.Value.(*ast.SelectorExpr).Sel.Name // e.g. TypeString
		case "Optional", "Required":
			fld.ReadOnly = false
		default:
			continue
		}
	}
	return fld
}

type ErrNotSupported struct {
	Details string
}

func (e *ErrNotSupported) Error() string {
	return fmt.Sprintf("this kind of resource definition is not supported now: %s", e.Details)
}

func searchForMap(src *ast.BlockStmt) (*ast.CompositeLit, error) {
	if len(src.List) != 1 {
		return nil, &ErrNotSupported{"more than one expression in the function"}
	}
	ret, ok := src.List[0].(*ast.ReturnStmt)
	if !ok {
		return nil, &ErrNotSupported{"first statement is not a return"}
	}
	if len(ret.Results) != 1 {
		// should be impossible
		return nil, &ErrNotSupported{"more than one return value in a schema function"}
	}
	uExp := ret.Results[0].(*ast.UnaryExpr)
	comp := uExp.X.(*ast.CompositeLit)
	for _, expr := range comp.Elts {
		kvExpr := expr.(*ast.KeyValueExpr)
		if kvExpr.Key.(*ast.Ident).Name == "Schema" {
			return kvExpr.Value.(*ast.CompositeLit), nil
		}
	}
	return nil, fmt.Errorf("can't find schema map")
}

func schemaImportName(src *ast.File) string {
	for _, imp := range src.Imports {
		if imp.Path.Value == `"github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"` {
			if imp.Name != nil {
				return imp.Name.Name
			}
			return "schema"
		}
	}
	return ""
}

func isResourceGeneratorFn(fn *ast.FuncDecl) bool {
	results := fn.Type.Results
	if results.NumFields() != 1 {
		return false
	}
	argument := results.List[0]
	pp, isP := argument.Type.(*ast.StarExpr)
	if !isP { // should be a pointer
		return false
	}
	ps, isS := pp.X.(*ast.SelectorExpr)
	if !isS {
		return false
	}
	return isSchemaResource(ps)
}

func isSchemaResource(exp *ast.SelectorExpr) bool {
	return exp.Sel.Name == "Resource"
}

var setRegexp = regexp.MustCompile(`^\t+.+d\.Set\("(\w+?)".+`)

// FindFieldSetters goes through the file to find all `d.Set` calls
func FindFieldSetters(filePath string) ([]string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		err := file.Close()
		if err != nil {
			log.Fatal("error closing file")
		}
	}()

	var fields []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		fieldName := setRegexp.FindStringSubmatch(line)
		if fieldName == nil {
			continue
		}
		fields = append(fields, fieldName[1])
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading source file: %w", err)
	}

	return fields, nil
}
