package component

// BuffType: rod (lightning), launcher (missile), chain (pull)
type BuffType int

const (
	BuffRod BuffType = iota
	BuffLauncher
	BuffChain
)

// BuffComponent tracks cursor active buffs
type BuffComponent struct {
	Active map[BuffType]bool
}