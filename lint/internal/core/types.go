package core

import (
	"fmt"
	"strings"

	"golang.org/x/tools/go/packages"
)

type Type interface {
	String() string
	Matches(expected string) bool
	BindToPackage(pkg string)
}

type SimpleType struct {
	Value string
}

func (s *SimpleType) String() string {
	return s.Value
}

func (s *SimpleType) Matches(expected string) bool {
	return s.String() == expected
}

func (s *SimpleType) BindToPackage(pkg string) {
	s.Value = MethodName(pkg, s.String())
}

type ArrayType struct {
	ItemType Type
}

func (s *ArrayType) String() string {
	return fmt.Sprintf("array:%s", s.ItemType.String())
}

func (s *ArrayType) Matches(expected string) bool {
	return expected == "array"
}

func (s *ArrayType) BindToPackage(pkg string) {
	s.ItemType.BindToPackage(pkg)
}

type MapType struct {
	KeyType   *SimpleType
	ValueType Type
}

func (m *MapType) String() string {
	return fmt.Sprintf("map[%s]%s", m.KeyType.String(), m.ValueType.String())
}

func (m *MapType) Matches(expected string) bool {
	return expected == "map"
}

func (m *MapType) BindToPackage(string) {
	return
}

type StructType struct {
	Name   string
	Fields map[string]Type
}

func (s *StructType) String() string {
	return s.Name
}

func (s *StructType) Matches(expected string) bool {
	panic("implement me")
}

func (s *StructType) BindToPackage(pkgName string) {
	panic("implement me")
}

type FuncType struct {
	Name    string
	Args    []Type
	Results []Type

	Ref *FunctionReference
}

func (f *FuncType) String() string {
	return f.Name
}

func (f *FuncType) Matches(expected string) bool {
	panic("implement me")
}

func (f *FuncType) BindToPackage(pkg string) {
	f.Name = MethodName(pkg, f.Name)
}

func LocalName(t Type, pkg *packages.Package) string {
	name := t.String()
	if strings.HasPrefix(name, pkg.ID+".") {
		name = strings.TrimPrefix(name, pkg.ID+".")
	}
	return name
}

type StubType struct {
}

func (s StubType) String() string {
	return "stub"
}

func (s StubType) Matches(string) bool {
	return false
}

func (s StubType) BindToPackage(string) {
	return
}
