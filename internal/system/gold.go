package system

import (
	"encoding/json"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// GoldSystem manages the gold sequence mechanic autonomously
type GoldSystem struct {
	world *engine.World

	rng *vmath.FastRand

	// Internal state; the sequence's deadline rides its header's timer
	headerEntity core.Entity               // Phantom Head
	contrib      [parameter.MaxPlayers]int // Members typed per roster slot, for completion credit
	active       bool
	spawnEnabled bool

	// An installed sequence this instance never spawned has no timer splash:
	// spawnGold raises it and an install replaces state without replaying it (D-6)
	splashDue bool

	// Footprint clearing collector
	sweep cellSweep

	// Cached metric pointers
	statActive        *atomic.Bool
	stateHeaderEntity *atomic.Int64
	statTimer         *atomic.Int64
	statSpawnFailures *atomic.Int64
	rejects           rejectionTelemetry

	toggle
}

// NewGoldSystem creates a new gold sequence system
func NewGoldSystem(world *engine.World) engine.System {
	s := &GoldSystem{
		world: world,
	}

	s.statActive = s.world.Resources.Status.Bools.Get("gold.active")
	s.stateHeaderEntity = s.world.Resources.Status.Ints.Get("gold.header_entity")
	s.statTimer = s.world.Resources.Status.Ints.Get("gold.timer")
	s.statSpawnFailures = s.world.Resources.Status.Ints.Get("gold.spawn_failures")
	s.rejects = newRejectionTelemetry(s.world.Resources.Status, "gold")

	s.Init()
	return s
}

// Init resets session state for new game
func (s *GoldSystem) Init() {
	s.active = false
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	s.headerEntity = 0
	s.contrib = [parameter.MaxPlayers]int{}
	s.spawnEnabled = true
	s.splashDue = false
	s.statActive.Store(false)
	s.stateHeaderEntity.Store(0)
	s.statTimer.Store(0)
	s.statSpawnFailures.Store(0)
	s.rejects.Reset()
	s.enabled = true
}

// Name returns system's name
func (s *GoldSystem) Name() string {
	return "gold"
}

// Priority returns the system's priority
func (s *GoldSystem) Priority() int {
	return parameter.PriorityGold
}

// EventTypes returns the event types GoldSystem handles
func (s *GoldSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGoldSpawnRequest,
		event.EventGoldCancel,
		event.EventGoldJumpRequest,
		event.EventCompositeMemberDestroyed,
		event.EventCompositeIntegrityBreach,
		event.EventMetaSystemCommandRequest,
	}
}

// HandleEvent processes gold events
func (s *GoldSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		if ev.Type != event.EventMetaSystemCommandRequest {
			s.rejects.disabled.Add(1)
		}
		return
	}

	switch ev.Type {
	case event.EventGoldCancel:
		s.cancelGold()

	case event.EventGoldJumpRequest:
		if payload, ok := ev.Payload.(*event.GoldJumpRequestPayload); ok {
			if cursor := s.world.ResolveCursor(payload.Entity); cursor != 0 {
				s.handleJumpRequest(cursor)
			} else {
				s.rejects.cursor.Add(1)
			}
		}

	case event.EventGoldSpawnRequest:
		switch {
		case s.active:
			// A region taking over a running sequence adopts it instead of a new one
			s.announceRunning()
		case !s.spawnEnabled || !s.spawnGold():
			s.statSpawnFailures.Add(1)
			s.world.PushEvent(event.EventGoldSpawnFailed, nil)
		}

	case event.EventCompositeMemberDestroyed:
		if payload, ok := ev.Payload.(*event.CompositeMemberDestroyedPayload); ok {
			if payload.HeaderEntity != s.headerEntity {
				return
			}
			s.recordContribution(payload.Entity)
			if payload.RemainingCount == 0 {
				s.handleGoldComplete()
			}
		}

	case event.EventCompositeIntegrityBreach:
		if payload, ok := ev.Payload.(*event.CompositeIntegrityBreachPayload); ok {
			if payload.HeaderEntity == s.headerEntity {
				// Gold: any external member loss = full destruction
				s.handleGoldDestroyed()
			}
		}
	}
}

