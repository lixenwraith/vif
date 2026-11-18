package bot

import (
	"io/fs"
	"maps"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/machine"
)

// everySensor is a graph whose one network lists every sensor
func everySensor() string {
	names := make([]string, len(sensors))
	for i, s := range sensors {
		names[i] = strconv.Quote(s.name)
	}
	return steering("inputs = [" + strings.Join(names, ", ") + "]")
}

// readings is every sensor's reading on ctx's instance by name, as a network
// listing them all takes them through the view around the cursor
func readings(t *testing.T, ctx *engine.GameContext) map[string]float64 {
	t.Helper()
	d, _ := botFor(t, ctx, everySensor(), 0)
	m, n := &d.mind, d.mind.nets[0]
	if !m.frame() {
		t.Fatal("the instance shows no view to read through")
	}
	n.read(m)
	got := make(map[string]float64, len(sensors))
	for i, s := range sensors {
		got[s.name] = n.in[i]
	}
	return got
}

// TestTheSensorsReadWhatTheViewShows: each sensor is an offset from the cursor
// scaled to the view, of the three nearest of its kind the view shows, nearest
// first, the rock's edges at the cursor's column and ahead, and zero for what is
// not sighted.
func TestTheSensorsReadWhatTheViewShows(t *testing.T) {
	w, ctx := soloWorld(16, 10)
	cfg := w.Resources.Config
	cfg.MapWidth, cfg.CameraX = 200, 10
	w.Positions.Resize(cfg.MapWidth, cfg.MapHeight)
	minX, minY, maxX, maxY, _ := cfg.VisibleMapRect()
	width, height := maxX-minX+1, maxY-minY+1
	own := w.Resources.Player.Entity
	w.Components.Energy.SetComponent(own, component.EnergyComponent{Current: -energyHalf})
	w.Components.Heat.SetComponent(own, component.HeatComponent{Current: heatHalf})

	cols := make([]engine.RockColumn, cfg.MapWidth)
	for x := range cols {
		cols[x] = engine.RockColumn{Ceil: 2, Floor: cfg.MapHeight - 3}
	}
	cols[16+width/8] = engine.RockColumn{Ceil: 5, Floor: 15}
	cols[16+width/4] = engine.RockColumn{Ceil: 8, Floor: 12}
	w.Positions.SetRock(component.WallBlockCursor, 0, cols)

	// Each kind has one at 9,10, nearer than any the view shows and left of it
	species := func(x, y int, enraged bool) {
		place(w, core.DomainShared, x, y, func(e core.Entity) {
			w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm, IsEnraged: enraged})
		})
	}
	// and of the four the view shows the farthest is dropped
	species(30, 10, false)
	species(60, 10, false)
	species(24, 12, true)
	species(40, 6, false)
	species(9, 10, true)
	for i, b := range []component.BehaviorComponent{
		{Landing: true, TargetX: 30, TargetY: 12},
		{Landing: true, TargetX: 18, TargetY: 6, Machine: machine.Instance[component.BehaviorState]{Elapsed: 300 * time.Millisecond, Duration: 1200 * time.Millisecond}},
		{Landing: true, TargetX: 9, TargetY: 10}, {TargetX: 16, TargetY: 10},
	} {
		place(w, core.DomainShared, 150, i, func(e core.Entity) { w.Components.Behavior.SetComponent(e, b) })
	}

	// A Kraken's body at 40,14 covers the cells its members stand on: of those the
	// view shows 22,12 is the nearest, 9,10 lying left of it
	kraken := place(w, core.DomainShared, 40, 14, func(e core.Entity) {
		w.Components.Kraken.SetComponent(e, component.KrakenComponent{TargetX: 17.5, TargetY: 9.5, AttackT: 0.25})
		var header component.HeaderComponent
		for _, cell := range [][2]int{{30, 14}, {22, 12}, {9, 10}} {
			member := place(w, core.DomainShared, cell[0], cell[1], func(core.Entity) {})
			header.MemberEntries = append(header.MemberEntries, component.MemberEntry{Entity: member})
		}
		w.Components.Header.SetComponent(e, header)
	})

	across := func(cells int) float64 { return float64(cells) / float64(width) }
	down := func(cells int) float64 { return float64(cells) / float64(height) }
	// Nearness points at the thing, rows counted twice, and falls from 1 beside the
	// cursor to nothing a quarter of the view's width away
	near := func(dx, dy int) (float64, float64) {
		r := math.Sqrt(float64(dx*dx + 4*dy*dy))
		pull := max(1-r/(float64(width)/4), 0)
		return pull * float64(dx) / r, pull * float64(2*dy) / r
	}
	want := map[string]float64{
		"ceiling": down(1 - 10), "floor": down(cfg.MapHeight - 3 - 10),
		"ceiling_near": down(4 - 10), "floor_near": down(15 - 10),
		"ceiling_far": down(7 - 10), "floor_far": down(12 - 10),
		"species_enraged": 1, "landing_soon": 0.25, // a quarter of its warning has run
		"kraken_x": across(40 - 16), "kraken_y": down(14 - 10), "kraken_reach": 0.25,
		"column": 2*float64(16-minX)/float64(width-1) - 1, "energy": 0.5, "heat": 0.5,
	}
	want["kraken_near_x"], want["kraken_near_y"] = near(22-16, 12-10)
	// at is a thing dx, dy cells from the cursor, read under prefix
	at := func(prefix string, dx, dy int) {
		want[prefix+"_x"], want[prefix+"_y"] = across(dx), down(dy)
		want[prefix+"_near_x"], want[prefix+"_near_y"] = near(dx, dy)
	}
	at("species", 8, 2)
	at("species2", 14, 0)
	at("species3", 24, -4)
	at("landing", 2, -4)
	at("landing2", 14, 2)
	differ := func(what string) {
		t.Helper()
		got := readings(t, ctx)
		for _, s := range sensors {
			if got[s.name] != want[s.name] {
				t.Errorf("%s, %s reads %g, want %g", what, s.name, got[s.name], want[s.name])
			}
		}
	}
	differ("as laid")

	// A Kraken spinning up its charge, or on it, lands where the charge ends, the
	// sooner the less of its spin is left
	charging, _ := w.Components.Kraken.GetPtr(kraken)
	charging.State, charging.StateRemaining = component.KrakenSpin, parameter.KrakenSpinDuration/5
	at("landing", 1, -1)
	at("landing2", 2, -4)
	at("landing3", 14, 2)
	want["landing_soon"], want["landing2_soon"] = 0.8, 0.25
	differ("with a Kraken charging")
	if on := sight(0, 0, float64(width), float64(height), false); on[fieldNearX] != 1 || on[fieldNearY] != 0 {
		t.Fatalf("a thing on the cursor's cell reads a nearness of %g, %g, want it beside the cursor on the right", on[fieldNearX], on[fieldNearY])
	}
}

