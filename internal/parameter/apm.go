package parameter

import "time"

// APM admission policy. APM exists solely to drive adaptive music, so it counts player
// gestures rather than events: one per dispatch pass of input, diminishing when one key
// is mashed, one per pointer reversal or APMPointerSweep of one-way travel, and at most
// APMMaxPerSecond in a one-second bucket. Macro playback and auto-fire, like every
// non-input origin, admit nothing.
const (
	// A saturated five-second window reads 480, the APM at which tempo reaches its top
	APMMaxPerSecond = 8
	// Columns a pointer must have moved before a reversal counts: a dodge, not a tremor
	APMPointerCells = 6
	// Columns of travel in one direction worth a gesture on their own, a third of the
	// map: a sweep across the screen is one or two, a slow round of it a few
	APMPointerSweep = 40
	// A pointer alone fills four of a bucket's eight: dodging reaches intense, while
	// peak and the top tempo ask for keys or fire as well
	APMPointerPerSecond = 4
	// A key repeated within this of its last press extends a mashing streak
	APMRepeatTicks = uint64(500 * time.Millisecond / GameUpdateInterval)
)

// Tier thresholds the music conductor reads a five-second burst against, as actions
// per minute; the engine's game state measures the burst in every build
const (
	TierNormalAPM   = 60
	TierElevatedAPM = 140
	TierIntenseAPM  = 220
	TierPeakAPM     = 300
)
