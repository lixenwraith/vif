package component

import "sync/atomic"

// HeatComponent tracks the heat state
type HeatComponent struct {
	Current atomic.Int64
}