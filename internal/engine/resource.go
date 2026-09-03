package engine

import (
	"sort"
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
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
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

	// D-18: the cells this instance's own input has placed the local cursor on and
	// not yet seen announced, oldest first. A placement crosses as a D-3 artifact
	// and reaches the store a playout lead later, so a producer that resolved its
	// next motion from the store would resolve it from a cell the player has
	// already left. Ring rather than slice: the depth is bounded by the lead, and
	// prediction must not allocate on the input path.
	predicted  [parameter.MaxPredictedCursorCells]component.PositionComponent
	predHead   int
	predCount  int
	predLatest component.PositionComponent
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

// SetLocal rebinds which slot input and camera follow. parameter.NoPlayerSlot
// leaves this instance driving nothing, which is what a dedicated host does: it
// authors the shared world and puts no cursor on the map.
func (pr *PlayerResource) SetLocal(slot uint8) {
	if slot == parameter.NoPlayerSlot {
		pr.local, pr.Entity = slot, 0
		pr.DropPrediction()
		return
	}
	if int(slot) >= parameter.MaxPlayers {
		return
	}
	pr.local = slot
	pr.Entity = pr.slots[slot]
	pr.DropPrediction()
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
		pr.DropPrediction()
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
		pr.DropPrediction()
	}
}

// Clear empties the roster; paired with World.Clear
func (pr *PlayerResource) Clear() {
	pr.slots = [parameter.MaxPlayers]core.Entity{}
	pr.Entity = 0
	pr.count = 0
	pr.DropPrediction()
}

// Predict records a cell this instance has requested for its own cursor (D-18).
// The caller has already resolved the cell CursorSystem will announce, so a
// prediction that survives to its announcement matches it exactly.
func (pr *PlayerResource) Predict(pos component.PositionComponent) {
	if pr.predCount == len(pr.predicted) {
		// Nothing is reconciling. Fall back to the store rather than carry a queue
		// whose head no announcement will ever match.
		pr.DropPrediction()
	}
	pr.predicted[(pr.predHead+pr.predCount)%len(pr.predicted)] = pos
	pr.predCount++
	pr.predLatest = pos
}

// Reconcile settles one announced placement of the local cursor against the
// prediction queue. Matching the oldest outstanding prediction pops it; anything
// else is an authoritative value the prediction did not produce, and D-18 discards
// the prediction rather than merging it.
func (pr *PlayerResource) Reconcile(pos component.PositionComponent) {
	if pr.predCount == 0 {
		return
	}
	if pr.predicted[pr.predHead] != pos {
		pr.DropPrediction()
		return
	}
	pr.predHead = (pr.predHead + 1) % len(pr.predicted)
	pr.predCount--
}

// DropPrediction abandons every outstanding prediction, so the local cell reads
// the store again. Paired with anything that rebinds or retires the local cursor:
// a queue outliving the entity it described would place its successor.
func (pr *PlayerResource) DropPrediction() {
	pr.predHead, pr.predCount = 0, 0
	pr.predLatest = component.PositionComponent{}
}

// PredictedCell returns the cell this instance's own input has placed the local
// cursor on, valid only while a prediction is outstanding.
func (pr *PlayerResource) PredictedCell() (component.PositionComponent, bool) {
	return pr.predLatest, pr.predCount > 0
}

// PredictedDepth returns the number of outstanding predictions, for the view record
func (pr *PlayerResource) PredictedDepth() int { return pr.predCount }

// --- Random Resource ---

// RandResource is the root of every simulation RNG stream.
// Systems draw a labelled generator in Init and keep it for their lifetime; no
// simulation path seeds from a clock.
type RandResource struct {
	root    uint64
	session atomic.Uint64

	// The generator most recently issued for each domain and label. A system draws
	// its stream once in Init and holds that pointer for its lifetime, so these are
	// the very generators the simulation draws from — which is what lets a snapshot
	// resume all of them without every system having to hand its own over.
	//
	// Not cleared by NextSession: the counter advances immediately after a game's
	// systems have finished drawing, so a map cleared there would be empty for the
	// whole of the game it describes. A re-Init overwrites each key with the fresh
	// generator it just drew, which is the same replacement the system does to its
	// own field, so the two stay the same object.
	streamMu sync.Mutex
	streams  map[streamKey]*vmath.FastRand
}

// streamKey names one labelled generator. Domain is part of the identity: a dual
// system draws the same label in both domains and the two must not collapse (D-8).
type streamKey struct {
	domain core.Domain
	label  string
}

