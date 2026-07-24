package engine

import (
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/genetic/registry"
	"github.com/lixenwraith/vi-fighter/navigation"
	"github.com/lixenwraith/vi-fighter/network"
	"github.com/lixenwraith/vi-fighter/status"
)

// Resource holds singleton game resources, initialized during GameContext creation, accessed via World.Resources
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

	// Bandit adaptation resource for route distribution
	Adaptation *AdaptationResource

	// Genetics
	Genetics *GeneticResource

	// Transient visual effects
	Transient *TransientResource

	// Telemetry
	Status *status.Registry

	// Bridged resources from services
	Content *ContentResource
	Audio   *AudioResource
	Network *NetworkResource
}

// === World Resources ===

// --- Time Resource ---

// TimeResource is time data snapshot for systems and is updated by ClockScheduler at the start of a tick
type TimeResource struct {
	// GameTime is the current time in the game world (affected by pause)
	GameTime time.Time

	// RealTime is the wall-clock time (unaffected by pause)
	RealTime time.Time

	// DeltaTime is the duration since the last update
	DeltaTime time.Duration
}

// Update overwrites all three fields
// Caller MUST hold updateMutex
func (tr *TimeResource) Update(gameTime, realTime time.Time, deltaTime time.Duration) {
	tr.GameTime = gameTime
	tr.RealTime = realTime
	tr.DeltaTime = deltaTime
}

// GameTimeNano returns game time as Unix nanoseconds
// Retained for fixed-point and integer comparison paths
func (tr *TimeResource) GameTimeNano() int64 { return tr.GameTime.UnixNano() }

// RealTimeNano returns wall-clock time as Unix nanoseconds
func (tr *TimeResource) RealTimeNano() int64 { return tr.RealTime.UnixNano() }

// DeltaTimeNano returns the tick delta in nanoseconds
func (tr *TimeResource) DeltaTimeNano() int64 { return int64(tr.DeltaTime) }

// --- Config Resource ---

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

// --- EventQueue Resource ---

// EventQueueResource wraps the event queue for systems access
type EventQueueResource struct {
	Queue *event.EventQueue
}

// --- GameState Resource ---

// GameStateResource wraps GameState for read access by systems
type GameStateResource struct {
	State *GameState
}

// --- Player Resource ---

// PlayerResource holds the player cursor entity and derived state
type PlayerResource struct {
	Entity core.Entity
	bounds atomic.Pointer[PingBounds]
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

// === Target Resource ===

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
	mu     sync.RWMutex
	groups [component.MaxTargetGroups]TargetGroupState
}

// GetGroup returns the resolved target state for a group
// Group 0 is always cursor; uninitialized groups return zero-value (Valid=false)
func (tr *TargetResource) GetGroup(groupID uint8) TargetGroupState {
	tr.mu.RLock()
	defer tr.mu.RUnlock()
	if int(groupID) >= len(tr.groups) {
		return TargetGroupState{}
	}
	return tr.groups[groupID]
}

// SetGroup configures a target group
func (tr *TargetResource) SetGroup(groupID uint8, state TargetGroupState) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	if int(groupID) < len(tr.groups) {
		tr.groups[groupID] = state
	}
}

func (tr *TargetResource) SetGroupTarget(groupID uint8, targetID int, td TargetData) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.groups[groupID].Targets[targetID] = td
}

func (tr *TargetResource) SetGroupCount(groupID uint8, count int) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.groups[groupID].Count = count
}

func (tr *TargetResource) SetGroupValidity(groupID uint8, valid bool) {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	tr.groups[groupID].Valid = valid
}

// === RouteGraph ===

// RouteGraphResource provides route graph geometry and constrained flow fields.
// Written by NavigationSystem, read by AdaptationSystem and movement logic.
type RouteGraphResource struct {
	graphs map[uint32]*navigation.RouteGraph
}

// Get returns the route graph for the given ID, or nil
func (r *RouteGraphResource) Get(id uint32) *navigation.RouteGraph {
	if r == nil || r.graphs == nil {
		return nil
	}
	return r.graphs[id]
}

// Set stores a route graph under the given ID
func (r *RouteGraphResource) Set(id uint32, rg *navigation.RouteGraph) {
	if r.graphs == nil {
		r.graphs = make(map[uint32]*navigation.RouteGraph)
	}
	r.graphs[id] = rg
}

// Remove deletes a route graph by ID
func (r *RouteGraphResource) Remove(id uint32) {
	if r.graphs != nil {
		delete(r.graphs, id)
	}
}

// Clear removes all route graphs
func (r *RouteGraphResource) Clear() {
	if r.graphs != nil {
		clear(r.graphs)
	}
}

