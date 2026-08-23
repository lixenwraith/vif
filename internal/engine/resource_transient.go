package engine

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TransientResource holds short-lived visual effect state
// Systems write, renderers read. All fields are render-frame stable.
type TransientResource struct {
	// Screen-space post-process effects (local view state)
	Grayout GrayoutState
	Strobe  StrobeState

	// Spatial explosion effects, shared (fixed backing, zero alloc)
	ExplosionBacking [parameter.ExplosionCenterCap]ExplosionCenter
	ExplosionCount   int
}

// GrayoutState controls screen desaturation effect
type GrayoutState struct {
	Active    bool
	Intensity float64
}

// StrobeState controls screen flash overlay
type StrobeState struct {
	Active          bool
	Color           color.RGB
	Intensity       float64       // Base intensity (0.0-1.0)
	InitialDuration time.Duration // Original duration for envelope calculation
	Remaining       time.Duration // Time until auto-deactivate
}

// ExplosionCenter represents a single explosion for rendering
type ExplosionCenter struct {
	X, Y      int
	Radius    float64             // Cells
	Intensity float64             // Scale = 1.0 base
	Age       int64               // Nanoseconds since spawn
	DurNano   int64               // Lifetime in nanoseconds
	Type      event.ExplosionType // Explosion variant for palette selection
}

// NewTransientResource creates initialized resource
func NewTransientResource() *TransientResource {
	return &TransientResource{}
}

// Reset clears view-effect state for a new game; centers are cleared by their owning system
func (r *TransientResource) Reset() {
	r.Grayout = GrayoutState{}
	r.Strobe = StrobeState{}
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
