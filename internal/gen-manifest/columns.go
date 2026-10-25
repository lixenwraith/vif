package main

import (
	"fmt"
	"go/types"
	"reflect"
	"slices"
	"strings"
)

// ColumnFunc is one store's generated column layout: the loops that lay its rows
// out one field at a time and the loops that read them back.
type ColumnFunc struct {
	Field  string
	Type   string
	Encode string
	Decode string
}

// columnFuncs renders, for every store a capture carries, the column layout of its
// component: one column per leaf field in declaration order, each over every row,
// so a keyframe's bytes repeat by field rather than by row and deflate as a column
// does. A field without a column layout fails generation rather than travelling
// some other way.
func columnFuncs(defs Definitions) []ColumnFunc {
	pkg := componentPackage()
	names := []string{"PositionComponent"}
	fields := []string{"Positions"}
	for _, c := range defs.Components {
		names, fields = append(names, c.Type), append(fields, c.Field)
	}
	out := make([]ColumnFunc, 0, len(names))
	for i, name := range names {
		obj := pkg.Scope().Lookup(name)
		if obj == nil {
			panic(fmt.Sprintf("component type %s not found", name))
		}
		var g columnGen
		g.walk(obj.Type(), []string{"for i := range rows {"}, "rows[i].Value", name)
		out = append(out, ColumnFunc{
			Field:  fields[i],
			Type:   name,
			Encode: strings.Join(g.enc, "\n"),
			Decode: strings.Join(g.dec, "\n"),
		})
	}
	return out
}

// columnGen collects one store's columns, each the loops that reach a leaf over
// every row around that leaf's append or read.
type columnGen struct{ enc, dec []string }

func (g *columnGen) column(loops []string, enc, dec string) {
	open, close := strings.Join(loops, "\n"), strings.Repeat("\n}", len(loops))
	g.enc = append(g.enc, open+"\n"+enc+close)
	g.dec = append(g.dec, open+"\n"+dec+close)
}

// walk lays t out, reached through expr inside loops: a leaf is a column, a struct
// its fields in order, an array its elements under one more loop, a slice its
// lengths' column and then its elements'.
func (g *columnGen) walk(t types.Type, loops []string, expr, where string) {
	if isTime(t) {
		g.column(loops, "dst = appendTime(dst, "+expr+")", "readTime(&"+expr+", r)")
		return
	}
	if n, ok := t.(*types.Named); ok && marshals(n) {
		panic(fmt.Sprintf("%s: %s marshals itself and has no column layout", where, n))
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		kind := basicColumn(u, where)
		g.column(loops, "dst = append"+kind+"(dst, "+expr+")", "read"+kind+"(&"+expr+", r)")
	case *types.Struct:
		for i := range u.NumFields() {
			f := u.Field(i)
			if reflect.StructTag(u.Tag(i)).Get("json") == "-" {
				continue
			}
			if !f.Exported() {
				if f.Embedded() {
					panic(fmt.Sprintf("%s: unexported embedded %s has no column layout", where, f.Name()))
				}
				continue // never encoded
			}
			g.walk(f.Type(), loops, expr+"."+f.Name(), where+"."+f.Name())
		}
	case *types.Array:
		g.elements(u.Elem(), loops, expr, where)
	case *types.Slice:
		// An element costs a byte at least, which bounds what a length may claim.
		if leaves(u.Elem()) == 0 {
			panic(fmt.Sprintf("%s: a slice of %s, which has no column, cannot be bounded", where, u.Elem()))
		}
		g.column(loops, "dst = appendLen(dst, "+expr+")", "readSlice(&"+expr+", r)")
		g.elements(u.Elem(), loops, expr, where)
	default:
		panic(fmt.Sprintf("%s: %s has no column layout", where, t))
	}
}

// elements lays a sequence's elements out under one more loop.
func (g *columnGen) elements(elem types.Type, loops []string, expr, where string) {
	v := string(rune('i' + len(loops)))
	inner := append(slices.Clone(loops), "for "+v+" := range "+expr+" {")
	g.walk(elem, inner, expr+"["+v+"]", where+"[]")
}

// basicColumn names the helper pair a basic kind travels by.
func basicColumn(u *types.Basic, where string) string {
	switch u.Kind() {
	case types.Bool:
		return "Bool"
	case types.Int, types.Int8, types.Int16, types.Int32, types.Int64:
		return "Int"
	case types.Uint, types.Uint8, types.Uint16, types.Uint32, types.Uint64, types.Uintptr:
		return "Uint"
	case types.Float32:
		return "F32"
	case types.Float64:
		return "F64"
	case types.String:
		return "String"
	}
	panic(fmt.Sprintf("%s: %s has no column layout", where, u))
}

// leaves counts the columns t lays out to.
func leaves(t types.Type) int {
	if isTime(t) {
		return 1
	}
	switch u := t.Underlying().(type) {
	case *types.Basic:
		return 1
	case *types.Struct:
		n := 0
		for i := range u.NumFields() {
			if f := u.Field(i); f.Exported() && reflect.StructTag(u.Tag(i)).Get("json") != "-" {
				n += leaves(f.Type())
			}
		}
		return n
	case *types.Array:
		if u.Len() == 0 {
			return 0
		}
		return leaves(u.Elem())
	case *types.Slice:
		return 1 // its length
	}
	return 0
}