// Update runs the gold sequence system logic
func (s *GoldSystem) Update() {
	if !s.enabled {
		return
	}

	s.statActive.Store(s.active)
	if !s.active {
		s.statTimer.Store(0)
		s.splashDue = false
		return
	}

	timer, timed := s.world.Components.Timer.GetPtr(s.headerEntity)
	var remaining time.Duration
	if timed {
		remaining = timer.Slots[component.TimerDeadline].Remaining
	}
	s.statTimer.Store(int64(remaining))
	s.stateHeaderEntity.Store(int64(s.headerEntity))
	if s.splashDue {
		s.splashDue = false
		s.requestTimerSplash(remaining)
	}

	// Timeout check only - integrity handled via event. A header the composite
	// destroyed this tick ends through its breach; a live one without its
	// deadline has no time left
	if timed && timer.Expired(component.TimerDeadline) ||
		!timed && s.world.Components.Header.HasEntity(s.headerEntity) {
		s.handleGoldTimeout()
	}
}

// handleJumpRequest jumps one cursor to the first living member of the gold sequence.
func (s *GoldSystem) handleJumpRequest(cursorEntity core.Entity) {
	if !s.active || s.headerEntity == 0 {
		return
	}

	// 1. Find target position (First living member)
	header, ok := s.world.Components.Header.GetPtr(s.headerEntity)
	if !ok {
		return
	}

	var targetEntity core.Entity
	for _, m := range header.MemberEntries {
		if m.Entity != 0 {
			targetEntity = m.Entity
			break
		}
	}

	if targetEntity == 0 {
		// No living members, should rely on update loop to clean up, but exit here
		return
	}

	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return
	}

	// 2. Move Cursor
	s.world.PushEvent(event.EventCursorMoveRequest, &event.CursorMoveRequestPayload{
		Entity: cursorEntity,
		X:      targetPos.X,
		Y:      targetPos.Y,
	})

	// 3. Pay Energy Cost (spend, non-convergent)
	s.world.PushLocal(event.EventEnergyAddRequest, &event.EnergyAddPayload{
		Entity:     cursorEntity,
		Delta:      parameter.GoldJumpCostPercent,
		Percentage: true,
		Type:       component.EnergyDeltaSpend,
	})

	// // 4. Play Sound
	s.world.PushLocal(event.EventSoundRequest, &event.SoundRequestPayload{
		ID: parameter.Sfx.Coin,
	})
}

