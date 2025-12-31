package component

import "github.com/lixenwraith/vi-fighter/terminal"

// SpiritComponent represents a converging visual effect entity
// Position presence is at StartX/StartY to avoid target saturation
// Actual render position is calculated via Lerp from Start to Target
type SpiritComponent struct {
	// Starting position in Q16.16 (where the spirit spawned)
	StartX, StartY int32

	// Target position in Q16.16 (convergence point)
	TargetX, TargetY int32

	// Animation progress in Q16.16: 0 = start, Scale = complete
	Progress int32

	// Speed increment per tick in Q16.16 (distance-dependent)
	Speed int32

	// Visual properties
	Rune       rune
	BaseColor  terminal.RGB
	BlinkColor terminal.RGB
}