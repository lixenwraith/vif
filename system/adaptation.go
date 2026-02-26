package system

import (
	"fmt"
	"math"
	"math/rand/v2"
	"sort"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/status"
)

type routeOutcome struct {
	RouteIndex int
	Fitness    float64
}

// trackedRoute caches navigation state because components are wiped before EventEnemyKilled is processed
type trackedRoute struct {
	GraphID uint32
	RouteID int
	SubType uint8
}

// AdaptationSystem handles multi-armed bandit (EXP3) adaptation for alternative routes
// Decouples topological fitness evaluation and probability distribution from genetics and navigation
type AdaptationSystem struct {
	world         *engine.World
	outcomes      map[uint32]map[uint8][]routeOutcome // Buffer: graphID -> subType -> outcomes
	tracking      map[core.Entity]trackedRoute
	pendingDeaths []event.EnemyKilledPayload

	// Telemetry
	statGraphs      *atomic.Int64
	statPopulations *atomic.Int64
	statG1          *status.AtomicString
	statG2          *status.AtomicString
	statG3          *status.AtomicString
	statG4          *status.AtomicString

	enabled bool
}

func NewAdaptationSystem(world *engine.World) engine.System {
	s := &AdaptationSystem{
		world:         world,
		outcomes:      make(map[uint32]map[uint8][]routeOutcome),
		tracking:      make(map[core.Entity]trackedRoute),
		pendingDeaths: make([]event.EnemyKilledPayload, 0, 16),
	}

	s.statGraphs = world.Resources.Status.Ints.Get("adapt.graphs")
	s.statPopulations = world.Resources.Status.Ints.Get("adapt.populations")
	// Register short-string format telemetry for up to 4 route groups
	s.statG1 = world.Resources.Status.Strings.Get("adapt.g1")
	s.statG2 = world.Resources.Status.Strings.Get("adapt.g2")
	s.statG3 = world.Resources.Status.Strings.Get("adapt.g3")
	s.statG4 = world.Resources.Status.Strings.Get("adapt.g4")

	s.Init()
	return s
}

func (s *AdaptationSystem) Init() {
	if s.world.Resources.Adaptation == nil {
		s.world.Resources.Adaptation = &engine.AdaptationResource{
			Entries: make(map[uint32]*engine.AdaptationEntry),
		}
	}
	clear(s.outcomes)
	clear(s.tracking)
	s.pendingDeaths = s.pendingDeaths[:0]

	s.statGraphs.Store(0)
	s.statPopulations.Store(0)
	s.statG1.Store("-")
	s.statG2.Store("-")
	s.statG3.Store("-")
	s.statG4.Store("-")

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
		event.EventGameReset,
		event.EventMetaSystemCommandRequest,
		event.EventRouteGraphComputed,
		event.EventGatewayDespawned,
		event.EventEnemyCreated,
		event.EventEnemyKilled,
	}
}

func (s *AdaptationSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
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

	case event.EventEnemyCreated:
		if payload, ok := ev.Payload.(*event.EnemyCreatedPayload); ok {
			s.handleEnemyCreated(payload)
		}

	case event.EventEnemyKilled:
		if payload, ok := ev.Payload.(*event.EnemyKilledPayload); ok {
			s.pendingDeaths = append(s.pendingDeaths, *payload)
		}
	}
}

func (s *AdaptationSystem) Update() {
	if !s.enabled || s.world.Resources.Adaptation == nil {
		return
	}

	// Defer death handling ensures the ECS component wipe doesn't create race conditions against tracking
	s.processPendingDeaths()
	s.cleanupStaleTracking()

	ar := s.world.Resources.Adaptation

	// Process outcomes, update EXP3 weights, and refill pools
	for graphID, subTypes := range s.outcomes {
		entry, ok := ar.Entries[graphID]
		if !ok || entry.Draining {
			continue
		}

		for subType, outcomes := range subTypes {
			if len(outcomes) == 0 {
				continue
			}

			pop, exists := entry.Populations[subType]
			if !exists {
				continue
			}

			s.applyEXP3(pop, outcomes)
			s.samplePool(pop)

			// Clear processed outcomes
			s.outcomes[graphID][subType] = s.outcomes[graphID][subType][:0]
		}
	}

	// Pre-emptive pool refill for active entries running low
	for _, entry := range ar.Entries {
		if entry.Draining {
			continue
		}
		for _, pop := range entry.Populations {
			if len(pop.Pool)-pop.Head < (parameter.RoutePoolDefaultSize / 4) {
				s.samplePool(pop)
			}
		}
	}

	s.pruneDrained(ar)
	s.updateTelemetry(ar)
}

