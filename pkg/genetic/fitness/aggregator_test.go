package fitness

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/pkg/genetic/tracking"
)

const eps = 1e-12

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if d := got - want; d > eps || d < -eps {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestWeightedAggregator_Calculate(t *testing.T) {
	agg := &WeightedAggregator{
		Weights: map[string]float64{
			"survival": 0.5,
			"energy":   0.5,
		},
	}

	metrics := tracking.MetricBundle{
		"survival": 0.8,
		"energy":   0.6,
	}

	approx(t, agg.Calculate(metrics, nil), 0.5*0.8+0.5*0.6)
}

func TestWeightedAggregator_WithNormalizers(t *testing.T) {
	agg := &WeightedAggregator{
		Weights: map[string]float64{
			tracking.MetricTicksAlive: 1.0,
		},
		Normalizers: map[string]NormalizeFunc{
			tracking.MetricTicksAlive: NormalizeCap(100),
		},
	}

	// 50 ticks with cap at 100 = 0.5 normalized
	metrics := tracking.MetricBundle{tracking.MetricTicksAlive: 50}
	approx(t, agg.Calculate(metrics, nil), 0.5)

	// 150 ticks capped at 1.0
	metrics[tracking.MetricTicksAlive] = 150
	approx(t, agg.Calculate(metrics, nil), 1.0)
}

func TestWeightedAggregator_ContextAdjuster(t *testing.T) {
	agg := &WeightedAggregator{
		Weights: map[string]float64{"attack": 0.5, "defense": 0.5},
		WeightAdjuster: func(key string, w float64, ctx Context) float64 {
			if threat, ok := ctx.Get(ContextThreatLevel); ok && threat > 0.7 && key == "defense" {
				return w * 2.0
			}
			return w
		},
	}

	metrics := tracking.MetricBundle{"attack": 1.0, "defense": 1.0}

	// Low threat: normal weights
	approx(t, agg.Calculate(metrics, MapContext{ContextThreatLevel: 0.3}), 1.0)

	// High threat: defense doubled (0.5*1 + 1.0*1)
	approx(t, agg.Calculate(metrics, MapContext{ContextThreatLevel: 0.8}), 1.5)
}

func TestNormalizeInverse(t *testing.T) {
	norm := NormalizeInverse(100.0)

	approx(t, norm(0), 1.0)   // 1/(1+0)
	approx(t, norm(100), 0.5) // 1/(1+1)
	approx(t, norm(300), 0.25)
}

func TestNormalizeLinear(t *testing.T) {
	norm := NormalizeLinear(10, 20)

	approx(t, norm(10), 0.0)
	approx(t, norm(20), 1.0)
	approx(t, norm(15), 0.5)
	approx(t, norm(5), 0.0)  // below min
	approx(t, norm(25), 1.0) // above max
}
