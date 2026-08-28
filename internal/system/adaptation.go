package system

import (
	"cmp"
	"fmt"
	"math"
	"slices"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/pkg/navigation"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

type routeOutcome struct {
	RouteIndex int
	Fitness    float64
}

// trackedRoute caches navigation state because components are wiped before EventSpeciesKilled is processed
type trackedRoute struct {
	RouteID int
	GraphID uint32
	SubType uint8
}

// AdaptationSystem handles multi-armed bandit (EXP3) adaptation for alternative routes
// Decouples topological fitness evaluation and probability distribution from genetics and navigation
type AdaptationSystem struct {
	world         *engine.World
	outcomes      map[uint32]map[uint8][]routeOutcome // Buffer: graphID -> subType -> outcomes
	tracking      map[core.Entity]trackedRoute
	pendingDeaths []event.SpeciesKilledPayload

	rng *vmath.FastRand

	// Iteration and computation scratch, reused across ticks
	// No need for init, each is written before read
	graphKeys     []uint32
	subKeys       []uint8
	trackKeys     []core.Entity
	sumFitness    []float64
	counts        []int
	cdf           []float64
	weightScratch []float64

	// Ticks since last telemetry refresh
	telemetryTicks int

	// Telemetry
	statGraphs      *atomic.Int64
	statPopulations *atomic.Int64
	statG           [4]*status.AtomicString
	buffers         bufferTelemetry

	enabled bool
}

func NewAdaptationSystem(world *engine.World) engine.System {
	s := &AdaptationSystem{
		world:         world,
		outcomes:      make(map[uint32]map[uint8][]routeOutcome),
		tracking:      make(map[core.Entity]trackedRoute),
		pendingDeaths: make([]event.SpeciesKilledPayload, 0, 16),
	}

	s.statGraphs = world.Resources.Status.Ints.Get("adapt.graphs")
	s.statPopulations = world.Resources.Status.Ints.Get("adapt.populations")
	// Short-string weight summaries for up to 4 route groups
	for i, k := range []string{"adapt.g1", "adapt.g2", "adapt.g3", "adapt.g4"} {
		s.statG[i] = world.Resources.Status.Strings.Get(k)
	}
	s.buffers = newBufferTelemetry(world.Resources.Status, "adapt", "pending_deaths", "graph_keys", "sub_keys", "track_keys", "sum_fitness", "counts", "cdf", "weight_scratch", "outcome_graphs", "tracking", "outcome_samples")

	s.Init()
	return s
}

func (s *AdaptationSystem) Init() {
	if s.world.Resources.Adaptation == nil {
		s.world.Resources.Adaptation = &engine.AdaptationResource{
			Entries: make(map[uint32]*engine.AdaptationEntry),
		}
	}
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	clear(s.outcomes)
	clear(s.tracking)
	s.pendingDeaths = s.pendingDeaths[:0]
	s.telemetryTicks = 0

	s.statGraphs.Store(0)
	s.statPopulations.Store(0)
	for _, g := range s.statG {
		g.StoreIfChanged("-")
	}
	s.buffers.Reset()

	s.enabled = true
}

func (s *AdaptationSystem) Name() string {
	return "adaptation"
}

func (s *AdaptationSystem) Priority() int {
	return parameter.PriorityAdaptation
}

func (s *AdaptationSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameResetRequest,
		event.EventMetaSystemCommandRequest,
		event.EventRouteGraphComputed,
		event.EventGatewayDespawned,
		event.EventSpeciesCreated,
		event.EventSpeciesKilled,
	}
}

func (s *AdaptationSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
		return
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventRouteGraphComputed:
		if payload, ok := ev.Payload.(*event.RouteGraphComputedPayload); ok {
			s.handleGraphComputed(payload.RouteGraphID, payload.RouteCount)
		}

	case event.EventGatewayDespawned:
		if payload, ok := ev.Payload.(*event.GatewayDespawnedPayload); ok {
			s.world.Resources.Adaptation.MarkDraining(uint32(payload.GatewayEntity), s.world.Resources.Time.GameTime)
		}

	case event.EventSpeciesCreated:
		if payload, ok := ev.Payload.(*event.SpeciesCreatedPayload); ok {
			s.handleSpeciesCreated(payload)
		}

	case event.EventSpeciesKilled:
		if payload, ok := ev.Payload.(*event.SpeciesKilledPayload); ok {
			s.pendingDeaths = append(s.pendingDeaths, *payload)
			s.buffers.Observe(0, len(s.pendingDeaths))
		}
	}
}

