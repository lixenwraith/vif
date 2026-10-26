// Package dialect is what the scenario machine, bot graphs and behavior programs
// share of their TOML: the comparison Op, the strict Decode of an argument table
// into a typed struct, the And, Or and Not of a guard Tree, and the vocabulary
// entries -schema lists. Names resolve once, at load; only Op.Holds runs per
// evaluation.
package dialect

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"strings"

	"github.com/lixenwraith/toml"
)

// Op is a comparison a guard makes. An args struct's Op field takes one of the six
// names Ops lists; the zero Op is eq, what an omitted op means.
type Op uint8

const (
	eq Op = iota
	neq
	gt
	gte
	lt
	lte
)

var opNames = [...]string{"eq", "neq", "gt", "gte", "lt", "lte"}

var opType = reflect.TypeFor[Op]()

// Ops lists the names an op takes
func Ops() []string { return slices.Clone(opNames[:]) }

func (o Op) String() string { return opNames[o] }

// Holds reports whether a compares to b as o says
func (o Op) Holds[T cmp.Ordered](a, b T) bool {
	switch o {
	case neq:
		return a != b
	case gt:
		return a > b
	case gte:
		return a >= b
	case lt:
		return a < b
	case lte:
		return a <= b
	}
	return a == b
}

func parseOp(raw any) (Op, error) {
	name, ok := raw.(string)
	if i := slices.Index(opNames[:], name); ok && i >= 0 {
		return Op(i), nil
	}
	list := strings.Join(opNames[:], ", ")
	if !ok {
		return 0, fmt.Errorf("want one of %s, got %T", list, raw)
	}
	return 0, fmt.Errorf("%.32q is not one of %s", name, list)
}

// Decode reads raw, an argument table as the parser produced it, into a new A as
// DecodeInto does
func Decode[A any](raw any) (*A, error) {
	a := new(A)
	if err := DecodeInto(raw, a); err != nil {
		return nil, err
	}
	return a, nil
}

// DecodeInto reads raw into v, a pointer to a struct, refusing a key no field takes
// and a value its field cannot hold, each named by its path. An Op field of v's
// struct takes an op's name. A nil raw leaves v as it is.
func DecodeInto(raw, v any) error {
	if raw == nil {
		return nil
	}
	if keys := toml.Unmapped(raw, v); len(keys) > 0 {
		return toml.UnknownKeys(keys)
	}
	// toml has no hook for a named type, so an op's name leaves the table it decodes
	type opAt struct {
		field int
		op    Op
	}
	var ops []opAt
	rest := raw
	table, _ := raw.(map[string]any)
	if t := reflect.TypeOf(v); table != nil && t != nil && t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct {
		for i := range t.Elem().NumField() {
			f := t.Elem().Field(i)
			key := keyOf(f)
			name, set := table[key]
			if f.Type != opType || key == "" || !set {
				continue
			}
			op, err := parseOp(name)
			if err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
			if ops == nil {
				table = maps.Clone(table)
				rest = table
			}
			delete(table, key)
			ops = append(ops, opAt{i, op})
		}
	}
	if err := toml.Decode(rest, v); err != nil {
		return err
	}
	for _, o := range ops {
		reflect.ValueOf(v).Elem().Field(o.field).Set(reflect.ValueOf(o.op))
	}
	return nil
}

// keyOf is the key a document sets field f by, as toml matches it; "" for a field
// toml skips
func keyOf(f reflect.StructField) string {
	name, _, _ := strings.Cut(f.Tag.Get("toml"), ",")
	if !f.IsExported() || name == "-" {
		return ""
	}
	return cmp.Or(name, f.Name)
}

// Field is one argument of a vocabulary entry: its key and the kind of value it
// takes, string, int, float, bool, op, array, table or any
type Field struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Fields lists the args the struct A takes, in declaration order
func Fields[A any]() []Field {
	t := reflect.TypeFor[A]()
	var out []Field
	for i := range t.NumField() {
		if key := keyOf(t.Field(i)); key != "" {
			out = append(out, Field{Name: key, Type: typeName(t.Field(i).Type)})
		}
	}
	return out
}

