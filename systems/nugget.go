package systems

import (
	"fmt"
	"math"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// NuggetSystem manages nugget spawn and respawn logic
type NuggetSystem struct {
	mu               sync.RWMutex
	ctx              *engine.GameContext
	nuggetID         atomic.Int32
	lastSpawnAttempt time.Time
}

// NewNuggetSystem creates a new nugget system
func NewNuggetSystem(ctx *engine.GameContext) *NuggetSystem {
	return &NuggetSystem{
		ctx: ctx,
	}
}

// Priority returns the system's priority
func (s *NuggetSystem) Priority() int {
	return constants.PriorityNugget
}

// Update runs the nugget system logic using generic stores
func (s *NuggetSystem) Update(world *engine.World, dt time.Duration) {
	// Fetch resources
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	now := timeRes.GameTime

	s.mu.Lock()
	defer s.mu.Unlock()

	activeNuggetEntity := s.ctx.State.GetActiveNuggetID()

	if activeNuggetEntity == 0 {
		if now.Sub(s.lastSpawnAttempt) >= constants.NuggetSpawnIntervalSeconds*time.Second {
			s.lastSpawnAttempt = now
			s.spawnNugget(world, now)
		}
		return
	}

	if !world.Nuggets.Has(engine.Entity(activeNuggetEntity)) {
		s.ctx.State.ClearActiveNuggetID(activeNuggetEntity)
	}
}

// spawnNugget creates a new nugget at a random valid position using generic stores
// Caller must hold s.mu lock
func (s *NuggetSystem) spawnNugget(world *engine.World, now time.Time) {
	x, y := s.findValidPosition(world)
	if x < 0 || y < 0 {
		return
	}

	nuggetID := s.nuggetID.Add(1)
	entity := world.CreateEntity()

	pos := components.PositionComponent{
		X: x,
		Y: y,
	}

	randomChar := constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]
	char := components.CharacterComponent{
		Rune: randomChar,
		// Use semantic color
		Color: components.ColorNugget,
		Style: components.StyleNormal,
		// SeqType/SeqLevel default to zero values (unused for nuggets)
	}

	nugget := components.NuggetComponent{
		ID:        int(nuggetID),
		SpawnTime: now,
	}

	// Use batch for atomic position validation
	batch := world.Positions.BeginBatch()
	batch.Add(entity, pos)
	if err := batch.Commit(); err != nil {
		// Position was taken while we were creating the nugget
		world.DestroyEntity(entity)
		return
	}

	// Add other components after position is committed
	world.Characters.Add(entity, char)
	world.Nuggets.Add(entity, nugget)

	s.ctx.State.SetActiveNuggetID(uint64(entity))
}

// findValidPosition finds a valid random position for a nugget using generic stores
// Caller must hold s.mu lock
func (s *NuggetSystem) findValidPosition(world *engine.World) (int, int) {
	config := engine.MustGetResource[*engine.ConfigResource](world.Resources)
	cursorPos, ok := world.Positions.Get(s.ctx.CursorEntity)
	if !ok {
		panic(fmt.Errorf("cursor destroyed"))
	}

	for attempt := 0; attempt < constants.NuggetMaxAttempts; attempt++ {
		x := rand.Intn(config.GameWidth)
		y := rand.Intn(config.GameHeight)

		if math.Abs(float64(x-cursorPos.X)) <= constants.CursorExclusionX || math.Abs(float64(y-cursorPos.Y)) <= constants.CursorExclusionY {
			continue
		}

		if world.Positions.HasAny(x, y) {
			continue
		}

		return x, y
	}

	return -1, -1
}

// ClearActiveNugget clears the active nugget reference (called when collected)
// This uses unconditional Store(0) for backward compatibility
func (s *NuggetSystem) ClearActiveNugget() {
	s.ctx.State.SetActiveNuggetID(0)
}

// ClearActiveNuggetIfMatches clears the active nugget if it matches the entity
// Returns true if cleared, false if already cleared or a different nugget was active
func (s *NuggetSystem) ClearActiveNuggetIfMatches(entity engine.Entity) bool {
	return s.ctx.State.ClearActiveNuggetID(uint64(entity))
}

// GetSystemState returns a debug string describing the current system state
func (s *NuggetSystem) GetSystemState() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeNuggetEntity := s.ctx.State.GetActiveNuggetID()

	if activeNuggetEntity == 0 {
		now := time.Now()
		timeSinceLastSpawn := now.Sub(s.lastSpawnAttempt)
		timeUntilNext := (constants.NuggetSpawnIntervalSeconds * time.Second) - timeSinceLastSpawn
		if timeUntilNext < 0 {
			timeUntilNext = 0
		}
		return "Nugget[inactive, nextSpawn=" + timeUntilNext.Round(100*time.Millisecond).String() + "]"
	}

	return "Nugget[active, entityID=" + strconv.Itoa(int(activeNuggetEntity)) + "]"
}