// TestANetworkScansOnlyTheKindsItsInputsName: a steer walks the world once for
// each kind its network's inputs read, and for no other.
func TestANetworkScansOnlyTheKindsItsInputsName(t *testing.T) {
	var scans [kindCount]int
	for k := range kinds {
		if scan := kinds[k].scan; scan != nil {
			kinds[k].scan = func(m *mind, to visitor) { scans[k]++; scan(m, to) }
			t.Cleanup(func() { kinds[k].scan = scan })
		}
	}
	_, ctx := soloWorld(10, 2)
	botFor(t, ctx, steering(`inputs = ["species2_x", "species_near_y", "column", "ceiling"]`), 2)
	want := [kindCount]int{kindSpecies: 1}
	if scans != want {
		t.Fatalf("one steer by species, the cursor's column and the rock scanned %v by kind, want the species once", scans)
	}
}

// TestASteerAndASightingAllocateNothing: a kind's scan tells a visitor its network
// or its mind already holds, so neither an update's readings nor a guard's
// sighting leaves the collector anything.
func TestASteerAndASightingAllocateNothing(t *testing.T) {
	w, ctx := soloWorld(16, 10)
	for i := range 3 {
		place(w, core.DomainShared, 20+5*i, 8, func(e core.Entity) {
			w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm})
			w.Components.Behavior.SetComponent(e, component.BehaviorComponent{Landing: true, TargetX: 30 + i, TargetY: 5})
		})
	}
	place(w, core.DomainShared, 40, 14, func(e core.Entity) {
		w.Components.Kraken.SetComponent(e, component.KrakenComponent{State: component.KrakenSpin, TargetX: 18, TargetY: 9})
		leg := place(w, core.DomainShared, 22, 12, func(core.Entity) {})
		w.Components.Header.SetComponent(e, component.HeaderComponent{MemberEntries: []component.MemberEntry{{Entity: leg}}})
	})
	d, _ := botFor(t, ctx, everySensor(), 0)
	m, n := &d.mind, d.mind.nets[0]
	if allocs := testing.AllocsPerRun(100, func() {
		m.fresh = [targetCount]bool{}
		m.frame()
		n.read(m)
		n.pass()
		m.nearest(kindThreat)
		m.nearest(kindKraken)
	}); allocs != 0 {
		t.Fatalf("reading every sensor and sighting two targets allocated %v times an update, want none", allocs)
	}
}

