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

	// A data species' physics, which its program's [prototype.physics] keys replace
	// one by one: the swarm's chase, mid between the drain's crawl and the snake's
	// dart, with no arrival steering. BehaviorSpeed is the cruise speed its homing
	// holds (cells/sec)
	BehaviorSpeed = 8.0
	// BehaviorAccel is its acceleration toward the target (cells/sec²)
	BehaviorAccel = 6.0
	// BehaviorDrag is its deceleration above the cruise speed (1/sec)
	BehaviorDrag = 2.0
	// BehaviorArrivalRadius is how near the target its arrival steering starts
	// (cells); 0 steers none
	BehaviorArrivalRadius = 0.0
	// BehaviorArrivalDrag is the drag's added multiple at the target, ramped in
	// over the arrival radius; 0 adds none
	BehaviorArrivalDrag = 0.0
	// BehaviorDeadZone is how near the target it snaps onto it once slow (cells);
	// 0 takes the homing default, half a cell
	BehaviorDeadZone = 0.0
	// BehaviorRestitution is velocity it keeps on a wall or boundary bounce
	BehaviorRestitution = 0.5

	// BehaviorChargeLinePulseShare is one charge-line pulse's share of the entry its
	// state flags, the swarm's charge to its lock: the pulses run the entry's end, so
	// a raider timed as the swarm draws the swarm's line and another its own lock's
	BehaviorChargeLinePulseShare = float64(SwarmChargeDuration) / float64(SwarmLockDuration)

	// BehaviorChargeRemainingMin floors the time a charge divides its distance by,
	// so the last tick of an entry does not ask for unbounded speed
	BehaviorChargeRemainingMin = time.Millisecond
)