func (s *AdaptationSystem) Update() {
	if !s.enabled {
		return
	}

	if s.world.Resources.Adaptation == nil {
		return
	}

	// Deferred death processing avoids racing the ECS component wipe
	s.processPendingDeaths()
	s.cleanupStaleTracking()

	ar := s.world.Resources.Adaptation

	// Process outcomes, update EXP3 weights, and refill pools
	s.graphKeys = sortedKeys(s.graphKeys, s.outcomes)
	s.buffers.Observe(1, len(s.graphKeys))
	for _, graphID := range s.graphKeys {
		subTypes := s.outcomes[graphID]
		entry, ok := ar.Entries[graphID]
		if !ok || entry.Draining {
			continue
		}

		s.subKeys = sortedKeys(s.subKeys, subTypes)
		s.buffers.Observe(2, len(s.subKeys))
		for _, subType := range s.subKeys {
			outcomes := subTypes[subType]
			if len(outcomes) == 0 {
				continue
			}

			pop, exists := entry.Populations[subType]
			if !exists {
				// Drop unattributable outcomes instead of buffering forever
				subTypes[subType] = outcomes[:0]
				continue
			}

			s.applyEXP3(pop, outcomes)
			s.samplePool(pop)

			// Clear processed outcomes
			subTypes[subType] = outcomes[:0]
		}
	}

	// Pre-emptive pool refill for active entries running low
	s.graphKeys = sortedKeys(s.graphKeys, ar.Entries)
	s.buffers.Observe(1, len(s.graphKeys))
	for _, graphID := range s.graphKeys {
		entry := ar.Entries[graphID]
		if entry.Draining {
			continue
		}
		s.subKeys = sortedKeys(s.subKeys, entry.Populations)
		s.buffers.Observe(2, len(s.subKeys))
		for _, subType := range s.subKeys {
			pop := entry.Populations[subType]
			if len(pop.Pool)-pop.Head < (parameter.RoutePoolDefaultSize / 4) {
				s.samplePool(pop)
			}
		}
	}

	s.pruneDrained(ar)

	// Debug overlay only; 2 Hz avoids per-tick sort/format garbage
	s.telemetryTicks++
	if s.telemetryTicks >= parameter.AdaptTelemetryInterval {
		s.telemetryTicks = 0
		s.updateTelemetry(ar)
	}
}

// sortedKeys refills dst with m's keys in ascending order.
// RNG-consuming loops must not range a map directly: draw order would depend
// on the run rather than the seed.
func sortedKeys[K cmp.Ordered, V any](dst []K, m map[K]V) []K {
	dst = dst[:0]
	for k := range m {
		dst = append(dst, k)
	}
	slices.Sort(dst)
	return dst
}

// handleGraphComputed creates the bandit entry for a computed graph and seeds
// SubType 0 with topological route weights; PopRoute lazily clones this baseline
func (s *AdaptationSystem) handleGraphComputed(graphID uint32, routeCount int) {
	ar := s.world.Resources.Adaptation
	if ar.Entries == nil {
		ar.Entries = make(map[uint32]*engine.AdaptationEntry)
	}

	// Recompute invalidates prior arms: drop stale tracking and buffered outcomes
	if _, existed := ar.Entries[graphID]; existed {
		for entity, t := range s.tracking {
			if t.GraphID == graphID {
				delete(s.tracking, entity)
			}
		}
		delete(s.outcomes, graphID)
	}

	entry := &engine.AdaptationEntry{
		RouteCount:  routeCount,
		Populations: make(map[uint8]*engine.RoutePopulation),
	}
	ar.Entries[graphID] = entry

	pop := &engine.RoutePopulation{
		Weights: make([]float64, routeCount),
		Pool:    make([]int, 0),
	}
	// Pre-populate SubType 0 to preserve the initial distance-based weights
	graph := s.world.Resources.RouteGraph.Get(graphID)
	if graph != nil && len(graph.Routes) == routeCount {

		for i, r := range graph.Routes {
			pop.Weights[i] = r.Weight
		}
	} else if routeCount > 0 {
		// uniform fallback instead of skipping — population 0 must
		// always exist with correct arity
		u := 1.0 / float64(routeCount)
		for i := range pop.Weights {
			pop.Weights[i] = u
		}
	}
	entry.Populations[0] = pop
	s.samplePool(pop)
}

