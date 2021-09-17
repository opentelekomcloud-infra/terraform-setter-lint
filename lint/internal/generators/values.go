package generators

import (
	"fmt"
	"go/ast"
	"go/token"
	"math/rand"
	"regexp"
	"strings"

	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/core"
	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/set"
	"golang.org/x/tools/go/packages"
)

var (
	tokenToType = map[token.Token]string{
		token.STRING: "string",
		token.INT:    "int",
		token.MAP:    "map",
	}
	typeMapping = map[string]string{
		"TypeString": "string",
		"TypeInt":    "int",
		"TypeList":   "array",
		"TypeSet":    "array",
		"TypeMap":    "map",
		"TypeBool":   "bool",
		"TypeFloat":  "float",
	}
)

func discoverParents(base *ast.GenDecl) []string {
	res := make([]string, 0)
	spec := base.Specs[0]
	typeSpec := spec.(*ast.TypeSpec)
	switch ts := typeSpec.Type.(type) {
	case *ast.StructType:
		for _, f := range ts.Fields.List {
			if len(f.Names) == 0 { // methods will be included
				ident, ok := f.Type.(*ast.Ident)
				if !ok {
					continue
				}
				res = append(res, ident.Name)
			}
		}
	}
	return res
}

func (g Generator) getCachedFunctionType(methodName string, scope *core.Scope) (*core.FuncType, error) {
	decl, ok := scope.FuncDecls[methodName]
	if !ok {
		return nil, fmt.Errorf("can't find function declaration %s", methodName)
	}
	funcType, ok := scope.FuncTypes[methodName]
	if ok {
		return funcType, nil
	}
	funcType, err := g.getFunctionTypes(decl, scope.Package) // load types from declaration
	if err != nil {
		return nil, fmt.Errorf("error resolving types of %s.%s", scope.Package.ID, methodName)
	}
	scope.FuncTypes[methodName] = funcType
	return funcType, nil
}

func (g Generator) getStructMethodTypes(funcName, receiverName string, pkg *packages.Package) (*core.FuncType, error) {
	depID, structName, err := getPkgAndMember(receiverName)
	if err == nil {
		pkg, err = importByName(pkg, depID)
		if err != nil {
			return nil, err
		}
	} else {
		structName = receiverName
	}
	scope, err := g.getCachedScope(pkg)
	if err != nil {
		return nil, err
	}
	// get struct and its parents
	child, ok := scope.StructDecls[structName]
	if !ok {
		return nil, fmt.Errorf("can't find struct with name %s", receiverName)
	}
	possibleReceivers := append([]string{receiverName}, discoverParents(child)...)

	var typ *core.FuncType
	for _, rec := range possibleReceivers {
		methodName := core.MethodName(rec, funcName)
		typ, err = g.getCachedFunctionType(methodName, scope)
		if err != nil {
			continue
		}
	}
	if typ == nil {
		return nil, fmt.Errorf("can't find types for method %s", core.MethodName(structName, funcName))
	}
	return typ, nil
}

func (g Generator) getCachedScope(pkg *packages.Package) (*core.Scope, error) {
	scope, ok := g.scopeCache[pkg.ID]
	if ok {
		return scope, nil
	}
	scope, err := g.packageScope(pkg)
	if err != nil {
		return nil, fmt.Errorf("error getting package scope: %w", err)
	}
	g.scopeCache[pkg.ID] = scope
	return scope, nil
}

func getPkgName(pkg *packages.Package) string {
	for _, file := range pkg.Syntax {
		return file.Name.Name
	}
	return ""
}

func (g Generator) absoluteImport(pkgName string, expr ast.Expr, pkg *packages.Package) string {
	if pkgName == pkg.ID {
		return pkg.ID
	}
	srcFile := g.FSet.File(expr.Pos())
	for _, fl := range pkg.Syntax {
		flPos := g.FSet.File(fl.Package)
		if flPos.Name() != srcFile.Name() { // we found file with the expression
			continue
		}
		for _, i := range fl.Imports {
			var alias string
			path := strings.Trim(i.Path.Value, `"`)
			if i.Name != nil {
				alias = i.Name.Name
			} else {
				importedPackageName := getPkgName(pkg.Imports[path])
				alias = importedPackageName
			}
			if pkgName == alias {
				return path
			}
		}
	}
	fallback := pkg.Imports[pkgName].ID // use package import as a fallback
	return fallback
}

