package components

import "github.com/gdamore/tcell/v2"

// SequenceType represents the type of character sequence
type SequenceType int

const (
	SequenceGreen SequenceType = iota // Positive scoring
	SequenceRed                        // Negative scoring
	SequenceBlue                       // Positive scoring + trail effect
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
	Type  SequenceType  // Type of sequence (Green, Red, Blue)
	Level SequenceLevel // Sequence level (Dark, Normal, Bright)
}
