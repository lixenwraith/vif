package parameter

import "time"

// APM admission policy. APM exists solely to drive adaptive music, so it counts
// player gestures rather than events: a tick admits at most one action, pointer
// travel at most one per APMPointerTicks of no other action, and a one-second bucket at most
// APMMaxPerSecond. Macro playback and auto-fire, like every non-input origin, admit nothing.
const (
	// A saturated five-second window reads 360, so TierPeakAPM survives a brief lull
	APMMaxPerSecond = 6
	// Two placements a second: pointer travel alone stays in the Normal tier
	APMPointerTicks = uint64(500 * time.Millisecond / GameUpdateInterval)
)
