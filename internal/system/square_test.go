package system

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// newSquare lays the Lo Shu square, 6 7 2 over 1 5 9 over 8 3 4, in the middle of an
// 80 by 40 world, a cursor standing on its middle cell
func newSquare(t *testing.T) (*engine.World, *SquareSystem, *component.SquareComponent) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	s := ready(NewSquareSystem(w)).(*SquareSystem)
	s.HandleEvent(event.GameEvent{Type: event.EventSquareSpawnRequest, Payload: &event.SquareSpawnRequestPayload{
		X: 40, Y: 18, Layout: "672159834", Groups: []string{"213", "654", "879"}, ShieldStep: 200,
	}})
	ready(NewCursorSystem(w)).(*CursorSystem).HandleEvent(event.GameEvent{Type: event.EventCursorSpawnRequest,
		Payload: &event.CursorSpawnRequestPayload{X: 40, Y: 18, Control: uint8(component.ControlLocal)}})
	squares := w.Components.Square.Entities()
	if len(squares) != 1 || w.Resources.Player.Slot(0) == 0 {
		t.Fatalf("%d squares and cursor %d, want one of each", len(squares), w.Resources.Player.Slot(0))
	}
	sq, _ := w.Components.Square.GetPtr(squares[0])
	w.Resources.Event.Queue.Consume()
	return w, s, sq
}

func pressSquare(s *SquareSystem, keys ...string) {
	for _, key := range keys {
		s.HandleEvent(event.GameEvent{Type: event.EventInputPressed, Payload: &event.InputPayload{Key: key}})
	}
}

func stepSquare(w *engine.World, s *SquareSystem) []event.GameEvent {
	w.Resources.Time.Update(engine.SimTime(0, parameter.GameUpdateInterval),
		engine.SimTime(0, parameter.GameUpdateInterval), parameter.GameUpdateInterval)
	s.Update()
	return slices.Clone(w.Resources.Event.Queue.Consume())
}

// squareDigits reads the square's digits by place
func squareDigits(sq *component.SquareComponent) string {
	var out []byte
	for _, d := range sq.Digits {
		out = append(out, '0'+d)
	}
	return string(out)
}

// standSwarm stands a bare swarm composite with its header at (x, y)
func standSwarm(w *engine.World, x, y int) core.Entity {
	foot := component.Footprints[component.SpeciesSwarm]
	header := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(header, component.PositionComponent{X: x, Y: y})
	w.Components.Combat.SetComponent(header, component.CombatComponent{
		OwnerEntity: header, CombatEntityType: component.CombatEntitySwarm, HitPoints: parameter.CombatInitialHPSwarm,
	})
	var members []component.MemberEntry
	for row := range foot.Height {
		for col := range foot.Width {
			dx, dy := col-foot.HeaderX, row-foot.HeaderY
			m := w.CreateEntity(core.DomainShared)
			w.Positions.SetPosition(m, component.PositionComponent{X: x + dx, Y: y + dy})
			w.Components.Member.SetComponent(m, component.MemberComponent{HeaderEntity: header})
			members = append(members, component.MemberEntry{Entity: m, OffsetX: dx, OffsetY: dy})
		}
	}
	w.Components.Header.SetComponent(header, component.HeaderComponent{
		Behavior: component.BehaviorSwarm, Type: component.CompositeTypeUnit, MemberEntries: members,
	})
	return header
}

// TestATurnTurnsTheSelectedBlocksLineAPlace: 159 turned right is 915, the selection
// follows its block into the column it turns next, and a d s f turn as h j k l do
func TestATurnTurnsTheSelectedBlocksLineAPlace(t *testing.T) {
	for _, keys := range [2][4]string{{"h", "j", "k", "l"}, {"a", "d", "s", "f"}} {
		left, down, up, right := keys[0], keys[1], keys[2], keys[3]
		_, s, sq := newSquare(t)
		pressSquare(s, "5", right)
		if got := squareDigits(sq); got != "672915834" {
			t.Fatalf("5 turned right by %s: %s, want 672915834", right, got)
		}
		pressSquare(s, down)
		if got := squareDigits(sq); got != "674912835" {
			t.Fatalf("then turned down by %s: %s, want 674912835", down, got)
		}
		pressSquare(s, up, left)
		if got := squareDigits(sq); got != "672159834" {
			t.Fatalf("then back up by %s and left by %s: %s, want 672159834", up, left, got)
		}
	}
}

