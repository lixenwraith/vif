package visual

// A swarm teleport's beam, by fraction of the teleport: the head runs from the swarm to
// its landing until TeleportFillEnd, the full line holds until TeleportHoldEnd, and the
// tail follows the head into the landing over the TeleportRecede that is left
const (
	TeleportFillEnd = 0.3
	TeleportHoldEnd = 0.7
	TeleportRecede  = 1.0 - TeleportHoldEnd
)
