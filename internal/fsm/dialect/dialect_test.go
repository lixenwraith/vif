package dialect_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/fsm/dialect"
)

type compare struct {
	Var   string     `toml:"var"`
	Op    dialect.Op `toml:"op"`
	Value int64      `toml:"value"`
	Inner struct {
		X int `toml:"x"`
	} `toml:"inner"`
}

// TestAnOpNotOfTheSixIsRefused: a misspelt op fails the load naming its key and the
// six names, where it once compared for equality.
func TestAnOpNotOfTheSixIsRefused(t *testing.T) {
	for _, raw := range []any{"ge", "", "EQ", int64(1)} {
		_, err := dialect.Decode[compare](map[string]any{"op": raw})
		if err == nil || !strings.HasPrefix(err.Error(), "op: ") || !strings.Contains(err.Error(), "eq, neq, gt, gte, lt, lte") {
			t.Errorf("op %v: error %v", raw, err)
		}
	}
}

// TestEachOpHoldsAsNamedAndAnOmittedOneIsEq: each name compares as it says, and an
// args table without an op compares for equality.
func TestEachOpHoldsAsNamedAndAnOmittedOneIsEq(t *testing.T) {
	for _, c := range []struct {
		op   any     // nil omits it
		want [3]bool // 1, 2 and 3 against 2
	}{
		{nil, [3]bool{false, true, false}},
		{"eq", [3]bool{false, true, false}},
		{"neq", [3]bool{true, false, true}},
		{"gt", [3]bool{false, false, true}},
		{"gte", [3]bool{false, true, true}},
		{"lt", [3]bool{true, false, false}},
		{"lte", [3]bool{true, true, false}},
	} {
		raw := map[string]any{"var": "x"}
		if c.op != nil {
			raw["op"] = c.op
		}
		a, err := dialect.Decode[compare](raw)
		if err != nil {
			t.Fatalf("op %v: %v", c.op, err)
		}
		for i, x := range []int64{1, 2, 3} {
			if got := a.Op.Holds(x, 2); got != c.want[i] {
				t.Errorf("op %v: %d against 2 holds %t", c.op, x, got)
			}
		}
	}
}

// TestAnUnknownKeyIsRefusedByItsPath: a key no field takes fails the decode, named
// as the document spells its path, where it once read as absent.
func TestAnUnknownKeyIsRefusedByItsPath(t *testing.T) {
	_, err := dialect.Decode[compare](map[string]any{"vr": "x", "inner": map[string]any{"y": int64(1)}})
	if err == nil || err.Error() != `unknown keys "inner.y", "vr"` {
		t.Fatalf("error %v", err)
	}
}

// TestAMistypedValueIsRefusedByItsPath: a value its field cannot hold fails the
// decode naming its key, where a mistyped number once read as zero.
func TestAMistypedValueIsRefusedByItsPath(t *testing.T) {
	for _, c := range []struct {
		raw  any
		want string
	}{
		{map[string]any{"value": "3"}, "value: cannot convert string to int"},
		{map[string]any{"inner": map[string]any{"x": 1.5}}, "inner.x: cannot convert float64 to int"},
		{int64(5), "expected map for struct, got int64"},
	} {
		if _, err := dialect.Decode[compare](c.raw); err == nil || err.Error() != c.want {
			t.Errorf("%v: error %v, want %s", c.raw, err, c.want)
		}
	}
}

// tree is a vocabulary of two guards, T and F, which take no args
var tree = dialect.Tree[bool]{
	Leaf: func(name string) (func(map[string]any) (bool, error), bool) {
		if name != "T" && name != "F" {
			return nil, false
		}
		return func(args map[string]any) (bool, error) {
			_, err := dialect.Decode[struct{}](args)
			return name == "T", err
		}, true
	},
	All: func(gs []bool) bool { return !slices.Contains(gs, false) },
	Any: func(gs []bool) bool { return len(gs) == 0 || slices.Contains(gs, true) },
	Not: func(g bool) bool { return !g },
}

func guard(name string) map[string]any { return map[string]any{"name": name} }

