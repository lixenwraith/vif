package components

import "time"

// TrailComponent represents a trail effect particle
type TrailComponent struct {
	Intensity float64
	Timestamp time.Time
}
