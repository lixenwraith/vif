package parameter

import (
	"time"
)

// Route Distribution — Batched Softmax Bandit
const (
	// RoutePoolDefaultSize is pre-sampled assignments per weight update cycle
	RoutePoolDefaultSize = 100

	// RouteLearningRate (eta) for EXP3-style weight update
	RouteLearningRate = 0.1

	// RouteMinWeight floor prevents route starvation
	RouteMinWeight = 0.05

	// RouteDrainTimeout is max time to retain draining route state after gateway death
	RouteDrainTimeout = 60 * time.Second
)