func typeName(t reflect.Type) string {
	if t == opType {
		return "op"
	}
	switch t.Kind() {
	case reflect.Pointer:
		return typeName(t.Elem())
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return "int"
	case reflect.Float32, reflect.Float64:
		return "float"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct:
		return "table"
	}
	return "any"
}

// The kinds of a vocabulary entry
const (
	KindGuard  = "guard"
	KindAction = "action"
)

// Entry is a name of a vocabulary. A guard's Args are its guard_args; an action
// reads the keys of its own table that Keys lists, and Args are its payload's.
type Entry struct {
	Name string   `json:"name"`
	Kind string   `json:"kind"`
	Keys []string `json:"keys,omitempty"`
	Args []Field  `json:"args,omitempty"`
}

// Sort puts entries in the order -schema lists a vocabulary: by kind, then name
func Sort(entries []Entry) {
	slices.SortFunc(entries, func(a, b Entry) int {
		return cmp.Or(strings.Compare(a.Kind, b.Kind), strings.Compare(a.Name, b.Name))
	})
}

// Tree builds a vocabulary's guards: And and Or over guards = [{ name, args }], Not
// over guard = { name, args }, nested freely, and any other name through Leaf. All
// and Any take the guards a table lists, which may be none.
type Tree[G any] struct {
	Leaf     func(name string) (build func(args map[string]any) (G, error), ok bool)
	All, Any func([]G) G
	Not      func(G) G
}

// node is one { name, args } table of a tree
type node struct {
	Name string         `toml:"name"`
	Args map[string]any `toml:"args"`
}

type compound struct {
	Guards []node `toml:"guards"`
}

type negation struct {
	Guard *node `toml:"guard"`
}

// TreeEntries are the vocabulary entries of And, Or and Not
func TreeEntries() []Entry {
	return []Entry{
		{Name: "And", Kind: KindGuard, Args: Fields[compound]()},
		{Name: "Not", Kind: KindGuard, Args: Fields[negation]()},
		{Name: "Or", Kind: KindGuard, Args: Fields[compound]()},
	}
}

// Build makes the guard name and args spell; an error names each guard it is in
func (t Tree[G]) Build(name string, args map[string]any) (G, error) {
	var zero G
	switch name {
	case "And", "Or":
		c, err := Decode[compound](args)
		if err != nil {
			return zero, fmt.Errorf("guard '%s': %w", name, err)
		}
		gs := make([]G, len(c.Guards))
		for i, n := range c.Guards {
			if n.Name == "" {
				return zero, fmt.Errorf("guard '%s': guards[%d] has no name", name, i)
			}
			if gs[i], err = t.Build(n.Name, n.Args); err != nil {
				return zero, fmt.Errorf("guard '%s': guards[%d]: %w", name, i, err)
			}
		}
		if name == "And" {
			return t.All(gs), nil
		}
		return t.Any(gs), nil
	case "Not":
		n, err := Decode[negation](args)
		switch {
		case err != nil:
			return zero, fmt.Errorf("guard 'Not': %w", err)
		case n.Guard == nil:
			return zero, errors.New("guard 'Not': requires 'guard'")
		case n.Guard.Name == "":
			return zero, errors.New("guard 'Not': guard has no name")
		}
		g, err := t.Build(n.Guard.Name, n.Guard.Args)
		if err != nil {
			return zero, fmt.Errorf("guard 'Not': %w", err)
		}
		return t.Not(g), nil
	}
	build, ok := t.Leaf(name)
	if !ok {
		return zero, fmt.Errorf("unknown guard '%s'", name)
	}
	g, err := build(args)
	if err != nil {
		return zero, fmt.Errorf("guard '%s': %w", name, err)
	}
	return g, nil
}
