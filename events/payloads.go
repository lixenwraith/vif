package events

import (
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
)

// DeleteRangeType defines the scope of deletion
type DeleteRangeType int

const (
	DeleteRangeChar DeleteRangeType = iota
	DeleteRangeLine
)

// DeleteRequestPayload contains coordinates for deletion
type DeleteRequestPayload struct {
	RangeType DeleteRangeType
	StartX    int
	EndX      int
	StartY    int
	EndY      int
}

// ShieldDrainPayload contains energy drain amount from external sources
type ShieldDrainPayload struct {
	Amount int
}

// DirectionalCleanerPayload contains origin for 4-way cleaner spawn
type DirectionalCleanerPayload struct {
	OriginX int
	OriginY int
}

// CharacterTypedPayload captures keypress state for EnergySystem
type CharacterTypedPayload struct {
	Char rune
	X    int // Cursor position when typed
	Y    int
}

// CharacterTypedPayloadPool reduces GC pressure during high-frequency typing
var CharacterTypedPayloadPool = sync.Pool{
	New: func() any { return &CharacterTypedPayload{} },
}

// EnergyAddPayload contains energy delta
type EnergyAddPayload struct {
	Delta  int
	Source string
}

// EnergySetPayload contains energy value
type EnergySetPayload struct {
	Value int
}

// EnergyBlinkPayload triggers visual blink state
type EnergyBlinkPayload struct {
	Type  uint32 // 0=error, 1=blue, 2=green, 3=red, 4=gold
	Level uint32 // 0=dark, 1=normal, 2=bright
}

// HeatAddPayload contains heat delta
type HeatAddPayload struct {
	Delta  int
	Source string // Debug identifier
}

// HeatSetPayload contains absolute heat value
type HeatSetPayload struct {
	Value int
}

// GoldSpawnedPayload anchors countdown timer to sequence position
type GoldSpawnedPayload struct {
	SequenceID int
	OriginX    int
	OriginY    int
	Length     int
	Duration   time.Duration
}

// GoldCompletionPayload identifies which timer to destroy
type GoldCompletionPayload struct {
	SequenceID int
}

// SplashRequestPayload creates transient visual flash
type SplashRequestPayload struct {
	Text    string
	Color   components.SplashColor
	OriginX int // Origin position (usually cursor)
	OriginY int
}

// PingGridRequestPayload carries configuration for the ping grid activation
type PingGridRequestPayload struct {
	Duration float64 // Duration in seconds
}

// SpawnChangePayload carries configuration for spawn state
type SpawnChangePayload struct {
	Enabled bool
}