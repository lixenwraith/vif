// @lixen: #dev{feature[shield(render,system)]}
package component

import (
	"time"
)

// BoostComponent tracks the state of the player's boost ability
// Uses duration-based tracking for pause safety and simulation determinism
type BoostComponent struct {
	Active        bool
	Remaining     time.Duration
	TotalDuration time.Duration // For UI progress calculation
}