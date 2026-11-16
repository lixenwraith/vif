package behavior

import (
	"slices"
	"strings"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
)

// TestAMapLaysWhatItsLegendNamesWhereItsRowsPutIt: a row is a character a cell, a
// space open map, and each other one an instance of its legend's program in row
// order; maps take their places in sorted-name order
func TestAMapLaysWhatItsLegendNamesWhereItsRowsPutIt(t *testing.T) {
	programs := map[string]map[string]any{"dart": parse(t, warden), "warden": parse(t, warden)}
	const wave = `
[maps.wave]
legend = { W = { program = "warden" }, d = { program = "dart" } }
rows = [
    " W  W ",
    "d d d ",
]

[maps.pair]
legend = { d = { program = "dart" } }
rows = ["dd"]
`
	set, err := Compile(testVocabulary(), programs, parse(t, wave))
	if err != nil {
		t.Fatal(err)
	}
	const dart, warden = 0, 1
	want := []component.MapLayout{
		{Name: "pair", Width: 2, Height: 1, Spawns: []component.MapSpawn{{X: 0, Program: dart}, {X: 1, Program: dart}}},
		{Name: "wave", Width: 6, Height: 2, Spawns: []component.MapSpawn{
			{X: 1, Y: 0, Program: warden}, {X: 4, Y: 0, Program: warden},
			{X: 0, Y: 1, Program: dart}, {X: 2, Y: 1, Program: dart}, {X: 4, Y: 1, Program: dart}}},
	}
	if got := set.Maps(); len(got) != 2 || got[0].Name != want[0].Name || !slices.Equal(got[0].Spawns, want[0].Spawns) ||
		got[1].Width != 6 || got[1].Height != 2 || !slices.Equal(got[1].Spawns, want[1].Spawns) {
		t.Fatalf("maps %+v, want %+v", got, want)
	}
	if got := set.Declared().Maps; !slices.Equal(got, []string{"pair", "wave"}) {
		t.Fatalf("declared maps %v", got)
	}

	for name, c := range map[string]struct{ old, new, want string }{
		"ragged":  {`"d d d "`, `"d d d"`, "map 'wave': rows[1] is 5 cells wide, and the first 6"},
		"unknown": {`" W  W "`, `" W  X "`, `map 'wave': rows[0]: 'X' at column 4 is no entry of the legend`},
		"program": {`program = "warden"`, `program = "ghost"`, `map 'wave': legend: "W": program "ghost" is not one of the scenario's behavior programs`},
		"key":     {`W = { program = "warden" }`, `WW = { program = "warden" }`, `map 'wave': legend: "WW" is not one character`},
		"no rows": {`rows = ["dd"]`, `rows = []`, "map 'pair': rows lists no row"},
		"a tile":  {`d = { program = "dart" } }` + "\nrows = [\"dd\"]", `d = { glyph = "#" } }` + "\nrows = [\"dd\"]", `unknown key "maps.pair.legend.d.glyph"`},
	} {
		if !strings.Contains(wave, c.old) {
			t.Fatalf("%s: the map holds no %q", name, c.old)
		}
		_, err := Compile(testVocabulary(), programs, parse(t, strings.Replace(wave, c.old, c.new, 1)))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: compiled: %v, want %q", name, err, c.want)
		}
	}
}