// squareFired reads the kinds a step's events fired, in order
func squareFired(events []event.GameEvent) (kinds []component.SquareKind) {
	for _, ev := range events {
		if p, ok := ev.Payload.(*event.SquareAlignedPayload); ok {
			kinds = append(kinds, p.Kind)
		}
	}
	return kinds
}

// TestAnEdgeFiresAgainEachCooldownTheAttackOnATurn: with the kinds down the columns
// the two on the edges fire at once, a turn away and back inside the cooldown firing
// nothing and the middle column's never; then a whirl or a defense fires again as its
// cooldown runs out, and the attack only on a turn that leaves it there. A defense's
// fire boosts its cursor and blows a gust out from the middle; a whirl turns a wind.
func TestAnEdgeFiresAgainEachCooldownTheAttackOnATurn(t *testing.T) {
	for _, c := range []struct {
		keys  []string
		edges []component.SquareKind // the left column's, then the right's
	}{
		{[]string{"5", "h", "3", "l"}, []component.SquareKind{component.SquareWhirl, component.SquareAttack}},
		{[]string{"7", "l", "3", "h"}, []component.SquareKind{component.SquareAttack, component.SquareDefense}},
	} {
		w, s, sq := newSquare(t)
		cursor := w.Resources.Player.Slot(0)
		pressSquare(s, c.keys...)
		events := slices.Clone(w.Resources.Event.Queue.Consume())
		if got := squareFired(events); !slices.Equal(got, c.edges) {
			t.Fatalf("%v fired %v, want the edges' %v", c.keys, got, c.edges)
		}
		boosts, gusts, whirls := 0, 0, 0
		cx, cy := sq.Center()
		for _, ev := range events {
			if p, ok := ev.Payload.(*event.BoostActivatePayload); ok && p.Entity == cursor && p.Duration == parameter.SquareBoostDuration {
				boosts++
			}
			if p, ok := ev.Payload.(*event.WindStartPayload); ok && p.Radial && p.X == cx && p.Y == cy {
				switch {
				case p.Force == parameter.SquareWindForce && p.Direction == 0:
					gusts++
				case p.Force == parameter.SquareWhirlForce && p.Direction == math.Pi/2 && p.Duration == parameter.SquareWhirlDuration:
					whirls++
				}
			}
		}
		defends, turns := 0, 0
		if slices.Contains(c.edges, component.SquareDefense) {
			defends = 1
		}
		if slices.Contains(c.edges, component.SquareWhirl) {
			turns = 1
		}
		if gusts != defends || whirls != turns || boosts != defends {
			t.Fatalf("%v blew %d gusts, turned %d winds and boosted %d times, want %d, %d and a boost a gust",
				c.keys, gusts, whirls, boosts, defends, turns)
		}
		pressSquare(s, "l", "h")
		if got := squareFired(w.Resources.Event.Queue.Consume()); len(got) != 0 {
			t.Fatalf("%v fired again inside the cooldown", got)
		}

		ticks := func(d time.Duration) int { return int(d / parameter.GameUpdateInterval) }
		var again [component.SquareKindCount]int
		last := 0
		for _, spec := range component.SquareKinds {
			last = max(last, ticks(spec.Cooldown))
		}
		for tick := 1; tick <= last; tick++ {
			for _, kind := range squareFired(stepSquare(w, s)) {
				if again[kind] == 0 {
					again[kind] = tick
				}
			}
		}
		for kind, spec := range component.SquareKinds {
			want := 0
			if slices.Contains(c.edges, component.SquareKind(kind)) && !spec.Turned {
				want = ticks(spec.Cooldown)
			}
			if again[kind] != want {
				t.Errorf("%v: %s fired again at tick %d, want %d", c.keys, spec.Name, again[kind], want)
			}
		}
		l := sq.Line(component.SquareAttack)
		along := map[bool]string{false: "h", true: "k"}[l >= 2]
		pressSquare(s, string(rune('0'+sq.Digits[component.EdgePlaces(l)[0]])), along)
		if got := squareFired(w.Resources.Event.Queue.Consume()); !slices.Equal(got, []component.SquareKind{component.SquareAttack}) {
			t.Fatalf("%v then a turn along the attack's edge fired %v, want the attack", c.keys, got)
		}
	}
}

