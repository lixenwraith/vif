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

// TypeableLevel represents brightness/intensity affecting multiplier
type TypeableLevel uint8

const (
	LevelDark   TypeableLevel = iota // x1
	LevelNormal                      // x2
	LevelBright                      // x3
)

// TypeableComponent marks an entity as a valid typing target
type TypeableComponent struct {
	Char  rune
	Type  TypeableType
	Level TypeableLevel
}