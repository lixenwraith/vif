package component

// TypeableType categorizes entities that can be typed
type TypeableType uint8

const (
	TypeGreen TypeableType = iota
	TypeRed
	TypeBlue
	TypeGold
	TypeNugget
)

// TypeableLevel mirrors SequenceLevel for typeable entities
type TypeableLevel = SequenceLevel

// TypeableComponent marks an entity as a valid typing target
// Decouples interaction capability from visual rendering (CharacterComponent)
type TypeableComponent struct {
	Char  rune
	Type  TypeableType
	Level TypeableLevel
}