package engine

import (
	"testing"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
)

// TestADeathTheAuthorityNeverIssuedIsDropped: an install restores the authority's
// shared allocator, so an id past it existed only on this instance and will be
// issued again to another entity. Its predicted death is dropped, not paid.
func TestADeathTheAuthorityNeverIssuedIsDropped(t *testing.T) {
	w := NewWorld()
	NewGameContextWithClock(w, 80, 24, NewManualClock())
	authority := NewWorld()
	died := authority.CreateEntity(core.DomainShared)
	phantom := core.MakeEntity(core.DomainShared, died.ID()+1)

	w.recordPrediction(event.EventSpeciesKilled, &event.SpeciesKilledPayload{Entity: died})
	w.recordPrediction(event.EventSpeciesKilled, &event.SpeciesKilledPayload{Entity: phantom})
	w.ConfirmPredictedDeaths(0, authority)

	var confirmed []core.Entity
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if p, ok := ev.Payload.(*event.SpeciesKilledPayload); ok && ev.Type == event.EventSpeciesKillConfirmed {
			confirmed = append(confirmed, p.Entity)
		}
	}
	if len(confirmed) != 1 || confirmed[0] != died {
		t.Fatalf("confirmed %v, want only %v", confirmed, died)
	}
	if len(w.predicted) != 0 {
		t.Fatalf("the ledger still holds %d entries", len(w.predicted))
	}
}
