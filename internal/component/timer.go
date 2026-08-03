package component

import "time"

// TimerComponent tracks time until an action is triggered
type TimerComponent struct {
	Remaining time.Duration
}