// TestALineFacesTheGatewaysOnItsSide: an edge faces the two corners of its side,
// past the middle block's edge, and the cells between them, and no edge the
// square's own rows and columns between two sides
func TestALineFacesTheGatewaysOnItsSide(t *testing.T) {
	w, _, sq := newSquare(t)
	right, bottom := w.Resources.Config.MapWidth-1, w.Resources.Config.MapHeight-1
	cx, cy := sq.Center()
	for _, c := range []struct {
		name  string
		x, y  int
		lines []int
	}{
		{"top left", 0, 0, []int{0, 2}}, {"top", cx, 0, []int{0}}, {"top right", right, 0, []int{0, 3}},
		{"right", right, cy, []int{3}}, {"bottom right", right, bottom, []int{1, 3}},
		{"bottom", cx, bottom, []int{1}}, {"bottom left", 0, bottom, []int{1, 2}}, {"left", 0, cy, []int{2}},
		{"middle", cx, cy, nil},
	} {
		var got []int
		for l := range component.SquareLines {
			if sq.Faces(l)(c.x, c.y) {
				got = append(got, l)
			}
		}
		if !slices.Equal(got, c.lines) {
			t.Errorf("the %s cell is faced by lines %v, want %v", c.name, got, c.lines)
		}
	}
}

// TestAnOrbStandsOnTheSideItsLineFaces: an edge stands its orb at the middle of its
// side of the outer orbit
func TestAnOrbStandsOnTheSideItsLineFaces(t *testing.T) {
	w, _, sq := newSquare(t)
	cfg := w.Resources.Config
	aspect := cfg.CellAspect
	cx, cy := sq.Center()
	reach := sq.ShieldRadius(aspect) + parameter.SquareOrbGap
	rx, ry := int(math.Round(reach)), int(math.Round(aspect.FromCircular(reach)))
	for l, want := range [component.SquareLines]vmath.Point{
		{X: cx, Y: cy - ry}, {X: cx, Y: cy + ry}, {X: cx - rx, Y: cy}, {X: cx + rx, Y: cy},
	} {
		if got := sq.Orb(l, aspect, cfg.MapWidth, cfg.MapHeight); got != want {
			t.Errorf("line %d stands its orb at %v, want %v", l, got, want)
		}
	}
}

// TestASquareSizesItsCursorsShieldToCoverIt: the cursor standing on the square
// takes a shield reaching past the square's corners, its orbs' orbit with it, and
// under that stepped shield its kills earn it no boost
func TestASquareSizesItsCursorsShieldToCoverIt(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	stepSquare(w, s)
	boosts := ready(NewBoostSystem(w)).(*BoostSystem)
	for range 10 {
		boosts.HandleEvent(event.GameEvent{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: cursor}})
	}
	if boost, _ := w.Components.Boost.GetComponent(cursor); boost.Active {
		t.Fatalf("ten kills boost a square's cursor for %v, want none under its stepped shield", boost.Remaining)
	}
	shield, _ := w.Components.Shield.GetPtr(cursor)
	aspect := w.Resources.Config.CellAspect
	width, height := sq.Size()
	if corner := vmath.Magnitude(float64(width)/2, aspect.Circular(float64(height)/2)); shield.Radius <= corner {
		t.Fatalf("the shield reaches %v, short of the square's corner at %v", shield.Radius, corner)
	}
	weapons := ready(NewWeaponSystem(w)).(*WeaponSystem)
	if rx, _ := weapons.orbit(cursor); rx != shield.Radius+parameter.OrbOrbitRadiusX-parameter.PlayerShieldRadius {
		t.Fatalf("its orbs circle at %v, not as far past a shield of %v as the default orbit past the default", rx, shield.Radius)
	}
}

// TestAStrikeKillsWhatStandsInTheThirdItsLineFaces: attack in the left column lands
// as its cursor's on the swarm standing left of the square and on none above it,
// takes every hit point it holds whatever their count, and marks the fire as landed
func TestAStrikeKillsWhatStandsInTheThirdItsLineFaces(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	cx, cy := sq.Center()
	left, above := standSwarm(w, 4, cy), standSwarm(w, cx, 2)
	hp := 100 * parameter.CombatInitialHPSwarm
	for _, swarm := range []core.Entity{left, above} {
		c, _ := w.Components.Combat.GetPtr(swarm)
		c.HitPoints = hp
	}
	pressSquare(s, "7", "l", "3", "h")
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.CombatAttackAreaRequestPayload); ok && (p.OwnerEntity != cursor || p.OriginEntity != cursor) {
			t.Fatalf("a %s lands as %d's from %d, want its cursor's", component.CombatAttackNames[p.AttackType], p.OwnerEntity, p.OriginEntity)
		}
		combat.HandleEvent(ev)
	}
	struck, _ := w.Components.Combat.GetPtr(left)
	spared, _ := w.Components.Combat.GetPtr(above)
	if struck.HitPoints != 0 || struck.LastDamagedBy != cursor || spared.HitPoints != hp {
		t.Fatalf("the swarm left of the square holds %d hit points, last hit by %d, and the one above it %d: want none by cursor %d, and %d",
			struck.HitPoints, struck.LastDamagedBy, spared.HitPoints, cursor, hp)
	}
	if !sq.Effects[component.SquareAttack].Struck {
		t.Fatal("a strike that killed left its fire marked as landing on nothing")
	}
}

