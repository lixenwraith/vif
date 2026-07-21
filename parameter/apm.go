package parameter

import "time"

// APM admission policy — APM exists solely to drive adaptive music.
// The Router is the single admission point; machine input (macro playback,
// mouse auto-fire) admits zero by design.
// Units are milli-actions; GameState divides by APMUnit at publish.
const (
	APMUnit         = 1000
	APMWeightFull   = 1000                   // distinct action
	APMWeightRepeat = 400                    // same action outside the dedup window
	APMRepeatWindow = 250 * time.Millisecond // identical actions inside window: dropped
	APMMaxPerSecond = 5000                   // ceiling: 5 full actions/s ~= TierPeakAPM

	MouseAPMSampleInterval = 150 * time.Millisecond
)