func (g Generator) packageScope(pkg *packages.Package) (s *core.Scope, err error) {
	// we should be very accurate here, going too deep can lead to infinite recursion
	objects := map[string]*ast.Object{}
	fnDeclarations := map[string]*ast.FuncDecl{}
	structDeclarations := map[string]*ast.GenDecl{}
	for _, fl := range pkg.Syntax {
		for _, d := range fl.Decls {
			switch dd := d.(type) {
			case *ast.FuncDecl:
				recv, err := g.getFuncReceiverName(dd)
				if err != nil {
					return nil, err
				}
				key := core.MethodName(recv, dd.Name.Name)
				fnDeclarations[key] = dd
			case *ast.GenDecl:
				if dd.Tok == token.TYPE {
					structDeclarations[dd.Specs[0].(*ast.TypeSpec).Name.Name] = dd
				}
			}
		}
		// FIXME solve recursion problems
		for k, v := range fl.Scope.Objects {
			objects[k] = v
		}
	}
	return &core.Scope{
		Package:     pkg,
		Objects:     objects,
		FuncDecls:   fnDeclarations,
		FuncTypes:   map[string]*core.FuncType{},
		StructDecls: structDeclarations,
		StructTypes: map[string]*core.StructType{},
	}, nil
}

func (g Generator) importScope(expr *ast.SelectorExpr, pkg *packages.Package) (*core.Scope, error) {
	imps := pkg.Imports
	pkgName := expr.X.(*ast.Ident).Name
	fullImportName := g.absoluteImport(pkgName, expr, pkg)
	scope, ok := g.scopeCache[fullImportName]
	if !ok {
		var err error
		scope, err = g.packageScope(imps[fullImportName])
		if err != nil {
			return nil, fmt.Errorf("error importing package scope: %w", err)
		}
		g.scopeCache[fullImportName] = scope
	}
	return scope, nil
}

func randomName(prefix string) string {
	val := fmt.Sprintf("%s%08d", prefix, rand.Intn(0xffffff))
	return val
}

