package engine

import (
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
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
	if !ok || p == nil || et != event.EventSpeciesKilled {
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

// ConfirmPredictedDeaths settles the ledger against a world this instance just
// installed: an entity the authority does not have is proved dead, and one it still
// holds a convergence floor later is the misprediction it was.
// Caller MUST hold updateMutex: it reads the installed stores.
func (w *World) ConfirmPredictedDeaths(authorityTick uint64) {
	w.settle(func(d predictedDeath) (release, drop bool) {
		if d.tick > authorityTick {
			return false, false // the capture predates the death; it claims nothing
		}
		if !w.Components.Combat.HasEntity(d.payload.Entity) {
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

// confirm raises the proved derivations. Player domain and system origin: the
// receiver derived this from its own authoritative world, so it neither crosses
// nor enters the journal a replay re-derives it from.
func (w *World) confirm(released []event.SpeciesKilledPayload) {
	for i := range released {
		p := released[i]
		w.pushEvent(event.EventSpeciesKillConfirmed, &p, event.OriginSystem, core.DomainPlayer)
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
