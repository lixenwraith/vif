package visual

import "time"

// A swarm teleport's line, by fraction of the teleport: the head runs from the swarm to
// its landing until TeleportFillEnd, the full line holds until TeleportHoldEnd, and the
// tail follows the head into the landing over the TeleportRecede that is left
const (
	TeleportFillEnd = 0.3
	TeleportHoldEnd = 0.7
	TeleportRecede  = 1.0 - TeleportHoldEnd
)

// LandingBlinkStep is how long the landing marker holds each colour of its cycle
const LandingBlinkStep = 100 * time.Millisecond
