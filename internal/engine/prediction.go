package engine

import (
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/status"
)

// predictedDeath is one shared death a predicting instance derived, held until an
// authoritative world proves it. The payload is a copy because the producer's own
// is a literal it does not keep.
type predictedDeath struct {
	payload event.SpeciesKilledPayload
	tick    uint64
}

// recordPrediction holds one predicted derivation. Keyed by entity and idempotent:
// deriving the same death again after a correction restored its species is the
// case this exists for, not a second death.
func (w *World) recordPrediction(et event.EventType, payload any) {
	p, ok := payload.(*event.SpeciesKilledPayload)
	if !ok || p == nil || et != event.EventSpeciesKilled || w.followJournal.Load() {
		return
	}
	var tick uint64
	if w.Resources.Game != nil {
		tick = w.Resources.Game.State.GetGameTicks()
	}

	w.predictionMu.Lock()
	for i := range w.predicted {
		if w.predicted[i].payload.Entity == p.Entity {
			w.predictionMu.Unlock()
			return
		}
	}
	var overflow []event.SpeciesKilledPayload
	if len(w.predicted) >= parameter.PredictionLedgerMax {
		// Past the bound the ledger pays out unproved rather than losing a reward:
		// a guest killing this many shared species inside one convergence floor has
		// a correction problem, and eating its progression would not fix it.
		overflow = append(overflow, w.predicted[0].payload)
		w.predicted = append(w.predicted[:0], w.predicted[1:]...)
	}
	w.predicted = append(w.predicted, predictedDeath{payload: *p, tick: tick})
	w.publishPredictionLocked()
	w.predictionMu.Unlock()

	w.confirm(overflow)
}

// ConfirmPredictedDeaths settles the ledger against the authority's world at
// authorityTick: an entity it does not have is proved dead, one it still holds a
// convergence floor later is the misprediction it was, and one its allocator never
// reached existed only here — the install re-issues that id, so the entry is dropped.
// The world asked is the authority's as installed, never a projection of it.
func (w *World) ConfirmPredictedDeaths(authorityTick uint64, authority *World) {
	w.settle(func(d predictedDeath) (release, drop bool) {
		switch {
		case d.tick > authorityTick:
			return false, false // the capture predates the death; it claims nothing
		case !authority.Issued(d.payload.Entity):
			return false, true
		case !authority.Components.Combat.HasEntity(d.payload.Entity):
			return true, false
		}
		return false, authorityTick-d.tick > parameter.SnapshotFloorKeyframeTicks
	})
}

// SettlePredictedDeaths releases everything held once nothing will correct this
// instance any more — it took the term, or the last peer went. The world it
// predicted is the only one there is, so its derivations are settled.
func (w *World) SettlePredictedDeaths() {
	if w.PredictsShared() {
		return
	}
	w.settle(func(predictedDeath) (bool, bool) { return true, false })
}

// FollowJournal makes this world a replay's: the session state it predicts under
// and the confirmations the recorded run raised arrive as records, so no ledger holds.
func (w *World) FollowJournal() { w.followJournal.Store(true) }

// ResetPredictedDeaths drops the ledger for a run that has been replaced. A reward
// held across a reset would land in a world that never saw the death.
func (w *World) ResetPredictedDeaths() {
	w.predictionMu.Lock()
	defer w.predictionMu.Unlock()
	w.predicted = w.predicted[:0]
	w.publishPredictionLocked()
}

// settle applies one verdict to every held derivation and publishes the releases
// outside the ledger lock, because publishing re-enters the push path.
func (w *World) settle(verdict func(predictedDeath) (release, drop bool)) {
	w.predictionMu.Lock()
	if len(w.predicted) == 0 {
		w.predictionMu.Unlock()
		return
	}
	var release []event.SpeciesKilledPayload
	kept, dropped := w.predicted[:0], int64(0)
	for _, d := range w.predicted {
		r, drop := verdict(d)
		switch {
		case r:
			release = append(release, d.payload)
		case drop:
			dropped++
		default:
			kept = append(kept, d)
		}
	}
	w.predicted = kept
	if dropped > 0 && w.statPredictionDropped != nil {
		w.statPredictionDropped.Add(dropped)
	}
	w.publishPredictionLocked()
	w.predictionMu.Unlock()

	w.confirm(release)
}

// confirm raises the proved derivations. Player domain, so it never crosses; session
// origin, so it is journaled: the authoritative worlds that proved it are not.
func (w *World) confirm(released []event.SpeciesKilledPayload) {
	for i := range released {
		p := released[i]
		w.pushEvent(event.EventSpeciesKillConfirmed, &p, event.OriginSession, core.DomainPlayer)
	}
	if n := int64(len(released)); n > 0 && w.statPredictionConfirmed != nil {
		w.statPredictionConfirmed.Add(n)
	}
}

// publishPredictionLocked mirrors the ledger depth. Caller MUST hold predictionMu.
func (w *World) publishPredictionLocked() {
	if w.statPredictionPending != nil {
		w.statPredictionPending.Store(int64(len(w.predicted)))
	}
}

// BindPredictionTelemetry reserves the ledger's cells while the registry is still
// open: a key first written after Freeze is counted late rather than stored.
func (w *World) BindPredictionTelemetry(reg *status.Registry) {
	w.statPredictionPending = reg.Ints.Get("snapshot.predictions_pending")
	w.statPredictionConfirmed = reg.Ints.Get("snapshot.predictions_confirmed")
	w.statPredictionDropped = reg.Ints.Get("snapshot.predictions_dropped")
}
