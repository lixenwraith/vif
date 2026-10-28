package component

import (
	"time"

	"github.com/lixenwraith/vif/pkg/machine"
)

// SwarmState is a swarm's place in its behavior machine
type SwarmState uint8

const (
	SwarmStateChase      SwarmState = iota // homes on its target until the interval runs out
	SwarmStateLock                         // frozen on the cell it stored, the charge line counting down
	SwarmStateCharge                       // flies at the stored cell, arriving as the entry ends
	SwarmStateTeleport                     // teleports to a landing by the stored cell, out of line of sight
	SwarmStateDecelerate                   // brakes after the attack, counted as a charge
	SwarmStateArrive                       // stands where the teleport set it down, counted as a charge
)

// SwarmComponent holds swarm-specific runtime state, composite structure managed via HeaderComponent
type SwarmComponent struct {
	Machine machine.Instance[SwarmState]

	PatternIndex   int           // glyph pattern shown
	PatternElapsed time.Duration // time the pattern has shown

	LockedTargetX, LockedTargetY int // the cell the lock stored, which the attack goes to

	ChargeStartX, ChargeStartY   float64 // sub-cell position the charge left from
	ChargeTargetX, ChargeTargetY float64 // the stored cell's centre

	TeleportTargetX, TeleportTargetY int // header cell the teleport aims at, which the arrival takes while clear

	ChargesCompleted int // Decelerate and Arrive entries: charges, teleports and locks that could do neither
}