// spawnGold creates a new gold sequence
func (s *GoldSystem) spawnGold() bool {
	// Generate random 10-character sequence
	sequence := make([]rune, parameter.GoldSequenceLength)
	for i := range parameter.GoldSequenceLength {
		sequence[i] = parameter.AlphanumericRunes[s.rng.Intn(len(parameter.AlphanumericRunes))]
	}

	// Find empty space to spawn gold
	x, y := s.findValidPosition(parameter.GoldSequenceLength)
	if x < 0 || y < 0 {
		return false
	}

	// 1. Create Phantom Head entity (NO position yet)
	headerEntity := s.world.CreateEntity(core.DomainShared)

	// 2. Create member entities
	type entityData struct {
		entity core.Entity
		pos    component.PositionComponent
		offset int
	}
	entities := make([]entityData, 0, parameter.GoldSequenceLength)
	// Create member entities
	members := make([]component.MemberEntry, 0, parameter.GoldSequenceLength)

	// Set position component to gold entities
	for i := range parameter.GoldSequenceLength {
		entity := s.world.CreateEntity(core.DomainShared)
		entities = append(entities, entityData{
			entity: entity,
			pos:    component.PositionComponent{X: x + i, Y: y},
			offset: i,
		})
	}

	// 3. Batch position commit (anchor NOT in grid - no collision at x,y)
	batch := s.world.Positions.BeginBatch(len(entities))
	for _, ed := range entities {
		batch.Add(ed.entity, ed.pos)
	}

	if err := batch.CommitShared(); err != nil {
		for _, ed := range entities {
			s.world.DestroyEntity(ed.entity)
		}
		s.world.DestroyEntity(headerEntity)
		return false
	}

	// 4. Clear the footprint of this instance's own occupants, then set the
	// Phantom Head position AFTER batch success
	s.clearGoldSpawnArea(x, y, parameter.GoldSequenceLength)
	s.world.Positions.SetPosition(headerEntity, component.PositionComponent{X: x, Y: y})
	s.world.Components.Protection.SetComponent(headerEntity, component.ProtectionComponent{
		Mask: component.ProtectAll ^ component.ProtectFromDeath,
	})

	// 5. Set components to members
	for i, ed := range entities {
		// Typing target
		s.world.Components.Glyph.SetComponent(ed.entity, component.GlyphComponent{
			Rune:  sequence[i],
			Type:  component.GlyphGold,
			Level: component.GlyphBright,
		})

		// Composite membership
		s.world.Components.Member.SetComponent(ed.entity, component.MemberComponent{
			HeaderEntity: headerEntity,
		})

		// Protect gold entities from particles and explicit deletion.
		s.world.Components.Protection.SetComponent(ed.entity, component.ProtectionComponent{
			Mask: component.ProtectFromDelete | component.ProtectFromParticle,
		})

		// Set gold entity to composite member entities
		members = append(members, component.MemberEntry{
			Entity:  ed.entity,
			OffsetX: ed.offset,
			OffsetY: 0,
		})
	}

	// 6. Create composite header
	s.world.Components.Header.SetComponent(headerEntity, component.HeaderComponent{
		Behavior:      component.BehaviorGold,
		Type:          component.CompositeTypeContainer,
		MemberEntries: members,
	})

	// 7. Activate internal state
	s.active = true
	s.headerEntity = headerEntity
	armTimer(s.world, headerEntity, component.TimerDeadline, parameter.GoldDuration)

	// Emit spawn event
	s.world.PushEvent(event.EventGoldSpawned, &event.GoldSpawnedPayload{
		HeaderEntity: headerEntity,
		Length:       parameter.GoldSequenceLength,
		Duration:     parameter.GoldDuration,
	})
	s.splashDue = false
	s.requestTimerSplash(parameter.GoldDuration)

	return true
}

// announceRunning answers a spawn request with the running sequence and its time left
func (s *GoldSystem) announceRunning() {
	var remaining time.Duration
	if timer, ok := s.world.Components.Timer.GetPtr(s.headerEntity); ok {
		remaining = timer.Slots[component.TimerDeadline].Remaining
	}
	s.world.PushEvent(event.EventGoldSpawned, &event.GoldSpawnedPayload{
		HeaderEntity: s.headerEntity,
		Length:       parameter.GoldSequenceLength,
		Duration:     remaining,
	})
}

// clearGoldSpawnArea empties the sequence footprint of player-domain occupants.
// The cells are shared geometry claimed by a shared spawn (D-12), and each
// participant's own glyphs, drains and nuggets sit in them differently; leaving
// them there is a per-instance difference inside a replicated footprint. Shared
// occupancy stays CommitShared's to refuse.
func (s *GoldSystem) clearGoldSpawnArea(x, y, length int) {
	s.sweep.reset()
	for i := range length {
		s.sweep.collect(s.world, x+i, y, func(e core.Entity) bool {
			return e.Domain() == core.DomainPlayer && speciesClearable(s.world, e, nil, nil)
		})
	}
	s.sweep.destroy(s.world)
}