func (g Generator) getExpType(r ast.Expr, pkg *packages.Package) (core.Type, error) {
	pScope, err := g.getCachedScope(pkg)
	if err != nil {
		return nil, fmt.Errorf("error getting package scope for %s: %w", pkg.ID, err)
	}

	switch exp := r.(type) {
	case *ast.ArrayType:
		elemType, err := g.getExpType(exp.Elt, pkg)
		if err != nil {
			return nil, fmt.Errorf("error getting array element type: %w", err)
		}
		return &core.ArrayType{ItemType: elemType}, nil
	case *ast.Ident:
		return g.getIdentType(exp, pkg)
	case *ast.MapType:
		valType, err := g.getExpType(exp.Value, pkg)
		if err != nil {
			return nil, fmt.Errorf("error getting map value type: %w", err)
		}
		keyType, err := g.getExpType(exp.Key, pkg)
		if err != nil {
			return nil, fmt.Errorf("error getting map value type: %w", err)
		}
		keySimple, ok := keyType.(*core.SimpleType)
		if !ok {
			return nil, fmt.Errorf("can't use not simple type as a map key")
		}
		return &core.MapType{KeyType: keySimple, ValueType: valType}, nil
	case *ast.SelectorExpr:
		return g.getSelectorType(exp, pkg)
	case *ast.StarExpr:
		return g.getExpType(exp.X, pkg) // for us doesn't matter, pointer or not
	case *ast.InterfaceType:
		return &core.SimpleType{Value: "interface"}, nil
	case *ast.CallExpr:
		// we need only one call result
		typ, err := g.getExpType(exp.Fun, pkg)
		if err != nil {
			return nil, fmt.Errorf("error fiding called function: %w", err)
		}
		switch t := typ.(type) {
		case *core.FuncType:
			return t.Results[0], err
		case *core.WrapperType, *core.SimpleType:
			return t, err
		default:
			return nil, fmt.Errorf("can't use non-callable type in call")
		}
	case *ast.FuncType:
		var args []core.Type
		var results []core.Type
		for _, a := range exp.Params.List {
			argT, err := g.getFieldType(a, pkg)
			if err != nil {
				return nil, fmt.Errorf("error getting function argument types: %w", err)
			}
			args = append(args, argT)
		}
		for _, a := range exp.Results.List {
			resT, err := g.getFieldType(a, pkg)
			if err != nil {
				return nil, fmt.Errorf("error getting function result types: %w", err)
			}
			results = append(results, resT)
		}
		key := randomName("func")
		fnType := &core.FuncType{
			Name:    key,
			Args:    args,
			Results: results,
			Ref: &core.FunctionReference{
				Package: pkg,
			},
		}
		pScope, err := g.getCachedScope(pkg)
		if err != nil {
			return nil, fmt.Errorf("error getting package scope for %s: %w", pkg.ID, err)
		}
		pScope.FuncTypes[key] = fnType
		return fnType, nil
	case *ast.IndexExpr:
		return g.getIndexExprType(exp, pkg)
	case *ast.StructType: // locally defined types
		key := randomName("struct")
		strct, err := g.fieldsToMap(exp.Fields, pkg)
		if err != nil {
			return nil, err
		}
		sType := &core.StructType{Name: key, Fields: strct}
		pScope.StructTypes[key] = sType
		return sType, nil
	case *ast.TypeAssertExpr:
		return g.getExpType(exp.Type, pkg)
	case *ast.BasicLit:
		tt, ok := tokenToType[exp.Kind]
		if !ok {
			return nil, fmt.Errorf("unresolved basic literal type: %s", exp.Kind.String())
		}
		return &core.SimpleType{Value: tt}, nil
	case *ast.UnaryExpr:
		return g.getUnaryExprType(exp, pkg)
	}
	return &core.StubType{}, nil
}

func (g Generator) getUnaryExprType(u *ast.UnaryExpr, pkg *packages.Package) (core.Type, error) {
	if u.Op == token.RANGE {
		typ, err := g.getExpType(u.X, pkg)
		if err != nil {
			return nil, fmt.Errorf("invalid range expression")
		}
		aType, ok := typ.(*core.ArrayType)
		if !ok {
			return nil, fmt.Errorf("invalid range expression type: %+v", typ)
		}
		return aType.ItemType, nil
	}
	return nil, fmt.Errorf("unsupported unary operation")
}

func (g Generator) getFunctionTypes(decl *ast.FuncDecl, pkg *packages.Package) (*core.FuncType, error) {
	ft := &core.FuncType{
		Name: decl.Name.Name,
		Ref: &core.FunctionReference{
			Package: pkg,
			Decl:    decl,
		},
	}
	if decl.Type.Results == nil {
		return ft, nil
	}
	for _, r := range decl.Type.Results.List {
		typ, err := g.getExpType(r.Type, pkg)
		if err != nil {
			return nil, err
		}
		ft.Results = append(ft.Results, typ)
	}
	return ft, nil
}

