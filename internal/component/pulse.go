package component

import "time"

// PulseComponent tracks disruptor pulse visual effect on cursor
type PulseComponent struct {
	OriginX, OriginY int           // Map position at fire time (effect stays here)
	Duration         time.Duration // Total effect duration
	Remaining        time.Duration // Time remaining
}