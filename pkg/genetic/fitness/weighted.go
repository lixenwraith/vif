package fitness

import "github.com/lixenwraith/vi-fighter/pkg/genetic/tracking"

// WeightedAggregator calculates fitness as a weighted sum of normalized metrics
type WeightedAggregator struct {
	Weights     map[string]float64
	Normalizers map[string]NormalizeFunc

	// WeightAdjuster rescales one weight from context; nil leaves weights unchanged
	WeightAdjuster func(key string, weight float64, ctx Context) float64
}

func (a *WeightedAggregator) Calculate(metrics tracking.MetricBundle, ctx Context) float64 {
	var fitness float64

	for key, weight := range a.Weights {
		raw, ok := metrics[key]
		if !ok {
			continue
		}
		if a.WeightAdjuster != nil && ctx != nil {
			weight = a.WeightAdjuster(key, weight, ctx)
		}
		if a.Normalizers != nil {
			if n := a.Normalizers[key]; n != nil {
				raw = n(raw)
			}
		}
		fitness += weight * raw
	}
	return fitness
}
