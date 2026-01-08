package input

// InputMode mirrors game modes for parser context
// Kept in sync by mode.Router via SetMode()
// Values match engine.GameMode for easy conversion
type InputMode uint8

const (
	ModeNormal InputMode = iota
	ModeVisual
	ModeInsert
	ModeSearch
	ModeCommand
	ModeOverlay
)

// InputState tracks Normal-mode parser state machine
type InputState uint8

const (
	StateIdle InputState = iota
	StateCount
	StateCharWait
	StateOperatorWait
	StateOperatorCharWait
	StatePrefixG
	StateOperatorPrefixG
)