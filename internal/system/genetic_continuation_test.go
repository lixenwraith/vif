package system

import (
	"encoding/json"
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/registry"
)

// newGeneticFixture is one instance's genetic system, on a world of its own. The
// two fixtures a test builds share a seed by construction — engine.NewWorld derives
// its streams from the same root — so a species registered on either produces the
// same configuration, which is what lets a capture carry the declaration rather
// than the configuration.
func newGeneticFixture(t *testing.T) *GeneticSystem {
	t.Helper()
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
	s := NewGeneticSystem(w).(*GeneticSystem)
	s.Init()
	s.enabled = true
	return s
}

// A species declaration is a shared fact with a private effect, and the two
// participants reach it at different ticks.
//
// The declaration lives in an FSM region's entry actions — config/main/tower.toml
// raises the one this game has — so both instances derive it rather than
// transporting it. A guest predicts the transition, so it can register a species
// several hundred ticks before the authority does, or not yet when the authority
// already has. Registry.Import requires the registered set to match exactly, and
// before Phase 6 either mismatch was terminal: the guest refused every correction
// from then on, the store pass had already rewritten its world by the time the
// refusal arrived, and the session forked at the level transition. These are the
// two directions and the refusal that is now atomic.

// geneticRecord renders one genetic system's declared state, as a capture carries
// it.
func geneticRecord(t *testing.T, s *GeneticSystem) []byte {
	t.Helper()
	data, err := s.SaveShared()
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	return data
}

// registerSpecies is the declaration an FSM region raises.
func registerSpecies(s *GeneticSystem, id component.SpeciesType) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handleRegistration(&event.GeneticRegisterSpeciesPayload{
		Species:            id,
		GeneCount:          1,
		Bounds:             []event.ParameterBoundDef{{Min: 0, Max: 1}},
		PerturbationStdDev: 0.1,
		ProbeBins:          7,
		IsComposite:        true,
	})
}

// TestAGuestAheadOfTheAuthorityDropsItsSpeculativeSpecies is the direction the
// desync was reported in: the guest reached the tower region first.
func TestAGuestAheadOfTheAuthorityDropsItsSpeculativeSpecies(t *testing.T) {
	authority := newGeneticFixture(t)
	guest := newGeneticFixture(t)
	registerSpecies(guest, 7)

	if len(guest.registry.Registered()) != 1 {
		t.Fatalf("the guest holds %d species, want the one it declared", len(guest.registry.Registered()))
	}
	record := geneticRecord(t, authority)

	if err := guest.CheckShared(record); err != nil {
		t.Fatalf("the authority's state was refused before it was applied: %v", err)
	}
	if err := guest.LoadShared(record); err != nil {
		t.Fatalf("a guest that ran ahead could not adopt the authority: %v", err)
	}
	if got := guest.registry.Registered(); len(got) != 0 {
		t.Fatalf("the guest kept %v after adopting a state that declares none", got)
	}
}

// TestAGuestBehindTheAuthorityRegistersFromTheCapture is the other direction, and
// the ordinary one: the authority crossed the transition first.
func TestAGuestBehindTheAuthorityRegistersFromTheCapture(t *testing.T) {
	authority := newGeneticFixture(t)
	guest := newGeneticFixture(t)
	registerSpecies(authority, 7)
	record := geneticRecord(t, authority)

	if err := guest.CheckShared(record); err != nil {
		t.Fatalf("a capture carrying a declaration was refused: %v", err)
	}
	if err := guest.LoadShared(record); err != nil {
		t.Fatalf("a guest behind the authority could not adopt it: %v", err)
	}
	got := guest.registry.Registered()
	if len(got) != 1 || got[0] != registry.SpeciesID(7) {
		t.Fatalf("the guest registered %v, want species 7 from the capture's declaration", got)
	}

	// And the round trip is stable: what the guest now saves declares the same set.
	var snap geneticSnapshot
	if err := json.Unmarshal(geneticRecord(t, guest), &snap); err != nil {
		t.Fatalf("re-save: %v", err)
	}
	if len(snap.Registrations) != 1 || snap.Registrations[0].Species != 7 {
		t.Fatalf("the adopted state declares %+v", snap.Registrations)
	}
}

// TestACaptureWithNoDeclarationIsRefusedBeforeAnythingIsWritten is the atomic half.
// A record this instance cannot install has to say so while it is still a
// question — the live install writes the component stores before it reaches a
// carrier, and a staging world has never entered a level region, so it accepts
// exactly what the live world would refuse.
func TestACaptureWithNoDeclarationIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	authority := newGeneticFixture(t)
	registerSpecies(authority, 7)

	var snap geneticSnapshot
	if err := json.Unmarshal(geneticRecord(t, authority), &snap); err != nil {
		t.Fatalf("save: %v", err)
	}
	// A population with no declaration beside it: nothing can construct the species
	// it belongs to, so it is refused rather than half-installed.
	snap.Registrations = nil
	record, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	guest := newGeneticFixture(t)
	if err := guest.CheckShared(record); err == nil {
		t.Fatal("a population with no declaration was accepted")
	}
	if got := guest.registry.Registered(); len(got) != 0 {
		t.Fatalf("a refused check registered %v", got)
	}
}
