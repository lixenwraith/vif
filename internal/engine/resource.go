package engine

import (
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/pkg/audio"
	"github.com/lixenwraith/vi-fighter/pkg/genetic/registry"
	"github.com/lixenwraith/vi-fighter/pkg/navigation"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// Resource holds singleton game resources, initialized during GameContext creation, accessed via World.Resources
type Resource struct {
	// World Resource
	Time   *TimeResource
	Config *ConfigResource
	Game   *GameStateResource
	Player *PlayerResource
	Event  *EventQueueResource
	Rand   *RandResource

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

	// Player-domain view effects
	View *ViewResource

	// Per-runtime operator view of navigation internals
	NavigationDebug *NavigationDebugState

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
// Simulation code must not read wall time. CI guard:
// rg 'time\.Now\(\)|time\.Since' internal/system internal/mode must return zero hits.
type TimeResource struct {
	// GameTime is the current time in the game world (affected by pause)
	GameTime time.Time

	// RealTime is the clock's unpaused, unscaled instant. Wall time under
	// PausableClock, virtual under ManualClock — never read time.Now() instead.
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

// GameTimeNano returns game time as Unix nanoseconds for integer comparison paths.
// Not a seed source: tick granularity makes concurrent draws identical.
func (tr *TimeResource) GameTimeNano() int64 { return tr.GameTime.UnixNano() }

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

// PlayerResource indexes the cursor roster. Components.Cursor is the truth;
// this is the hot-path lookup and is written only by CursorSystem.
type PlayerResource struct {
	// Entity is the local cursor, 0 when none exists. Every read is under updateMutex.
	Entity core.Entity

	slots [parameter.MaxPlayers]core.Entity
	local uint8
	count int

	// The first scripted spawn declares the configuration's starting resources.
	// Session admission and late arrivals reuse the declaration instead of
	// hard-coding one game's values in app or NetworkSystem.
	initialHeat   int
	initialEnergy int
	initialSet    bool

	// A full game reset clears every entity. MetaSystem snapshots the closed
	// roster here first; CursorSystem consumes it when the reset FSM emits its
	// ordinary boot spawn, preserving deterministic shared creation order.
	restore      [parameter.MaxPlayers]CursorRosterEntry
	restoreCount int
	restoreLocal uint8
}

// CursorRosterEntry is the instance-local control assignment for one shared
// cursor. Every participant stores the same slots and peer IDs, but marks only
// its own assignment non-remote.
type CursorRosterEntry struct {
	Slot    uint8
	Control component.ControlKind
	PeerID  uint32
}

// PingAbsoluteBounds holds absolute coordinates derived from cursor position and radius
type PingAbsoluteBounds struct {
	MinX, MaxX int
	MinY, MaxY int
	Active     bool
}

// Valid reports whether a local cursor exists
func (pr *PlayerResource) Valid() bool { return pr.Entity != 0 }

// IsLocal reports whether e explicitly names the local cursor.
func (pr *PlayerResource) IsLocal(e core.Entity) bool {
	return pr.Entity != 0 && e == pr.Entity
}

// LocalSlot returns the roster slot the local cursor occupies
func (pr *PlayerResource) LocalSlot() uint8 { return pr.local }

// SetLocal rebinds which slot input and camera follow
func (pr *PlayerResource) SetLocal(slot uint8) {
	if int(slot) >= parameter.MaxPlayers {
		return
	}
	pr.local = slot
	pr.Entity = pr.slots[slot]
}

// Slot returns the entity in a roster slot, 0 when empty
func (pr *PlayerResource) Slot(i uint8) core.Entity {
	if int(i) >= parameter.MaxPlayers {
		return 0
	}
	return pr.slots[i]
}

// Count returns the number of occupied slots
func (pr *PlayerResource) Count() int { return pr.count }

// SetInitialResources records the gameplay configuration's cursor template.
func (pr *PlayerResource) SetInitialResources(heat, energy int) {
	if pr.initialSet {
		return
	}
	pr.initialHeat, pr.initialEnergy, pr.initialSet = heat, energy, true
}

// InitialResources returns the cursor template declared by the boot script.
func (pr *PlayerResource) InitialResources() (heat, energy int) {
	return pr.initialHeat, pr.initialEnergy
}

// PrepareRestore snapshots a closed roster before World.Clear. The caller walks
// slots in order, so consuming this fixed array recreates identical shared IDs.
func (pr *PlayerResource) PrepareRestore(entries []CursorRosterEntry) {
	pr.restore = [parameter.MaxPlayers]CursorRosterEntry{}
	pr.restoreCount = min(len(entries), parameter.MaxPlayers)
	copy(pr.restore[:], entries[:pr.restoreCount])
	pr.restoreLocal = pr.local
}

// TakeRestore returns and clears the reset roster pending behind World.Clear.
func (pr *PlayerResource) TakeRestore() ([parameter.MaxPlayers]CursorRosterEntry, int, uint8) {
	entries, count, local := pr.restore, pr.restoreCount, pr.restoreLocal
	pr.restore = [parameter.MaxPlayers]CursorRosterEntry{}
	pr.restoreCount = 0
	return entries, count, local
}

// FreeSlot returns the lowest unoccupied slot
func (pr *PlayerResource) FreeSlot() (uint8, bool) {
	for i := range parameter.MaxPlayers {
		if pr.slots[i] == 0 {
			return uint8(i), true
		}
	}
	return 0, false
}

// Bind installs a cursor in a slot; CursorSystem only
func (pr *PlayerResource) Bind(slot uint8, e core.Entity) {
	if int(slot) >= parameter.MaxPlayers || pr.slots[slot] != 0 {
		return
	}
	pr.slots[slot] = e
	pr.count++
	if slot == pr.local {
		pr.Entity = e
	}
}

// Unbind releases a slot; CursorSystem only
func (pr *PlayerResource) Unbind(slot uint8) {
	if int(slot) >= parameter.MaxPlayers || pr.slots[slot] == 0 {
		return
	}
	pr.slots[slot] = 0
	pr.count--
	if slot == pr.local {
		pr.Entity = 0
	}
}

// Clear empties the roster; paired with World.Clear
func (pr *PlayerResource) Clear() {
	pr.slots = [parameter.MaxPlayers]core.Entity{}
	pr.Entity = 0
	pr.count = 0
}

// --- Random Resource ---

// RandResource is the root of every simulation RNG stream.
// Systems draw a labelled generator in Init and keep it for their lifetime; no
// simulation path seeds from a clock.
type RandResource struct {
	root    uint64
	session atomic.Uint64
}

// NewRandResource creates the stream factory for a root seed
func NewRandResource(root uint64) *RandResource {
	return &RandResource{root: root}
}

// Root returns the seed the run was started with
func (r *RandResource) Root() uint64 { return r.root }

// Session returns the current session counter
func (r *RandResource) Session() uint64 { return r.session.Load() }

// NextSession advances the session counter. Called once a game's systems have
// finished initializing, so the next game draws different streams from one root.
func (r *RandResource) NextSession() uint64 { return r.session.Add(1) }

// Stream returns the labelled generator for a domain in the current session
func (r *RandResource) Stream(d core.Domain, label string) *vmath.FastRand {
	return vmath.NewSeededRand(r.DomainRoot(d), label)
}

// DomainRoot returns a domain's root seed, for packages that build their own
// generator instead of a vmath stream.
func (r *RandResource) DomainRoot(d core.Domain) uint64 {
	return vmath.DeriveSeed(r.sessionRoot(), core.DomainNames[d])
}

// sessionRoot folds the session counter into the root; session 0 is the root itself
func (r *RandResource) sessionRoot() uint64 {
	s := r.session.Load()
	if s == 0 {
		return r.root
	}
	return vmath.DeriveSeed(r.root, "session:"+strconv.FormatUint(s, 10))
}

// FollowCamera soft-follows one cursor: the camera shifts by the least amount that
// brings the cell back inside the dead zone, then clamps to the map. It lives here
// rather than in CameraSystem because a resize has to re-anchor the view without
// announcing a cursor move — an announcement is a shared event, and the flow-field
// throttle it dirties is shared state, so a local view change that emitted one put
// two instances on different recompute phases.
func (c *ConfigResource) FollowCamera(cursorX, cursorY int) {
	// Nothing to scroll: the renderer centres a map smaller than its viewport
	if c.MapWidth <= c.ViewportWidth && c.MapHeight <= c.ViewportHeight {
		c.CameraX, c.CameraY = 0, 0
		return
	}

	marginX := min(parameter.CameraDeadZoneMarginX, c.ViewportWidth/2)
	marginY := min(parameter.CameraDeadZoneMarginY, c.ViewportHeight/2)

	shift := func(v, camera, viewport, margin int, scroll bool) int {
		if !scroll {
			return 0
		}
		rel := v - camera
		switch {
		case rel < margin:
			return rel - margin
		case rel > viewport-margin-1:
			return rel - (viewport - margin - 1)
		}
		return 0
	}
	shiftX := shift(cursorX, c.CameraX, c.ViewportWidth, marginX, c.MapWidth > c.ViewportWidth)
	shiftY := shift(cursorY, c.CameraY, c.ViewportHeight, marginY, c.MapHeight > c.ViewportHeight)
	if shiftX == 0 && shiftY == 0 {
		return
	}
	// Clamped low last, so an axis whose map is shorter than the viewport settles at
	// zero rather than at a negative bound. Moving this out of CameraSystem changed
	// that one case: the old order clamped high last and drove the camera negative on
	// an axis it was not even scrolling.
	c.CameraX = max(0, min(c.CameraX+shiftX, c.MapWidth-c.ViewportWidth))
	c.CameraY = max(0, min(c.CameraY+shiftY, c.MapHeight-c.ViewportHeight))
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
	spin    int       // Fallback rotation when the pool is exhausted
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
		// Exhausted pool: rotate deterministically rather than draw from a
		// global generator no seed reaches
		route := pop.spin % entry.RouteCount
		pop.spin++
		return route
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

// ContentProvider supplies spawn content; implementations are goroutine-safe
type ContentProvider interface {
	NextBlock() (core.CodeBlock, bool)
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

// NetworkPort is the service-side endpoint driven by NetworkSystem.
// Outbound: direct calls. Inbound: Drain per game tick (poll model keeps
// network goroutines out of the world event queue).
// Interface is transitional; collapses to a concrete network type once the
// package matures (same path as audio).
type NetworkPort interface {
	Send(peerID uint32, msgType uint8, payload []byte) bool
	Broadcast(msgType uint8, payload []byte)
	// BroadcastExcept sends to every connected participant but one. It is how a
	// relayed artifact reaches the rest of a mesh without going back down the link
	// it arrived on, where the sender already holds it.
	BroadcastExcept(exclude uint32, msgType uint8, payload []byte)
	PeerCount() int
	IsRunning() bool
	// Drain fills dst with pending inbound notifications, returns count
	Drain(dst []network.Inbound) int
}

// NetworkSessionPort exposes barrier metadata negotiated before simulation starts.
type NetworkSessionPort interface {
	ParticipantID() uint32
	BarrierDelayTicks() uint64
}

// SharedStateDigest is the runtime D-11 probe. Hash covers the complete
// SnapshotShared surface; the parts make a mismatch diagnosable without sending
// the snapshot itself.
type SharedStateDigest struct {
	Hash      uint64
	Positions uint64
	Kinetics  uint64
	Combat    uint64
	Context   uint64
	Status    uint64
	Surface   uint64
}

// NetworkResource wraps the network endpoint for ECS access
type NetworkResource struct {
	Port              NetworkPort
	ParticipantID     uint32
	BarrierDelayTicks uint64
	// SharedDigest is supplied by App after construction. NetworkSystem calls it
	// under the world lock when publishing periodic runtime parity probes.
	SharedDigest func() SharedStateDigest

	// OnDeparture is called under the world lock when a participant leaves, so the
	// session layer can return its identity to the pool. It must not take a lock
	// that a world-lock acquisition waits behind.
	OnDeparture func(participant uint32)
}

// NewNetworkResource binds a poll endpoint and its deterministic barrier identity.
func NewNetworkResource(port NetworkPort) *NetworkResource {
	r := &NetworkResource{Port: port, ParticipantID: 1, BarrierDelayTicks: parameter.NetworkBarrierDelayTicks}
	if session, ok := port.(NetworkSessionPort); ok {
		if id := session.ParticipantID(); id != 0 {
			r.ParticipantID = id
		}
		if delay := session.BarrierDelayTicks(); delay != 0 {
			r.BarrierDelayTicks = delay
		}
	}
	return r
}