func (g Generator) getCallTypes(call *ast.CallExpr, pkg *packages.Package) (*core.FuncType, error) {
	pScope, err := g.getCachedScope(pkg)
	if err != nil {
		return nil, fmt.Errorf("error getting package scope for %s: %w", pkg.ID, err)
	}

	switch fn := call.Fun.(type) {
	case *ast.Ident:
		if fn.Name == "make" {
			typeArg := call.Args[0]
			typ, err := g.getExpType(typeArg, pkg)
			if err != nil {
				return nil, fmt.Errorf("error getting types from make: %w", err)
			}
			return &core.FuncType{
				Name:    "make",
				Results: []core.Type{typ},
			}, nil
		}
		return g.getCachedFunctionType(fn.Name, pScope)
	case *ast.SelectorExpr:
		var dep *packages.Package
		var memberName string
		switch x := fn.X.(type) {
		case *ast.CallExpr:
			xType, err := g.getCallTypes(x, pkg)
			if err != nil {
				return nil, fmt.Errorf("error getting recursive func types: %w", err)
			}
			dep, err = importByName(pkg, xType.Ref.Package.ID)
			if err != nil {
				return nil, err
			}
			memberName = core.LocalName(xType.Results[0], dep)
		default:
			xType, err := g.getSelectorType(fn, pkg)
			if err != nil {
				return nil, err
			}
			if xType == nil {
				println("ad")
			}

			pkgID, structName, err := getPkgAndMember(xType.String())
			if err != nil {
				return nil, err
			}
			dep, err = importByName(pkg, pkgID)
			if err != nil {
				return nil, err
			}
			memberName = structName
		}
		scope, err := g.getCachedScope(dep)
		if err != nil {
			return nil, err
		}
		if i, ok := fn.X.(*ast.Ident); ok && i.Obj == nil {
			return g.getCachedFunctionType(memberName, scope)
		}
		structType, ok := scope.Objects[memberName]
		if !ok {
			return nil, fmt.Errorf("struct %s is not found in package %s", memberName, dep.ID)
		}
		return g.getStructMethodTypes(fn.Sel.Name, structType.Name, dep)
	}
	return nil, fmt.Errorf("can't get function declaration for call %v", call)
}

var builtIns = set.StringSetFromSlice([]string{"error", "string", "int", "bool", "float"})
var floats = set.StringSetFromSlice([]string{"float", "float64"})
var ints = set.StringSetFromSlice([]string{"int", "int64"})

func (g Generator) getTypeNameOnly(exp ast.Expr) (string, error) {
	// finding name is a shallow operation in most cases, not going deep inside
	switch e := exp.(type) {
	case *ast.Ident:
		return e.Name, nil
	case *ast.SelectorExpr:
		xName, err := g.getTypeNameOnly(e.X)
		if err != nil {
			return "", nil
		}
		return core.MethodName(xName, e.Sel.Name), nil
	case *ast.StarExpr:
		return g.getTypeNameOnly(e.X)
	}
	return "", fmt.Errorf("getting name is not supported for %v", exp)
}

func (g Generator) getTypeSpec(spec *ast.TypeSpec, pkg *packages.Package) (core.Type, error) {
	pScope, err := g.getCachedScope(pkg)
	if err != nil {
		return nil, fmt.Errorf("error getting package scope for %s: %w", pkg.ID, err)
	}

	name := spec.Name.Name
	if pkg.ID != g.Pkg.ID {
		name = core.MethodName(pkg.ID, name)
	}
	// first, maybe it's already cached?
	if structType, ok := pScope.StructTypes[name]; ok {
		return structType, nil
	}
	// second, just get parent name
	baseTypeName, err := g.getTypeNameOnly(spec.Type)
	if err != nil { // if there is no parent name
		// maybe this is just a struct definition?
		tt, ok := spec.Type.(*ast.StructType)
		if !ok {
			return nil, fmt.Errorf("error getting wrapped type: %w", err)
		}
		fMap, err := g.fieldsToMap(tt.Fields, pkg)
		if err != nil {
			return nil, err
		}
		sType := &core.StructType{Name: name, Fields: fMap}
		pScope.StructTypes[name] = sType // dirty trick to avoid duplications
		return sType, nil
	}
	// if it's ok, just remember to resolve it later
	return &core.WrapperType{Name: name, Wrapped: baseTypeName}, nil
}