// handleSpeciesCreated caches routing data before entity destruction
func (s *AdaptationSystem) handleSpeciesCreated(payload *event.SpeciesCreatedPayload) {
	nav, ok := s.world.Components.Navigation.GetComponent(payload.Entity)
	if !ok || !nav.UseRouteGraph || nav.RouteGraphID == 0 || nav.RouteID < 0 {
		return
	}
	s.tracking[payload.Entity] = trackedRoute{
		GraphID: nav.RouteGraphID,
		RouteID: nav.RouteID,
		SubType: payload.SubType,
	}
	s.buffers.Observe(9, len(s.tracking))
}

// recordOutcome buffers a fitness sample for the next Update pass
func (s *AdaptationSystem) recordOutcome(graphID uint32, subType uint8, routeID int, fitness float64) {
	if s.outcomes[graphID] == nil {
		s.outcomes[graphID] = make(map[uint8][]routeOutcome)
	}
	outcomes := append(s.outcomes[graphID][subType], routeOutcome{
		RouteIndex: routeID,
		Fitness:    fitness,
	})
	s.outcomes[graphID][subType] = outcomes
	s.buffers.Observe(8, len(s.outcomes))
	s.buffers.Observe(10, len(outcomes))
}

// processPendingDeaths converts tracked deaths into route outcomes:
// corridor progress at death scaled by route efficiency (minDist/routeDist)
func (s *AdaptationSystem) processPendingDeaths() {
	for _, death := range s.pendingDeaths {
		t, ok := s.tracking[death.Entity]
		if !ok {
			continue
		}

		graph := s.world.Resources.RouteGraph.Get(t.GraphID)
		if graph != nil && t.RouteID >= 0 && t.RouteID < len(graph.Routes) {
			route := graph.Routes[t.RouteID]
			if route.Field != nil && route.Field.Valid {
				deathDist := route.Field.GetDistance(death.X, death.Y)

				// Recover topological distance if knocked slightly out of corridor
				if deathDist < 0 {
					deathDist = findCorridorDistance(route.Field, death.X, death.Y, 5)
				}

				var fitness float64
				if deathDist < 0 {
					fitness = 0.0
				} else {
					spawnDist := float64(route.TotalDistance)
					if spawnDist <= 0 {
						spawnDist = 1.0
					}

					progress := 1.0 - (float64(deathDist) / spawnDist)
					if progress < 0.0 {
						progress = 0.0
					} else if progress > 1.0 {
						progress = 1.0
					}

					// EFFICIENCY SCALING: Scale progress by how optimal this route actually is.
					minDist := spawnDist
					for _, r := range graph.Routes {
						d := float64(r.TotalDistance)
						if d > 0 && d < minDist {
							minDist = d
						}
					}

					efficiency := minDist / spawnDist
					fitness = progress * efficiency
				}

				s.recordOutcome(t.GraphID, t.SubType, t.RouteID, fitness)
			}
		}

		delete(s.tracking, death.Entity)
	}
	s.pendingDeaths = s.pendingDeaths[:0]
}

// findCorridorDistance performs a spiral search to find the nearest valid topological
// distance in the route corridor, penalizing the gap using aspect-weighted costs.
func findCorridorDistance(field *navigation.FlowField, startX, startY, maxRadius int) int {
	for r := 1; r <= maxRadius; r++ {
		bestDist := -1
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if vmath.IntAbs(dx) != r && vmath.IntAbs(dy) != r {
					continue // Perimeter only
				}
				d := field.GetDistance(startX+dx, startY+dy)
				if d >= 0 {
					// Manhattan gap penalty using engine-defined axis costs
					penalty := vmath.IntAbs(dx)*navigation.CostX + vmath.IntAbs(dy)*navigation.CostY
					total := d + penalty
					if bestDist < 0 || total < bestDist {
						bestDist = total
					}
				}
			}
		}
		// Return the best distance found at this radius ring before expanding further
		if bestDist >= 0 {
			return bestDist
		}
	}
	return -1
}

