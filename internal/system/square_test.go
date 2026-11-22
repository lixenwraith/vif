package system

import (
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// newSquare lays the Lo Shu square in the middle of an 80 by 40 world
func newSquare(t *testing.T) (*engine.World, *SquareSystem, *component.SquareComponent) {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	s := ready(NewSquareSystem(w)).(*SquareSystem)
	s.HandleEvent(event.GameEvent{Type: event.EventSquareSpawnRequest, Payload: &event.SquareSpawnRequestPayload{
		X: 40, Y: 18, Layout: "276951438", Groups: []string{"213", "654", "879"}, TargetGroupID: 1,
	}})
	squares := w.Components.Square.Entities()
	if len(squares) != 1 {
		t.Fatalf("%d squares, want one", len(squares))
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
func squareDigits(w *engine.World, sq *component.SquareComponent) string {
	var out []byte
	for _, block := range sq.Places {
		b, _ := w.Components.Block.GetPtr(block)
		out = append(out, '0'+b.Digit)
	}
	return string(out)
}

// blockAt is the block whose digit is d
func blockAt(w *engine.World, sq *component.SquareComponent, d uint8) core.Entity {
	for _, block := range sq.Places {
		if b, _ := w.Components.Block.GetPtr(block); b.Digit == d {
			return block
		}
	}
	return 0
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
// follows its block into the column it turns next, and every cell moves with its
// block
func TestATurnTurnsTheSelectedBlocksLineAPlace(t *testing.T) {
	w, s, sq := newSquare(t)
	pressSquare(s, "5", "l")
	if got := squareDigits(w, sq); got != "276195438" {
		t.Fatalf("5 turned right: %s, want 276195438", got)
	}
	pressSquare(s, "j")
	if got := squareDigits(w, sq); got != "278196435" {
		t.Fatalf("then turned down: %s, want 278196435", got)
	}
	for p, block := range sq.Places {
		x, y := sq.Place(p)
		at, _ := w.Positions.GetPosition(block)
		header, _ := w.Components.Header.GetPtr(block)
		if at.X != x+sq.Width/2 || at.Y != y+sq.Height/2 {
			t.Fatalf("place %d's block stands at %v, not its middle", p, at)
		}
		for _, m := range header.MemberEntries {
			if pos, _ := w.Positions.GetPosition(m.Entity); pos.X != at.X+m.OffsetX || pos.Y != at.Y+m.OffsetY {
				t.Fatalf("place %d's cell at %v, off its block", p, pos)
			}
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
// runs out, a turn away and back inside it firing nothing. The shield wards every
// cell for its duration and pushes the swarm it covers only while it holds.
func TestALineFiresItsKindAgainEachCooldownWhileItHolds(t *testing.T) {
	w, s, sq := newSquare(t)
	width, _ := sq.Size()
	swarm := standSwarm(w, sq.X+width/2, sq.Y-2)
	pressSquare(s, "1", "l", "3", "h")
	down := []component.SquareKind{component.SquareAttack, component.SquareDefense, component.SquareHold}
	if got := squareFired(w.Resources.Event.Queue.Consume()); !slices.Equal(got, down) {
		t.Fatalf("fired %v, want %v down the columns", got, down)
	}
	for _, block := range sq.Places {
		header, _ := w.Components.Header.GetPtr(block)
		for _, m := range header.MemberEntries {
			if c, _ := w.Components.Combat.GetPtr(m.Entity); c.Status[component.StatusWard].Remaining != parameter.SquareShieldDuration {
				t.Fatal("a cell is unwarded under the shield")
			}
		}
	}
	pressSquare(s, "l", "h")
	if got := squareFired(w.Resources.Event.Queue.Consume()); len(got) != 0 {
		t.Fatalf("%v fired again inside the cooldown", got)
	}

	ticks := func(d time.Duration) int { return int(d / parameter.GameUpdateInterval) }
	var again [component.SquareKindCount]int
	last, pushes := 0, 0
	for _, spec := range component.SquareKinds {
		last = max(last, ticks(spec.Cooldown))
	}
	for tick := 1; tick <= last; tick++ {
		events := stepSquare(w, s)
		for _, kind := range squareFired(events) {
			if again[kind] == 0 {
				again[kind] = tick
			}
		}
		for _, ev := range events {
			p, ok := ev.Payload.(*event.CombatAttackAreaRequestPayload)
			if ok && p.TargetEntity == swarm && p.AttackType == component.CombatAttackShield && tick <= ticks(parameter.SquareShieldCooldown) {
				pushes++
			}
		}
	}
	for kind, spec := range component.SquareKinds {
		if again[kind] != ticks(spec.Cooldown) {
			t.Errorf("%s fired again at tick %d, want %d", spec.Name, again[kind], ticks(spec.Cooldown))
		}
	}
	if pushes != ticks(parameter.SquareShieldDuration) {
		t.Fatalf("the shield pushed %d ticks of its cooldown, want %d", pushes, ticks(parameter.SquareShieldDuration))
	}
}

// TestALineFacesItsThirdOfTheMap: the edges of the middle block run on across the
// map cut it in thirds, each line facing the gateways in its own: a side's row or
// column the three along that side, the middle row and column the two at its ends
func TestALineFacesItsThirdOfTheMap(t *testing.T) {
	w, _, sq := newSquare(t)
	right, bottom := w.Resources.Config.MapWidth-1, w.Resources.Config.MapHeight-1
	width, height := sq.Size()
	cx, cy := sq.X+width/2, sq.Y+height/2
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

// TestALineLandsOnTheGatewaysItFacesAndTheSwarmsTheySent: gateways at the top left,
// the top and the right each send a swarm, which keeps its gateway. With the kinds
// down the columns, attack in the left column strikes the top left's swarm alone and
// hold in the right column stuns the right's alone and holds that gateway alone, a
// bolt in the kind's color flying to each gateway and swarm it lands on.
func TestALineLandsOnTheGatewaysItFacesAndTheSwarmsTheySent(t *testing.T) {
	w, s, sq := newSquare(t)
	gateways := ready(NewGatewaySystem(w)).(*GatewaySystem)
	swarms := ready(NewSwarmSystem(w)).(*SwarmSystem)
	width, height := sq.Size()
	var anchors [3]core.Entity
	for i, at := range [3]vmath.Point{{X: 4, Y: 1}, {X: sq.X + width/2, Y: 1}, {X: w.Resources.Config.MapWidth - 4, Y: sq.Y + height/2}} {
		anchors[i] = w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(anchors[i], component.PositionComponent{X: at.X, Y: at.Y})
		gateways.HandleEvent(event.GameEvent{Type: event.EventGatewaySpawnRequest, Payload: &event.GatewaySpawnRequestPayload{
			AnchorEntity: anchors[i], Species: component.SpeciesSwarm, GroupID: 1, BaseIntervalMs: 1,
		}})
	}
	w.Resources.Time.Update(engine.SimTime(0, parameter.GameUpdateInterval),
		engine.SimTime(0, parameter.GameUpdateInterval), parameter.GameUpdateInterval)
	gateways.Update()
	for _, ev := range w.Resources.Event.Queue.Consume() {
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
		tint component.LightningColorType
		x, y int
	}
	var bolts []bolt
	for _, ev := range events {
		switch p := ev.Payload.(type) {
		case *event.CombatAttackAreaRequestPayload:
			if p.AttackType == component.CombatAttackStrike {
				struck = append(struck, p.TargetEntity)
			} else if p.AttackType == component.CombatAttackHold {
				stunned = append(stunned, p.TargetEntity)
			}
		case *event.GatewayHoldRequestPayload:
			held = append(held, p.AnchorEntity)
		case *event.LightningSpawnRequestPayload:
			bolts = append(bolts, bolt{p.ColorType, p.TargetX, p.TargetY})
		}
	}
	if !slices.Equal(struck, sent[:1]) || !slices.Equal(stunned, sent[2:]) || !slices.Equal(held, anchors[2:]) {
		t.Fatalf("struck %v, stunned %v, held %v: want %v, %v and %v", struck, stunned, held, sent[:1], sent[2:], anchors[2:])
	}
	var want []bolt
	for _, b := range []struct {
		tint component.LightningColorType
		e    core.Entity
	}{{component.LightningRed, anchors[0]}, {component.LightningRed, sent[0]}, {component.LightningGold, anchors[2]}, {component.LightningGold, sent[2]}} {
		at, _ := w.Positions.GetPosition(b.e)
		want = append(want, bolt{b.tint, at.X, at.Y})
	}
	if !slices.Equal(bolts, want) {
		t.Fatalf("bolts %v, want %v", bolts, want)
	}

	for _, ev := range events {
		gateways.HandleEvent(ev)
	}
	w.Components.Gateway.Each(func(_ core.Entity, g *component.GatewayComponent) bool {
		if (g.Held > 0) != (g.AnchorEntity == anchors[2]) {
			t.Fatalf("the gateway on %d holds for %v", g.AnchorEntity, g.Held)
		}
		return true
	})
}

// TestAFallenBlockStillAlignsButHoldsNoAnchor: nine standing blocks fill a target
// group but the middle one; a block worn through falls, told with the count still
// standing, gives up its anchor to the middle block and still aligns
func TestAFallenBlockStillAlignsButHoldsNoAnchor(t *testing.T) {
	w, s, sq := newSquare(t)
	stepSquare(w, s)
	middle, _ := w.Components.Block.GetPtr(sq.Places[component.SquareBlocks/2])
	if n := w.Components.TargetAnchor.CountEntities(); n != parameter.MaxTargetsPerGroup || middle.Anchor != 0 {
		t.Fatalf("%d anchors, the middle block's %d: want %d and none", n, middle.Anchor, parameter.MaxTargetsPerGroup)
	}

	two := blockAt(w, sq, 2)
	header, _ := w.Components.Header.GetPtr(two)
	for _, m := range header.MemberEntries {
		c, _ := w.Components.Combat.GetPtr(m.Entity)
		c.HitPoints = 0
	}
	events := stepSquare(w, s)
	if n := countEvents(events, event.EventCompositeMemberDestroyed); n != len(header.MemberEntries) {
		t.Fatalf("%d cells ended, want %d", n, len(header.MemberEntries))
	}
	var fallen []event.SquareBlockFallenPayload
	for _, ev := range events {
		if p, ok := ev.Payload.(*event.SquareBlockFallenPayload); ok {
			fallen = append(fallen, *p)
		}
	}
	if len(fallen) != 1 || fallen[0] != (event.SquareBlockFallenPayload{Digit: 2, Remaining: 8}) {
		t.Fatalf("falls told %v, want block 2 with 8 standing", fallen)
	}
	if b, _ := w.Components.Block.GetPtr(two); b.Anchor != 0 || middle.Anchor == 0 {
		t.Fatal("the fallen block keeps its anchor, or the middle block takes none")
	}

	pressSquare(s, "1", "l", "3", "h")
	if n := countEvents(w.Resources.Event.Queue.Consume(), event.EventSquareAligned); n != int(component.SquareKindCount) {
		t.Fatalf("%d kinds fired with block 2 fallen, want %d", n, component.SquareKindCount)
	}
}

// TestABodyWearsTheBlockCellsItCovers: a swarm on a block's corner lands one
// contact on that block, naming each cell it covers once, its header's included
func TestABodyWearsTheBlockCellsItCovers(t *testing.T) {
	w, s, sq := newSquare(t)
	x, y := sq.Place(0)
	foot := component.Footprints[component.SpeciesSwarm]
	swarm := standSwarm(w, x+foot.HeaderX, y+foot.HeaderY)

	var contacts []*event.CombatAttackAreaRequestPayload
	for _, ev := range stepSquare(w, s) {
		if p, ok := ev.Payload.(*event.CombatAttackAreaRequestPayload); ok && p.AttackType == component.CombatAttackContact {
			contacts = append(contacts, p)
		}
	}
	if len(contacts) != 1 || contacts[0].OriginEntity != swarm || contacts[0].TargetEntity != sq.Places[0] {
		t.Fatalf("%d contacts, want one from the swarm on the corner block", len(contacts))
	}
	header, _ := w.Components.Header.GetPtr(sq.Places[0])
	var covered []core.Entity
	for _, m := range header.MemberEntries {
		if pos, _ := w.Positions.GetPosition(m.Entity); pos.Y < y+foot.Height && pos.X < x+foot.Width {
			covered = append(covered, m.Entity)
		}
	}
	if !slices.Equal(contacts[0].HitEntities, covered) {
		t.Fatalf("contact names %v, want the covered cells %v", contacts[0].HitEntities, covered)
	}
}
