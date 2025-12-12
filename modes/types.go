// @lixen: #focus{control[types,motion,range]}
// @lixen: #interact{state[motion]}
package modes

import "github.com/lixenwraith/vi-fighter/engine"

// RangeType defines the shape of the target region
type RangeType int

const (
	RangeChar RangeType = iota
	RangeLine
)

// MotionStyle defines how operators interpret the range endpoint
type MotionStyle int

const (
	StyleExclusive MotionStyle = iota // Endpoint not included (w, b)
	StyleInclusive                    // Endpoint included (e, f, $)
)

// MotionResult encapsulates the calculated target of a motion
type MotionResult struct {
	StartX, StartY int
	EndX, EndY     int
	Type           RangeType
	Style          MotionStyle
	Valid          bool // False if motion found no target
}

// MotionFunc calculates target without world modification
type MotionFunc func(ctx *engine.GameContext, startX, startY, count int) MotionResult

// CharMotionFunc requires additional character input
type CharMotionFunc func(ctx *engine.GameContext, startX, startY, count int, char rune) MotionResult