// cleanupStaleTracking removes entities destroyed by map resets/resizes without death events.
// Sorted iteration: recordOutcome appends, and applyEXP3 sums those appends in order.
func (s *AdaptationSystem) cleanupStaleTracking() {
	s.trackKeys = sortedKeys(s.trackKeys, s.tracking)
	s.buffers.Observe(3, len(s.trackKeys))
	for _, entity := range s.trackKeys {
		if s.world.Components.Navigation.HasEntity(entity) {
			continue
		}
		t := s.tracking[entity]
		// Entity destroyed without a pending death event. Record a flat 0 outcome and abandon.
		s.recordOutcome(t.GraphID, t.SubType, t.RouteID, 0.0)
		delete(s.tracking, entity)
	}
}

// applyEXP3 implements MWU/Hedge on per-route mean fitness, not canonical EXP3.
// Rewards are not importance-weighted, weight floor (0.5%) prevents underflow
// Exploration is enforced in samplePool
func (s *AdaptationSystem) applyEXP3(pop *engine.RoutePopulation, outcomes []routeOutcome) {
	k := len(pop.Weights)
	if k == 0 {
		return
	}

	// Both accumulate, so a reused buffer must be cleared
	if cap(s.sumFitness) < k {
		s.sumFitness = make([]float64, k)
	}
	if cap(s.counts) < k {
		s.counts = make([]int, k)
	}
	sumFitness := s.sumFitness[:k]
	counts := s.counts[:k]
	s.buffers.Observe(4, len(sumFitness))
	s.buffers.Observe(5, len(counts))
	clear(sumFitness)
	clear(counts)

	for _, o := range outcomes {
		if o.RouteIndex >= 0 && o.RouteIndex < k {
			sumFitness[o.RouteIndex] += o.Fitness
			counts[o.RouteIndex]++
		}
	}

	// 1. Multiply weights by exponential fitness (Multiplicative Weight Updates)
	for i := range k {
		if counts[i] > 0 {
			avg := sumFitness[i] / float64(counts[i])
			pop.Weights[i] *= math.Exp(parameter.RouteLearningRate * avg)
		}
	}

	// 2. Normalize raw weights to sum = 1.0
	totalWeight := 0.0
	for i := range k {
		totalWeight += pop.Weights[i]
	}
	if totalWeight > 0 {
		for i := range k {
			pop.Weights[i] /= totalWeight
		}
	}

	// 3. Mathematical minimum boundary (0.5%) solely to prevent extinct weights from underflowing to 0
	// Exploration is actively enforced during selection (samplePool), NOT baked directly into latent weights
	minWeight := 0.005
	floorApplied := false
	for i := range k {
		if pop.Weights[i] < minWeight {
			pop.Weights[i] = minWeight
			floorApplied = true
		}
	}

	// Re-normalize if any floors were applied
	if floorApplied {
		totalWeight = 0.0
		for i := range k {
			totalWeight += pop.Weights[i]
		}
		for i := range k {
			pop.Weights[i] /= totalWeight
		}
	}
}

