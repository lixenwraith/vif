package dialect_test

import (
	"fmt"
	"reflect"
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

// The enums a names test decodes: a plain one with a gap, a mask, and a variant
// whose names its struct's species picks
type (
	kind    uint8
	bits    uint8
	species uint8
	sub     uint8
	room    struct {
		Kind  kind `toml:"kind"`
		Walls bits `toml:"walls"`
	}
	spawn struct {
		Species species `toml:"species"`
		Sub     sub     `toml:"sub_type"`
	}
	plan struct {
		Kind   kind            `toml:"kind"`
		Rooms  []room          `toml:"rooms"`
		Door   *room           `toml:"door"`
		ByName map[string]kind `toml:"by_name"`
		Kinds  []kind          `toml:"kinds"`
		Spawn  spawn           `toml:"spawn"`
		Count  int             `toml:"count"`
	}
)

func planNames() *dialect.Names {
	return dialect.NewNames().
		Enum[kind]("hall", "", "vault").
		Mask[bits](map[string]bits{"none": 0, "north": 1, "south": 2, "all": 3}).
		Enum[species]("none", "eye", "snake").
		Variant[sub](species(1), "void", "flame")
}

// TestANameResolvesWhereverItsTypeSits: a field of a type the names hold takes a
// name at the top, in a nested table, an array of tables, a pointer, a map and a
// list, and the raw table the caller holds is left as it was.
func TestANameResolvesWhereverItsTypeSits(t *testing.T) {
	raw := func() map[string]any {
		return map[string]any{
			"kind":    "vault",
			"rooms":   []map[string]any{{"kind": "hall"}, {"kind": "vault", "walls": "north"}},
			"door":    map[string]any{"kind": "vault"},
			"by_name": map[string]any{"a": "vault"},
			"kinds":   []any{"vault", "hall"},
			"spawn":   map[string]any{"species": "eye", "sub_type": "flame"},
			"count":   int64(3),
		}
	}
	held := raw()
	p, err := planNames().Decode[plan](held)
	if err != nil {
		t.Fatal(err)
	}
	want := plan{Kind: 2, Rooms: []room{{0, 0}, {2, 1}}, Door: &room{Kind: 2}, ByName: map[string]kind{"a": 2},
		Kinds: []kind{2, 0}, Spawn: spawn{1, 1}, Count: 3}
	if !reflect.DeepEqual(*p, want) {
		t.Errorf("decoded %+v, want %+v", *p, want)
	}
	if !reflect.DeepEqual(held, raw()) {
		t.Errorf("raw table changed to %v", held)
	}
}

// TestAnUnknownNameIsRefusedByItsPath: a name its type lacks, or a value that is
// no name, fails naming its path and the names it may take, and a type with more
// names than an error lists is named instead.
func TestAnUnknownNameIsRefusedByItsPath(t *testing.T) {
	many := make([]string, 40)
	for i := range many {
		many[i] = fmt.Sprintf("n%d", i)
	}
	for _, c := range []struct {
		raw  map[string]any
		want string
	}{
		{map[string]any{"kind": "attic"}, `kind: "attic" is not one of hall, vault`},
		{map[string]any{"rooms": []map[string]any{{}, {"kind": "attic"}}}, `rooms[1].kind: "attic" is not one of hall, vault`},
		{map[string]any{"kinds": []any{"hall", true}}, "kinds[1]: want one of hall, vault, got bool"},
		{map[string]any{"by_name": map[string]any{"a": "attic"}}, `by_name.a: "attic" is not one of hall, vault`},
		{map[string]any{"count": "n9"}, "count: cannot convert string to int"},
	} {
		if _, err := planNames().Decode[plan](c.raw); err == nil || err.Error() != c.want {
			t.Errorf("%v: error %v, want %s", c.raw, err, c.want)
		}
	}
	long := dialect.NewNames().Enum[kind](many...)
	if _, err := long.Decode[plan](map[string]any{"kind": "attic"}); err == nil ||
		err.Error() != `kind: "attic" is not one of the 40 names of dialect_test.kind` {
		t.Errorf("error %v", err)
	}
}

// TestANumberDecodesOnlyWhileItsTypeMigrates: an enum field refuses a number,
// which a reordered enum would silently retarget, unless its type still takes
// numbers while its documents move to names.
func TestANumberDecodesOnlyWhileItsTypeMigrates(t *testing.T) {
	raw := map[string]any{"kind": int64(2)}
	if _, err := planNames().Decode[plan](raw); err == nil || err.Error() != "kind: want one of hall, vault, got int64" {
		t.Errorf("closed: error %v", err)
	}
	p, err := planNames().Numbers[kind]().Decode[plan](raw)
	if err != nil || p.Kind != 2 {
		t.Errorf("migrating: %v, %v", p, err)
	}
}

// TestAMaskTakesANameOrAListOfThem: a mask field takes one name, or a list that
// stands for the union of its names, an empty one for none.
func TestAMaskTakesANameOrAListOfThem(t *testing.T) {
	for _, c := range []struct {
		walls any
		want  bits
		err   string
	}{
		{"all", 3, ""},
		{[]any{"north", "south"}, 3, ""},
		{[]any{}, 0, ""},
		{[]any{"north", "west"}, 0, `door.walls[1]: "west" is not one of none, north, south, all`},
		{int64(1), 0, "door.walls: want one of none, north, south, all, or a list of them, got int64"},
	} {
		p, err := planNames().Decode[plan](map[string]any{"door": map[string]any{"walls": c.walls}})
		switch {
		case c.err != "" && (err == nil || err.Error() != c.err):
			t.Errorf("%v: error %v, want %s", c.walls, err, c.err)
		case c.err == "" && (err != nil || p.Door.Walls != c.want):
			t.Errorf("%v: %v, %v; want %d", c.walls, p, err, c.want)
		}
	}
}

// TestAVariantTakesTheNamesItsKeyPicks: a variant field takes the names its
// struct's key value has, and where that value has none it takes none, unless
// its type still takes numbers.
func TestAVariantTakesTheNamesItsKeyPicks(t *testing.T) {
	decode := func(n *dialect.Names, s map[string]any) (*plan, error) {
		return n.Decode[plan](map[string]any{"spawn": s})
	}
	if p, err := decode(planNames(), map[string]any{"sub_type": "flame", "species": "eye"}); err != nil || p.Spawn.Sub != 1 {
		t.Errorf("eye flame: %v, %v", p, err)
	}
	for _, c := range []struct {
		spawn map[string]any
		want  string
	}{
		{map[string]any{"species": "eye", "sub_type": "frost"}, `spawn.sub_type: "frost" is not one of void, flame`},
		{map[string]any{"species": "snake", "sub_type": "void"}, `spawn.sub_type: snake takes no names, got "void"`},
		{map[string]any{"sub_type": "void"}, `spawn.sub_type: none takes no names, got "void"`},
		{map[string]any{"species": "snake", "sub_type": int64(0)}, "spawn.sub_type: snake takes no names, got int64"},
	} {
		if _, err := decode(planNames(), c.spawn); err == nil || err.Error() != c.want {
			t.Errorf("%v: error %v, want %s", c.spawn, err, c.want)
		}
	}
	if _, err := decode(planNames().Numbers[sub](), map[string]any{"species": "snake", "sub_type": int64(0)}); err != nil {
		t.Errorf("migrating snake 0: %v", err)
	}
}

// TestAClonedNamesBuildsWithoutChangingItsSource: names built on a clone, as a
// scenario adds its programs to the code's, leave the names it was cloned from as
// they were.
func TestAClonedNamesBuildsWithoutChangingItsSource(t *testing.T) {
	base := planNames()
	clone := base.Clone().Variant[sub](species(2), "long").Numbers[kind]()
	snake := map[string]any{"spawn": map[string]any{"species": "snake", "sub_type": "long"}, "kind": int64(2)}
	if _, err := clone.Decode[plan](snake); err != nil {
		t.Errorf("clone: %v", err)
	}
	if _, err := base.Decode[plan](snake); err == nil {
		t.Error("base took what only its clone names")
	}
}
