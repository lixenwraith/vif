package engine

import (
	"math"
	"math/rand/v2"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/navigation"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/status"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// Resources holds singleton game resources, initialized during GameContext creation, accessed via World.Resources
type Resource struct {
	// World Resource
	Time   *TimeResource
	Config *ConfigResource
	Game   *GameStateResource
	Player *PlayerResource
	Event  *EventQueueResource

	// Targeting
	Target *TargetResource
	// Route graphs for multi-path navigation
	RouteGraph *RouteGraphResource

	// Transient visual effects
	Transient *TransientResource

	// Telemetry
	Status *status.Registry

	// Bridged resources from services
	Content *ContentResource
	Audio   *AudioResource
	Network *NetworkResource
}

// ServiceBridge routes a service-contributed resource to its typed field
func (r *Resource) ServiceBridge(res any) {
	switch v := res.(type) {
	case *AudioResource:
		r.Audio = v
	case *ContentResource:
		r.Content = v
	case *NetworkResource:
		r.Network = v
	}
}

// === World Resources ===

// TimeResource is time data snapshot for systems and is updated by ClockScheduler at the start of a tick
type TimeResource struct {
	// GameTime is the current time in the game world (affected by pause)
	GameTime time.Time

	// RealTime is the wall-clock time (unaffected by pause)
	RealTime time.Time

	// DeltaTime is the duration since the last update
	DeltaTime time.Duration
}

// Update modifies TimeResource fields in-place, Must be called under world lock
func (tr *TimeResource) Update(gameTime, realTime time.Time, deltaTime time.Duration) {
	tr.GameTime = gameTime
	tr.RealTime = realTime
	tr.DeltaTime = deltaTime
}

// ConfigResource holds static or semi-static configuration data
type ConfigResource struct {
	// Map Dimensions (simulation bounds)
	// Defines playable area within the fixed spatial grid
	MapWidth  int `toml:"map_width"`
	MapHeight int `toml:"map_height"`

	// Viewport Dimensions (render window)
	// Terminal-derived visible area; may differ from Map
	ViewportWidth  int `toml:"viewport_width"`
	ViewportHeight int `toml:"viewport_height"`

	// Camera Position (top-left corner of viewport in map coordinates)
	// When Map > Viewport: scrollable, clamped to [0, Map - Viewport]
	// When Map <= Viewport: fixed at 0, map centered by renderer
	CameraX int `toml:"camera_x"`
	CameraY int `toml:"camera_y"`

	// CropOnResize controls terminal resize behavior
	// true: Map resizes to match Viewport, OOB entities destroyed
	// false: Map persists, Viewport/Camera updated, entities preserved
	CropOnResize bool `toml:"crop_on_resize"`

	// ColorMode for rendering pipeline (256-color vs TrueColor)
	// Set after terminal initialization
	ColorMode terminal.ColorMode `toml:"color_mode"`
}

// EventQueueResource wraps the event queue for systems access
type EventQueueResource struct {
	Queue *event.EventQueue
}

// PingBounds holds the boundaries for ping crosshair and operations
type PingBounds struct {
	RadiusX int  // Half-width from cursor (0 = single column)
	RadiusY int  // Half-height from cursor (0 = single row)
	Active  bool // True when visual mode + shield active
}

// PingAbsoluteBounds holds absolute coordinates derived from cursor position and radius
type PingAbsoluteBounds struct {
	MinX, MaxX int
	MinY, MaxY int
	Active     bool
}

// GameStateResource wraps GameState for read access by systems
type GameStateResource struct {
	State *GameState
}

// PlayerResource holds the player cursor entity and derived state
type PlayerResource struct {
	Entity core.Entity
	bounds atomic.Pointer[PingBounds]
}

// GetBounds returns current bounds snapshot (lock-free read)
func (pr *PlayerResource) GetBounds() PingBounds {
	if b := pr.bounds.Load(); b != nil {
		return *b
	}
	return PingBounds{}
}

// SetBounds atomically updates bounds
func (pr *PlayerResource) SetBounds(b PingBounds) {
	pr.bounds.Store(&b)
}

// MaxTargetsPerGroup sets the hard limit for concurrent anchors in a single target group
const MaxTargetsPerGroup = 8

// TargetData holds coordinates and entity ID for a single target instance
type TargetData struct {
	Entity core.Entity
	PosX   int
	PosY   int
}

// TargetGroupState holds resolved navigation targets for a group
type TargetGroupState struct {
	Type    component.TargetType
	Targets [MaxTargetsPerGroup]TargetData
	Count   int
	Valid   bool // False if target entity destroyed or uninitialized
}

// TargetResource provides per-group target resolution accessible by all systems
// Written by NavigationSystem, read by species systems
type TargetResource struct {
	Groups [component.MaxTargetGroups]TargetGroupState
}

// GetGroup returns the resolved target state for a group
// Group 0 is always cursor; uninitialized groups return zero-value (Valid=false)
func (tr *TargetResource) GetGroup(groupID uint8) TargetGroupState {
	if int(groupID) >= len(tr.Groups) {
		return TargetGroupState{}
	}
	return tr.Groups[groupID]
}

// SetGroup configures a target group
func (tr *TargetResource) SetGroup(groupID uint8, state TargetGroupState) {
	if int(groupID) < len(tr.Groups) {
		tr.Groups[groupID] = state
	}
}

// === RouteGraph ===

// RouteGraphEntry wraps a computed route graph with per-SubType bandit populations
type RouteGraphEntry struct {
	Graph       *navigation.RouteGraph
	Populations map[uint8]*RoutePopulation // Keyed by species SubType (e.g. EyeType)
	Draining    bool
	DrainTime   time.Time
}

// RoutePopulation maintains a softmax bandit over routes for a single SubType
type RoutePopulation struct {
	Weights   []float64      // W[k] for K routes, normalized to sum ~1.0
	Pool      []int          // Pre-sampled route assignments, consumed FIFO
	PoolIndex int            // Next assignment to distribute
	Outcomes  []RouteOutcome // Accumulated since last weight update
}

// RouteOutcome records distance-at-death fitness for a single entity's route
type RouteOutcome struct {
	RouteIndex int
	Fitness    float64
}

// RouteGraphResource provides route graph and bandit population storage
// Written by NavigationSystem (graph computation), read/written by GatewaySystem and GeneticSystem (route distribution)
// Keyed by opaque uint32 ID (typically uint32(gatewayEntity))
type RouteGraphResource struct {
	graphs map[uint32]*RouteGraphEntry
}

// Get returns the route graph entry for the given ID, or nil
func (r *RouteGraphResource) Get(id uint32) *RouteGraphEntry {
	if r == nil || r.graphs == nil {
		return nil
	}
	return r.graphs[id]
}

// Set stores a route graph under the given ID, wrapping it in an entry
func (r *RouteGraphResource) Set(id uint32, rg *navigation.RouteGraph) {
	if r.graphs == nil {
		r.graphs = make(map[uint32]*RouteGraphEntry)
	}
	r.graphs[id] = &RouteGraphEntry{
		Graph: rg,
	}
}

// Remove deletes a route graph entry by ID
func (r *RouteGraphResource) Remove(id uint32) {
	if r.graphs != nil {
		delete(r.graphs, id)
	}
}

// Clear removes all route graph entries (used on regraph/reset)
func (r *RouteGraphResource) Clear() {
	if r.graphs != nil {
		clear(r.graphs)
	}
}

// PopRoute returns and consumes the next pre-sampled route for the given graph and SubType
// Returns -1 if graph missing, draining, pool exhausted, or no routes exist
func (r *RouteGraphResource) PopRoute(id uint32, subType uint8) int {
	entry := r.Get(id)
	if entry == nil || entry.Draining || entry.Graph == nil || len(entry.Graph.Routes) == 0 {
		return -1
	}
	pop := entry.getOrInitPopulation(subType)
	if pop.PoolIndex >= len(pop.Pool) {
		return -1
	}
	route := pop.Pool[pop.PoolIndex]
	pop.PoolIndex++
	return route
}

// RecordOutcome appends a fitness result for the given graph, SubType, and route
// Discards outcomes for draining or unknown entries
func (r *RouteGraphResource) RecordOutcome(id uint32, subType uint8, routeID int, fitness float64) {
	entry := r.Get(id)
	if entry == nil || entry.Draining {
		return
	}
	pop, ok := entry.Populations[subType]
	if !ok {
		return
	}
	pop.Outcomes = append(pop.Outcomes, RouteOutcome{
		RouteIndex: routeID,
		Fitness:    fitness,
	})
}

// ProcessUpdates runs EXP3 weight update and pool resampling for all populations with pending outcomes
func (r *RouteGraphResource) ProcessUpdates() {
	if r == nil || r.graphs == nil {
		return
	}
	for _, entry := range r.graphs {
		if entry.Draining {
			continue
		}
		for _, pop := range entry.Populations {
			if len(pop.Outcomes) == 0 {
				continue
			}
			pop.updateWeights()
			pop.samplePool()
		}
	}
}

// MarkDraining flags an entry for deferred cleanup after gateway death
// Subsequent PopRoute and RecordOutcome calls are rejected
func (r *RouteGraphResource) MarkDraining(id uint32, t time.Time) {
	entry := r.Get(id)
	if entry == nil {
		return
	}
	entry.Draining = true
	entry.DrainTime = t
}

// PruneDrained removes entries that have been draining past RouteDrainTimeout
func (r *RouteGraphResource) PruneDrained(now time.Time) {
	if r == nil || r.graphs == nil {
		return
	}
	for id, entry := range r.graphs {
		if entry.Draining && now.Sub(entry.DrainTime) >= parameter.RouteDrainTimeout {
			delete(r.graphs, id)
		}
	}
}

// getOrInitPopulation returns or lazily creates the population for a SubType
func (entry *RouteGraphEntry) getOrInitPopulation(subType uint8) *RoutePopulation {
	if entry.Populations == nil {
		entry.Populations = make(map[uint8]*RoutePopulation)
	}
	pop, ok := entry.Populations[subType]
	if ok {
		return pop
	}
	pop = &RoutePopulation{
		Weights:  make([]float64, len(entry.Graph.Routes)),
		Pool:     make([]int, parameter.RoutePoolDefaultSize),
		Outcomes: make([]RouteOutcome, 0, parameter.RoutePoolDefaultSize),
	}
	for i, route := range entry.Graph.Routes {
		pop.Weights[i] = route.Weight
	}
	pop.samplePool()
	entry.Populations[subType] = pop
	return pop
}

// updateWeights applies EXP3-style multiplicative weight update from accumulated outcomes
func (pop *RoutePopulation) updateWeights() {
	k := len(pop.Weights)
	if k == 0 {
		pop.Outcomes = pop.Outcomes[:0]
		return
	}

	sumFitness := make([]float64, k)
	counts := make([]int, k)
	for _, o := range pop.Outcomes {
		if o.RouteIndex >= 0 && o.RouteIndex < k {
			sumFitness[o.RouteIndex] += o.Fitness
			counts[o.RouteIndex]++
		}
	}

	for i := 0; i < k; i++ {
		if counts[i] > 0 {
			avg := sumFitness[i] / float64(counts[i])
			pop.Weights[i] *= math.Exp(parameter.RouteLearningRate * avg)
		}
	}

	// Floor + renormalize
	total := 0.0
	for i := 0; i < k; i++ {
		if pop.Weights[i] < parameter.RouteMinWeight {
			pop.Weights[i] = parameter.RouteMinWeight
		}
		total += pop.Weights[i]
	}
	if total > 0 {
		for i := 0; i < k; i++ {
			pop.Weights[i] /= total
		}
	}

	pop.Outcomes = pop.Outcomes[:0]
}

// samplePool fills pool via multinomial sampling from weights, then shuffles
func (pop *RoutePopulation) samplePool() {
	n := len(pop.Pool)
	k := len(pop.Weights)
	if n == 0 || k == 0 {
		pop.PoolIndex = n
		return
	}

	// Build CDF
	cdf := make([]float64, k)
	cdf[0] = pop.Weights[0]
	for i := 1; i < k; i++ {
		cdf[i] = cdf[i-1] + pop.Weights[i]
	}
	total := cdf[k-1]
	if total <= 0 {
		// Uniform fallback
		for i := 0; i < n; i++ {
			pop.Pool[i] = rand.IntN(k)
		}
	} else {
		for i := 0; i < n; i++ {
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

	// Fisher-Yates shuffle for unbiased distribution order
	for i := n - 1; i > 0; i-- {
		j := rand.IntN(i + 1)
		pop.Pool[i], pop.Pool[j] = pop.Pool[j], pop.Pool[i]
	}

	pop.PoolIndex = 0
}

// === Bridged Resources from Service ===

// ContentProvider defines the interface for content access
// Matches content.Service public API
type ContentProvider interface {
	CurrentContent() *core.PreparedContent
	NotifyConsumed(count int)
}

// ContentResource wraps a ContentProvider for the Resource
type ContentResource struct {
	Provider ContentProvider
}

// AudioPlayer defines the audio interface used by game systems
type AudioPlayer interface {
	// Global Pause
	SetPaused(paused bool)

	// Sound effects
	Play(core.SoundType) bool
	ToggleEffectMute() bool
	IsEffectMuted() bool
	IsRunning() bool

	// Music playback control
	ToggleMusicMute() bool
	IsMusicMuted() bool
	StartMusic()
	StopMusic()

	// Sequencer control
	SetMusicBPM(bpm int)
	SetMusicSwing(amount float64)
	SetMusicVolume(vol float64)
	SetBeatPattern(pattern core.PatternID, crossfadeSamples int, quantize bool)
	SetMelodyPattern(pattern core.PatternID, root int, crossfadeSamples int, quantize bool)
	TriggerMelodyNote(note int, velocity float64, durationSamples int, instr core.InstrumentType)
	ResetMusic()
	IsMusicPlaying() bool
}

// AudioResource wraps the audio player interface
type AudioResource struct {
	Player AudioPlayer
}

// NetworkProvider defines the interface for network access
type NetworkProvider interface {
	Send(peerID uint32, msgType uint8, payload []byte) bool
	Broadcast(msgType uint8, payload []byte)
	PeerCount() int
	IsRunning() bool
}

// NetworkResource wraps network provider for ECS access
type NetworkResource struct {
	Transport NetworkProvider
}