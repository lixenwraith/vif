package systems

import (
	"math"
	"math/rand"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

const (
	nuggetSpawnIntervalSeconds = 5 // Attempt spawn every 5 seconds
	nuggetMaxAttempts          = 100
)

// NuggetSystem manages nugget spawn and respawn logic
type NuggetSystem struct {
	mu               sync.RWMutex
	ctx              *engine.GameContext
	activeNugget     atomic.Uint64
	nuggetID         atomic.Int32
	lastSpawnAttempt time.Time
}

// NewNuggetSystem creates a new nugget system
func NewNuggetSystem(ctx *engine.GameContext) *NuggetSystem {
	return &NuggetSystem{
		ctx: ctx,
	}
}

// Priority returns the system's priority (between SpawnSystem and GoldSequenceSystem)
func (s *NuggetSystem) Priority() int {
	return 18
}

// Update runs the nugget system logic
func (s *NuggetSystem) Update(world *engine.World, dt time.Duration) {
	now := s.ctx.TimeProvider.Now()

	s.mu.Lock()
	defer s.mu.Unlock()

	activeNuggetEntity := s.activeNugget.Load()

	if activeNuggetEntity == 0 {
		if now.Sub(s.lastSpawnAttempt) >= nuggetSpawnIntervalSeconds*time.Second {
			s.lastSpawnAttempt = now
			s.spawnNugget(world, now)
		}
		return
	}

	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if !world.HasComponent(engine.Entity(activeNuggetEntity), nuggetType) {
		s.activeNugget.CompareAndSwap(activeNuggetEntity, 0)
	}
}

// spawnNugget creates a new nugget at a random valid position
// Caller must hold s.mu lock
func (s *NuggetSystem) spawnNugget(world *engine.World, now time.Time) {
	x, y := s.findValidPosition(world)
	if x < 0 || y < 0 {
		return
	}

	nuggetID := s.nuggetID.Add(1)
	entity := world.CreateEntity()

	world.AddComponent(entity, components.PositionComponent{
		X: x,
		Y: y,
	})

	randomChar := constants.AlphanumericRunes[rand.Intn(len(constants.AlphanumericRunes))]
	style := tcell.StyleDefault.
		Foreground(render.RgbNuggetOrange).
		Background(render.RgbBackground)
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  randomChar,
		Style: style,
	})

	world.AddComponent(entity, components.NuggetComponent{
		ID:        int(nuggetID),
		SpawnTime: now,
	})

	tx := world.BeginSpatialTransaction()
	result := tx.Spawn(entity, x, y)
	if result.HasCollision {
		// Position was taken while we were creating the nugget
		world.DestroyEntity(entity)
		return
	}
	tx.Commit()
	s.activeNugget.Store(uint64(entity))
}

// findValidPosition finds a valid random position for a nugget
// Caller must hold s.mu lock
func (s *NuggetSystem) findValidPosition(world *engine.World) (int, int) {
	gameWidth := s.ctx.GameWidth
	gameHeight := s.ctx.GameHeight
	cursor := s.ctx.State.ReadCursorPosition()

	for attempt := 0; attempt < nuggetMaxAttempts; attempt++ {
		x := rand.Intn(gameWidth)
		y := rand.Intn(gameHeight)

		if math.Abs(float64(x-cursor.X)) <= 5 || math.Abs(float64(y-cursor.Y)) <= 3 {
			continue
		}

		if world.GetEntityAtPosition(x, y) != 0 {
			continue
		}

		return x, y
	}

	return -1, -1
}

// GetActiveNugget returns the entity ID of the active nugget (0 if none)
func (s *NuggetSystem) GetActiveNugget() uint64 {
	return s.activeNugget.Load()
}

// ClearActiveNugget clears the active nugget reference (called when collected)
// This uses unconditional Store(0) for backward compatibility
func (s *NuggetSystem) ClearActiveNugget() {
	s.activeNugget.Store(0)
}

// ClearActiveNuggetIfMatches clears the active nugget reference only if it matches the given entity ID
// Returns true if the nugget was cleared, false if it was already cleared or a different nugget was active
// This is the race-safe version that should be preferred when the entity ID is known
func (s *NuggetSystem) ClearActiveNuggetIfMatches(entityID uint64) bool {
	return s.activeNugget.CompareAndSwap(entityID, 0)
}

// GetSystemState returns a debug string describing the current system state
func (s *NuggetSystem) GetSystemState() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	activeNuggetEntity := s.activeNugget.Load()

	if activeNuggetEntity == 0 {
		now := s.ctx.TimeProvider.Now()
		timeSinceLastSpawn := now.Sub(s.lastSpawnAttempt)
		timeUntilNext := (nuggetSpawnIntervalSeconds * time.Second) - timeSinceLastSpawn
		if timeUntilNext < 0 {
			timeUntilNext = 0
		}
		return "Nugget[inactive, nextSpawn=" + timeUntilNext.Round(100*time.Millisecond).String() + "]"
	}

	return "Nugget[active, entityID=" + strconv.Itoa(int(activeNuggetEntity)) + "]"
}

// JumpToNugget returns the position of the active nugget, or (-1, -1) if no nugget exists
func (s *NuggetSystem) JumpToNugget(world *engine.World) (int, int) {
	// Get active nugget entity ID
	activeNuggetEntity := s.activeNugget.Load()
	if activeNuggetEntity == 0 {
		return -1, -1
	}

	// Get position component from entity
	posType := reflect.TypeOf(components.PositionComponent{})
	posComp, ok := world.GetComponent(engine.Entity(activeNuggetEntity), posType)
	if !ok {
		// No position component (shouldn't happen, but handle gracefully)
		return -1, -1
	}

	// Extract position
	pos := posComp.(components.PositionComponent)
	return pos.X, pos.Y
}