// StreamState is one generator's position, named by the domain and label that
// identify it rather than by an index, so a snapshot survives a system being
// added, removed or reordered.
type StreamState struct {
	Domain core.Domain `toml:"domain"`
	Label  string      `toml:"label"`
	State  uint64      `toml:"state"`
}

// NewRandResource creates the stream factory for a root seed
func NewRandResource(root uint64) *RandResource {
	return &RandResource{root: root, streams: make(map[streamKey]*vmath.FastRand, 64)}
}

// Root returns the seed the run was started with
func (r *RandResource) Root() uint64 { return r.root }

// Session returns the current session counter
func (r *RandResource) Session() uint64 { return r.session.Load() }

// NextSession advances the session counter. Called once a game's systems have
// finished initializing, so the next game draws different streams from one root.
func (r *RandResource) NextSession() uint64 { return r.session.Add(1) }

// Stream returns the labelled generator for a domain in the current session, and
// records it as that name's live generator so SaveStreams can report where the
// stream has reached.
//
// Every call issues a fresh generator, which is what a re-Init after a reset
// needs: a resumed one would carry the finished game's position into the new one.
// Two live holders of a single name would therefore be one stream by name and two
// by behaviour, and only the later would be restorable —
// TestSystemStreamLabelsAreUnique rules that out over the real system set.
func (r *RandResource) Stream(d core.Domain, label string) *vmath.FastRand {
	g := vmath.NewSeededRand(r.DomainRoot(d), label)
	r.streamMu.Lock()
	r.streams[streamKey{domain: d, label: label}] = g
	r.streamMu.Unlock()
	return g
}

// SaveStreams reports every issued generator's position, sorted by domain then
// label so two instances that issued the same streams produce byte-identical
// output. This is the D-19 answer to §4.1's "~24 per-system RNG streams": they are
// enumerable because they are issued through one factory, not because anything
// keeps a list by hand.
func (r *RandResource) SaveStreams() []StreamState {
	r.streamMu.Lock()
	defer r.streamMu.Unlock()
	out := make([]StreamState, 0, len(r.streams))
	for k, g := range r.streams {
		out = append(out, StreamState{Domain: k.domain, Label: k.label, State: g.State()})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Domain != out[j].Domain {
			return out[i].Domain < out[j].Domain
		}
		return out[i].Label < out[j].Label
	})
	return out
}

