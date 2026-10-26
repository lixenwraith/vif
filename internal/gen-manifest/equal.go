package main

import (
	"fmt"
	"go/importer"
	"go/token"
	"go/types"
	"reflect"
	"strings"
)

const componentPkgPath = "github.com/lixenwraith/vif/internal/component"

// EqualFunc is one store value type's generated comparison: Name(a, b *component.Type).
type EqualFunc struct {
	Name string
	Type string
	Body string
}

// equalFuncs type-checks the component package and renders, for every value type a
// capture carries, a comparison that is true only when both values encode to the
// same JSON: floats by bits, slices by nil-ness and element. A field whose encoding
// == cannot vouch for fails generation rather than comparing loosely.
func equalFuncs(defs Definitions) []EqualFunc {
	pkg := componentPackage()
	names := []string{"PositionComponent"}
	fields := []string{"Positions"}
	for _, c := range defs.Components {
		names, fields = append(names, c.Type), append(fields, c.Field)
	}
	out := make([]EqualFunc, 0, len(names))
	for i, name := range names {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			panic(fmt.Sprintf("component type %s not found", name))
		}
		var g equalGen
		body := "*a == *b"
		if !exactType(obj.Type()) {
			body = g.expr(obj.Type(), "a", "b", name)
		}
		out = append(out, EqualFunc{Name: "equal" + fields[i], Type: name, Body: body})
	}
	return out
}

// componentPackage type-checks the component package, once per generator run.
func componentPackage() *types.Package {
	if componentPkg == nil {
		imp := importer.ForCompiler(token.NewFileSet(), "source", nil)
		pkg, err := imp.(types.ImporterFrom).ImportFrom(componentPkgPath, ".", 0)
		if err != nil {
			panic(fmt.Sprintf("type-check %s: %v", componentPkgPath, err))
		}
		componentPkg = pkg
	}
	return componentPkg
}

var componentPkg *types.Package

type equalGen struct{ depth int }

// exactType reports whether == on t implies equal JSON: no float, pointer, slice,
// map or interface anywhere in it.
func exactType(t types.Type) bool {
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return u.Info()&(types.IsBoolean|types.IsInteger|types.IsString) != 0
	case *types.Array:
		return exactType(u.Elem())
	case *types.Struct:
		for i := range u.NumFields() {
			if !exactType(u.Field(i).Type()) {
				return false
			}
		}
		return true
	}
	return false
}

// expr renders the comparison of a and b, two addressable expressions of type t.
func (g *equalGen) expr(t types.Type, a, b, where string) string {
	if exactType(t) {
		return a + " == " + b
	}
	if n, ok := t.(*types.Named); ok && marshals(n) {
		panic(fmt.Sprintf("%s: %s marshals itself and == cannot vouch for its encoding", where, n))
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		switch u.Kind() {
		case types.Float64:
			return "math.Float64bits(float64(" + a + ")) == math.Float64bits(float64(" + b + "))"
		case types.Float32:
			return "math.Float32bits(float32(" + a + ")) == math.Float32bits(float32(" + b + "))"
		}
	case *types.Struct:
		var terms []string
		for i := range u.NumFields() {
			f := u.Field(i)
			tag := reflect.StructTag(u.Tag(i)).Get("json")
			if tag == "-" {
				continue
			}
			if !f.Exported() {
				if f.Embedded() {
					panic(fmt.Sprintf("%s: unexported embedded %s needs a hand-written comparison", where, f.Name()))
				}
				continue // never encoded
			}
			terms = append(terms, g.expr(f.Type(), a+"."+f.Name(), b+"."+f.Name(), where+"."+f.Name()))
		}
		if len(terms) == 0 {
			return "true"
		}
		return strings.Join(terms, " && ")
	case *types.Array:
		return g.loop(u.Elem(), a, b, where)
	case *types.Slice:
		return "(" + a + " == nil) == (" + b + " == nil) && len(" + a + ") == len(" + b + ") && " + g.loop(u.Elem(), a, b, where)
	}
	panic(fmt.Sprintf("%s: %s has no exact comparison", where, t))
}

// loop compares two sequences element by element in an immediately called literal.
func (g *equalGen) loop(elem types.Type, a, b, where string) string {
	i := fmt.Sprintf("i%d", g.depth)
	g.depth++
	inner := g.expr(elem, a+"["+i+"]", b+"["+i+"]", where+"[]")
	g.depth--
	return "func() bool {\nfor " + i + " := range " + a + " {\nif !(" + inner + ") {\nreturn false\n}\n}\nreturn true\n}()"
}

// marshals reports whether a named type encodes itself.
func marshals(n *types.Named) bool {
	for _, t := range []types.Type{n, types.NewPointer(n)} {
		ms := types.NewMethodSet(t)
		for i := range ms.Len() {
			if name := ms.At(i).Obj().Name(); name == "MarshalJSON" || name == "MarshalText" {
				return true
			}
		}
	}
	return false
}
