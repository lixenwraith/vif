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
	"github.com/lixenwraith/vif/pkg/position"
)

// newGatewayWorld is an 80 by 40 terminal's world with its gateway system
func newGatewayWorld() (*engine.World, *GatewaySystem) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	return w, ready(NewGatewaySystem(w)).(*GatewaySystem)
}

// standGateway asks for a gateway of species every interval on a new anchor at (x, y)
func standGateway(w *engine.World, s *GatewaySystem, x, y int, species component.SpeciesType, interval time.Duration) core.Entity {
	anchor := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(anchor, component.PositionComponent{X: x, Y: y})
	armGateway(s, anchor, species, interval)
	return anchor
}

func armGateway(s *GatewaySystem, anchor core.Entity, species component.SpeciesType, interval time.Duration) {
	s.HandleEvent(event.GameEvent{Type: event.EventGatewaySpawnRequest, Payload: &event.GatewaySpawnRequestPayload{
		AnchorEntity: anchor, Species: species, BaseIntervalMs: int(interval / time.Millisecond),
	}})
}

// stepGateways runs one tick of the gateways and returns what they asked for
func stepGateways(w *engine.World, s *GatewaySystem) []event.GameEvent {
	w.Resources.Time.Update(engine.SimTime(0, parameter.GameUpdateInterval),
		engine.SimTime(0, parameter.GameUpdateInterval), parameter.GameUpdateInterval)
	s.Update()
	return slices.Clone(w.Resources.Event.Queue.Consume())
}

// TestARequestForAnAnchorsGatewayRearmsIt: a second request on an anchor leaves it
// one gateway, of the species and pace asked last, with what it had accumulated
func TestARequestForAnAnchorsGatewayRearmsIt(t *testing.T) {
	w, gateways := newGatewayWorld()
	anchor := standGateway(w, gateways, 4, 1, component.SpeciesSwarm, time.Hour)
	if n := countEvents(stepGateways(w, gateways), event.EventSwarmSpawnRequest); n != 0 {
		t.Fatalf("%d swarms asked for inside the hour", n)
	}

	armGateway(gateways, anchor, component.SpeciesSnake, parameter.GameUpdateInterval)
	if n := w.Components.Gateway.CountEntities(); n != 1 {
		t.Fatalf("%d gateways on the anchor, want the one re-armed", n)
	}
	events := stepGateways(w, gateways)
	if countEvents(events, event.EventSnakeSpawnRequest) != 1 || countEvents(events, event.EventSwarmSpawnRequest) != 0 {
		t.Fatal("the re-armed gateway did not ask for its new species at its new pace")
	}
}

// TestAGatewaysDataSpeciesKeepsIt: a gateway asks for a data species by its land
// request, and the body that lands names the gateway that sent it
func TestAGatewaysDataSpeciesKeepsIt(t *testing.T) {
	ctx, behaviors, _ := newWardenWorld(t, wardenX+20, wardenY)
	w := ctx.World
	gateways := ready(NewGatewaySystem(w)).(*GatewaySystem)
	standGateway(w, gateways, 60, 20, w.Resources.Behaviors[0].Species, parameter.GameUpdateInterval)
	for _, ev := range stepGateways(w, gateways) {
		behaviors.HandleEvent(ev)
	}
	sent := 0
	w.Components.Behavior.Each(func(_ core.Entity, b *component.BehaviorComponent) bool {
		if w.Components.Gateway.HasEntity(b.Gateway) {
			sent++
		}
		return true
	})
	if sent != 1 {
		t.Fatalf("%d wardens keep the gateway that sent them, want one", sent)
	}
}

// TestAGatewaySendsOneOfASpeciesWhenAsked: a send request has the gateway on its
// anchor ask for one of the species it names at once, a hold notwithstanding, and
// the swarm asked for names that gateway
func TestAGatewaySendsOneOfASpeciesWhenAsked(t *testing.T) {
	w, gateways := newGatewayWorld()
	anchor := standGateway(w, gateways, 4, 1, component.SpeciesSnake, time.Hour)
	gateways.HandleEvent(event.GameEvent{Type: event.EventGatewayHoldRequest, Payload: &event.GatewayHoldRequestPayload{DurationMs: 1000}})
	gateways.HandleEvent(event.GameEvent{Type: event.EventGatewaySendRequest, Payload: &event.GatewaySendRequestPayload{
		AnchorEntity: anchor, Species: component.SpeciesSwarm,
	}})
	var sent []*event.SwarmSpawnRequestPayload
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.SwarmSpawnRequestPayload); ok {
			sent = append(sent, p)
		}
	}
	if len(sent) != 1 || sent[0].X != 4 || sent[0].Y != 1 || !w.Components.Gateway.HasEntity(sent[0].Gateway) {
		t.Fatalf("the send asked for %d swarms, want one at the anchor naming its gateway", len(sent))
	}
}

// TestAnInertPylonIsNoTarget: a pylon stood inert is aimed at by no finder, while
// one stood plain is the nearest target
func TestAnInertPylonIsNoTarget(t *testing.T) {
	for _, inert := range []bool{false, true} {
		w, _ := newGatewayWorld()
		ready(NewPylonSystem(w)).(*PylonSystem).HandleEvent(event.GameEvent{Type: event.EventPylonSpawnRequest,
			Payload: &event.PylonSpawnRequestPayload{X: 20, Y: 10, RadiusX: 2, RadiusY: 1, Inert: inert}})
		if w.Components.Pylon.CountEntities() != 1 {
			t.Fatal("no pylon stood")
		}
		if aimed := len(FindNearestTargets(w, 30, 10, 1, position.ScopeAll, 0)) > 0; aimed == inert {
			t.Fatalf("a pylon stood inert=%t is aimed at: %t", inert, aimed)
		}
	}
}
