package systems

import (
	"fmt"
	"math"
	"math/rand"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/audio"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// GoldSystem manages the gold sequence mechanic and emits events for visualization
type GoldSystem struct {
	mu  sync.RWMutex
	ctx *engine.GameContext
}

// NewGoldSystem creates a new gold sequence system
func NewGoldSystem(ctx *engine.GameContext) *GoldSystem {
	return &GoldSystem{
		ctx: ctx,
	}
}

// Priority returns the system's priority
func (s *GoldSystem) Priority() int {
	return constants.PriorityGold
}

// EventTypes returns the event types GoldSystem handles
func (s *GoldSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventGoldComplete,
	}
}

// HandleEvent processes gold events
func (s *GoldSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	if event.Type == events.EventGoldComplete {
		if payload, ok := event.Payload.(*events.GoldCompletionPayload); ok {
			s.handleCompletion(world, payload.SequenceID, event.Timestamp)
		}
	}
}

// Update runs the gold sequence system logic
// Gold Timer visualization is now handled by SplashSystem via events
func (s *GoldSystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Read state snapshots
	goldSnapshot := s.ctx.State.ReadGoldState(now)
	phaseSnapshot := s.ctx.State.ReadPhaseState(now)

	// Gold spawning: Only in PhaseNormal when no gold is active
	if phaseSnapshot.Phase == engine.PhaseNormal && !goldSnapshot.Active {
		s.spawnGold(world)
	}

	// Consistency Check: If gold is active, check if any sequence members are flagged OutOfBounds
	// Happens on resize and must fail the sequence BEFORE CullSystem destroys the entities
	if goldSnapshot.Active {
		// Check for OOB gold entities
		// Use Sequences store instead of GoldSequences (which are not attached to entities)
		oobEntities := world.Query().
			With(world.Sequences).
			With(world.OutOfBounds).
			Execute()

		for _, e := range oobEntities {
			// Verify this entity belongs to the active sequence
			if seq, ok := world.Sequences.Get(e); ok && seq.Type == components.SequenceGold && seq.ID == goldSnapshot.SequenceID {
				// Found an active gold char marked for deletion (resize/cull)
				// Trigger failure logic
				s.failSequence(world, seq.ID, now)
				break // One failure is enough to kill the sequence
			}
		}
	}
}

// failSequence handles the destruction of a gold sequence due to external causes (OOB/Drain)
func (s *GoldSystem) failSequence(world *engine.World, sequenceID int, now time.Time) {
	// Emit event to notify UI/SplashSystem
	s.ctx.PushEvent(events.EventGoldDestroyed, &events.GoldCompletionPayload{
		SequenceID: sequenceID,
	}, now)

	// Call removeGold to destroy ALL entities in the sequence, not just the one that triggered the failure, handling partial resize culling
	s.removeGold(world, sequenceID)
}

// spawnGold creates a new gold sequence at a random position on the screen using generic stores
// Returns true if spawn succeeded, false if spawn failed (e.g., no valid position)
func (s *GoldSystem) spawnGold(world *engine.World) bool {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Read phase and gold state snapshots for consistent checks
	phaseSnapshot := s.ctx.State.ReadPhaseState(now)
	goldSnapshot := s.ctx.State.ReadGoldState(now)

	// Phase consistency check: Gold can only spawn in PhaseNormal
	if phaseSnapshot.Phase != engine.PhaseNormal {
		return false
	}

	// Check active state from snapshot
	if goldSnapshot.Active {
		// Already have an active gold sequence
		return false
	}

	s.mu.Lock()
	// Generate random 10-character sequence
	sequence := make([]rune, constants.GoldSequenceLength)
	for i := 0; i < constants.GoldSequenceLength; i++ {
		sequence[i] = constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]
	}

	// Find random valid position (similar to spawn system)
	x, y := s.findValidPosition(world, constants.GoldSequenceLength)
	s.mu.Unlock()

	if x < 0 || y < 0 {
		// No valid position found - spawn failed
		return false
	}

	// Get next sequence ID from GameState
	sequenceID := s.ctx.State.IncrementGoldSequenceID()

	// Create entities and components
	type entityData struct {
		entity engine.Entity
		pos    components.PositionComponent
		char   components.CharacterComponent
		seq    components.SequenceComponent
	}

	entities := make([]entityData, 0, constants.GoldSequenceLength)

	for i := 0; i < constants.GoldSequenceLength; i++ {
		entity := world.CreateEntity()
		entities = append(entities, entityData{
			entity: entity,
			pos: components.PositionComponent{
				X: x + i,
				Y: y,
			},
			char: components.CharacterComponent{
				Rune: sequence[i],
				// Color defaults to ColorNone, renderer uses SeqType/SeqLevel
				Style:    components.StyleNormal,
				SeqType:  components.SequenceGold,
				SeqLevel: components.LevelBright,
			},
			seq: components.SequenceComponent{
				ID:    sequenceID,
				Index: i,
				Type:  components.SequenceGold,
				Level: components.LevelBright,
			},
		})
	}

	// Batch position validation and commit
	batch := world.Positions.BeginBatch()
	for _, ed := range entities {
		batch.Add(ed.entity, ed.pos)
	}

	if err := batch.Commit(); err != nil {
		// Collision detected - cleanup entities
		for _, ed := range entities {
			world.DestroyEntity(ed.entity)
		}
		return false
	}

	// Add other components (positions already committed)
	for _, ed := range entities {
		world.Characters.Add(ed.entity, ed.char)
		world.Sequences.Add(ed.entity, ed.seq)
		// Protect gold entities from delete operators
		world.Protections.Add(ed.entity, components.ProtectionComponent{
			Mask: components.ProtectFromDelete,
		})
	}

	// Activate gold sequence in GameState (sets phase to PhaseGoldActive)
	if !s.ctx.State.ActivateGoldSequence(sequenceID, constants.GoldDuration, now) {
		// Phase transition failed - clean up created entities
		for _, ed := range entities {
			world.DestroyEntity(ed.entity)
		}
		return false
	}

	// Emit Spawn Event for Visualization (Timer)
	s.ctx.PushEvent(events.EventGoldSpawned, &events.GoldSpawnedPayload{
		SequenceID: sequenceID,
		OriginX:    x,
		OriginY:    y,
		Length:     constants.GoldSequenceLength,
		Duration:   constants.GoldDuration,
	}, now)

	return true
}