// TestEveryInputReadsWithinItsScale: whatever the world holds, a sensor reads a
// number from its scale's least to 1, bar the body of a Kraken, which may stand
// past the view that shows one of its legs; and with nothing to sight, every
// kind's sensors and the ship's counts read 0.
func TestEveryInputReadsWithinItsScale(t *testing.T) {
	past := map[string]bool{}
	for _, row := range kinds {
		if row.covers {
			past[row.name+"_"+fields[fieldX].name], past[row.name+"_"+fields[fieldY].name] = true, true
		}
	}
	rng := rand.New(rand.NewPCG(7, 11))
	for trial := range 200 {
		w, ctx := soloWorld(0, 0)
		cfg := w.Resources.Config
		cfg.MapWidth = cfg.ViewportWidth + rng.IntN(120)
		cfg.CameraX = rng.IntN(cfg.MapWidth - cfg.ViewportWidth + 1)
		w.Positions.Resize(cfg.MapWidth, cfg.MapHeight)
		minX, minY, maxX, maxY, _ := cfg.VisibleMapRect()
		cell := func() (int, int) { return rng.IntN(cfg.MapWidth), rng.IntN(cfg.MapHeight) }
		own := w.Resources.Player.Entity
		w.Positions.SetPosition(own, component.PositionComponent{X: minX + rng.IntN(maxX-minX+1), Y: minY + rng.IntN(maxY-minY+1)})
		w.Components.Energy.SetComponent(own, component.EnergyComponent{Current: rng.Int64N(20001) - 10000})
		w.Components.Heat.SetComponent(own, component.HeatComponent{Current: rng.IntN(101)})
		if rng.IntN(3) > 0 {
			cols := make([]engine.RockColumn, cfg.MapWidth)
			for x := range cols {
				cols[x] = engine.RockColumn{Ceil: rng.IntN(cfg.MapHeight/2 + 1), Floor: cfg.MapHeight/2 + rng.IntN(cfg.MapHeight/2+1)}
			}
			w.Positions.SetRock(component.WallBlockCursor, 0, cols)
		}
		for range rng.IntN(7) {
			x, y := cell()
			place(w, core.DomainShared, x, y, func(e core.Entity) {
				w.Components.Combat.SetComponent(e, component.CombatComponent{CombatEntityType: component.CombatEntitySwarm, IsEnraged: rng.IntN(2) == 0})
			})
		}
		for range rng.IntN(5) {
			x, y := cell()
			b := component.BehaviorComponent{Landing: rng.IntN(4) > 0, TargetX: x, TargetY: y}
			b.Machine.Elapsed, b.Machine.Duration = time.Duration(rng.IntN(2000))*time.Millisecond, time.Duration(rng.IntN(1500))*time.Millisecond
			place(w, core.DomainShared, 0, 0, func(e core.Entity) { w.Components.Behavior.SetComponent(e, b) })
		}
		for range rng.IntN(2) {
			x, y := cell()
			tx, ty := cell()
			place(w, core.DomainShared, x, y, func(e core.Entity) {
				w.Components.Kraken.SetComponent(e, component.KrakenComponent{State: component.KrakenState(rng.IntN(4)),
					StateRemaining: time.Duration(rng.Int64N(int64(2 * parameter.KrakenSpinDuration))), TargetX: float64(tx) + 0.5, TargetY: float64(ty) + 0.5, AttackT: rng.Float64()})
				var header component.HeaderComponent
				for range rng.IntN(12) {
					mx, my := cell()
					header.MemberEntries = append(header.MemberEntries, component.MemberEntry{Entity: place(w, core.DomainShared, mx, my, func(core.Entity) {})})
				}
				w.Components.Header.SetComponent(e, header)
			})
		}
		got := readings(t, ctx)
		for _, s := range sensors {
			if v := got[s.name]; math.IsNaN(v) || math.IsInf(v, 0) || !past[s.name] && (v < scales[s.scale].low || v > 1) {
				t.Fatalf("trial %d: %s reads %g, outside its scale %s", trial, s.name, v, scales[s.scale].name)
			}
		}
	}

	// The rock's edges are the view's own rows there, and the column is the cursor's
	_, ctx := soloWorld(10, 2)
	got := readings(t, ctx)
	for _, s := range sensors {
		if v := got[s.name]; v != 0 && s.scale != scalePlace && s.scale != scaleDown {
			t.Errorf("with nothing to sight %s reads %g, want 0", s.name, v)
		}
	}
}

