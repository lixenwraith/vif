package component

// SequenceType represents the type of character sequence
type SequenceType int

const (
	SequenceGreen SequenceType = iota // Positive scoring
	SequenceRed                       // Negative scoring
	SequenceBlue                      // Positive scoring + trail effect
	SequenceGold                      // Bonus sequence - fills heat to max when completed
)

// SequenceLevel represents the brightness level of a sequence
type SequenceLevel int

const (
	LevelDark   SequenceLevel = iota // x1 multiplier
	LevelNormal                      // x2 multiplier
	LevelBright                      // x3 multiplier
)

// CharacterComponent represents a character entity
// Uses semantic types resolved at render time
type CharacterComponent struct {
	Rune  rune
	Color ColorClass // Semantic color, resolved by renderer
	Style TextStyle  // Semantic style, resolved by renderer
	// Sequence info used to derive actual color
	SeqType  SequenceType
	SeqLevel SequenceLevel
}

// SequenceComponent represents membership in a character sequence
type SequenceComponent struct {
	ID    int           // Unique sequence ID
	Index int           // Position in the sequence (0-based)
	Type  SequenceType  // Type of sequence (Green, Red, Blue, Gold)
	Level SequenceLevel // Sequence level (Dark, Normal, Bright)
}