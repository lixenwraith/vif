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
// rewards its cursor's boost as many times as its hold is of a kill's, and blows
// one gust out from the square's middle.
func TestALineFiresItsKindAgainEachCooldownWhileItHolds(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	pressSquare(s, "1", "l", "3", "h")
	down := []component.SquareKind{component.SquareAttack, component.SquareDefense, component.SquareHold}
	events := slices.Clone(w.Resources.Event.Queue.Consume())
	if got := squareFired(events); !slices.Equal(got, down) {
		t.Fatalf("fired %v, want %v down the columns", got, down)
	}
	rewards, gusts := 0, 0
	cx, cy := sq.Center()
	for _, ev := range events {
		if p, ok := ev.Payload.(*event.BoostRewardPayload); ok && p.Entity == cursor {
			rewards++
		}
		if p, ok := ev.Payload.(*event.WindStartPayload); ok && p.Radial && p.X == cx && p.Y == cy && p.Force == parameter.SquareWindForce {
			gusts++
		}
	}
	if gusts != 1 {
		t.Fatalf("the defense blew %d gusts out from the square's middle, want one", gusts)
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

// TestALineLandsOnTheGatewaysItFacesAndTheBodiesTheySent: of swarms sent from the
// top left, the top and the right, attack in the left column strikes the top left's
// alone, and hold in the right column stuns the right's alone and holds that gateway
// alone. Each hit is its cursor's, with a bolt in the kind's color from the line's
// orb to each body, and for the hold to each gateway.
func TestALineLandsOnTheGatewaysItFacesAndTheBodiesTheySent(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	gateways := ready(NewGatewaySystem(w)).(*GatewaySystem)
	swarms := ready(NewSwarmSystem(w)).(*SwarmSystem)
	cx, cy := sq.Center()
	var anchors [3]core.Entity
	for i, at := range [3]vmath.Point{{X: 4, Y: 1}, {X: cx, Y: 1}, {X: w.Resources.Config.MapWidth - 4, Y: cy}} {
		anchors[i] = standGateway(w, gateways, at.X, at.Y, component.SpeciesSwarm, parameter.GameUpdateInterval)
	}
	for _, ev := range stepGateways(w, gateways) {
		swarms.HandleEvent(ev)
	}
	var sent [3]core.Entity
	w.Components.Swarm.Each(func(swarm core.Entity, sw *component.SwarmComponent) bool {
		if g, ok := w.Components.Gateway.GetPtr(sw.Gateway); ok {
			sent[slices.Index(anchors[:], g.AnchorEntity)] = swarm
		}
		return true
	})
	if slices.Contains(sent[:], 0) {
		t.Fatalf("the gateways sent %v, want a swarm each that keeps its gateway", sent)
	}
	w.Resources.Event.Queue.Consume()

	pressSquare(s, "1", "l", "3", "h")
	events := slices.Clone(w.Resources.Event.Queue.Consume())
	var struck, stunned, held []core.Entity
	type bolt struct {
		tint   component.LightningColorType
		from   vmath.Point
		tx, ty int
	}
	var bolts []bolt
	for _, ev := range events {
		switch p := ev.Payload.(type) {
		case *event.CombatAttackAreaRequestPayload:
			if p.OwnerEntity != cursor || p.OriginEntity != cursor {
				t.Fatalf("a %s lands as %d's from %d, want its cursor's", component.CombatAttackNames[p.AttackType], p.OwnerEntity, p.OriginEntity)
			}
			if p.AttackType == component.CombatAttackStrike {
				struck = append(struck, p.TargetEntity)
			} else if p.AttackType == component.CombatAttackHold {
				stunned = append(stunned, p.TargetEntity)
			}
		case *event.GatewayHoldRequestPayload:
			held = append(held, p.AnchorEntity)
		case *event.LightningSpawnRequestPayload:
			bolts = append(bolts, bolt{p.ColorType, vmath.Point{X: p.OriginX, Y: p.OriginY}, p.TargetX, p.TargetY})
		}
	}
	if !slices.Equal(struck, sent[:1]) || !slices.Equal(stunned, sent[2:]) || !slices.Equal(held, anchors[2:]) {
		t.Fatalf("struck %v, stunned %v, held %v: want %v, %v and %v", struck, stunned, held, sent[:1], sent[2:], anchors[2:])
	}
	aspect := w.Resources.Config.CellAspect
	attack, _ := sq.Orbs(component.SquareSide, aspect)
	hold, _ := sq.Orbs(component.SquareLines-1, aspect)
	var want []bolt
	for _, b := range []struct {
		tint component.LightningColorType
		from vmath.Point
		e    core.Entity
	}{{component.LightningRed, attack[0], sent[0]}, {component.LightningGold, hold[0], anchors[2]}, {component.LightningGold, hold[0], sent[2]}} {
		at, _ := w.Positions.GetPosition(b.e)
		want = append(want, bolt{b.tint, b.from, at.X, at.Y})
	}
	if !slices.Equal(bolts, want) {
		t.Fatalf("bolts %v, want %v", bolts, want)
	}
}

// TestAStrikeKillsABodyWhateverItHoldsAsItsCursors: combat lands a square's strike
// as a cursor's on a swarm of any hit points, taking them all and crediting it
func TestAStrikeKillsABodyWhateverItHoldsAsItsCursors(t *testing.T) {
	w, s, sq := newSquare(t)
	cursor := w.Resources.Player.Slot(0)
	combat := ready(NewCombatSystem(w)).(*CombatSystem)
	swarm := standSwarm(w, sq.X, sq.Y-6)
	c, _ := w.Components.Combat.GetPtr(swarm)
	c.HitPoints = 100 * parameter.CombatInitialHPSwarm
	if !s.hit(&component.SquareKinds[component.SquareAttack], cursor, vmath.Point{X: sq.X, Y: sq.Y - 2}, swarm) {
		t.Fatal("the strike found no row for a cursor on a swarm")
	}
	for _, ev := range w.Resources.Event.Queue.Consume() {
		combat.HandleEvent(ev)
	}
	if c.HitPoints != 0 || c.LastDamagedBy != cursor {
		t.Fatalf("the struck swarm holds %d hit points, last hit by %d: want none, by its cursor %d", c.HitPoints, c.LastDamagedBy, cursor)
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

// TestATurnIntoAMagicStateShedsDustOnceAWait: turned out of the Lo Shu square and
// back, every row and column sums alike again and each block sheds its dust; the
// same turns inside the wait shed none, and after it they shed again
func TestATurnIntoAMagicStateShedsDustOnceAWait(t *testing.T) {
	w, s, sq := newSquare(t)
	shed := func(keys ...string) (dust int) {
		pressSquare(s, keys...)
		for _, ev := range w.Resources.Event.Queue.Consume() {
			if batch, ok := ev.Payload.(*event.BatchPayload[event.ParticleSpawnEntry]); ok {
				dust += len(batch.Entries)
			}
		}
		return dust
	}
	if dust := shed("5", "l"); dust != 0 || sq.IsMagic() {
		t.Fatalf("a turn out of the Lo Shu square shed %d dust, magic %t", dust, sq.IsMagic())
	}
	want := component.SquareBlocks * parameter.SquareMagicDust
	if dust := shed("h"); dust != want || !sq.IsMagic() {
		t.Fatalf("the turn back shed %d dust, magic %t, want %d from a magic state", dust, sq.IsMagic(), want)
	}
	if dust := shed("l", "h"); dust != 0 {
		t.Fatalf("a magic state inside the wait shed %d dust", dust)
	}
	now := engine.SimTime(0, parameter.GameUpdateInterval)
	w.Resources.Time.Update(now, now, parameter.SquareMagicCooldown)
	s.Update()
	if dust := shed("l", "h"); dust != want {
		t.Fatalf("a magic state after the wait shed %d dust, want %d", dust, want)
	}
}
