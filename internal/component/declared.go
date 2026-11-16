package component

// Declared names what a scenario declares beside the code's own: its behavior
// programs, the species from SpeciesDataFirst, and its loot kinds, the kinds from
// LootCount, each in sorted-name order
type Declared struct {
	Programs []string
	Loot     []string
	Maps     []string
	Signals  map[string][]string // each program's signals, in the order it declares them
}

// MapID names one of a scenario's maps in a payload: 0 none, else its place among
// them, in sorted-name order, from 1
type MapID uint8

// MapLayout is a map a scenario carries, compiled: its size in cells and what its
// legend lays, each kind in row order
type MapLayout struct {
	Name          string
	Width, Height int
	Spawns        []MapSpawn
}

// MapSpawn is an instance a map lays: its program's index, and its header's cell
// counted from the map's top left
type MapSpawn struct {
	X, Y    int
	Program uint8
}
