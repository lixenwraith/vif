package app

import (
	"encoding/json"
	"strings"
	"testing"
)

// A refusal has to arrive before the world is written, and the staging pass cannot
// always make it.
//
// StageShared asks "can this build load this capture" of a second world, and for
// most carriers that is the whole question. It is not the whole question for a
// carrier whose acceptance depends on state the staging world does not have: the
// genetic registry's registered species set is entered by a level region the
// staging world has never been in, so it accepts what the live world refuses — and
// the refusal then arrives after the store pass has already rewritten every shared
// entity. That is the shape of the desync reported at the tower transition, and it
// is what the pre-flight below is for.

// TestACarrierRefusalLeavesTheLiveWorldUntouched drives the refusal through the
// live install path and asserts the world is exactly what it was.
func TestACarrierRefusalLeavesTheLiveWorldUntouched(t *testing.T) {
	author := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer author.Close()
	tickUntilCursor(t, author)
	author.Tick(40)

	receiver := mustHeadless(t, 0x5EEDBEEF, 120, 40)
	defer receiver.Close()
	tickUntilCursor(t, receiver)
	receiver.Tick(55)

	cap, err := author.CaptureShared()
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	unloadable := withUnregisterableGeneticState(t, cap)

	before := receiver.Snapshot()
	err = receiver.InstallShared(unloadable)
	if err == nil {
		t.Fatal("a capture no carrier could load was installed")
	}
	if !strings.Contains(err.Error(), "genetic") {
		t.Fatalf("the refusal does not name the carrier that made it: %v", err)
	}
	after := receiver.Snapshot()
	if idx, want, got, differs := FirstDiff(before, after); differs {
		t.Fatalf("a refused capture changed the live world, line %d\n  before: %s\n  after:  %s",
			idx, want, got)
	}
}

// withUnregisterableGeneticState rewrites one carrier's record so that no build can
// install it: a population for a species with no declaration beside it.
//
// The integrity hash is recomputed, so the capture is intact and describes this
// session — the refusal under test is the carrier's, not the envelope's.
func withUnregisterableGeneticState(t *testing.T, cap SharedCapture) SharedCapture {
	t.Helper()
	out := cloneCapture(t, cap)
	replaced := false
	for i := range out.Systems {
		if out.Systems[i].System != "genetic" {
			continue
		}
		var snap map[string]any
		if err := json.Unmarshal(out.Systems[i].Data, &snap); err != nil {
			t.Fatalf("decode genetic record: %v", err)
		}
		snap["registrations"] = nil
		snap["registry"] = []map[string]any{{"id": 7, "name": "species_7"}}
		data, err := json.Marshal(snap)
		if err != nil {
			t.Fatalf("encode genetic record: %v", err)
		}
		out.Systems[i].Data = data
		replaced = true
	}
	if !replaced {
		t.Fatal("the capture carries no genetic record to make unloadable")
	}
	integrity, err := captureIntegrity(out)
	if err != nil {
		t.Fatalf("integrity: %v", err)
	}
	out.Header.Integrity = integrity
	return out
}
