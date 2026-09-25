package engine

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
)

// TransientResource holds player-domain spatial explosion and pulse presentation.
// Systems write, renderers read. All fields are render-frame stable.
type TransientResource struct {
	// Fixed backing, zero alloc
	ExplosionBacking [parameter.ExplosionCenterCap]ExplosionCenter
	ExplosionCount   int

	PulseBacking [parameter.PulseEffectCap]PulseEffect
	PulseCount   int
}

// ViewResource holds player-domain screen-space effect state; never replicated.
type ViewResource struct {
	Grayout GrayoutState
	Strobe  StrobeState
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

// PulseEffect is one disruptor ring for rendering, fixed at its firing cell
type PulseEffect struct {
	X, Y    int
	Age     int64 // Nanoseconds since spawn
	DurNano int64 // Lifetime in nanoseconds
	Palette component.WeaponPalette
}

// NewTransientResource creates initialized resource
func NewTransientResource() *TransientResource {
	return &TransientResource{}
}

// NewViewResource creates initialized view state
func NewViewResource() *ViewResource {
	return &ViewResource{}
}

// Reset clears view-effect state for a new game
func (r *ViewResource) Reset() {
	r.Grayout = GrayoutState{}
	r.Strobe = StrobeState{}
}

// --- Explosion API ---

// ExplosionCenters returns active slice view (no allocation)
func (r *TransientResource) ExplosionCenters() []ExplosionCenter {
	return r.ExplosionBacking[:r.ExplosionCount]
}

// Clear drops every explosion center and pulse ring
func (r *TransientResource) Clear() {
	r.ExplosionCount = 0
	r.PulseCount = 0
}

// --- Pulse API ---

// PulseEffects returns active slice view (no allocation)
func (r *TransientResource) PulseEffects() []PulseEffect {
	return r.PulseBacking[:r.PulseCount]
}
