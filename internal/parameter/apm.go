package parameter

import "time"

// APM admission policy. APM exists solely to drive adaptive music, so it counts player
// gestures rather than events: one per dispatch pass of input, diminishing when one key
// is mashed, one per APMPointerCells of pointer travel, and at most APMMaxPerSecond in a
// one-second bucket. Macro playback and auto-fire, like every non-input origin, admit nothing.
const (
	// A saturated five-second window reads 480, the APM at which tempo reaches its top
	APMMaxPerSecond = 8
	// Columns of pointer travel worth one keystroke, about a word motion: a dodging
	// pointer reads as fast as mashed keys, a slow reposition as a few presses
	APMPointerCells = 6
	// A pointer alone fills six of a bucket's eight, reaching peak but not top tempo
	APMPointerPerSecond = 6
	// A key repeated within this of its last press extends a mashing streak
	APMRepeatTicks = uint64(500 * time.Millisecond / GameUpdateInterval)
)