// TestABodyOnTheBlocksUnderNoShieldCostsHeatOnceATouch: with its cursor's shield
// down, a swarm standing on a block costs that cursor the swarm's touch in heat, is
// thrown off as a shield throws it and crosses the hit; under a shield, or still
// flying from a touch, it lands nothing
func TestABodyOnTheBlocksUnderNoShieldCostsHeatOnceATouch(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	shield, _ := w.Components.Shield.GetPtr(cursor)
	x, y := sq.Place(0)
	body := standSwarm(w, x+1, y+1)
	w.Components.Swarm.SetComponent(body, component.SwarmComponent{})
	touch := func() (heat, thrown, hits int) {
		for _, ev := range stepSquare(w, s) {
			switch p := ev.Payload.(type) {
			case *event.HeatAddRequestPayload:
				heat += p.Delta
			case *event.CombatAttackAreaRequestPayload:
				if p.AttackType == component.CombatAttackShield && p.TargetEntity == body && p.OwnerEntity == cursor {
					thrown++
				}
			case *event.SquareHitPayload:
				hits++
			}
		}
		return heat, thrown, hits
	}

	shield.Active = true
	if heat, thrown, hits := touch(); heat != 0 || thrown != 0 || hits != 0 {
		t.Fatalf("under its shield a body on the blocks cost %d heat, %d throws, %d hits", heat, thrown, hits)
	}
	shield.Active = false
	want := profile.Contact[component.SpeciesSwarm].HeatDelta
	if heat, thrown, hits := touch(); heat != want || thrown != 1 || hits != 1 {
		t.Fatalf("a touch on the blocks cost %d heat, %d throws, %d hits, want %d, one and one", heat, thrown, hits, want)
	}
	c, _ := w.Components.Combat.GetPtr(body)
	c.RemainingKineticImmunity = time.Second
	if heat, thrown, hits := touch(); heat != 0 || thrown != 0 || hits != 0 {
		t.Fatalf("a body still flying from a touch cost %d heat, %d throws, %d hits", heat, thrown, hits)
	}
}

// TestASquaresMagicChargesAndItsDefenseShedsIt: the meter charges with time and with
// each kill its cursor makes, a turn into a magic state fills it at once, and a
// defense's fire sheds the share of SquareMagicDust it holds as dust and empties
// it. The digits turned into order, 1 to 9 or 9 to 1, fill it too, and the defense
// along an edge of either sheds it whole.
func TestASquaresMagicChargesAndItsDefenseShedsIt(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	stepSquare(w, s)
	for _, killer := range []core.Entity{cursor, cursor + 99} {
		s.HandleEvent(event.GameEvent{Type: event.EventSpeciesKilled, Payload: &event.SpeciesKilledPayload{KillerEntity: killer}})
	}
	if want := parameter.GameUpdateInterval + parameter.SquareMagicKill; sq.Magic != want {
		t.Fatalf("a tick and a kill of its cursor's charged %v, want %v", sq.Magic, want)
	}
	if pressSquare(s, "5", "l", "h"); !sq.IsMagic() || sq.Magic != parameter.SquareMagicFull {
		t.Fatalf("a turn into a magic state (%t) left the meter at %v, want it full", sq.IsMagic(), sq.Magic)
	}

	// The keys' turns line the defense up on an edge
	shed := func(keys ...string) (dust int) {
		w.Resources.Event.Queue.Consume()
		pressSquare(s, keys...)
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if batch, ok := ev.Payload.(*event.BatchPayload[event.ParticleSpawnEntry]); ok {
				dust += len(batch.Entries)
			}
		}
		return dust
	}
	if dust := shed("7", "l", "3", "h"); dust != parameter.SquareMagicDust || sq.Magic != 0 {
		t.Fatalf("a defense on a full meter shed %d dust and left %v, want %d and none", dust, sq.Magic, parameter.SquareMagicDust)
	}
	w, s, sq = newSquare(t)
	sq.Magic = parameter.SquareMagicFull / 4
	if dust := shed("7", "l", "3", "h"); dust != parameter.SquareMagicDust/4 || sq.Magic != 0 {
		t.Fatalf("a defense on a quarter meter shed %d dust and left %v, want %d and none", dust, sq.Magic, parameter.SquareMagicDust/4)
	}
	for _, c := range []struct {
		digits [component.SquareBlocks]uint8
		key    string
	}{
		{[component.SquareBlocks]uint8{3, 1, 2, 4, 5, 6, 7, 8, 9}, "3"},
		{[component.SquareBlocks]uint8{7, 9, 8, 6, 5, 4, 3, 2, 1}, "7"},
	} {
		w, s, sq = newSquare(t)
		sq.Digits = c.digits
		if dust := shed(c.key, "h"); !sq.IsMagic() || dust != parameter.SquareMagicDust {
			t.Fatalf("%s turned into order (magic %t) shed %d dust from an empty meter, want a full meter's %d",
				squareDigits(sq), sq.IsMagic(), dust, parameter.SquareMagicDust)
		}
	}
}

