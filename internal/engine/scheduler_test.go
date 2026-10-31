package engine

import (
	"slices"
	"testing"
	"time"

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
