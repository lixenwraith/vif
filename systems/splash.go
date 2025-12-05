package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// SplashSystem manages the lifecycle of splash entities
// Primarily responsible for cleaning up expired transient splashes
type SplashSystem struct {
	ctx *engine.GameContext
}

// NewSplashSystem creates a new splash system
func NewSplashSystem(ctx *engine.GameContext) *SplashSystem {
	return &SplashSystem{ctx: ctx}
}

// Priority returns the system's priority (low, after game logic)
func (s *SplashSystem) Priority() int {
	return constants.PrioritySplash
}

// Update checks for expired transient splashes and destroys them
func (s *SplashSystem) Update(world *engine.World, dt time.Duration) {
	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	nowNano := timeRes.GameTime.UnixNano()

	// Collect entities to destroy (avoid modifying during iteration)
	var toDestroy []engine.Entity

	entities := world.Splashes.All()
	for _, entity := range entities {
		splash, ok := world.Splashes.Get(entity)
		if !ok {
			continue
		}

		// Only manage lifecycle for Transient splashes
		if splash.Mode == components.SplashModeTransient {
			elapsed := nowNano - splash.StartNano
			if elapsed >= splash.Duration {
				toDestroy = append(toDestroy, entity)
			}
		}
	}

	// Cleanup expired entities
	for _, entity := range toDestroy {
		world.DestroyEntity(entity)
	}
}