// TestCavesLayoutsAreTheOnesItsPopulationsAreKeptUnder: a layout's digest names the
// populations it fits, so the shipped cave's two stand until its networks are
// changed on purpose, which starts every population kept for them over.
func TestCavesLayoutsAreTheOnesItsPopulationsAreKeptUnder(t *testing.T) {
	data, err := fs.ReadFile(asset.DefaultBots, "cave.toml")
	if err != nil {
		t.Fatal(err)
	}
	g, err := ParseGraph("cave", data)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, n := range g.nets {
		got[n.name] = n.digest
	}
	if want := map[string]string{"kraken": "2bebd53cfda5", "raid": "2323553c0684"}; !maps.Equal(got, want) {
		t.Fatalf("cave's layouts are %v, want %v", got, want)
	}
}

// TestEveryCatalogueRowSaysWhatItIs: -schema and the reference print each row's
// own line, so none is left without one.
func TestEveryCatalogueRowSaysWhatItIs(t *testing.T) {
	rows := map[string]string{}
	for _, row := range kinds {
		rows["kind "+row.name] = row.doc
	}
	for _, row := range fields {
		rows["field "+row.name] = row.doc
	}
	for _, row := range scales {
		rows["scale "+row.name] = row.doc
	}
	for _, row := range edges {
		rows["edge "+row.name] = row.doc
	}
	for _, row := range places {
		rows["place "+strconv.Quote(row.suffix)] = row.doc
	}
	for _, row := range self {
		rows["own "+row.name] = row.doc
	}
	for _, row := range outputs {
		rows["output "+row.name] = row.doc
	}
	for row, doc := range rows {
		if doc == "" || strings.HasSuffix(row, " ") {
			t.Errorf("the %s row has no name or no line saying what it is", row)
		}
	}
}