func (g Generator) getIdentType(ident *ast.Ident, pkg *packages.Package) (core.Type, error) {
	if pkg == nil {
		pkg = g.Pkg
	}
	if ident.Obj == nil {
		name := ident.Name

		if floats.Contains(name) {
			name = "float"
		} else if ints.Contains(name) {
			name = "int"
		}
		if !builtIns.Contains(name) && pkg.ID != g.Pkg.ID {
			name = core.MethodName(pkg.ID, name)
		}
		return &core.SimpleType{Value: name}, nil
	}
	decl := ident.Obj.Decl
	switch d := decl.(type) {
	case *ast.AssignStmt:
		// if it was assigned, we will search on the left side for the name
		pos := -1
		// find argument position
		for i, l := range d.Lhs {
			lv, ok := l.(*ast.Ident)
			if !ok || lv.Name != ident.Name {
				continue // ignore other things
			}
			pos = i
			break
		}
		if pos == -1 {
			return nil, fmt.Errorf("can't find argument position")
		}
		// check right side types
		if len(d.Rhs) != 1 {
			println("Not supported now")
			return nil, fmt.Errorf("can't work with number or right side values != 1")
		}
		var typ core.Type
		var err error
		switch exp := d.Rhs[0].(type) {
		case *ast.CallExpr:
			funTypes, err := g.getCallTypes(exp, pkg)
			if err != nil {
				return nil, fmt.Errorf("error getting type of function: %w", err)
			}
			if funTypes == nil {
				return nil, fmt.Errorf("can't get function return types")
			}
			typ = funTypes.Results[pos]
		default:
			typ, err = g.getExpType(exp, pkg)
		}
		if err != nil {
			return nil, fmt.Errorf("error handling assignment typing: %w", err)
		}
		return typ, nil
	case *ast.TypeSpec:
		return g.getTypeSpec(d, pkg)
	case *ast.Field:
		fieldType, err := g.getFieldType(d, pkg)
		if err != nil {
			return nil, fmt.Errorf("error handling field typing: %w", err)
		}
		return fieldType, nil
	case *ast.ValueSpec:
		return g.getExpType(d.Type, pkg)
	}
	return nil, fmt.Errorf("failed to get ident type")
}

func (g Generator) getIndexExprType(ind *ast.IndexExpr, pkg *packages.Package) (core.Type, error) {
	typ, err := g.getExpType(ind.X, pkg)
	if err != nil {
		err = fmt.Errorf("error getting index expression type: %w", err)
	}
	switch tt := typ.(type) {
	case *core.ArrayType:
		return tt.ItemType, nil
	case *core.MapType:
		return tt.ValueType, nil
	}
	return nil, fmt.Errorf("unknown type of value in the index expression %v", typ)
}

func (g Generator) getFieldType(field *ast.Field, pkg *packages.Package) (core.Type, error) {
	return g.getExpType(field.Type, pkg)
}

func baseName(fullName string) string {
	parts := strings.Split(fullName, ".")
	if len(parts) < 2 {
		return fullName
	}
	return parts[len(parts)-1]
}

func (g Generator) fieldsToMap(lst *ast.FieldList, pkg *packages.Package) (StructFields, error) {
	fields := map[string]core.Type{}
	for _, f := range lst.List {
		if len(f.Names) == 0 {
			// we should include all 'parent' fields inside
			typ, err := g.getExpType(f.Type, pkg)
			if err != nil {
				return nil, fmt.Errorf("error finding `parent` type: %w", err)
			}
			typeName := typ.String()
			pFields, err := g.getStructTypes(typ, pkg)
			if err != nil {
				return nil, fmt.Errorf("error getting `parent` type fields: %w", err)
			}
			baseFldName := baseName(typeName)
			fields[baseFldName] = typ
			for k, v := range pFields {
				fields[k] = v
			}
			continue
		}
		for _, name := range f.Names {
			typ, err := g.getFieldType(f, pkg)
			if err != nil {
				return nil, fmt.Errorf("can't get field type: %w", err)
			}
			fields[name.Name] = typ
		}
	}
	return fields, nil
}

var elRe = regexp.MustCompile(`(.+)\.(\w+?)$`)

func getPkgAndMember(typeName string) (string, string, error) {
	parts := elRe.FindStringSubmatch(typeName)
	if len(parts) < 3 {
		return "", "", fmt.Errorf("not valid struct type: %s", typeName)
	}
	return parts[1], parts[2], nil
}

