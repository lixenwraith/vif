package engine

import (
	"time"

	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/terminal"
)

// TransientResource holds short-lived visual effect state
// Systems write, renderers read. All fields are render-frame stable.
type TransientResource struct {
	// Screen-space post-process effects
	Grayout GrayoutState
	Strobe  StrobeState

	// Spatial explosion effects (fixed backing, zero alloc)
	ExplosionBacking [parameter.ExplosionCenterCap]ExplosionCenter
	ExplosionCount   int
	ExplosionDurNano int64
}

// GrayoutState controls screen desaturation effect
type GrayoutState struct {
	Active    bool
	Intensity float64
}

// StrobeState controls screen flash overlay
type StrobeState struct {
	Active          bool
	Color           terminal.RGB
	Intensity       float64       // Base intensity (0.0-1.0)
	InitialDuration time.Duration // Original duration for envelope calculation
	Remaining       time.Duration // Time until auto-deactivate
}

// ExplosionCenter represents a single explosion for rendering
type ExplosionCenter struct {
	X, Y      int
	Radius    int64               // Q32.32 cells
	Intensity int64               // Q32.32, Scale = 1.0 base
	Age       int64               // Nanoseconds since spawn
	Type      event.ExplosionType // Explosion variant for palette selection
}

// NewTransientResource creates initialized resource
func NewTransientResource() *TransientResource {
	return &TransientResource{
		ExplosionDurNano: parameter.ExplosionFieldDuration.Nanoseconds(),
	}
}

// Reset clears all transient state for new game
func (r *TransientResource) Reset() {
	r.Grayout = GrayoutState{}
	r.Strobe = StrobeState{}
	r.ExplosionCount = 0
}

// --- Explosion API (prep for Phase 3) ---

// ExplosionCenters returns active slice view (no allocation)
func (r *TransientResource) ExplosionCenters() []ExplosionCenter {
	return r.ExplosionBacking[:r.ExplosionCount]
}

// ClearExplosions resets explosion state
func (r *TransientResource) ClearExplosions() {
	r.ExplosionCount = 0
}