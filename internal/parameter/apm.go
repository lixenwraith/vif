package parameter

// APM admission policy. APM exists solely to drive adaptive music, so it counts player
// gestures rather than events: one per dispatch pass of input, one per APMPointerCells
// of pointer travel, and at most APMMaxPerSecond in a one-second bucket. Macro playback
// and auto-fire, like every non-input origin, admit nothing.
const (
	// A saturated five-second window reads 360, so TierPeakAPM survives a brief lull
	APMMaxPerSecond = 6
	// Columns of pointer travel worth one keystroke, about a word motion: a dodging
	// pointer reads as fast as mashed keys, a slow reposition as a few presses
	APMPointerCells = 6
)
