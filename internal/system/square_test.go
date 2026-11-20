package system

import (
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
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

// TestAnAlignmentFiresItsKindOnceUntilItRecharges: 1 turned right and 3 left stand
// the kinds down the columns, each firing once: the strike and the stun land on a
// swarm, the hold halts the gateways and the shield wards every cell and pushes the
// swarm it covers. Turned away and back while they recharge, nothing fires.
func TestAnAlignmentFiresItsKindOnceUntilItRecharges(t *testing.T) {
	w, s, sq := newSquare(t)
	width, _ := sq.Size()
	swarm := standSwarm(w, sq.X+width/2, sq.Y-2)
	pressSquare(s, "1", "l", "3", "h")

	events := w.Resources.Event.Queue.Consume()
	var fired, landed []int
	for _, ev := range events {
		switch p := ev.Payload.(type) {
		case *event.SquareAlignedPayload:
			fired = append(fired, int(p.Kind))
		case *event.CombatAttackAreaRequestPayload:
			if p.TargetEntity == swarm {
				landed = append(landed, int(p.AttackType))
			}
		}
	}
	if want := []int{int(component.SquareAttack), int(component.SquareDefense), int(component.SquareHold)}; !slices.Equal(fired, want) {
		t.Fatalf("fired %v, want %v down the columns", fired, want)
	}
	if want := []int{int(component.CombatAttackStrike), int(component.CombatAttackHold)}; !slices.Equal(landed, want) {
		t.Fatalf("landed %v on the swarm, want %v", landed, want)
	}
	if countEvents(events, event.EventGatewayHoldRequest) != 1 || countEvents(events, event.EventLightningSpawnRequest) != 1 {
		t.Fatal("the hold halts no gateway or the strike draws no bolt")
	}
	for _, block := range sq.Places {
		header, _ := w.Components.Header.GetPtr(block)
		for _, m := range header.MemberEntries {
			if c, _ := w.Components.Combat.GetPtr(m.Entity); c.Status[component.StatusWard].Remaining != parameter.SquareShieldDuration {
				t.Fatal("a cell is unwarded under the shield")
			}
		}
	}

	pushed := false
	for _, ev := range stepSquare(w, s) {
		if p, ok := ev.Payload.(*event.CombatAttackAreaRequestPayload); ok && p.TargetEntity == swarm {
			pushed = pushed || p.AttackType == component.CombatAttackShield
		}
	}
	if !pushed {
		t.Fatal("the shield does not push the swarm it covers")
	}

	pressSquare(s, "l", "h")
	if n := countEvents(w.Resources.Event.Queue.Consume(), event.EventSquareAligned); n != 0 {
		t.Fatalf("%d kinds fired again while recharging", n)
	}
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
