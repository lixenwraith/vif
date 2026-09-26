package engine

import (
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/prof"
	"github.com/lixenwraith/vif/internal/vlog"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// World contains all entities and their components using typed stores
type World struct {
	// === Immutable after init ===
	// Pointers set once in NewWorld; the state they reference is
	// update-mutex guarded, the pointers themselves are not

	Resources  *Resource
	Components Component
	Positions  *Position

	// systems pairs each registered system with its declared profile. Appended only
	// by AddSystem during single-threaded
	// construction, then frozen by Seal. UpdateLocked ranges it every tick
	// without synchronization, so runtime registration is not permitted
	systems []systemEntry
	sealed  atomic.Bool

	// === Update-mutex guarded ===
	// Entity ids, component stores, positions and masks.
	// The tick, event, input and render paths all acquire it.
	// There is no inner locking.

	// nextEntityID counts per domain; index with core.Domain. Both start at 1
	// so ID 0 is never issued and stays the "no entity" value.
	nextEntityID  [core.DomainCount]uint64
	componentMask map[core.Entity]uint64
	updateMutex   UpdateMutex
	audit         auditScope // the system a domain violation is attributed to

	// === Self-synchronized ===
	// Readable from any goroutine, including the post-tick telemetry tail
	// that runs after the update mutex is released

	// Entity counts per domain; index with core.Domain. A cross-instance
	// comparison reads the shared half, which must match exactly.
	createdCount   [core.DomainCount]atomic.Int64
	destroyedCount [core.DomainCount]atomic.Int64

	// origin tags events pushed while a non-simulation producer drives the world.
	// Written only under updateMutex via WithOrigin; atomic because lock-free
	// pushers read it. CI guard: WithOrigin must not appear outside a locked path.
	origin atomic.Int32

	// domain tags events pushed by a system serving both domains. Written only
	// under updateMutex via WithDomain; atomic because lock-free pushers read it.
	domain atomic.Int32

	// sessionShared records that this world is, or reproduces, one shared with
	// another participant. It is monotone — a session cannot be un-shared within a
	// run — and it is not derived from the live transport, because a replay and a
	// mid-run catch-up hold none and must still reach the bounds of the run they
	// reproduce. Set from the join anchor at construction and by AttachTransport.
	sessionShared atomic.Bool

	// degradedSystems remembers the disabled systems that already reported an
	// optional dependent, so a region re-applying its config reports once
	degradedSystems sync.Map

	// predicted holds the shared derivations this instance has made against a world
	// an authority may still correct; see prediction.go. Its own lock rather than
	// the update mutex, because a derivation is recorded from the lock-free push
	// path and released from an install that already holds the update mutex.
	predictionMu            sync.Mutex
	predicted               []predictedDeath
	predicting              atomic.Bool // PredictsShared, latched per tick
	followJournal           atomic.Bool // a replay: session state and settlements are records
	statPredictionPending   *atomic.Int64
	statPredictionConfirmed *atomic.Int64
	statPredictionDropped   *atomic.Int64
}

// NewWorld creates a new ECS world with dynamic component store support
func NewWorld() *World {
	w := &World{
		componentMask: make(map[core.Entity]uint64, 16384), // reasonable small screen size that doesn't require increase
		Resources:     &Resource{Rand: NewRandResource(0)}, // app overwrites with the run seed
		systems:       make([]systemEntry, 0),
	}
	for d := range w.nextEntityID {
		w.nextEntityID[d] = 1
	}

	initComponents(w)

	return w
}

// CreateEntity reserves a new entity ID in the given domain
// Caller holds updateMutex (all creation paths: systems, event handlers)
func (w *World) CreateEntity(d core.Domain) core.Entity {
	id := w.nextEntityID[d]
	w.nextEntityID[d]++
	// Mirror the count so telemetry never reads nextEntityID off-lock
	w.createdCount[d].Add(1)
	return core.MakeEntity(d, id)
}

// WithOrigin runs fn with PushEvent tagging its events as origin, restoring the
// previous tag. Caller MUST hold updateMutex: no system may run inside fn.
func (w *World) WithOrigin(o event.Origin, fn func()) {
	prev := w.origin.Swap(int32(o))
	defer w.origin.Store(prev)
	fn()
}

// WithDomain runs fn with PushEvent tagging its events as domain d, restoring the
// previous tag. Caller MUST hold updateMutex: no system may run inside fn.
func (w *World) WithDomain(d core.Domain, fn func()) {
	prev := w.domain.Swap(int32(d))
	defer w.domain.Store(prev)
	fn()
}

// Domain returns the ambient producer domain
func (w *World) Domain() core.Domain { return core.Domain(w.domain.Load()) }

// AddComponentMask marks a component bit as active for the specified entity
// Callers MUST hold updateMutex, matching removeEntity/wipeAll.
func (w *World) AddComponentMask(e core.Entity, bit uint64) {
	if domainAudit.Load() {
		auditComponentDomain(w, e, bit)
		auditEntityDomain(w, e)
	}
	w.componentMask[e] |= bit
}

// GetComponentMask returns the entity signature and whether the entity is tracked
// Caller MUST hold updateMutex
func (w *World) GetComponentMask(e core.Entity) (uint64, bool) {
	bit, ok := w.componentMask[e]
	return bit, ok
}

// HasEntity reports whether the world still holds e. A correction install destroys
// the shared entities its capture omits, so this is how an effect keyed to one
// learns that it outlived its subject.
// Caller MUST hold updateMutex
func (w *World) HasEntity(e core.Entity) bool {
	_, ok := w.componentMask[e]
	return ok
}

// Issued reports whether this world's allocator has reached e. Caller MUST hold
// updateMutex.
func (w *World) Issued(e core.Entity) bool {
	return e.Domain() < core.DomainCount && e.ID() < w.nextEntityID[e.Domain()]
}

// RemoveComponentMask clears a component bit for the specified entity
// Caller MUST hold updateMutex
func (w *World) RemoveComponentMask(e core.Entity, bit uint64) {
	w.componentMask[e] &^= bit // &^= clears unconditionally
}

// DestroyEntity removes all components associated with an entity
// Caller guarantees entity doesn't have ProtectAll
func (w *World) DestroyEntity(e core.Entity) {
	w.removeEntity(e)
	w.destroyedCount[e.Domain()].Add(1)
}

// DestroyDomainEntities removes every entity of one domain. A projection world
// accumulates player-domain effects of the shared deaths it re-derives, and nothing
// else in it ever retires them.
// Caller MUST hold updateMutex.
func (w *World) DestroyDomainEntities(d core.Domain) {
	var doomed []core.Entity
	for e := range w.componentMask {
		if e.Domain() == d {
			doomed = append(doomed, e)
		}
	}
	slices.Sort(doomed)
	w.DestroyEntitiesBatch(doomed)
}

// DestroyEntitiesBatch removes entities without protection checks
// Caller guarantees no entity has ProtectAll - use for known-safe bulk operations
func (w *World) DestroyEntitiesBatch(entities []core.Entity) {
	if len(entities) == 0 {
		return
	}
	w.removeEntitiesBatch(entities)
	for _, e := range entities {
		w.destroyedCount[e.Domain()].Add(1)
	}
}

// Clear removes all entities and components from the world
// Caller MUST hold updateMutex: nextEntityID is update-mutex state
// Session counters and the cursor roster reset with the entities they describe
func (w *World) Clear() {
	for d := range w.nextEntityID {
		w.nextEntityID[d] = 1
		w.createdCount[d].Store(0)
		w.destroyedCount[d].Store(0)
	}
	w.Positions.ResetTelemetry()
	w.Resources.Player.Clear()
	w.wipeAll()
}

// systemEntry pairs a registered system with the profile it was registered under
type systemEntry struct {
	sys     System
	profile SystemProfile
	update  *prof.Timer // bound at Seal
}

// AddSystem registers a system under its declared profile and sorts by priority.
// Construction only; panics once Seal has frozen the set.
func (w *World) AddSystem(system System, profile SystemProfile) {
	if w.sealed.Load() {
		panic("engine: AddSystem after Seal")
	}

	w.systems = append(w.systems, systemEntry{sys: system, profile: profile})

	// Sort by priority (bubble sort, small N)
	for i := range len(w.systems) - 1 {
		for j := range len(w.systems) - i - 1 {
			if w.systems[j].sys.Priority() > w.systems[j+1].sys.Priority() {
				w.systems[j], w.systems[j+1] = w.systems[j+1], w.systems[j]
			}
		}
	}
}

// HasSystem reports whether a system with the given name is registered
func (w *World) HasSystem(name string) bool {
	for i := range w.systems {
		if w.systems[i].sys.Name() == name {
			return true
		}
	}
	return false
}

// SystemInitOrder resolves declared dependencies into a deterministic
// initialization order. This is not the tick order, which AddSystem fixes from
// Priority(); a system may legitimately initialize before one that ticks first.
func (w *World) SystemInitOrder() ([]string, error) {
	deps := make(map[string][]string, len(w.systems))
	for i := range w.systems {
		required := w.systems[i].profile.Requires
		names := make([]string, 0, len(required))
		for _, d := range required {
			names = append(names, d.Name)
		}
		deps[w.systems[i].sys.Name()] = names
	}
	return core.TopoSort(deps)
}

// SystemsRequiring returns the registered systems declaring name at the given
// strength, sorted. Callers use it to refuse or report a disable request.
func (w *World) SystemsRequiring(name string, strength DependencyStrength) []string {
	var dependents []string
	for i := range w.systems {
		for _, d := range w.systems[i].profile.Requires {
			if d.Name == name && d.Strength == strength {
				dependents = append(dependents, w.systems[i].sys.Name())
				break
			}
		}
	}
	slices.Sort(dependents)
	return dependents
}

// Systems returns a copy of all registered systems
// Used by Scheduler for event handler auto-registration
func (w *World) Systems() []System {
	result := make([]System, len(w.systems))
	for i := range w.systems {
		result[i] = w.systems[i].sys
	}
	return result
}

// UpdateLocked runs all systems assuming the caller already holds updateMutex
func (w *World) UpdateLocked() {
	p := w.Resources.Prof
	phase := p.BeginPhase(prof.PhaseSystems)
	audit := domainAudit.Load()
	for i := range w.systems {
		e := &w.systems[i]
		// Attribution is a per-tick decision, matching the audit gate itself
		if audit {
			w.setAuditScope(e.sys.Name(), e.profile.Domain)
		}
		span := p.Begin(e.update)
		e.sys.Update()
		span.End()
	}
	if audit {
		w.clearAuditScope()
	}
	phase.End()
}

// Seal freezes the system set and binds each system's profiler timer; called by
// Scheduler.Start before its tick and event goroutines begin ranging it
func (w *World) Seal() {
	if w.sealed.Swap(true) {
		return
	}
	for i := range w.systems {
		w.systems[i].update = w.Resources.Prof.Timer(prof.KindSystem, w.systems[i].sys.Name())
	}
}

// AllowSystemDisable reports whether name may be disabled. A system declaring
// it required refuses the request; an optional dependent is reported once.
// Region configs are checked at load time; this covers the runtime commands and
// FSM actions that validation cannot see.
func (w *World) AllowSystemDisable(name string) bool {
	if required := w.SystemsRequiring(name, DepRequired); len(required) > 0 {
		vlog.Warn("system", "msg", "disable refused", "system", name,
			"required_by", strings.Join(required, ","))
		return false
	}
	if optional := w.SystemsRequiring(name, DepOptional); len(optional) > 0 {
		if _, reported := w.degradedSystems.LoadOrStore(name, true); !reported {
			vlog.Info("system", "msg", "dependents degraded", "system", name,
				"optional_for", strings.Join(optional, ","))
		}
	}
	return true
}

// RunSafe executes a function while holding the world's update lock
func (w *World) RunSafe(fn func()) {
	w.updateMutex.Lock()
	defer w.updateMutex.Unlock()
	fn()
}

// Lock acquires a lock on the world's update mutex
func (w *World) Lock() {
	w.updateMutex.Lock()
}

// TryLock attempts to acquire the update mutex without blocking
// Returns true if lock acquired, false if already held
func (w *World) TryLock() bool {
	return w.updateMutex.TryLock()
}

// Unlock releases the update mutex
func (w *World) Unlock() {
	w.updateMutex.Unlock()
}

// SetLockSampling updates hold-time instrumentation for this world.
func (w *World) SetLockSampling(on bool) { w.updateMutex.SetSampling(on) }

// Update runs all systems sequentially
func (w *World) Update() {
	w.RunSafe(func() {
		w.UpdateLocked()
	})
}

// Rand returns the labelled RNG stream for a domain in the current session.
// Single seeding entry point for systems: never seed from a clock.
func (w *World) Rand(d core.Domain, label string) *vmath.FastRand {
	return w.Resources.Rand.Stream(d, label)
}

// PushEvent emits a game event carrying the ambient producer and domain tags. HOT-PATH for all systems communication
func (w *World) PushEvent(eventType event.EventType, payload any) {
	w.pushEvent(eventType, payload, event.Origin(w.origin.Load()), core.Domain(w.domain.Load()))
}

// PushEventFull emits with explicit origin and domain tags, for replay and
// transport, which restore both from a record rather than from the ambient tags
func (w *World) PushEventFull(eventType event.EventType, payload any, origin event.Origin, domain core.Domain) {
	w.pushEvent(eventType, payload, origin, domain)
}

// PushEventOrigin emits with an explicit origin tag, for producers outside any WithOrigin scope
func (w *World) PushEventOrigin(eventType event.EventType, payload any, origin event.Origin) {
	w.pushEvent(eventType, payload, origin, core.Domain(w.domain.Load()))
}

// PushLocalOrigin emits an explicitly attributed event that must never
// replicate. It is the out-of-band counterpart to PushLocal.
func (w *World) PushLocalOrigin(eventType event.EventType, payload any, origin event.Origin) {
	w.pushEvent(eventType, payload, origin, core.DomainPlayer)
}

// PushEventDomain emits with an explicit domain tag, for producers outside any WithDomain scope
func (w *World) PushEventDomain(eventType event.EventType, payload any, domain core.Domain) {
	w.pushEvent(eventType, payload, event.Origin(w.origin.Load()), domain)
}

// MapSizeLocal reports whether this world may derive map bounds from its own
// terminal: nobody else shares it, so a crop rewriting shared state is admissible
// (D-14).
//
// Map bounds are shared simulation state, so every writer of them must be a
// function of state every participant agrees on — and, because a run is reproduced
// by replaying its record stream, of state a reproduction agrees on too. That is
// the whole of SessionShared: a second rostered cursor, which is shared state, or
// the latch a session run carries, which travels in the journal anchor rather than
// being read off the live transport. Deriving the verdict from a transport made a
// replay crop where the run it reproduces did not.
func (w *World) MapSizeLocal() bool { return !w.SessionShared() }

// SessionShared reports whether this world is, or reproduces, one shared with
// another participant: a second rostered cursor, a bound session transport, or the
// latch a reproduction adopted from its anchor.
func (w *World) SessionShared() bool {
	if w.sessionShared.Load() || w.Resources.Player.Count() > 1 {
		return true
	}
	net := w.Resources.Network
	return net != nil && net.Port != nil
}

// SessionBarrier reports whether this run's crossings pass through the playout
// barrier. It is the latch alone, not the roster: a local multi-cursor setup shares
// no artifacts with anyone, while a reproduction of a session has to defer its
// re-derived crossings by the same lead the run it reproduces did, or it applies
// them earlier than the run did and drifts by exactly that lead.
func (w *World) SessionBarrier() bool { return w.sessionShared.Load() }

// MarkSessionShared latches the world as shared. Called when a session transport is
// bound and when a run is constructed to reproduce one; never cleared, because the
// bounds a participant has already adopted outlive the link that delivered them.
func (w *World) MarkSessionShared() { w.sessionShared.Store(true) }

// LiveSession reports whether this world is currently sharing ticks with peers.
// A local multi-cursor setup has no transport and remains operator-controlled.
func (w *World) LiveSession() bool {
	net := w.Resources.Network
	return net != nil && net.Port != nil && net.Port.IsRunning() && net.Port.PeerCount() > 0
}

// LocalParticipant is this instance's session identity, zero when no transport is
// attached. It is the seam every owner-authored rule turns on.
func (w *World) LocalParticipant() uint32 {
	r := w.Resources.Network
	if r == nil || r.Port == nil {
		return 0
	}
	return r.ParticipantID
}

// CoordinatorParticipant is the identity the handshake always assigns to the host.
// It is the one participant every topology the session can build has a path to,
// which is what makes it the single producer of a departure crossing, and the
// authority every session starts under.
const CoordinatorParticipant uint32 = 1

// IsSessionCoordinator reports whether this instance is the one authoring the
// session: the participant the current term names, which is the participant that
// opened it until a succession moves the term. It used to compare against identity
// 1 alone, so a successor was refused every rule reserved for the authority — its
// own resets among them — while still being the only instance able to apply one.
func (w *World) IsSessionCoordinator() bool {
	r := w.Resources.Network
	if r == nil {
		return false
	}
	if held := r.Authority.Load(); held != 0 {
		return r.ParticipantID == held
	}
	return r.ParticipantID == CoordinatorParticipant
}

// PredictsShared reports whether this world runs the shared domain ahead of an
// authority that may correct it. The authority's world is never a prediction, nor
// is a run with nobody to correct it: what either derives is settled at once.
func (w *World) PredictsShared() bool { return w.predicting.Load() }

// LatchSession opens a tick under the transport's answer to PredictsShared, so a
// derivation's phase does not hang on when a link noticed its peer, and journals
// a change; a replay takes it from the journal. Caller MUST hold updateMutex.
func (w *World) LatchSession() {
	if w.followJournal.Load() {
		return
	}
	net := w.Resources.Network
	p := net != nil && net.Port != nil && net.Port.IsRunning() && net.Port.PeerCount() > 0 &&
		net.Authority.Load() != net.ParticipantID
	if w.predicting.Swap(p) != p && w.Resources.Event.Queue != nil {
		w.Resources.Event.Queue.Note(event.GameEvent{
			Type: event.EventSessionPredicting, Payload: &event.SessionPredictingPayload{Predicting: p},
			Origin: event.OriginSession, Domain: core.DomainPlayer,
		})
	}
}

// PushLocal emits an event that must never replicate: an owner-authored grant, or an
// effect belonging to this instance alone. Replication classifies on the domain tag,
// so tagging here is what makes the classification mechanical rather than by inspection.
func (w *World) PushLocal(eventType event.EventType, payload any) {
	w.pushEvent(eventType, payload, event.Origin(w.origin.Load()), core.DomainPlayer)
}

// PushCrossing emits a D-3 crossing: the smallest artifact by which a player
// mechanic determines a shared outcome. Stamped player, which is what separates it
// from the same type pushed by a shared system re-deriving its own copy — every
// Bus type has producers of both kinds. The journal replicates both; the wire
// carries only this one (event.OnWire). A pending pointer placement crosses first.
func (w *World) PushCrossing(eventType event.EventType, payload any) {
	w.FlushPointerMove()
	w.pushEvent(eventType, payload, event.Origin(w.origin.Load()), core.DomainPlayer)
}

// pushEvent is the shared emit body; trace depth is measured from here
func (w *World) pushEvent(eventType event.EventType, payload any, origin event.Origin, domain core.Domain) {
	if w.Resources.Event.Queue == nil {
		return // Not yet initialized
	}

	if vlog.On("push", vlog.LevelTrace) {
		vlog.Trace("push", vlog.LevelTrace, 4, "msg", "push", "ev", event.GetEventName(eventType))
	}

	ev := event.GameEvent{
		Type:    eventType,
		Payload: payload,
		Origin:  origin,
		Domain:  domain,
	}
	// A shared derivation raised on a predicting instance is provisional: a
	// correction can restore what it announced dead and make this instance
	// announce it again. The ledger holds the reward; the stamp is what tells the
	// consumers that would pay it to wait.
	if domain == core.DomainShared && event.Derivation(eventType) && w.PredictsShared() {
		ev.Phase = event.PhasePredicted
		w.recordPrediction(eventType, payload)
	}
	w.Resources.Event.Queue.Push(ev)
}

// PushRecord republishes one journaled record without offering it to the wire,
// reporting whether it was queued. A record is stamped where it applied, so the
// barrier deferring it again would add a second playout lead; crossings no record
// carries are re-derived through Push and deferred as the recorded run did.
func (w *World) PushRecord(eventType event.EventType, payload any, origin event.Origin, domain core.Domain) bool {
	if w.Resources.Event.Queue == nil {
		return false
	}
	// A note is applied, never dispatched: a noted placement is a record of its
	// own, stamped where the barrier released it a lead later.
	switch p := payload.(type) {
	case *event.CursorMoveRequestPayload:
		if eventType == event.EventCursorPredicted {
			w.predictCursorMove(p.Entity, p.X, p.Y, p.Pointer)
			return false
		}
	case *event.SessionPredictingPayload:
		w.predicting.Store(p.Predicting)
		return false
	}
	w.Resources.Event.Queue.PushReady(event.GameEvent{
		Type: eventType, Payload: payload, Origin: origin, Domain: domain,
	})
	return true
}

// notePrediction journals the D-18 prediction a placement just made, at the
// keystroke; the view and every shot from the local cursor read it before the
// placement applies. Caller MUST hold updateMutex.
func (w *World) notePrediction(e core.Entity, x, y int, pointer bool) {
	w.Resources.Event.Queue.Note(event.GameEvent{
		Type:    event.EventCursorPredicted,
		Payload: &event.CursorMoveRequestPayload{Entity: e, X: x, Y: y, Pointer: pointer},
		Origin:  event.Origin(w.origin.Load()), Domain: core.DomainPlayer,
	})
}

// CreatedCount returns total entities created this session across both domains
// Lock-free: safe from any goroutine, including the post-tick telemetry tail
func (w *World) CreatedCount() int64 {
	var n int64
	for d := range w.createdCount {
		n += w.createdCount[d].Load()
	}
	return n
}

// DestroyedCount returns total entities destroyed across both domains
func (w *World) DestroyedCount() int64 {
	var n int64
	for d := range w.destroyedCount {
		n += w.destroyedCount[d].Load()
	}
	return n
}

// CreatedCountDomain returns entities created in one domain
func (w *World) CreatedCountDomain(d core.Domain) int64 { return w.createdCount[d].Load() }

// DestroyedCountDomain returns entities destroyed in one domain
func (w *World) DestroyedCountDomain(d core.Domain) int64 { return w.destroyedCount[d].Load() }

// === Base Entities ===

// UpdateBoundsRadius recomputes ping bounds for the local cursor from mode and shield
// state. Ping is pure local view (D-13) and this instance looks through one cursor, so
// a rostered remote keeps whatever ClearBoundsRadius left it.
// Caller MUST hold updateMutex
func (w *World) UpdateBoundsRadius() {
	e := w.Resources.Player.Entity
	ping, ok := w.Components.Ping.GetPtr(e)
	if !ok {
		return
	}

	visual := w.Resources.Game.State.GetMode() == core.ModeVisual
	shield, hasShield := w.Components.Shield.GetComponent(e)
	if !visual || !hasShield || !shield.Active {
		ping.BoundsActive = false
		return
	}
	ping.BoundsRadiusX = int(shield.RadiusX) / parameter.PingBoundFactor
	ping.BoundsRadiusY = int(shield.RadiusY) / parameter.PingBoundFactor
	ping.BoundsActive = true
}

// ClearBoundsRadius drops one cursor's ping bounds. Paired with a local rebind:
// the departing slot stops being recomputed, so it must not keep stale bounds.
// Caller MUST hold updateMutex
func (w *World) ClearBoundsRadius(e core.Entity) {
	if ping, ok := w.Components.Ping.GetPtr(e); ok {
		ping.BoundsActive = false
	}
}

// GetPingAbsoluteBounds returns the local cursor's absolute bounds; zero when no cursor exists
func (w *World) GetPingAbsoluteBounds() PingAbsoluteBounds {
	return w.PingAbsoluteBoundsOf(w.Resources.Player.Entity)
}

// PingAbsoluteBoundsOf derives absolute bounds for one cursor from its position and
// stored radius. Keyed to the cell the cursor is on, which for this instance's own
// is the D-18 prediction: every consumer is view or input, and a motion whose step
// is measured from the bounds would accelerate away from the player's own cursor if
// the two disagreed by a playout lead.
func (w *World) PingAbsoluteBoundsOf(e core.Entity) PingAbsoluteBounds {
	pos, ok := w.CursorCell(e)
	if !ok {
		return PingAbsoluteBounds{}
	}

	ping, ok := w.Components.Ping.GetComponent(e)
	if !ok || !ping.BoundsActive {
		return PingAbsoluteBounds{MinX: pos.X, MaxX: pos.X, MinY: pos.Y, MaxY: pos.Y}
	}

	config := w.Resources.Config
	return PingAbsoluteBounds{
		MinX:   max(0, pos.X-ping.BoundsRadiusX),
		MaxX:   min(config.MapWidth-1, pos.X+ping.BoundsRadiusX),
		MinY:   max(0, pos.Y-ping.BoundsRadiusY),
		MaxY:   min(config.MapHeight-1, pos.Y+ping.BoundsRadiusY),
		Active: true,
	}
}

// === Validation ===

// ResolveFreeCell returns the nearest cell to (x, y) that the mask does not block
// Reports false when no escape route exists within half the map
func (w *World) ResolveFreeCell(x, y int, mask component.WallBlockMask) (int, int, bool) {
	if !w.Positions.IsBlocked(x, y, mask) {
		return x, y, true
	}

	config := w.Resources.Config
	maxRadius := max(config.MapWidth, config.MapHeight) / 2

	newX, newY, found := w.Positions.FindFreeFromPattern(
		x, y, 1, 1, PatternCardinalFirst, 1, maxRadius, true, mask, nil,
	)
	if !found {
		return x, y, false
	}
	return newX, newY, true
}

// PushEntityFromBlocked relocates a non-cursor entity to the nearest valid position
// Cursor placement is CursorSystem-owned: resolve the cell, then emit a move request
func (w *World) PushEntityFromBlocked(entity core.Entity, mask component.WallBlockMask) (int, int, bool) {
	pos, ok := w.Positions.GetPosition(entity)
	if !ok {
		return 0, 0, false
	}

	newX, newY, found := w.ResolveFreeCell(pos.X, pos.Y, mask)
	if !found || (newX == pos.X && newY == pos.Y) {
		return pos.X, pos.Y, false
	}

	w.Positions.SetPosition(entity, component.PositionComponent{X: newX, Y: newY})
	// Physics integrates from the sub-cell position; left in the wall, the next
	// step would write the entity straight back into the cell it was pushed from.
	if k, ok := w.Components.Kinetic.GetPtr(entity); ok {
		k.PreciseX, k.PreciseY = vmath.Point{X: newX, Y: newY}.CenterF()
	}
	return newX, newY, true
}

// ResolveCursor validates that e names a live cursor. Commands must carry an
// explicit entity; zero is never rewritten to the local cursor.
// Caller MUST hold updateMutex
func (w *World) ResolveCursor(e core.Entity) core.Entity {
	if e == 0 || !w.Components.Cursor.HasEntity(e) {
		return 0
	}
	return e
}

// SimulatesLocally reports whether this instance owns a cursor's simulation (D-2).
// A remote cursor's owner-authored state arrives as transported values, so a local
// system that also wrote it would be a second authority for one cell.
// Caller MUST hold updateMutex
func (w *World) SimulatesLocally(e core.Entity) bool {
	c, ok := w.Components.Cursor.GetComponent(e)
	return ok && c.Control != component.ControlRemote
}

// ResolveOwnedCursor is ResolveCursor narrowed to the cursors this instance
// simulates. It is the single admission check for the D-13 owner-authored set:
// a grant naming a remote cursor resolves to zero and its writer does nothing.
// Caller MUST hold updateMutex
func (w *World) ResolveOwnedCursor(e core.Entity) core.Entity {
	if e == 0 || !w.SimulatesLocally(e) {
		return 0
	}
	return e
}

// LocalCursor returns the cell this instance's own cursor occupies.
//
// One accessor rather than the same three-line read at every input, view and
// player-domain producer. It is also the seam the locally predicted position
// installs itself behind (D-18): the authoritative cell is a D-3 crossing and
// reaches the store a playout lead later, so an input path that resolved the next
// motion from the store resolved it from a cell the player had already left. Every
// producer that must see the player's own latest cell reads it here; a shared
// system must not (D-1, D-18), which TestSystemDomainProfiles enforces.
// Caller MUST hold updateMutex
func (w *World) LocalCursor() (component.PositionComponent, bool) {
	return w.CursorCell(w.Resources.Player.Entity)
}

// CursorCell returns the cell a producer must resolve one cursor's next placement
// from: the D-18 prediction for the cursor this instance drives, the store for any
// other. A remote cursor is never predicted — its cells arrive as transported
// values and this instance authors none of them (D-2).
// Caller MUST hold updateMutex
func (w *World) CursorCell(e core.Entity) (component.PositionComponent, bool) {
	if w.Resources.Player.IsLocal(e) {
		if pos, ok := w.Resources.Player.PredictedCell(); ok {
			return pos, true
		}
	}
	return w.Positions.GetPosition(e)
}

// PushCursorMove requests a placement for a cursor this instance drives and
// advances the D-18 prediction in the same statement, so no producer of a local
// cursor move can emit the crossing without the prediction that answers it locally.
// The placement itself is unchanged: the artifact is the same D-3 crossing, deferred
// by the same playout lead, and the store still moves only when CursorSystem
// applies it.
// Caller MUST hold updateMutex
func (w *World) PushCursorMove(e core.Entity, x, y int) {
	if w.predictCursorMove(e, x, y, false) {
		w.notePrediction(e, x, y, false)
	}
	w.PushCrossing(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{Entity: e, X: x, Y: y})
}

// PushPointerMove is PushCursorMove for a cell the pointer named, which the camera
// follows with its pointer margins (see CameraPointerMarginX). The view moves at
// once; the placement waits for the next tick, retargeted by every later report,
// so a sweep crosses its newest cell once a tick rather than every cell it passed.
// Caller MUST hold updateMutex
func (w *World) PushPointerMove(e core.Entity, x, y int) {
	if !w.predictCursorMove(e, x, y, true) {
		w.PushCrossing(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{Entity: e, X: x, Y: y, Pointer: true})
		return
	}
	w.notePrediction(e, x, y, true)
	w.Resources.Player.prediction.pointer = pendingPointer{x: x, y: y, origin: event.Origin(w.origin.Load()), set: true}
}

// FlushPointerMove crosses the pending pointer placement, reporting whether there
// was one. The scheduler calls it before each tick; any other crossing this
// instance produces calls it first, so placements keep the order they were made in.
// Caller MUST hold updateMutex
func (w *World) FlushPointerMove() bool {
	roster := w.Resources.Player
	p := roster.prediction.pointer
	if !p.set {
		return false
	}
	roster.prediction.pointer = pendingPointer{}
	w.pushEvent(event.EventCursorMoveRequest,
		&event.CursorMoveRequestPayload{Entity: roster.Entity, X: p.x, Y: p.y, Pointer: true}, p.origin, core.DomainPlayer)
	return true
}

// predictCursorMove records the cell CursorSystem.move will announce for this
// request, reporting whether it did. It clamps exactly as that handler does: a
// prediction of the requested cell rather than the applied one would never match
// its own announcement, and every request would snap.
func (w *World) predictCursorMove(e core.Entity, x, y int, pointer bool) bool {
	if !w.Resources.Player.IsLocal(e) || !w.SimulatesLocally(e) {
		return false
	}
	w.Resources.Config.pointer = pointer
	if _, ok := w.Positions.GetPosition(e); !ok {
		return false // move announces nothing for a cursor with no cell, so nothing reconciles
	}
	config := w.Resources.Config
	cell := component.PositionComponent{
		X: max(0, min(x, config.MapWidth-1)),
		Y: max(0, min(y, config.MapHeight-1)),
	}
	if roster := w.Resources.Player; pointer && roster.prediction.pointer.set {
		roster.Retarget(cell)
	} else {
		roster.Predict(cell)
	}
	w.FollowLocalCursor()
	return true
}

// FollowLocalCursor re-anchors the view on the cell this instance's cursor occupies,
// which is the D-18 prediction while one is outstanding. The camera is local view
// state (D-14), so it follows the cell the renderer draws rather than the announced
// one a playout lead behind it.
// Caller MUST hold updateMutex
func (w *World) FollowLocalCursor() {
	if pos, ok := w.LocalCursor(); ok {
		w.Resources.Config.FollowCamera(pos.X, pos.Y)
	}
}

// ReconcileLocalCursor settles an announced placement against the D-18 prediction
// queue; own marks one applied from this instance's own crossing. CursorSystem calls
// it for every cursor it moves; only the local one holds predictions, and the caller
// learns nothing about them — they are read by player-domain producers and the view.
// Caller MUST hold updateMutex
func (w *World) ReconcileLocalCursor(e core.Entity, x, y int, own bool) {
	if !w.Resources.Player.IsLocal(e) {
		return
	}
	w.Resources.Player.Reconcile(component.PositionComponent{X: x, Y: y}, own)
}

// CursorSlot returns the roster slot a cursor entity occupies
func (w *World) CursorSlot(e core.Entity) (uint8, bool) {
	c, ok := w.Components.Cursor.GetComponent(e)
	if !ok || int(c.Slot) >= parameter.MaxPlayers {
		return 0, false
	}
	return c.Slot, true
}

// === Level ===

// SetupLevel reconfigures map dimensions and optionally clears entities
// Respects Protection component - entities with ProtectAll survive
// Repositions cursor if outside new bounds
func (w *World) SetupLevel(width, height int, clearEntities bool, cropOnResize bool) {
	config := w.Resources.Config

	// Clamped before it is recorded, not just before it is allocated. A LevelSetup
	// payload is replicated, so these dimensions can arrive from any participant;
	// clamping here is what keeps Config and the grid describing one map, where
	// clamping only inside the grid would leave map logic reading bounds no cell
	// exists for.
	width, height = ClampMapSize(width, height)

	// Update map dimensions
	config.MapWidth = width
	config.MapHeight = height

	// Apply explicit crop behavior
	config.CropOnResize = cropOnResize

	// Grid tracks map dimensions; grow-only backing makes this cheap
	w.Positions.ResizeGrid(width, height)

	// Reset camera to origin
	config.CameraX = 0
	config.CameraY = 0

	if clearEntities {
		w.clearNonProtectedEntities()
	}

	// Clamp every cursor into the new bounds; CursorSystem applies and announces
	w.Components.Cursor.Each(func(e core.Entity, _ *component.CursorComponent) bool {
		pos, ok := w.Positions.GetPosition(e)
		if !ok {
			return true
		}
		x, y, _ := w.ResolveFreeCell(
			max(0, min(pos.X, width-1)), max(0, min(pos.Y, height-1)), component.WallBlockCursor)
		w.PushEvent(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{Entity: e, X: x, Y: y})
		return true
	})
}

// clearNonProtectedEntities destroys all entities except those with ProtectAll
func (w *World) clearNonProtectedEntities() {
	// Collect entities to destroy (avoid mutation during iteration)
	var toDestroy []core.Entity

	allEntities := w.Positions.AllEntities()
	for _, e := range allEntities {
		// Check protection
		if prot, ok := w.Components.Protection.GetComponent(e); ok {
			if prot.Mask == component.ProtectAll {
				continue
			}
		}
		toDestroy = append(toDestroy, e)
	}

	w.removeEntitiesBatch(toDestroy)

	for _, e := range toDestroy {
		w.destroyedCount[e.Domain()].Add(1)
	}
}

// === Debug ===

// DebugPrint prints a message in status bar via meta system
func (w *World) DebugPrint(msg string) {
	w.PushLocal(event.EventMetaStatusMessageRequest, &event.MetaStatusMessagePayload{
		Message:          msg,
		Duration:         0,
		DurationOverride: true,
	})
}
