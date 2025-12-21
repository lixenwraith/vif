package component

// CharacterType represents the type of character sequence
type CharacterType int

const (
	CharacterGreen CharacterType = iota // Positive scoring
	CharacterRed                        // Negative scoring
	CharacterBlue                       // Positive scoring + trail effect
	CharacterGold                       // Bonus sequence - fills heat to max when completed
)

// CharacterLevel represents the brightness level of a sequence
type CharacterLevel int

const (
	LevelDark   CharacterLevel = iota // x1 multiplier
	LevelNormal                       // x2 multiplier
	LevelBright                       // x3 multiplier
)

// CharacterComponent represents a character entity
// Uses semantic types resolved at render time
type CharacterComponent struct {
	Rune  rune
	Color ColorClass // Semantic color, resolved by renderer
	Style TextStyle  // Semantic style, resolved by renderer
	// Sequence info used to derive actual color
	SeqType  CharacterType
	SeqLevel CharacterLevel
}