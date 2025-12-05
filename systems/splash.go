// FILE: systems/splash.go
package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// SplashSystem handles splash timeout
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

// Update checks splash timeout and deactivates if expired
func (s *SplashSystem) Update(world *engine.World, dt time.Duration) {
	splash, ok := world.Splashes.Get(s.ctx.SplashEntity)
	if !ok || splash.Length == 0 {
		return
	}

	timeRes := engine.MustGetResource[*engine.TimeResource](world.Resources)
	elapsed := timeRes.GameTime.UnixNano() - splash.StartNano

	if elapsed >= constants.SplashDuration.Nanoseconds() {
		splash.Length = 0
		world.Splashes.Add(s.ctx.SplashEntity, splash)
	}
}
