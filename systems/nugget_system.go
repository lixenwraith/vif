package systems

import (
	"math"
	"math/rand"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

const (
	nuggetSpawnIntervalSeconds = 5 // Attempt spawn every 5 seconds
	nuggetMaxAttempts          = 100
)

// NuggetSystem manages nugget spawn and respawn logic
type NuggetSystem struct {
	mu                sync.RWMutex
	ctx               *engine.GameContext
	activeNugget      atomic.Uint64   // Entity ID of active nugget (0 if none), stored as uint64
	nuggetID          atomic.Int32    // Atomic counter for unique nugget IDs
	lastSpawnAttempt  time.Time       // Protected by mu
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

	// Check if we have an active nugget
	activeNuggetEntity := s.activeNugget.Load()

	// If no active nugget, check if it's time to spawn one
	if activeNuggetEntity == 0 {
		// Check if enough time has passed since last spawn attempt
		if now.Sub(s.lastSpawnAttempt) >= nuggetSpawnIntervalSeconds*time.Second {
			s.lastSpawnAttempt = now
			s.spawnNugget(world, now)
		}
		return
	}

	// Verify active nugget still exists
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if !world.HasComponent(engine.Entity(activeNuggetEntity), nuggetType) {
		// Nugget was removed/destroyed, clear active reference
		s.activeNugget.Store(0)
	}
}

// spawnNugget creates a new nugget at a random valid position
// Caller must hold s.mu lock
func (s *NuggetSystem) spawnNugget(world *engine.World, now time.Time) {
	// Find a valid position
	x, y := s.findValidPosition(world)
	if x < 0 || y < 0 {
		// No valid position found
		return
	}

	// Get next nugget ID
	nuggetID := s.nuggetID.Add(1)

	// Create nugget entity
	entity := world.CreateEntity()

	// Add position component
	world.AddComponent(entity, components.PositionComponent{
		X: x,
		Y: y,
	})

	// Add character component (orange circle)
	style := tcell.StyleDefault.
		Foreground(render.RgbNuggetOrange).
		Background(render.RgbBackground)
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  '●',
		Style: style,
	})

	// Add nugget component
	world.AddComponent(entity, components.NuggetComponent{
		ID:        int(nuggetID),
		SpawnTime: now,
	})

	// Update spatial index
	world.UpdateSpatialIndex(entity, x, y)

	// Store active nugget reference
	s.activeNugget.Store(uint64(entity))
}

// findValidPosition finds a valid random position for a nugget
// Caller must hold s.mu lock
func (s *NuggetSystem) findValidPosition(world *engine.World) (int, int) {
	// Read dimensions from context
	gameWidth := s.ctx.GameWidth
	gameHeight := s.ctx.GameHeight

	// Read cursor position from GameState (atomic reads)
	cursor := s.ctx.State.ReadCursorPosition()

	for attempt := 0; attempt < nuggetMaxAttempts; attempt++ {
		x := rand.Intn(gameWidth)
		y := rand.Intn(gameHeight)

		// Check if far enough from cursor (same exclusion zone as spawn system)
		if math.Abs(float64(x-cursor.X)) <= 5 || math.Abs(float64(y-cursor.Y)) <= 3 {
			continue
		}

		// Check for overlaps with existing characters
		if world.GetEntityAtPosition(x, y) != 0 {
			continue
		}

		return x, y
	}

	return -1, -1 // No valid position found
}

// GetActiveNugget returns the entity ID of the active nugget (0 if none)
func (s *NuggetSystem) GetActiveNugget() uint64 {
	return s.activeNugget.Load()
}

// ClearActiveNugget clears the active nugget reference (called when collected)
func (s *NuggetSystem) ClearActiveNugget() {
	s.activeNugget.Store(0)
}