// reference renders the catalogue's tables as the document doc/ keeps of them
func reference() string {
	var b strings.Builder
	// item writes a list item wrapped at 80 columns, its later lines indented
	item := func(indent, text string) {
		line := indent + "-"
		for _, word := range strings.Fields(text) {
			if len(line)+1+len(word) > 80 {
				b.WriteString(line + "\n")
				line = indent + " "
			}
			line += " " + word
		}
		b.WriteString(line + "\n")
	}
	code := func(names ...string) string {
		for i, name := range names {
			names[i] = "`" + name + "`"
		}
		return strings.Join(names, ", ")
	}
	b.WriteString(`# Bot catalogue

What a bot graph's guards and ` + "`PointAt`" + ` take as a target, and what a network
reads and asks for ([Bots](bots.md#networks-and-learning)). It is generated from
the tables of ` + "`internal/bot/catalogue.go`" + `: change a row there, then run
` + "`VIF_WRITE_GENERATED=1 go test ./internal/bot -run Reference`" + `. ` + "`vif -schema`" + `
lists the same inputs, outputs and scales under ` + "`bot_catalogue`" + `.

## Kinds

A kind is a class of thing the world holds. A guard or ` + "`PointAt`" + ` names one as
its ` + "`target`" + `, nearest the cursor over the whole world; a network reads the
nearest ones the view shows, each by its fields.

`)
	for k, row := range kinds {
		text := code(row.name) + ": " + row.doc + "."
		if kind(k) < targetCount {
			text += " A target."
		}
		if row.ranks > 0 {
			names := []string{}
			for f := range fieldCount {
				if f < fieldsWhere || slices.Contains(row.knows, f) {
					names = append(names, fields[f].name)
				}
			}
			text += " A network reads the " + strconv.Itoa(row.ranks) + " nearest, each by " + code(names...) + "."
			if row.ranks == 1 {
				text = strings.Replace(text, "the 1 nearest, each by", "the nearest, by", 1)
			}
		}
		item("", text)
	}
	b.WriteString(`
## Inputs

A network's ` + "`inputs`" + ` name what it reads, in the order its weights take them.
` + "`<kind>_<field>`" + ` is the nearest of a kind, ` + "`<kind>2_<field>`" + ` the second nearest
and so on. Every reading keeps to its scale, and what is not sighted reads 0.

### Fields

`)
	for _, row := range fields {
		item("", code(row.name)+", "+scales[row.scale].name+": "+row.doc+".")
	}
	b.WriteString("\n### The rock\n\n")
	names := []string{}
	for _, row := range edges {
		names = append(names, row.name)
		item("", code(row.name)+", "+scales[scaleDown].name+": "+row.doc+" a column, the view's own row where no rock stands.")
	}
	b.WriteString("\nEach is read at a column, by what its name ends in:\n\n")
	for _, row := range places {
		at := []string{}
		for _, name := range names {
			at = append(at, name+row.suffix)
		}
		item("", code(at...)+": "+row.doc+".")
	}
	b.WriteString("\n### The bot's own\n\n")
	for _, row := range self {
		scale := scales[row.scale].name
		if row.half > 0 {
			scale += " at " + strconv.Itoa(row.half)
		}
		item("", code(row.name)+", "+scale+": "+row.doc+".")
	}
	b.WriteString("\n### Scales\n\n")
	for _, row := range scales {
		item("", row.name+", "+strconv.FormatFloat(row.low, 'g', -1, 64)+" to 1: "+row.doc+".")
	}
	b.WriteString(`
## Outputs

What a network asks for, in the order ` + "`Steer`" + ` acts on it. A graph writes an
output's weights in a table named for each of its units.

`)
	for _, row := range outputs {
		item("", code(row.name)+", units "+code(unitNames[row.at:row.at+row.units]...)+": "+row.doc+".")
	}
	return b.String()
}

// TestTheCatalogueReferenceIsItsTables keeps doc/bot-catalogue.md as the tables
// render it; VIF_WRITE_GENERATED=1 rewrites it after a row changes.
func TestTheCatalogueReferenceIsItsTables(t *testing.T) {
	const page = "../../doc/bot-catalogue.md"
	want := reference()
	if os.Getenv("VIF_WRITE_GENERATED") == "1" {
		if err := os.WriteFile(page, []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(page)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("%s is stale; VIF_WRITE_GENERATED=1 go test ./internal/bot -run Reference rewrites it", page)
	}
}
