package parameter

import "time"

// Data species: species a scenario defines by behavior programs
const (
	// BehaviorProgramsMax is how many programs a scenario may declare, the rows the
	// data species range reserves after the code species
	BehaviorProgramsMax = 8

	// BehaviorNameMax bounds a program name in bytes; the name keys its status rows
	BehaviorNameMax = 24

	// BehaviorFootprintMax bounds a data species' width and height in cells
	BehaviorFootprintMax = 8

	// BehaviorRestitution is velocity a data species keeps on a wall or boundary
	// bounce, the swarm's
	BehaviorRestitution = 0.5

	// BehaviorChargeRemainingMin floors the time a charge divides its distance by,
	// so the last tick of an entry does not ask for unbounded speed
	BehaviorChargeRemainingMin = time.Millisecond
)