// removeGold removes all gold sequence entities from the world using generic stores
func (s *GoldSystem) removeGold(world *engine.World, sequenceID int) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Read gold state snapshot for consistent check
	goldSnapshot := s.ctx.State.ReadGoldState(now)

	// Check active state from snapshot
	if !goldSnapshot.Active {
		return
	}

	// Only remove if the sequenceID matches
	if sequenceID != goldSnapshot.SequenceID {
		return
	}

	// Query entities with sequence components
	entities := world.Sequences.All()

	for _, entity := range entities {
		seq, ok := world.Sequences.Get(entity)
		if !ok {
			continue
		}

		// Only remove gold sequence entities with our ID
		if seq.Type == components.SequenceGold && seq.ID == sequenceID {
			world.DestroyEntity(entity)
		}
	}

	// Deactivate gold sequence in GameState (transitions to PhaseGoldComplete)
	if !s.ctx.State.DeactivateGoldSequence(now) {
		// Phase transition failed
		return
	}
}

// handleCompletion cleans up gold sequence and plays sound
func (s *GoldSystem) handleCompletion(world *engine.World, sequenceID int, now time.Time) {
	// Read gold state snapshot for consistent check
	goldSnapshot := s.ctx.State.ReadGoldState(now)

	if !goldSnapshot.Active {
		return
	}

	// Remove gold sequence entities
	// This will also trigger decay timer restart logic in removeGold
	s.removeGold(world, sequenceID)

	// Play coin sound for gold completion
	if s.ctx.AudioEngine != nil {
		cmd := audio.AudioCommand{
			Type:       audio.SoundCoin,
			Priority:   1,
			Generation: uint64(s.ctx.State.GetFrameNumber()),
			Timestamp:  now,
		}
		s.ctx.AudioEngine.SendRealTime(cmd)
	}
}

// TimeoutGoldSequence is called by ClockScheduler when gold sequence times out
// Required by GoldSystemInterface
func (s *GoldSystem) TimeoutGoldSequence(world *engine.World) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	// Read gold state snapshot to get current sequence ID
	goldSnapshot := s.ctx.State.ReadGoldState(now)

	// Emit Timeout Event first (so SplashSystem can remove timer)
	s.ctx.PushEvent(events.EventGoldTimeout, &events.GoldCompletionPayload{
		SequenceID: goldSnapshot.SequenceID,
	}, now)

	// Remove gold sequence entities (also starts decay timer)
	s.removeGold(world, goldSnapshot.SequenceID)
}

// findValidPosition finds a valid random position for the gold sequence using generic stores
// Caller holds s.mu lock
func (s *GoldSystem) findValidPosition(world *engine.World, seqLength int) (int, int) {
	// Fetch resources
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)

	// Read cursor position
	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		panic(fmt.Errorf("cursor destroyed"))
	}

	for attempt := 0; attempt < constants.GoldSpawnMaxAttempts; attempt++ {
		x := rand.Intn(config.GameWidth)
		y := rand.Intn(config.GameHeight)

		// Check if far enough from cursor
		if math.Abs(float64(x-cursorPos.X)) <= constants.CursorExclusionX || math.Abs(float64(y-cursorPos.Y)) <= constants.CursorExclusionY {
			continue
		}

		// Check if sequence fits within game width
		if x+seqLength > config.GameWidth {
			continue
		}

		// Check for overlaps with existing characters
		overlaps := false
		for i := 0; i < seqLength; i++ {
			if world.Positions.HasAny(x+i, y) {
				overlaps = true
				break
			}
		}

		if !overlaps {
			return x, y
		}
	}

	return -1, -1 // No valid position found
}