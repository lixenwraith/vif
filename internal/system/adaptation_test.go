package system

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
)

func newAdaptationFixture(t *testing.T) *AdaptationSystem {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	return NewAdaptationSystem(w).(*AdaptationSystem)
}

// A death is scored the tick after its event, from the route its eye took, so a
// capture at the tick boundary carries both or a joiner scores nothing for it.
func TestAdaptationCaptureCarriesTrackedRoutesAndPendingDeaths(t *testing.T) {
	authority := newAdaptationFixture(t)
	authority.tracking[7] = trackedRoute{GraphID: 3, RouteID: 1, SubType: 2}
	authority.tracking[5] = trackedRoute{GraphID: 3, RouteID: 0, SubType: 1}
	authority.pendingDeaths = append(authority.pendingDeaths,
		event.SpeciesKilledPayload{Entity: 7, X: 4, Y: 5, SubType: 2})
	record, err := authority.SaveShared()
	if err != nil {
		t.Fatalf("save: %v", err)
	}

	joiner := newAdaptationFixture(t)
	if err := joiner.LoadShared(record); err != nil {
		t.Fatalf("load: %v", err)
	}
	if !maps.Equal(joiner.tracking, authority.tracking) ||
		!slices.Equal(joiner.pendingDeaths, authority.pendingDeaths) {
		t.Fatalf("installed tracking %v and deaths %v, want %v and %v",
			joiner.tracking, joiner.pendingDeaths, authority.tracking, authority.pendingDeaths)
	}
	// The record is compared as well as installed, so its bytes must not depend on
	// which instance wrote them.
	again, err := joiner.SaveShared()
	if err != nil {
		t.Fatalf("save again: %v", err)
	}
	if !bytes.Equal(again, record) {
		t.Fatalf("the joiner's record differs from the authority's:\n%s\n%s", again, record)
	}
}
