package fitness

import "github.com/lixenwraith/vi-fighter/genetic/tracking"

// WeightedAggregator calculates fitness as weighted sum of metric scores
type WeightedAggregator struct {
	Weights         map[string]float64
	Normalizers     map[string]NormalizeFunc
	ContextAdjuster func(weights map[string]float64, ctx Context) map[string]float64
}

func (a *WeightedAggregator) Calculate(metrics tracking.MetricBundle, ctx Context) float64 {
	weights := a.Weights
	if a.ContextAdjuster != nil && ctx != nil {
		weights = a.ContextAdjuster(weights, ctx)
	}

	var fitness float64
	for key, weight := range weights {
		raw, ok := metrics[key]
		if !ok {
			continue
		}

		normalized := raw
		if a.Normalizers != nil {
			if normalizer, ok := a.Normalizers[key]; ok && normalizer != nil {
				normalized = normalizer(raw)
			}
		}

		fitness += weight * normalized
	}

	return fitness
}