// LoadStreams resumes each named generator in place, so a system holding the
// pointer it drew in Init continues from the captured position without knowing a
// transfer happened. A name the receiving build does not issue is reported rather
// than dropped: it means the two sides disagree about which streams exist, and a
// stream that silently restarts is a divergence nothing else would catch.
func (r *RandResource) LoadStreams(states []StreamState) []string {
	r.streamMu.Lock()
	defer r.streamMu.Unlock()
	var unknown []string
	for _, st := range states {
		g, ok := r.streams[streamKey{domain: st.Domain, label: st.Label}]
		if !ok {
			unknown = append(unknown, core.DomainNames[st.Domain]+":"+st.Label)
			continue
		}
		g.SetState(st.State)
	}
	return unknown
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

// OffTickDrainPort is a transport that can be polled between two ticks without the
// poll counting as one.
//
// The Drain contract is "once per game tick", and some transports measure
// themselves against it: a virtual link's clock, its shaping credit and its probe
// interval are all denominated in polls. Phase 6's correction exchange has to
// translate what has already arrived without moving that clock — a manifest
// answered a tick late is a manifest answered about a world the receiver has
// already predicted past — so a port that measures itself offers this second door.
// A transport with no such measurement (a socket) need not implement it; the
// caller falls back to Drain, which for it is the same operation.
type OffTickDrainPort interface {
	DrainOffTick(dst []network.Inbound) int
}

// NetworkSessionPort exposes barrier metadata negotiated before simulation starts.
type NetworkSessionPort interface {
	ParticipantID() uint32
	BarrierDelayTicks() uint64
}

// LinkMeasuringPort is a transport that measures its own links.
//
// The whole of it is optional and asserted for rather than required, because a
// port that cannot measure a link is still a perfectly good port: the crossing
// stream, the syncs and the digests do not depend on any of this. What depends
// on it is the *cadence*, and a session on an unmeasured transport simply keeps
// its nominal one.
//
// The direction of the two halves is what keeps network timing out of the
// simulation. SetLinkReport is the only thing the world tells the transport, and
// it is a scheduling hint the far end may publish sooner because of; LinkMetric
// is the only thing the transport tells the world, and nothing but transport
// scheduling may read it. No round trip, no delivery rate and no jitter estimate
// reaches a component store, an RNG stream, a replay or a game decision.
type LinkMeasuringPort interface {
	// Peers lists the directly connected participants in a stable order.
	Peers() []uint32

	// SetLinkReport publishes what this instance tells a probing peer about its
	// own picture: the tick it stands on, how far behind it believes it is, how
	// much the last correction had to move, and where its cursor is.
	SetLinkReport(network.LinkReport)

	// LinkMetric returns one link's estimate. The zero value is an unmeasured
	// link, which a controller must read as "no evidence" rather than as a slow
	// one.
	LinkMetric(peer uint32) linkpace.Metrics

	// ObserveTransfer folds a completed bulk transfer into a link's estimate —
	// the join's own capture, which is the only throughput measurement available
	// before a probe has completed a round trip.
	ObserveTransfer(peer uint32, bytes int64, elapsed time.Duration)
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

	// Groups is one hash per snapshot record, present only when the caller asked
	// for detail. A category tells an operator that the status surface disagrees;
	// it does not say which of a hundred records did, and a host's own log cannot
	// answer that on its own. Detail is therefore requested only while a mismatch
	// is already outstanding, so a healthy session carries none of it.
	Groups map[string]uint64
}

// NetworkResource wraps the network endpoint for ECS access
type NetworkResource struct {
	Port              NetworkPort
	ParticipantID     uint32
	BarrierDelayTicks uint64
	// SharedDigest is supplied by App after construction. NetworkSystem calls it
	// under the world lock when publishing periodic runtime parity probes.
	// detail asks for the per-record breakdown, which costs a map on the wire and
	// is worth it only once something disagrees.
	SharedDigest func(detail bool) SharedStateDigest

	// OnDeparture is called under the world lock when a participant leaves, so the
	// session layer can return its identity to the pool. It must not take a lock
	// that a world-lock acquisition waits behind.
	OnDeparture func(participant uint32)

	// OnCorrection hands one reassembled authoritative correction to the session
	// layer. It is called under the world lock, from the tick that drained the last
	// chunk, so it must do nothing but take the bytes: decoding a correction is
	// hundreds of kilobytes of JSON and installing one needs the lock this call
	// already holds. What it hands to is a queue the corrector drains between two
	// ticks.
	OnCorrection func(tick uint64, body []byte)

	// OnSelective hands one Phase 6 selective-correction frame to the session
	// layer: a manifest, a receiver's answer to one, or the repair that answers
	// that. Like OnCorrection it is called under the world lock, from the tick that
	// drained the frame, so it must do nothing but take the bytes — the comparison,
	// the hashing and the apply all happen between two ticks.
	//
	// kind is the network.MessageType the frame arrived as. One seam rather than
	// three keeps the transport's knowledge of the protocol to "this is a
	// selective-correction frame from that peer", which is all it can usefully
	// have.
	OnSelective func(kind uint8, from uint32, body []byte)

	// OnAuthority hands one Phase 7 succession frame — a report, a vote or a
	// handoff record — to the session layer, under the same rule as the two seams
	// above: called under the world lock, so it takes the bytes and decides
	// nothing. Succession is a decision about who may author, which belongs beside
	// the correction protocol rather than inside the transport.
	OnAuthority func(kind uint8, from uint32, body []byte)

	// OnPeerLost reports a direct neighbour's departure to the session layer,
	// beside the identity release OnDeparture does. It is a different question:
	// which identities the lobby may hand out again is local bookkeeping, and
	// whether the participant that just left was the one authoring is what starts
	// a succession.
	OnPeerLost func(participant uint32)

	// Authority is the participant currently authoring the Shared world, and Term
	// the generation it authors under. Both are written by the session layer when
	// a handoff is adopted and read here per tick, so the roster artifacts only
	// the authority may produce follow the authority rather than the participant
	// that opened the session.
	Authority atomic.Uint32
	Term      atomic.Uint64
}

// NewNetworkResource binds a poll endpoint and its deterministic barrier identity.
func NewNetworkResource(port NetworkPort) *NetworkResource {
	r := &NetworkResource{Port: port, ParticipantID: 1, BarrierDelayTicks: parameter.NetworkBarrierDelayTicks}
	r.Authority.Store(1)
	r.Term.Store(uint64(1))
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
