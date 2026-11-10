package engine

import (
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
)

// orderProbe is a system whose handler records how many Updates ran before each
// dispatch, and whose first Update pushes an event of its own
type orderProbe struct {
	w       *World
	updates int
	seen    []int
}

func (p *orderProbe) Init()         {}
func (p *orderProbe) Priority() int { return 0 }
func (p *orderProbe) Name() string  { return "probe" }
func (p *orderProbe) EventTypes() []event.EventType {
	return []event.EventType{event.EventMetaStatusMessageRequest}
}
func (p *orderProbe) HandleEvent(ev event.GameEvent) { p.seen = append(p.seen, p.updates) }
func (p *orderProbe) Update() {
	p.updates++
	if p.updates == 1 {
		p.w.PushLocal(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{Message: "system"})
	}
}

// TestATickSettlesWhatItAppliesAfterItsSystems: what was pushed since the last tick
// settles before the tick opens, and what the tick applies, a crossing due at it or an
// event a system pushed, after every system's Update, so a clock it starts first
// counts on the next tick; the body is settle group 0 and what follows is group 1.
func TestATickSettlesWhatItAppliesAfterItsSystems(t *testing.T) {
	t.Parallel()
	w := NewWorld()
	ctx := NewGameContextWithClock(w, 80, 24, NewManualClock())
	probe := &orderProbe{w: w}
	w.AddSystem(probe, SystemProfile{})
	s, _, _ := NewScheduler(w, ctx.TimeCtl, 50*time.Millisecond, nil)
	s.RegisterEventHandler(probe)

	queue := w.Resources.Event.Queue
	between := event.GameEvent{Type: event.EventMetaStatusMessageRequest,
		Payload: &event.MetaStatusMessagePayload{Message: "between"}, Origin: event.OriginDebug, Domain: core.DomainPlayer}
	queue.PushReady(between)
	due := between
	due.Payload = &event.MetaStatusMessagePayload{Message: "due"}
	queue.Defer(due)
	if st := queue.Stamp(); st != (event.Stamp{Boundary: 1}) {
		t.Fatalf("before the first tick the stamp is %+v, want run 0 tick 0 group 1", st)
	}

	s.RunTicks(1)

	if want := []int{0, 1, 1}; !slices.Equal(probe.seen, want) {
		t.Fatalf("dispatches saw %v Updates before them, want %v: the between-tick one before, the due and the system's after", probe.seen, want)
	}
	if st := queue.Stamp(); st != (event.Stamp{Tick: 1, Boundary: 1}) {
		t.Fatalf("after the tick the stamp is %+v, want tick 1 group 1", st)
	}
}

// crossingSink is a session barrier that takes every crossing the queue offers it
type crossingSink struct{ crossed []event.GameEvent }

func (c *crossingSink) Receive(uint64) bool { return false }
func (c *crossingSink) Cross(ev event.GameEvent) bool {
	c.crossed = append(c.crossed, ev)
	return true
}
func (c *crossingSink) CrossingApplied(uint64) {}
func (c *crossingSink) Flush(uint64)           {}

// pushProbe is a system of a declared profile making one push, from its Update or,
// once a status message dispatches, from its handler
type pushProbe struct {
	w      *World
	push   func(*World)
	handle bool
}

func (p *pushProbe) Init()         {}
func (p *pushProbe) Priority() int { return 0 }
func (p *pushProbe) Name() string  { return "push" }
func (p *pushProbe) EventTypes() []event.EventType {
	return []event.EventType{event.EventMetaStatusMessageRequest}
}
func (p *pushProbe) HandleEvent(event.GameEvent) {
	if p.handle {
		p.push(p.w)
	}
}
func (p *pushProbe) Update() {
	if !p.handle {
		p.push(p.w)
	}
}

