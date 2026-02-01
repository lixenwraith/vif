package fitness

import "github.com/lixenwraith/vi-fighter/genetic/tracking"

// Aggregator calculates fitness score from collected metrics
type Aggregator interface {
	Calculate(metrics tracking.MetricBundle, ctx Context) float64
}

// NormalizeFunc converts a raw metric to a 0-1 score
type NormalizeFunc func(raw float64) float64

// NormalizeLinear creates a linear normalizer
func NormalizeLinear(min, max float64) NormalizeFunc {
	rangeVal := max - min
	if rangeVal <= 0 {
		return func(raw float64) float64 { return 0 }
	}
	return func(raw float64) float64 {
		v := (raw - min) / rangeVal
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
}

// NormalizeInverse creates an inverse normalizer: 1 / (1 + raw/scale)
func NormalizeInverse(scale float64) NormalizeFunc {
	if scale <= 0 {
		scale = 1
	}
	return func(raw float64) float64 {
		return 1.0 / (1.0 + raw/scale)
	}
}

// NormalizeCap creates a capped normalizer: min(raw/max, 1.0)
func NormalizeCap(max float64) NormalizeFunc {
	if max <= 0 {
		return func(raw float64) float64 { return 0 }
	}
	return func(raw float64) float64 {
		v := raw / max
		if v > 1 {
			return 1
		}
		if v < 0 {
			return 0
		}
		return v
	}
}