// samplePool refills the consumer pool: 10% uniform scouts, remainder sampled
// proportionally from latent weights via CDF binary search, then shuffled
func (s *AdaptationSystem) samplePool(pop *engine.RoutePopulation) {
	n := parameter.RoutePoolDefaultSize
	k := len(pop.Weights)
	if k == 0 {
		return
	}

	if cap(pop.Pool) < n {
		pop.Pool = make([]int, n)
	} else {
		pop.Pool = pop.Pool[:n]
	}

	// Every index is written below, so no clear is needed
	if cap(s.cdf) < k {
		s.cdf = make([]float64, k)
	}
	cdf := s.cdf[:k]
	s.buffers.Observe(6, len(cdf))
	cdf[0] = pop.Weights[0]
	for i := 1; i < k; i++ {
		cdf[i] = cdf[i-1] + pop.Weights[i]
	}
	total := cdf[k-1]

	// Decoupled Scout Wave mechanic (epsilon-greedy). Ensures 10% of spawns uniformly probe routes
	// independent of the mathematical distributions of the exploitative EXP3 model
	const scoutRate = 0.10

	for i := range n {
		if total <= 0 || s.rng.Float64() < scoutRate {
			// Scout: Uniform random assignment
			pop.Pool[i] = s.rng.Intn(k)
		} else {
			// Exploit: Proportional execution
			r := s.rng.Float64() * total
			lo, hi := 0, k-1
			for lo < hi {
				mid := (lo + hi) / 2
				if cdf[mid] < r {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			pop.Pool[i] = lo
		}
	}

	// Fisher-Yates shuffle
	for i := n - 1; i > 0; i-- {
		j := s.rng.Intn(i + 1)
		pop.Pool[i], pop.Pool[j] = pop.Pool[j], pop.Pool[i]
	}

	pop.Head = 0
}

// pruneDrained deletes drained entries, their graphs, and buffered outcomes after timeout
func (s *AdaptationSystem) pruneDrained(ar *engine.AdaptationResource) {
	now := s.world.Resources.Time.GameTime
	for id, entry := range ar.Entries {
		if entry.Draining && now.Sub(entry.DrainTime) >= parameter.RouteDrainTimeout {
			delete(ar.Entries, id)
			s.world.Resources.RouteGraph.Remove(id)
			delete(s.outcomes, id)
		}
	}
}

// updateTelemetry publishes graph/population counts and top-weight summaries (G1–G4)
func (s *AdaptationSystem) updateTelemetry(ar *engine.AdaptationResource) {
	activeGraphs := int64(0)
	activePopulations := int64(0)

	// Deterministic order prevents layout shift in G1-G4 slots.
	// graphKeys and subKeys are free here: both Update loops have finished.
	s.graphKeys = s.graphKeys[:0]
	for id, entry := range ar.Entries {
		if !entry.Draining {
			s.graphKeys = append(s.graphKeys, id)
		}
	}
	slices.Sort(s.graphKeys)
	s.buffers.Observe(1, len(s.graphKeys))

	slot := 0

	for _, id := range s.graphKeys {
		entry := ar.Entries[id]
		activeGraphs++
		activePopulations += int64(len(entry.Populations))

		// Counting continues past the last slot; only publication stops
		if slot >= len(s.statG) {
			continue
		}

		// Report the subType with the sharpest peak; sorted so ties break identically every run
		var bestPop *engine.RoutePopulation
		var highestPeak float64

		s.subKeys = sortedKeys(s.subKeys, entry.Populations)
		s.buffers.Observe(2, len(s.subKeys))
		for _, subType := range s.subKeys {
			pop := entry.Populations[subType]
			peak := 0.0
			for _, w := range pop.Weights {
				if w > peak {
					peak = w
				}
			}
			if bestPop == nil || peak > highestPeak {
				bestPop = pop
				highestPeak = peak
			}
		}

		if bestPop == nil {
			// placeholder keeps G1-G4 slot ↔ gateway alignment
			s.statG[slot].StoreIfChanged(fmt.Sprintf("? /%d", entry.RouteCount))
			slot++
			continue
		}

		if cap(s.weightScratch) < len(bestPop.Weights) {
			s.weightScratch = make([]float64, len(bestPop.Weights))
		}
		wCopy := s.weightScratch[:len(bestPop.Weights)]
		s.buffers.Observe(7, len(wCopy))
		copy(wCopy, bestPop.Weights)
		slices.Sort(wCopy)
		slices.Reverse(wCopy)
		rc := len(wCopy)

		// Including denominator /rc provides context for low percentages directly in UI limits
		var str string
		switch {
		case rc >= 3:
			str = fmt.Sprintf("%.0f%% %.0f%% %.0f%% /%d", wCopy[0]*100, wCopy[1]*100, wCopy[2]*100, rc)
		case rc == 2:
			str = fmt.Sprintf("%.0f%% %.0f%% /%d", wCopy[0]*100, wCopy[1]*100, rc)
		case rc == 1:
			str = fmt.Sprintf("%.0f%% /%d", wCopy[0]*100, rc)
		default:
			str = "0% 0% 0% /0"
		}

		s.statG[slot].StoreIfChanged(str)
		slot++
	}

	s.statGraphs.Store(activeGraphs)
	s.statPopulations.Store(activePopulations)

	// Slots with no graph read as empty
	for ; slot < len(s.statG); slot++ {
		s.statG[slot].StoreIfChanged("-")
	}
}
