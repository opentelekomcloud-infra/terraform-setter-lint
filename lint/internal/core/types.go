package core

import (
	"fmt"
	"go/ast"

	"github.com/opentelekomcloud-infra/terraform-setter-lint/lint/internal/set"
)

const SchemaImportPath = "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"

type Type interface {
	String() string
	Matches(expected string) bool
	BindToPackage(pkg string)
	Package() string
	Name() string
}

type typeInPackage struct {
	pkg string
}

func (s *typeInPackage) BindToPackage(pkg string) {
	s.pkg = pkg
}

func (s *typeInPackage) Package() string {
	return s.pkg
}

type SimpleType struct {
	typeInPackage
	Value string
}

func (s *SimpleType) String() string {
	return MethodName(s.pkg, s.Value)
}

var floats = set.StringSetFromSlice([]string{"float", "float64", "float32"})
var ints = set.StringSetFromSlice([]string{"int", "int64", "int32"})
var bools = set.StringSetFromSlice([]string{"true", "false"})

func (s *SimpleType) Matches(expected string) bool {
	if floats.Contains(s.Value) {
		return "float" == expected
	}
	if ints.Contains(s.Value) {
		return "int" == expected
	}
	if bools.Contains(s.Value) {
		return "bool" == expected
	}
	return s.String() == expected
}

func (s *SimpleType) Name() string {
	return s.Value
}

// WrapperType is a type using other type
type WrapperType struct {
	*SimpleType
	Wrapped Type
}

// knownWrappers describes wrapper types that have known expected types
var knownWrappers = map[string]string{
	MethodName(SchemaImportPath, "Set"): "array",
}

func (w *WrapperType) Matches(expected string) bool {
	st, ok := knownWrappers[w.String()]
	if ok {
		return st == expected
	}
	return w.Wrapped.Matches(expected)
}

type ArrayType struct {
	ItemType Type
}

func (s *ArrayType) String() string {
	return fmt.Sprintf("array:%s", s.ItemType.String())
}

func (s *ArrayType) Package() string {
	return s.ItemType.Package()
}

func (s *ArrayType) BindToPackage(pkg string) {
	s.ItemType.BindToPackage(pkg)
}

func (s *ArrayType) Name() string {
	return fmt.Sprintf("array:%s", s.ItemType.Name())
}

func (s *ArrayType) Matches(expected string) bool {
	return expected == "array"
}

type MapType struct {
	typeInPackage
	KeyType   *SimpleType
	ValueType Type
}

func (m *MapType) String() string {
	return fmt.Sprintf("map[%s]%s", m.KeyType.String(), m.ValueType.String())
}

func (m *MapType) Name() string {
	return fmt.Sprintf("map[%s]%s", m.KeyType.Name(), m.ValueType.Name())
}

func (m *MapType) Matches(expected string) bool {
	return expected == "map"
}

func (m *MapType) BindToPackage(string) {}

type StructType struct {
	typeInPackage
	Value  string
	Fields map[string]Type
}

func (s *StructType) String() string {
	return MethodName(s.pkg, s.Value)
}

func (s *StructType) Matches(string) bool {
	panic("implement me")
}

func (s *StructType) Name() string {
	return s.Value
}

type FuncType struct {
	typeInPackage
	FName    string
	Receiver string
	Args     []Type
	Results  []Type
}

func (f *FuncType) String() string {
	return MethodName(f.pkg, f.FName)
}

func (f *FuncType) Name() string {
	return f.FName
}

func (f *FuncType) Matches(expected string) bool {
	// dirty hack for now
	return f.Results[0].Matches(expected)
}

type StubType struct {
}

func (s StubType) Package() string {
	panic("implement me")
}

func (s StubType) Name() string {
	panic("implement me")
}

func (s StubType) String() string {
	return "stub"
}

func (s StubType) Matches(string) bool {
	return false
}

func (s StubType) BindToPackage(string) {}

type InterfaceType struct {
	SimpleType
}

func (i *InterfaceType) String() string {
	return "interface{}"
}

func (i *InterfaceType) Matches(string) bool {
	return true
}

func GetTypeNameOnly(exp ast.Expr) (string, error) {
	// finding name is a shallow operation in most cases, not going deep inside
	switch e := exp.(type) {
	case *ast.Ident:
		return e.Name, nil
	case *ast.StarExpr:
		return GetTypeNameOnly(e.X)
	}
	return "", fmt.Errorf("getting name is not supported for expression")
}