// Pre-initialize SubType 0 with the graph's optimal weights so PopRoute doesn't overwrite them
func (s *AdaptationSystem) handleGraphComputed(graphID uint32, routeCount int) {
	ar := s.world.Resources.Adaptation
	if ar.Entries == nil {
		ar.Entries = make(map[uint32]*engine.AdaptationEntry)
	}

	entry := &engine.AdaptationEntry{
		RouteCount:  routeCount,
		Populations: make(map[uint8]*engine.RoutePopulation),
	}
	ar.Entries[graphID] = entry

	// Pre-populate SubType 0 to preserve the initial distance-based weights
	graph := s.world.Resources.RouteGraph.Get(graphID)
	if graph != nil && len(graph.Routes) == routeCount {
		pop := &engine.RoutePopulation{
			Weights: make([]float64, routeCount),
			Pool:    make([]int, 0),
			Head:    0,
		}
		for i, r := range graph.Routes {
			pop.Weights[i] = r.Weight // Preserves initial higher weights on short routes
		}
		entry.Populations[0] = pop
		s.samplePool(pop)
	}
}

// Factor route efficiency into fitness so EXP3 can distinguish between short and long paths
func (s *AdaptationSystem) handleEnemyKilled(payload *event.EnemyKilledPayload) {
	nav, ok := s.world.Components.Navigation.GetComponent(payload.Entity)
	if !ok || !nav.UseRouteGraph || nav.RouteGraphID == 0 || nav.RouteID < 0 {
		return // Ignore standard entities
	}

	graph := s.world.Resources.RouteGraph.Get(nav.RouteGraphID)
	if graph == nil || nav.RouteID >= len(graph.Routes) {
		return
	}

	route := graph.Routes[nav.RouteID]
	if route.Field == nil || !route.Field.Valid {
		return
	}

	deathDist := route.Field.GetDistance(payload.X, payload.Y)

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
		// A long route that reaches the tower gets a lower score than a short route.
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

	s.recordOutcome(nav.RouteGraphID, payload.SubType, route.ID, fitness)
}

// handleEnemyCreated caches routing data before entity destruction
func (s *AdaptationSystem) handleEnemyCreated(payload *event.EnemyCreatedPayload) {
	nav, ok := s.world.Components.Navigation.GetComponent(payload.Entity)
	if !ok || !nav.UseRouteGraph || nav.RouteGraphID == 0 || nav.RouteID < 0 {
		return
	}
	s.tracking[payload.Entity] = trackedRoute{
		GraphID: nav.RouteGraphID,
		RouteID: nav.RouteID,
		SubType: payload.SubType,
	}
}

func (s *AdaptationSystem) recordOutcome(graphID uint32, subType uint8, routeID int, fitness float64) {
	if s.outcomes[graphID] == nil {
		s.outcomes[graphID] = make(map[uint8][]routeOutcome)
	}
	s.outcomes[graphID][subType] = append(s.outcomes[graphID][subType], routeOutcome{
		RouteIndex: routeID,
		Fitness:    fitness,
	})
}

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

// cleanupStaleTracking removes entities destroyed by map resets/resizes without death events
func (s *AdaptationSystem) cleanupStaleTracking() {
	for entity, t := range s.tracking {
		if !s.world.Components.Navigation.HasEntity(entity) {
			// Entity destroyed without a pending death event. Record a flat 0 outcome and abandon.
			s.recordOutcome(t.GraphID, t.SubType, t.RouteID, 0.0)
			delete(s.tracking, entity)
		}
	}
}