// TestAPushCrossesByItsProducersDomain: a push reaches the wire when the system that
// made it, in its Update or its handler, is player-profile and it touches shared
// state, whatever API made it; a shared or dual system's crosses only by its explicit
// producer tag, and a peer's artifact, a chain follow-up, a shared-class event, a
// push of another origin and one after the systems ran never do
func TestAPushCrossesByItsProducersDomain(t *testing.T) {
	const (
		inUpdate = iota
		inHandler
		afterTick
	)
	shared := func(w *World) core.Entity { return w.CreateEntity(core.DomainShared) }
	player := func(w *World) core.Entity { return w.CreateEntity(core.DomainPlayer) }
	death := func(of func(*World) core.Entity) func(*World) {
		return func(w *World) { event.EmitDeath(w.Resources.Event.Queue, component.DeathSilent, of(w)) }
	}
	hit := func(chain uint8) func(*World) {
		return func(w *World) {
			w.PushEventDomain(event.EventCombatAttackDirectRequest,
				&event.CombatAttackDirectRequestPayload{TargetEntity: shared(w), ChainDepth: chain}, core.DomainShared)
		}
	}
	defeat := func(push func(w *World, p *event.DrainDefeatedPayload)) func(*World) {
		return func(w *World) { push(w, &event.DrainDefeatedPayload{}) }
	}
	ambient := defeat(func(w *World, p *event.DrainDefeatedPayload) { w.PushEvent(event.EventDrainDefeated, p) })
	for _, c := range []struct {
		name    string
		profile SystemDomain
		when    int
		push    func(*World)
		crosses bool
	}{
		{"player bus request", SystemPlayer, inUpdate, ambient, true},
		{"shared bus request", SystemShared, inUpdate, ambient, false},
		{"shared PushCrossing", SystemShared, inUpdate, defeat(func(w *World, p *event.DrainDefeatedPayload) {
			w.PushCrossing(event.EventDrainDefeated, p)
		}), true},
		{"dual bus request", SystemDual, inUpdate, ambient, false},
		{"dual bus request tagged player", SystemDual, inUpdate, defeat(func(w *World, p *event.DrainDefeatedPayload) {
			w.PushEventDomain(event.EventDrainDefeated, p, core.DomainPlayer)
		}), true},
		{"player death of a shared entity", SystemPlayer, inUpdate, death(shared), true},
		{"player death of a player entity", SystemPlayer, inUpdate, death(player), false},
		{"shared death of a shared entity", SystemShared, inUpdate, death(shared), false},
		{"dual death of a shared entity", SystemDual, inUpdate, death(shared), false},
		{"player handler death of a shared entity", SystemPlayer, inHandler, death(shared), true},
		{"shared handler death of a shared entity", SystemShared, inHandler, death(shared), false},
		{"player hit on a shared target", SystemPlayer, inUpdate, hit(0), true},
		{"player chain follow-up", SystemPlayer, inUpdate, hit(1), false},
		{"player shared-class event", SystemPlayer, inUpdate, func(w *World) {
			w.PushEvent(event.EventCursorMoved, &event.CursorMovedPayload{})
		}, false},
		{"player PushLocal of a stamped event", SystemPlayer, inUpdate, func(w *World) {
			w.PushLocal(event.EventSpeciesKilled, &event.SpeciesKilledPayload{})
		}, false},
		{"player push of another origin", SystemPlayer, inUpdate, defeat(func(w *World, p *event.DrainDefeatedPayload) {
			w.PushEventOrigin(event.EventDrainDefeated, p, event.OriginDebug)
		}), false},
		{"player push of a peer's artifact", SystemPlayer, inUpdate, defeat(func(w *World, p *event.DrainDefeatedPayload) {
			w.PushEventFull(event.EventDrainDefeated, p, event.OriginNetwork, core.DomainPlayer)
		}), false},
		{"death of a shared entity after a player system ran", SystemPlayer, afterTick, death(shared), false},
		{"session request tagged player outside any system", SystemPlayer, afterTick, defeat(func(w *World, p *event.DrainDefeatedPayload) {
			w.PushEventFull(event.EventDrainDefeated, p, event.OriginSession, core.DomainPlayer)
		}), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := NewWorld()
			ctx := NewGameContextWithClock(w, 80, 24, NewManualClock())
			probe := &pushProbe{w: w, push: c.push, handle: c.when == inHandler}
			if c.when == afterTick {
				probe.push = func(*World) {}
			}
			w.AddSystem(probe, SystemProfile{Domain: c.profile})
			s, _, _ := NewScheduler(w, ctx.TimeCtl, 50*time.Millisecond, nil)
			s.RegisterEventHandler(probe)
			sink := &crossingSink{}
			queue := w.Resources.Event.Queue
			queue.SetWireSink(sink)
			if c.when == inHandler {
				queue.PushReady(event.GameEvent{Type: event.EventMetaStatusMessageRequest,
					Payload: &event.MetaStatusMessagePayload{}, Origin: event.OriginDebug, Domain: core.DomainPlayer})
			}
			s.RunTicks(1)
			if c.when == afterTick {
				w.RunSafe(func() { c.push(w) })
			}
			if got := len(sink.crossed) == 1; got != c.crosses || len(sink.crossed) > 1 {
				t.Fatalf("%d crossings, want crossing %t", len(sink.crossed), c.crosses)
			}
		})
	}
}