// TestASquareShedsItsMagicAsItsShieldFalls: its cursor's energy crossing zero with
// the shield down crosses the fall, which sheds the meter as dust on every
// instance; a crossing of zero that leaves the shield standing crosses nothing,
// nor does one of a cursor no square stands under
func TestASquareShedsItsMagicAsItsShieldFalls(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	shield, _ := w.Components.Shield.GetPtr(cursor)
	sq.Magic = parameter.SquareMagicFull
	fall := func() (fell, dust int) {
		w.Resources.Event.Queue.SetOwn(true)
		s.HandleEvent(event.GameEvent{Type: event.EventEnergyCrossedZero, Payload: &event.EnergyCrossedZeroPayload{Entity: cursor}})
		w.Resources.Event.Queue.SetOwn(false)
		for _, ev := range slices.Clone(w.Resources.Event.Queue.Consume()) {
			if ev.Type == event.EventSquareShieldFell {
				fell++
				s.HandleEvent(ev)
			}
		}
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if batch, ok := ev.Payload.(*event.BatchPayload[event.ParticleSpawnEntry]); ok {
				dust += len(batch.Entries)
			}
		}
		return fell, dust
	}
	shield.Active = true
	if fell, dust := fall(); fell != 0 || dust != 0 || sq.Magic != parameter.SquareMagicFull {
		t.Fatalf("under a standing shield a crossing of zero crossed %d falls and shed %d dust", fell, dust)
	}
	shield.Active = false
	if fell, dust := fall(); fell != 1 || dust != parameter.SquareMagicDust || sq.Magic != 0 {
		t.Fatalf("the shield falling crossed %d falls, shed %d dust and left %v, want one, %d and none", fell, dust, sq.Magic, parameter.SquareMagicDust)
	}
	w.Components.Square.RemoveEntity(w.Components.Square.Entities()[0])
	if fell, _ := fall(); fell != 0 {
		t.Fatalf("a shield falling over no square crossed %d falls", fell)
	}
}

// TestADefenseStandsTheShieldItsCursorHasNoEnergyFor: while a defense's boost holds,
// the square raises its cursor's shield, which the energy system then stands at no
// energy and drops as the hold ends
func TestADefenseStandsTheShieldItsCursorHasNoEnergyFor(t *testing.T) {
	w, s, _ := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	energies := ready(NewEnergySystem(w)).(*EnergySystem)
	shield, _ := w.Components.Shield.GetPtr(cursor)
	energy, _ := w.Components.Energy.GetPtr(cursor)
	energy.Current = 0
	asked := func() (up, down int) {
		stepSquare(w, s)
		energies.Update()
		for _, ev := range w.Resources.Event.Queue.Consume() {
			switch ev.Type {
			case event.EventShieldActivate:
				up++
			case event.EventShieldDeactivate:
				down++
			}
		}
		return up, down
	}
	if up, down := asked(); shield.Raised || up != 0 || down != 0 {
		t.Fatalf("with no defense firing the shield is raised %t, asked up %d times and down %d", shield.Raised, up, down)
	}
	pressSquare(s, "7", "l", "3", "h")
	if up, _ := asked(); !shield.Raised || up != 1 {
		t.Fatalf("a defense's fire raised the shield %t and asked it up %d times, want raised and once", shield.Raised, up)
	}
	shield.Active = true
	for range int(parameter.SquareBoostDuration / parameter.GameUpdateInterval) {
		stepSquare(w, s)
	}
	if _, down := asked(); shield.Raised || down != 1 {
		t.Fatalf("past the defense's hold the shield is raised %t and asked down %d times, want lowered and once", shield.Raised, down)
	}
}
