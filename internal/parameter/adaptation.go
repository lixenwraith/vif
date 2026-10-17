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

	// RouteMinWeight keeps an extinct route's latent weight from underflowing to
	// zero; exploration is RouteScoutRate's job, not this floor's
	RouteMinWeight = 0.005

	// RouteScoutRate is the share of pool spawns assigned a uniform route,
	// independent of the weights
	RouteScoutRate = 0.10

	// RouteCorridorRecoveryRadius is how far, in cells, a death knocked out of its
	// route's corridor is searched back into it for a fitness distance
	RouteCorridorRecoveryRadius = 5

	// RouteDrainTimeout is max time to retain draining route state after gateway death
	RouteDrainTimeout = 60 * time.Second

	// AdaptTelemetryInterval is game ticks between bandit telemetry refreshes
	AdaptTelemetryInterval = 10
)
