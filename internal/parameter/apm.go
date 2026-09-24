package parameter

// APM admission policy — APM exists solely to drive adaptive music.
// The scheduler admits each dispatched input-origin event at full weight; macro
// playback and auto-fire, like every other origin, admit nothing.
// Units are milli-actions; GameState divides by APMUnit at publish.
const (
	APMUnit         = 1000
	APMWeightFull   = 1000 // one action
	APMMaxPerSecond = 5000 // ceiling: 5 full actions/s ~= TierPeakAPM

	// APMPendingBurstMax caps actions folded into a single APM bucket
	APMPendingBurstMax = 4 * APMMaxPerSecond
)
