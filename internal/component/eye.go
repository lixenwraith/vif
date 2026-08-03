package component

import "time"

// EyeType identifies the visual and parametric variant
type EyeType uint8

const (
	EyeTypeVoid EyeType = iota
	EyeTypeFlame
	EyeTypeFrost
	EyeTypeStorm
	EyeTypeBlood
	EyeTypeGolden
	EyeTypeAbyss
	eyeTypeSentinel // unexported; equals parameter.EyeTypeCount
)

// EyeComponent holds per-entity runtime state for eye composites
type EyeComponent struct {
	Type           EyeType
	FrameIndex     int
	FrameRemaining time.Duration
}