// === Adaptation ===

// AdaptationEntry holds the discrete probability distribution and pools for a gateway
type AdaptationEntry struct {
	RouteCount  int
	Populations map[uint8]*RoutePopulation // Keyed by species SubType (e.g. EyeType)
	Draining    bool
	DrainTime   time.Time
}

// RoutePopulation holds the EXP3 weights and a pre-sampled consumer pool
type RoutePopulation struct {
	Weights []float64 // Read-only for consumers, written by AdaptationSystem
	Pool    []int     // Pre-sampled route assignments
	Head    int       // Consumer index
}

// AdaptationResource provides lock-free route allocations for spawners.
// Pools and weights are asynchronously populated by AdaptationSystem.
type AdaptationResource struct {
	Entries map[uint32]*AdaptationEntry
}

// PopRoute returns a pre-sampled route assignment for the spawner.
// Falls back to uniform random sampling if the pool is exhausted or uninitialized.
func (ar *AdaptationResource) PopRoute(id uint32, subType uint8) int {
	if ar.Entries == nil {
		return -1
	}

	entry, ok := ar.Entries[id]
	if !ok || entry.Draining || entry.RouteCount == 0 {
		return -1
	}

	if entry.Populations == nil {
		entry.Populations = make(map[uint8]*RoutePopulation)
	}

	pop, ok := entry.Populations[subType]
	if !ok {
		pop = &RoutePopulation{
			Weights: make([]float64, entry.RouteCount),
			Pool:    make([]int, 0),
			Head:    0,
		}

		// Clone baseline topological weights from subType 0 if available
		if basePop, hasBase := entry.Populations[0]; hasBase && len(basePop.Weights) == entry.RouteCount {
			copy(pop.Weights, basePop.Weights)
		} else {
			// Uniform fallback
			uniform := 1.0 / float64(entry.RouteCount)
			for i := range entry.RouteCount {
				pop.Weights[i] = uniform
			}
		}

		entry.Populations[subType] = pop
	}

	if pop.Head >= len(pop.Pool) {
		// Exhausted pool fallback
		return rand.IntN(entry.RouteCount)
	}

	route := pop.Pool[pop.Head]
	pop.Head++
	return route
}

// MarkDraining flags an entry for deferred cleanup
func (ar *AdaptationResource) MarkDraining(id uint32, t time.Time) {
	if ar.Entries == nil {
		return
	}
	if entry, ok := ar.Entries[id]; ok {
		entry.Draining = true
		entry.DrainTime = t
	}
}

// === Genetics ===

// GeneticResource exposes the GA registry for synchronous gene sampling by spawners.
// Includes PopulationID for future-proofing multi-island populations.
type GeneticResource struct {
	Registry *registry.Registry
}

// Sample requests a genotype from the specified species and population pool
func (gr *GeneticResource) Sample(speciesID uint8, populationID uint32) ([]float64, uint64) {
	if gr == nil || gr.Registry == nil {
		return nil, 0
	}
	// For now, PopulationID is ignored. Future expansion will map (SpeciesID + PopulationID) to a unique tracker.
	return gr.Registry.Sample(registry.SpeciesID(speciesID))
}

// SampleScout requests a stratified probe genotype covering all phenotype bins.
func (gr *GeneticResource) SampleScout(speciesID uint8, populationID uint32) ([]float64, uint64) {
	if gr == nil || gr.Registry == nil {
		return nil, 0
	}
	// populationID reserved for multi-island; single tracker per species today.
	return gr.Registry.SampleScout(registry.SpeciesID(speciesID))
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

// AudioResource exposes the audio engine directly. The engine's internal
// command channel is the decoupling layer; no interface mirror is kept here.
// Nil Resources.Audio = audio unavailable.
type AudioResource struct {
	Engine *audio.AudioEngine
}

// TODO: refactor wrapper after network stub developement

// NetworkPort is the service-side endpoint driven by NetworkSystem.
// Outbound: direct calls. Inbound: Drain per game tick (poll model keeps
// network goroutines out of the world event queue).
// Interface is transitional; collapses to a concrete network type once the
// package matures (same path as audio).
type NetworkPort interface {
	Send(peerID uint32, msgType uint8, payload []byte) bool
	Broadcast(msgType uint8, payload []byte)
	PeerCount() int
	IsRunning() bool
	// Drain fills dst with pending inbound notifications, returns count
	Drain(dst []network.Inbound) int
}

// NetworkResource wraps the network endpoint for ECS access
type NetworkResource struct {
	Port NetworkPort
}
