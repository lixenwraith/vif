package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
)

// TestRouteAnchorsAreShared is the condition routeAnchorID narrows an entity
// under. A player-domain anchor would collide with a shared one of the same low
// bits, and nothing downstream could tell which route graph it had been handed.
func TestRouteAnchorsAreShared(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	gateways := NewGatewaySystem(w).(*GatewaySystem)

	anchor := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(anchor, component.PositionComponent{X: 5, Y: 5})
	gateways.HandleEvent(event.GameEvent{
		Type: event.EventGatewaySpawnRequest,
		Payload: &event.GatewaySpawnRequestPayload{
			AnchorEntity: anchor, BaseIntervalMs: 500, RateMultiplier: 1,
			GroupID: 1, Species: uint8(component.SpeciesEye), UseRouteGraph: true,
		},
	})

	var found bool
	w.Components.Gateway.Each(func(e core.Entity, g *component.GatewayComponent) bool {
		found = true
		if e.Domain() != core.DomainShared {
			t.Fatalf("a route anchor was created in domain %v, which routeAnchorID cannot narrow",
				e.Domain())
		}
		if e.ID() > 1<<32 {
			t.Fatalf("route anchor id %d is past what routeAnchorID keeps", e.ID())
		}
		if g.RouteDistID != routeAnchorID(e) {
			t.Fatalf("gateway carries route id %d for anchor %d", g.RouteDistID, e)
		}
		return false
	})
	if !found {
		t.Fatal("no gateway was created; the spawn payload has moved")
	}
}

// TestDeathGoesThroughTheOrdinaryBoundary is the exemption that was deleted rather
// than named. EmitDeath used to build its record and push it straight onto the
// queue, so a death carried no producer origin while every other event did; the
// domain split is still its own — a shared system claiming cells kills occupants of
// both domains (D-12) — but the push is PushEventDomain like everything else.
func TestDeathGoesThroughTheOrdinaryBoundary(t *testing.T) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	w.Resources.Event.Queue.Consume()

	shared := w.CreateEntity(core.DomainShared)
	player := w.CreateEntity(core.DomainPlayer)
	w.EmitDeath(event.EventFlashSpawnOneRequest, shared, player)

	byDomain := map[core.Domain][]core.Entity{}
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type != event.EventDeathBatch {
			continue
		}
		p, ok := ev.Payload.(*event.DeathRequestPayload)
		if !ok {
			t.Fatalf("death payload = %T", ev.Payload)
		}
		// Simulation-internal, so never journaled: the events that caused the
		// death are, and a replay re-derives it from them.
		if ev.Origin != event.OriginSystem {
			t.Fatalf("a death record carries origin %v, want the system origin", ev.Origin)
		}
		byDomain[ev.Domain] = append(byDomain[ev.Domain], p.Entities...)
	}
	if got := byDomain[core.DomainShared]; len(got) != 1 || got[0] != shared {
		t.Fatalf("shared batch = %v, want just the shared entity", got)
	}
	if got := byDomain[core.DomainPlayer]; len(got) != 1 || got[0] != player {
		t.Fatalf("player batch = %v, want just the player entity", got)
	}
}
