// @focus: #gameplay { resource }
package components

import "sync/atomic"

// HeatComponent tracks the heat meter state
// Attached to cursor entity (single-player)
type HeatComponent struct {
	Current atomic.Int64
}