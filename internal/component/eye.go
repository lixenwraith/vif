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

// EyeTypeNames are what a document calls each EyeType
var EyeTypeNames = [eyeTypeSentinel]string{
	EyeTypeVoid:   "void",
	EyeTypeFlame:  "flame",
	EyeTypeFrost:  "frost",
	EyeTypeStorm:  "storm",
	EyeTypeBlood:  "blood",
	EyeTypeGolden: "golden",
	EyeTypeAbyss:  "abyss",
}

// EyeComponent holds per-entity runtime state for eye composites
type EyeComponent struct {
	Type           EyeType
	FrameIndex     int
	FrameRemaining time.Duration
}