func guards(gs ...any) map[string]any { return map[string]any{"guards": gs} }

// TestATreeNestsAndOrAndNot: And, Or and Not take their guards from the tables they
// list, nested to any depth, and a list of none is the vocabulary's to judge.
func TestATreeNestsAndOrAndNot(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
		want bool
	}{
		{"And", nil, true},
		{"Or", guards(), true},
		{"And", guards(guard("T"), guard("F")), false},
		{"Or", guards(guard("F"), guard("T")), true},
		{"Not", map[string]any{"guard": guard("F")}, true},
		{"And", guards(guard("T"), map[string]any{"name": "Or", "args": guards(guard("F"),
			map[string]any{"name": "Not", "args": map[string]any{"guard": guard("F")}})}), true},
	} {
		got, err := tree.Build(c.name, c.args)
		if err != nil || got != c.want {
			t.Errorf("%s %v: %t, %v; want %t", c.name, c.args, got, err, c.want)
		}
	}
}

// TestABrokenTreeFailsNamingWhere: a fault anywhere in a tree fails the build naming
// each guard it sits in, down to the key at fault.
func TestABrokenTreeFailsNamingWhere(t *testing.T) {
	for _, c := range []struct {
		name string
		args map[string]any
		want string
	}{
		{"X", nil, "unknown guard 'X'"},
		{"T", map[string]any{"x": int64(1)}, `guard 'T': unknown key "x"`},
		{"And", guards(guard("T"), guard("X")), "guard 'And': guards[1]: unknown guard 'X'"},
		{"And", guards(map[string]any{"args": map[string]any{}}), "guard 'And': guards[0] has no name"},
		{"Or", guards(map[string]any{"name": "T", "arg": map[string]any{}}), `guard 'Or': unknown key "guards.arg"`},
		{"And", map[string]any{"guard": guard("T")}, `guard 'And': unknown key "guard"`},
		{"Or", map[string]any{"guards": "T"}, "guard 'Or': guards: expected slice, got string"},
		{"Not", nil, "guard 'Not': requires 'guard'"},
		{"Not", map[string]any{"guard": "T"}, "guard 'Not': guard: expected map for struct, got string"},
		{"Not", map[string]any{"guard": map[string]any{"name": "T", "args": map[string]any{"x": true}}}, `guard 'Not': guard 'T': unknown key "x"`},
	} {
		if _, err := tree.Build(c.name, c.args); err == nil || err.Error() != c.want {
			t.Errorf("%s %v: error %v, want %s", c.name, c.args, err, c.want)
		}
	}
}

// TestAVocabularyNamesEachArgAsADocumentDoesInAFixedOrder: an entry's args are
// spelled as a document sets them, and a vocabulary lists kind by kind, name by
// name, whatever order it registered in.
func TestAVocabularyNamesEachArgAsADocumentDoesInAFixedOrder(t *testing.T) {
	type args struct {
		Var   string         `toml:"var"`
		Op    dialect.Op     `toml:"op"`
		Max   *int64         `toml:"max,omitempty"`
		Rate  float64        `toml:",omitempty"`
		Names []string       `toml:"names"`
		Table map[string]any `toml:"table"`
		Any   any            `toml:"any"`
		Skip  bool           `toml:"-"`
	}
	want := []dialect.Field{{"var", "string"}, {"op", "op"}, {"max", "int"}, {"Rate", "float"},
		{"names", "array"}, {"table", "table"}, {"any", "any"}}
	if got := dialect.Fields[args](); !slices.Equal(got, want) {
		t.Errorf("fields %v, want %v", got, want)
	}
	entries := []dialect.Entry{{Name: "b", Kind: dialect.KindGuard}, {Name: "z", Kind: dialect.KindAction},
		{Name: "a", Kind: dialect.KindGuard}}
	dialect.Sort(entries)
	var order []string
	for _, e := range entries {
		order = append(order, e.Kind+" "+e.Name)
	}
	if want := []string{"action z", "guard a", "guard b"}; !slices.Equal(order, want) {
		t.Errorf("order %v, want %v", order, want)
	}
}