func importByName(pkg *packages.Package, name string) (*packages.Package, error) {
	if pkg.ID == name {
		return pkg, nil
	}
	imp, ok := pkg.Imports[name]
	if !ok {
		return nil, fmt.Errorf("can't find import for %s in %s package", name, pkg)
	}
	return imp, nil
}

func (g Generator) getStructTypes(typ core.Type, pkg *packages.Package) (StructFields, error) {
	// get field type
	var fullName string
	switch t := typ.(type) {
	case *core.ArrayType:
		fullName = t.ItemType.String()
	default:
		fullName = t.String()
	}
	impName, structName, err := getPkgAndMember(fullName)
	if err == nil {
		imp, ok := pkg.Imports[impName]
		if !ok {
			return nil, fmt.Errorf("error finding import %s in %s package", impName, pkg.ID)
		}
		pkg = imp
	}
	scope, err := g.getCachedScope(pkg)
	if err != nil {
		return nil, fmt.Errorf("error getting scope: %w", err)
	}

	structDecl, ok := scope.StructDecls[structName]
	if !ok {
		return nil, fmt.Errorf("no struct with name %s is found", structName)
	}
	typeSpec := structDecl.Specs[0].(*ast.TypeSpec)
	strct, ok := typeSpec.Type.(*ast.StructType)
	if !ok {
		return nil, fmt.Errorf("this is not struct: %v", typeSpec)
	}
	return g.fieldsToMap(strct.Fields, pkg)
}

func (g Generator) getSelectorType(sel *ast.SelectorExpr, pkg *packages.Package) (core.Type, error) {
	// this is import
	if ix, ok := sel.X.(*ast.Ident); ok && ix.Obj == nil {
		pkgAbs := g.absoluteImport(ix.Name, sel, pkg)
		innerPkg, err := importByName(pkg, pkgAbs)
		if err != nil {
			return nil, err
		}
		resType, err := g.getExpType(sel.Sel, innerPkg)
		if err != nil {
			return nil, err
		}
		return resType, nil
	}

	xType, err := g.getExpType(sel.X, pkg)
	if err != nil {
		return nil, fmt.Errorf("error extracting `X` type: %w", err)
	}
	if xType == nil {
		println("")
	}

	if g.Pkg.ID != pkg.ID {
		xType.BindToPackage(pkg.ID)
	}

	typeName := xType.String()
	pkgName, localName, err := getPkgAndMember(typeName)
	var pScope *core.Scope
	if err != nil {
		pkgName = pkg.ID
		localName = typeName
	}
	targetPkg, err := importByName(pkg, pkgName)
	if err != nil {
		return nil, err
	}
	pScope, err = g.getCachedScope(targetPkg)
	if err != nil {
		return nil, fmt.Errorf("error getting package scope for %s: %w", pkg.ID, err)
	}

	sType, ok := pScope.StructTypes[localName]
	if !ok {
		fields, err := g.getStructTypes(xType, pkg)
		if err != nil {
			return nil, err
		}
		sType = &core.StructType{Name: typeName, Fields: fields}
		pScope.StructTypes[localName] = sType
	}

	typ, ok := sType.Fields[sel.Sel.Name]
	if !ok {
		typ, err = g.getStructMethodTypes(sel.Sel.Name, localName, targetPkg)
		if err != nil {
			return nil, err
		}
	}
	return typ, nil
}

func (g Generator) getTypeAssertionType(asrt *ast.TypeAssertExpr, pkg *packages.Package) (core.Type, error) {
	typ, err := g.getExpType(asrt.X, pkg)
	if err != nil {
		return nil, fmt.Errorf("can't resolve type assertion: %w", err)
	}
	return typ, nil
}

func (g Generator) getFuncReceiverName(fDecl *ast.FuncDecl) (string, error) {
	if fDecl.Recv.NumFields() == 0 {
		return "", nil
	}
	if f := fDecl.Recv.List[0]; f != nil {
		recvType, err := g.getTypeNameOnly(f.Type)
		if err != nil {
			return "", err
		}
		return recvType, err
	}
	return "", nil
}