// requestTimerSplash raises the countdown anchored to the sequence header. It dies
// with its anchor, or with the install that replaces the header (LoadShared).
func (s *GoldSystem) requestTimerSplash(remaining time.Duration) {
	s.world.PushLocal(event.EventSplashTimerRequest, &event.SplashTimerRequestPayload{
		AnchorEntity: s.headerEntity,
		Color:        visual.RgbSplashWhite,
		MarginRight:  parameter.GoldSequenceLength,
		MarginBottom: 1, // One line height
		Duration:     remaining,
	})
}

// handleMemberTyped processes a gold character being typed
func (s *GoldSystem) handleMemberTyped(payload *event.CompositeMemberDestroyedPayload) {
	if !s.active || payload.HeaderEntity != s.headerEntity {
		return
	}

	// Check if sequence complete
	if payload.RemainingCount == 0 {
		s.handleGoldComplete()
	}
}

// recordContribution tallies one typed member against its cursor's roster slot
func (s *GoldSystem) recordContribution(cursor core.Entity) {
	if slot, ok := s.world.CursorSlot(cursor); ok && int(slot) < parameter.MaxPlayers {
		s.contrib[slot]++
	}
}

// creditedCursor returns the cursor that typed the most members; ties break to the
// lowest slot, so every instance credits the same one from the same event stream.
func (s *GoldSystem) creditedCursor() core.Entity {
	best, bestSlot := 0, -1
	for i := range parameter.MaxPlayers {
		if s.contrib[i] > best {
			best, bestSlot = s.contrib[i], i
		}
	}
	if bestSlot < 0 {
		return 0
	}
	return s.world.Resources.Player.Slot(uint8(bestSlot))
}

// handleGoldComplete processes successful gold sequence completion
func (s *GoldSystem) handleGoldComplete() {
	if !s.active {
		return
	}

	headerEntity := s.headerEntity

	// Emit completion event, FSM is the reward authority
	s.world.PushEvent(event.EventGoldCompleted, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
		Entity:       s.creditedCursor(),
	})

	// Silent destruction - members already dead from typing
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})

	s.clearState()
}

// handleGoldTimeout processes gold sequence expiration
func (s *GoldSystem) handleGoldTimeout() {
	if !s.active {
		return
	}

	headerEntity := s.headerEntity

	s.world.PushEvent(event.EventGoldTimeout, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
	})

	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       0,
	})

	s.clearState()
}

// handleGoldDestroyed processes external gold destruction
func (s *GoldSystem) handleGoldDestroyed() {
	if !s.active {
		return
	}

	headerEntity := s.headerEntity

	// Emit event for FSM
	s.world.PushEvent(event.EventGoldDestroyed, &event.GoldCompletionPayload{
		HeaderEntity: headerEntity,
	})

	// Request centralized destruction with flash effect
	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: headerEntity,
		Effect:       event.EventFlashSpawnOneRequest,
	})

	s.clearState()
}

// cancelGold handles explicit cancellation
func (s *GoldSystem) cancelGold() {
	if !s.active || s.headerEntity == 0 {
		return
	}

	s.world.PushEvent(event.EventCompositeDestroyRequest, &event.CompositeDestroyRequestPayload{
		HeaderEntity: s.headerEntity,
		Effect:       0,
	})

	s.clearState()
}

// clearState resets gold tracking
func (s *GoldSystem) clearState() {
	s.active = false
	s.headerEntity = 0
	s.contrib = [parameter.MaxPlayers]int{}
	s.statActive.Store(false)
	s.statTimer.Store(0)
	s.stateHeaderEntity.Store(0)
}

