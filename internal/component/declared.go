package component

// Declared names what a scenario declares beside the code's own: its behavior
// programs, the species from SpeciesDataFirst, and its loot kinds, the kinds from
// LootCount, each in sorted-name order
type Declared struct {
	Programs []string
	Loot     []string
}
