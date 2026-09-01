package engine

import (
	"sort"
	"time"
)

// AdaptationState is AdaptationResource's export contract (D-19).
//
// The EXP3 route learning is shared state that neither the domain document nor
// the digest listed: the weights, the pre-sampled pool and the consumer head
// together decide which route the next spawned eye takes. A divergence in any of
// them is silent until it moves an entity, and a snapshot that omitted them would
// install a world that routes differently from the one it claims to reproduce.
//
// The contract lives here rather than in the system because the pool's fallback
// rotation is unexported: an export written outside this package would leave it
// behind and a resumed pool would rotate from a different position.
type AdaptationState struct {
	Entries []AdaptationEntryState `json:"entries"`
}

// AdaptationEntryState is one gateway's learned routing, named by its gateway ID.
type AdaptationEntryState struct {
	ID         uint32 `json:"id"`
	RouteCount int    `json:"route_count"`
	Draining   bool   `json:"draining"`
	// DrainAge is how long the entry had been draining at the capture's tick, not
	// the instant it started (D-19, §4.2). An instant is a function of the tick
	// since the simulation clock became tick-derived, but the relative form stays
	// correct if a capture is ever rebased.
	DrainAge    time.Duration          `json:"drain_age"`
	Populations []RoutePopulationState `json:"populations"`
}

// RoutePopulationState is one species sub-type's EXP3 population.
type RoutePopulationState struct {
	SubType uint8     `json:"sub_type"`
	Weights []float64 `json:"weights"`
	Pool    []int     `json:"pool"`
	Head    int       `json:"head"`
	Spin    int       `json:"spin"`
}

// SaveState exports the learned routing as of now, which the caller passes as the
// capture's simulation instant so ages are measured against the tick being
// captured rather than against whenever the export ran.
//
// Entries and populations are emitted in key order: a capture is compared as well
// as installed, so map iteration order must not reach the bytes.
func (ar *AdaptationResource) SaveState(now time.Time) AdaptationState {
	var out AdaptationState
	if ar == nil || len(ar.Entries) == 0 {
		return out
	}
	ids := make([]uint32, 0, len(ar.Entries))
	for id := range ar.Entries {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	out.Entries = make([]AdaptationEntryState, 0, len(ids))
	for _, id := range ids {
		entry := ar.Entries[id]
		if entry == nil {
			continue
		}
		es := AdaptationEntryState{
			ID:         id,
			RouteCount: entry.RouteCount,
			Draining:   entry.Draining,
		}
		if entry.Draining && !entry.DrainTime.IsZero() {
			es.DrainAge = now.Sub(entry.DrainTime)
		}
		subs := make([]uint8, 0, len(entry.Populations))
		for sub := range entry.Populations {
			subs = append(subs, sub)
		}
		sort.Slice(subs, func(i, j int) bool { return subs[i] < subs[j] })
		for _, sub := range subs {
			pop := entry.Populations[sub]
			if pop == nil {
				continue
			}
			es.Populations = append(es.Populations, RoutePopulationState{
				SubType: sub,
				Weights: append([]float64(nil), pop.Weights...),
				Pool:    append([]int(nil), pop.Pool...),
				Head:    pop.Head,
				Spin:    pop.spin,
			})
		}
		out.Entries = append(out.Entries, es)
	}
	return out
}

// LoadState installs exported routing, rebasing every age onto the instant the
// receiving world is resuming at. The resource is replaced rather than merged:
// a partial install would leave one gateway's weights from the capture beside
// another's from whatever this instance had learned on its own.
func (ar *AdaptationResource) LoadState(state AdaptationState, now time.Time) {
	if ar == nil {
		return
	}
	ar.Entries = make(map[uint32]*AdaptationEntry, len(state.Entries))
	for _, es := range state.Entries {
		entry := &AdaptationEntry{
			RouteCount:  es.RouteCount,
			Draining:    es.Draining,
			Populations: make(map[uint8]*RoutePopulation, len(es.Populations)),
		}
		if es.Draining {
			entry.DrainTime = now.Add(-es.DrainAge)
		}
		for _, ps := range es.Populations {
			entry.Populations[ps.SubType] = &RoutePopulation{
				Weights: append([]float64(nil), ps.Weights...),
				Pool:    append([]int(nil), ps.Pool...),
				Head:    ps.Head,
				spin:    ps.Spin,
			}
		}
		ar.Entries[es.ID] = entry
	}
}
