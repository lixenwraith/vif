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

// newSquare lays the Lo Shu square in the middle of an 80 by 40 world, a cursor
// standing on its middle cell
func newSquare(t *testing.T) (*engine.World, *SquareSystem, *component.SquareComponent) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	s := ready(NewSquareSystem(w)).(*SquareSystem)
	s.HandleEvent(event.GameEvent{Type: event.EventSquareSpawnRequest, Payload: &event.SquareSpawnRequestPayload{
		X: 40, Y: 18, Layout: "276951438", Groups: []string{"213", "654", "879"},
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

// TestATurnTurnsTheSelectedBlocksLineAPlace: 951 turned right is 195, the selection
// follows its block into the column it turns next, and a d s f turn as h j k l do
func TestATurnTurnsTheSelectedBlocksLineAPlace(t *testing.T) {
	for _, keys := range [2][4]string{{"h", "j", "k", "l"}, {"a", "d", "s", "f"}} {
		left, down, up, right := keys[0], keys[1], keys[2], keys[3]
		_, s, sq := newSquare(t)
		pressSquare(s, "5", right)
		if got := squareDigits(sq); got != "276195438" {
			t.Fatalf("5 turned right by %s: %s, want 276195438", right, got)
		}
		pressSquare(s, down)
		if got := squareDigits(sq); got != "278196435" {
			t.Fatalf("then turned down by %s: %s, want 278196435", down, got)
		}
		pressSquare(s, up, left)
		if got := squareDigits(sq); got != "276951438" {
			t.Fatalf("then back up by %s and left by %s: %s, want 276951438", up, left, got)
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

// TestALineFiresItsKindAgainEachCooldownWhileItHolds: 1 turned right and 3 left
// stand the kinds down the columns, each firing at once and again as its cooldown
// runs out, a turn away and back inside it firing nothing. Each fire of the defense
// rewards its cursor's boost as many times as its hold is of a kill's and blows one
// gust out from the square's middle, and each of the whirl turns one wind round it.
func TestALineFiresItsKindAgainEachCooldownWhileItHolds(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	pressSquare(s, "1", "l", "3", "h")
	down := []component.SquareKind{component.SquareAttack, component.SquareDefense, component.SquareWhirl}
	events := slices.Clone(w.Resources.Event.Queue.Consume())
	if got := squareFired(events); !slices.Equal(got, down) {
		t.Fatalf("fired %v, want %v down the columns", got, down)
	}
	rewards, gusts, whirls := 0, 0, 0
	cx, cy := sq.Center()
	for _, ev := range events {
		if p, ok := ev.Payload.(*event.BoostRewardPayload); ok && p.Entity == cursor {
			rewards++
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
	if gusts != 1 || whirls != 1 {
		t.Fatalf("the defense blew %d gusts out from the square's middle and the whirl turned %d winds round it, want one each", gusts, whirls)
	}
	if want := int(parameter.SquareBoostDuration / parameter.BoostBaseDuration); rewards != want {
		t.Fatalf("the defense rewarded its cursor's boost %d times, want %d", rewards, want)
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
		if again[kind] != ticks(spec.Cooldown) {
			t.Errorf("%s fired again at tick %d, want %d", spec.Name, again[kind], ticks(spec.Cooldown))
		}
	}
}

// TestALineFacesItsThirdOfTheMap: the edges of the middle block run on across the
// map cut it in thirds, each line facing the gateways in its own: a side's row or
// column the three along that side, the middle row and column the two at its ends
func TestALineFacesItsThirdOfTheMap(t *testing.T) {
	w, _, sq := newSquare(t)
	right, bottom := w.Resources.Config.MapWidth-1, w.Resources.Config.MapHeight-1
	cx, cy := sq.Center()
	for _, c := range []struct {
		name  string
		x, y  int
		lines []int
	}{
		{"top left", 0, 0, []int{0, 3}}, {"top", cx, 0, []int{0, 4}}, {"top right", right, 0, []int{0, 5}},
		{"right", right, cy, []int{1, 5}}, {"bottom right", right, bottom, []int{2, 5}},
		{"bottom", cx, bottom, []int{2, 4}}, {"bottom left", 0, bottom, []int{2, 3}}, {"left", 0, cy, []int{1, 3}},
	} {
		var got []int
		for l := range component.SquareLines {
			if sq.Faces(l, c.x, c.y) {
				got = append(got, l)
			}
		}
		if !slices.Equal(got, c.lines) {
			t.Errorf("the %s gateway is faced by lines %v, want %v", c.name, got, c.lines)
		}
	}
}

// TestAnOrbStandsOnTheSideItsLineFaces: a side's row or column stands its orb at the
// middle of that side of the outer orbit, the middle row and column one at each end
func TestAnOrbStandsOnTheSideItsLineFaces(t *testing.T) {
	w, _, sq := newSquare(t)
	aspect := w.Resources.Config.CellAspect
	cx, cy := sq.Center()
	reach := sq.ShieldRadius(aspect) + parameter.SquareOrbGap
	rx, ry := int(math.Round(reach)), int(math.Round(aspect.FromCircular(reach)))
	top, bottom := vmath.Point{X: cx, Y: cy - ry}, vmath.Point{X: cx, Y: cy + ry}
	left, right := vmath.Point{X: cx - rx, Y: cy}, vmath.Point{X: cx + rx, Y: cy}
	for l, want := range [component.SquareLines][]vmath.Point{
		{top}, {left, right}, {bottom}, {left}, {top, bottom}, {right},
	} {
		cells, n := sq.Orbs(l, aspect)
		if !slices.Equal(cells[:n], want) {
			t.Errorf("line %d stands its orbs at %v, want %v", l, cells[:n], want)
		}
	}
}

// TestASquareSizesItsCursorsShieldToCoverIt: the cursor standing on the square
// takes a shield reaching past the square's corners, its orbs' orbit with it, and
// banks no more boost than the square's cap however many kills reward it
func TestASquareSizesItsCursorsShieldToCoverIt(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	stepSquare(w, s)
	boosts := ready(NewBoostSystem(w)).(*BoostSystem)
	for range 10 {
		boosts.HandleEvent(event.GameEvent{Type: event.EventBoostReward, Payload: &event.BoostRewardPayload{Entity: cursor}})
	}
	if boost, _ := w.Components.Boost.GetComponent(cursor); !boost.Active || boost.Remaining != parameter.SquareBoostCap {
		t.Fatalf("ten rewards bank %v of boost under the square, want its cap of %v", boost.Remaining, parameter.SquareBoostCap)
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
	pressSquare(s, "1", "l", "3", "h")
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
// defense's fire sheds the share of SquareMagicDust it holds as dust and empties it
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

	// 1 turned right and 3 left stand the kinds down the columns, the defense firing
	shed := func() (dust int) {
		w.Resources.Event.Queue.Consume()
		pressSquare(s, "1", "l", "3", "h")
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if batch, ok := ev.Payload.(*event.BatchPayload[event.ParticleSpawnEntry]); ok {
				dust += len(batch.Entries)
			}
		}
		return dust
	}
	if dust := shed(); dust != parameter.SquareMagicDust || sq.Magic != 0 {
		t.Fatalf("a defense on a full meter shed %d dust and left %v, want %d and none", dust, sq.Magic, parameter.SquareMagicDust)
	}
	w, s, sq = newSquare(t)
	sq.Magic = parameter.SquareMagicFull / 4
	if dust := shed(); dust != parameter.SquareMagicDust/4 || sq.Magic != 0 {
		t.Fatalf("a defense on a quarter meter shed %d dust and left %v, want %d and none", dust, sq.Magic, parameter.SquareMagicDust/4)
	}
}
