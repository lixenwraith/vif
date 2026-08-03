package genetic

import "math/rand/v2"

// ParameterBounds defines min/max for a single parameter
type ParameterBounds struct {
	Min, Max float64
}

// BoundedPerturbator applies perturbation with range clamping
type BoundedPerturbator struct {
	Bounds            []ParameterBounds
	StandardDeviation float64
}

func (bp *BoundedPerturbator) Perturb(solution *[]float64, rate float64, rng *rand.Rand) {
	if solution == nil || len(*solution) == 0 {
		return
	}

	for i := range *solution {
		if i >= len(bp.Bounds) {
			break
		}
		if rng.Float64() >= rate {
			continue
		}

		bounds := bp.Bounds[i]
		rangeSize := bounds.Max - bounds.Min
		noise := rng.NormFloat64() * bp.StandardDeviation * rangeSize

		(*solution)[i] += noise

		if (*solution)[i] < bounds.Min {
			(*solution)[i] = bounds.Min
		} else if (*solution)[i] > bounds.Max {
			(*solution)[i] = bounds.Max
		}
	}
}

// Clamp enforces bounds without mutation
func (bp *BoundedPerturbator) Clamp(solution []float64) []float64 {
	result := make([]float64, len(solution))
	for i, v := range solution {
		if i >= len(bp.Bounds) {
			result[i] = v
			continue
		}
		bounds := bp.Bounds[i]
		if v < bounds.Min {
			result[i] = bounds.Min
		} else if v > bounds.Max {
			result[i] = bounds.Max
		} else {
			result[i] = v
		}
	}
	return result
}