// applyEXP3 implements Multiplicative Weights Update, keeping exploitation math clean and untampered
func (s *AdaptationSystem) applyEXP3(pop *engine.RoutePopulation, outcomes []routeOutcome) {
	k := len(pop.Weights)
	if k == 0 {
		return
	}

	sumFitness := make([]float64, k)
	counts := make([]int, k)

	for _, o := range outcomes {
		if o.RouteIndex >= 0 && o.RouteIndex < k {
			sumFitness[o.RouteIndex] += o.Fitness
			counts[o.RouteIndex]++
		}
	}

	// 1. Multiply weights by exponential fitness (Multiplicative Weight Updates)
	for i := 0; i < k; i++ {
		if counts[i] > 0 {
			avg := sumFitness[i] / float64(counts[i])
			pop.Weights[i] *= math.Exp(parameter.RouteLearningRate * avg)
		}
	}

	// 2. Normalize raw weights to sum = 1.0
	totalWeight := 0.0
	for i := 0; i < k; i++ {
		totalWeight += pop.Weights[i]
	}
	if totalWeight > 0 {
		for i := 0; i < k; i++ {
			pop.Weights[i] /= totalWeight
		}
	}

	// 3. Mathematical minimum boundary (0.5%) solely to prevent extinct weights from underflowing to 0
	// Exploration is actively enforced during selection (samplePool), NOT baked directly into latent weights
	minWeight := 0.005
	floorApplied := false
	for i := 0; i < k; i++ {
		if pop.Weights[i] < minWeight {
			pop.Weights[i] = minWeight
			floorApplied = true
		}
	}

	// Re-normalize if any floors were applied
	if floorApplied {
		totalWeight = 0.0
		for i := 0; i < k; i++ {
			totalWeight += pop.Weights[i]
		}
		for i := 0; i < k; i++ {
			pop.Weights[i] /= totalWeight
		}
	}
}

// samplePool fills consumer pool using decoupled epsilon-greedy sampling, maintaining pure underlying weights
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

	cdf := make([]float64, k)
	cdf[0] = pop.Weights[0]
	for i := 1; i < k; i++ {
		cdf[i] = cdf[i-1] + pop.Weights[i]
	}
	total := cdf[k-1]

	// Decoupled Scout Wave mechanic (epsilon-greedy). Ensures 10% of spawns uniformly probe routes
	// independent of the mathematical distributions of the exploitative EXP3 model
	const scoutRate = 0.10

	for i := 0; i < n; i++ {
		if total <= 0 || rand.Float64() < scoutRate {
			// Scout: Uniform random assignment
			pop.Pool[i] = rand.IntN(k)
		} else {
			// Exploit: Proportional execution
			r := rand.Float64() * total
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
		j := rand.IntN(i + 1)
		pop.Pool[i], pop.Pool[j] = pop.Pool[j], pop.Pool[i]
	}

	pop.Head = 0
}

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

func (s *AdaptationSystem) updateTelemetry(ar *engine.AdaptationResource) {
	activeGraphs := int64(0)
	activePopulations := int64(0)

	// Deterministic sorting to prevent layout shift in G1-G4 slots
	var graphIDs []uint32
	for id, entry := range ar.Entries {
		if !entry.Draining {
			graphIDs = append(graphIDs, id)
		}
	}
	sort.Slice(graphIDs, func(i, j int) bool { return graphIDs[i] < graphIDs[j] })

	groupStrs := make([]string, 0, 4)

	for _, id := range graphIDs {
		entry := ar.Entries[id]
		activeGraphs++
		activePopulations += int64(len(entry.Populations))

		if len(groupStrs) >= 4 {
			continue
		}

		// Look for the subType with the sharpest peak to report
		var bestPop *engine.RoutePopulation
		var highestPeak float64

		for _, pop := range entry.Populations {
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

		if bestPop != nil {
			wCopy := make([]float64, len(bestPop.Weights))
			copy(wCopy, bestPop.Weights)
			sort.Sort(sort.Reverse(sort.Float64Slice(wCopy)))
			rc := len(wCopy)

			str := ""
			// Including denominator /rc provides context for low percentages directly in UI limits
			if rc >= 3 {
				str = fmt.Sprintf("%.0f%% %.0f%% %.0f%% /%d", wCopy[0]*100, wCopy[1]*100, wCopy[2]*100, rc)
			} else if rc == 2 {
				str = fmt.Sprintf("%.0f%% %.0f%% /%d", wCopy[0]*100, wCopy[1]*100, rc)
			} else if rc == 1 {
				str = fmt.Sprintf("%.0f%% /%d", wCopy[0]*100, rc)
			} else {
				str = "0% 0% 0% /0"
			}
			groupStrs = append(groupStrs, str)
		}
	}

	s.statGraphs.Store(activeGraphs)
	s.statPopulations.Store(activePopulations)

	if len(groupStrs) > 0 {
		s.statG1.Store(groupStrs[0])
	} else {
		s.statG1.Store("-")
	}
	if len(groupStrs) > 1 {
		s.statG2.Store(groupStrs[1])
	} else {
		s.statG2.Store("-")
	}
	if len(groupStrs) > 2 {
		s.statG3.Store(groupStrs[2])
	} else {
		s.statG3.Store("-")
	}
	if len(groupStrs) > 3 {
		s.statG4.Store(groupStrs[3])
	} else {
		s.statG4.Store("-")
	}
}