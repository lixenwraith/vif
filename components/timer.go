// @focus: #core { ecs, types } #game { timer }
package components

import "time"

// TimerComponent tracks time until an action is triggered (currently entity destruction)
type TimerComponent struct {
	Remaining time.Duration
}