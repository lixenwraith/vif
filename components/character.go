package components

import "github.com/gdamore/tcell/v2"

// SequenceType represents the type of character sequence
type SequenceType int

const (
	SequenceGreen SequenceType = iota // Positive scoring
	SequenceRed                        // Negative scoring
	SequenceBlue                       // Positive scoring + trail effect
	SequenceGold                       // Bonus sequence - fills heat to max when completed
)

// SequenceLevel represents the brightness level of a sequence
type SequenceLevel int

const (
	LevelDark   SequenceLevel = iota // x1 multiplier
	LevelNormal                      // x2 multiplier
	LevelBright                      // x3 multiplier
)

// CharacterComponent represents a character entity
type CharacterComponent struct {
	Rune  rune
	Style tcell.Style
}

// SequenceComponent represents membership in a character sequence
type SequenceComponent struct {
	ID    int           // Unique sequence ID
	Index int           // Position in the sequence (0-based)
	Type  SequenceType  // Type of sequence (Green, Red, Blue, Gold)
	Level SequenceLevel // Sequence level (Dark, Normal, Bright)
}

// GoldSequenceComponent tracks the active gold sequence state
type GoldSequenceComponent struct {
	Active       bool      // Whether a gold sequence is currently active
	SequenceID   int       // ID of the gold sequence
	StartTimeNano int64    // Start time in nanoseconds (for atomic operations)
	CharSequence []rune    // The 10-character sequence
	CurrentIndex int       // Current typing position (0-10)
}