// findValidPosition finds a valid random position for the gold sequence
// Caller must NOT hold s.mu lock
func (s *GoldSystem) findValidPosition(seqLength int) (int, int) {
	config := s.world.Resources.Config
	if s.world.Resources.Player.Count() == 0 {
		return -1, -1
	}

	var cells [parameter.GoldSpawnMaxAttempts]vmath.Point
	drawSpawnCells(s.rng, cells[:], 0, config.MapWidth, 0, config.MapHeight)

	for i := range cells {
		x, y := cells[i].X, cells[i].Y

		// Check if far enough from every cursor.
		nearCursor := false
		s.world.Components.Cursor.Each(func(e core.Entity, _ *component.CursorComponent) bool {
			cursorPos, ok := s.world.Positions.GetPosition(e)
			if ok && (math.Abs(float64(x-cursorPos.X)) <= parameter.CursorExclusionX ||
				math.Abs(float64(y-cursorPos.Y)) <= parameter.CursorExclusionY) {
				nearCursor = true
				return false
			}
			return true
		})
		if nearCursor {
			continue
		}

		// Check if sequence fits within game width
		if x+seqLength > config.MapWidth {
			continue
		}

		// Check for overlaps with existing characters
		overlaps := false
		for c := range seqLength {
			if s.world.Positions.IsBlocked(x+c, y, component.WallBlockParticle) {
				overlaps = true
				break
			}
		}

		if !overlaps {
			return x, y
		}
	}

	return -1, -1
}

// goldSnapshot is this system's D-19 record. The gold sequence is shared state
// that lives mostly outside the component stores: whether a sequence is
// running, which header it belongs to, and which participant has typed how much
// of it. Its deadline is the header's timer, which the stores carry.
type goldSnapshot struct {
	Active       bool                      `json:"active"`
	SpawnEnabled bool                      `json:"spawn_enabled"`
	HeaderEntity core.Entity               `json:"header_entity"`
	Contrib      [parameter.MaxPlayers]int `json:"contrib"`
}

// SaveShared carries the gold sequence's private state (D-19).
func (s *GoldSystem) SaveShared() ([]byte, error) {
	snap := goldSnapshot{
		Active:       s.active,
		SpawnEnabled: s.spawnEnabled,
		HeaderEntity: s.headerEntity,
		Contrib:      s.contrib,
	}
	return json.Marshal(snap)
}

// LoadShared installs a captured sequence and republishes the cells derived from
// it; the store pass has already installed the header and its timer.
func (s *GoldSystem) LoadShared(data []byte) error {
	var snap goldSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("gold: %w", err)
	}
	// A header this instance did not spawn arrived with the capture rather than
	// through spawnGold, so its timer splash was never raised here. The one it
	// replaces loses its timer: an install can hand that id to another composite,
	// which would otherwise keep the countdown alive.
	s.splashDue = snap.Active && snap.HeaderEntity != s.headerEntity
	if s.headerEntity != 0 && snap.HeaderEntity != s.headerEntity {
		s.world.PushLocal(event.EventSplashTimerCancel, &event.SplashTimerCancelPayload{AnchorEntity: s.headerEntity})
	}
	s.active = snap.Active
	s.spawnEnabled = snap.SpawnEnabled
	s.headerEntity = snap.HeaderEntity
	s.contrib = snap.Contrib

	s.statActive.Store(s.active)
	s.stateHeaderEntity.Store(int64(s.headerEntity))
	s.statTimer.Store(0)
	if timer, ok := s.world.Components.Timer.GetPtr(s.headerEntity); ok && s.active {
		s.statTimer.Store(int64(timer.Slots[component.TimerDeadline].Remaining))
	}
	return nil
}

type goldState struct {
	header                core.Entity
	contrib               [parameter.MaxPlayers]int
	active, spawn, splash bool
}

// CopyState carries the sequence as it stands, including the timer splash still
// owed, which LoadShared re-derives as a joiner would.
func (s *GoldSystem) CopyState() any {
	return goldState{s.headerEntity, s.contrib, s.active, s.spawnEnabled, s.splashDue}
}

func (s *GoldSystem) RestoreState(v any) error {
	c := v.(goldState)
	s.headerEntity, s.contrib = c.header, c.contrib
	s.active, s.spawnEnabled, s.splashDue = c.active, c.spawn, c.splash
	return nil
}
