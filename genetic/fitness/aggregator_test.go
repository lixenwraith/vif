package fitness

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/genetic/tracking"
)

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

	fitness := agg.Calculate(metrics, nil)
	expected := 0.5*0.8 + 0.5*0.6 // 0.7

	if fitness != expected {
		t.Errorf("expected %v, got %v", expected, fitness)
	}
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
	metrics := tracking.MetricBundle{
		tracking.MetricTicksAlive: 50,
	}

	fitness := agg.Calculate(metrics, nil)
	if fitness != 0.5 {
		t.Errorf("expected 0.5, got %v", fitness)
	}

	// 150 ticks capped at 1.0
	metrics[tracking.MetricTicksAlive] = 150
	fitness = agg.Calculate(metrics, nil)
	if fitness != 1.0 {
		t.Errorf("expected 1.0 (capped), got %v", fitness)
	}
}

func TestWeightedAggregator_ContextAdjuster(t *testing.T) {
	agg := &WeightedAggregator{
		Weights: map[string]float64{
			"attack":  0.5,
			"defense": 0.5,
		},
		ContextAdjuster: func(w map[string]float64, ctx Context) map[string]float64 {
			threat, ok := ctx.Get(ContextThreatLevel)
			if !ok {
				return w
			}
			adjusted := make(map[string]float64)
			for k, v := range w {
				adjusted[k] = v
			}
			if threat > 0.7 {
				adjusted["defense"] *= 2.0 // Double defense weight for skilled players
			}
			return adjusted
		},
	}

	metrics := tracking.MetricBundle{
		"attack":  1.0,
		"defense": 1.0,
	}

	// Low threat: normal weights
	ctx := MapContext{ContextThreatLevel: 0.3}
	fitness := agg.Calculate(metrics, ctx)
	if fitness != 1.0 {
		t.Errorf("expected 1.0 for low threat, got %v", fitness)
	}

	// High threat: defense doubled (0.5*1 + 1.0*1 = 1.5)
	ctx = MapContext{ContextThreatLevel: 0.8}
	fitness = agg.Calculate(metrics, ctx)
	if fitness != 1.5 {
		t.Errorf("expected 1.5 for high threat, got %v", fitness)
	}
}

func TestNormalizeInverse(t *testing.T) {
	norm := NormalizeInverse(100.0)

	// At 0: 1/(1+0) = 1.0
	if v := norm(0); v != 1.0 {
		t.Errorf("expected 1.0 at 0, got %v", v)
	}

	// At 100: 1/(1+1) = 0.5
	if v := norm(100); v != 0.5 {
		t.Errorf("expected 0.5 at scale, got %v", v)
	}

	// At 300: 1/(1+3) = 0.25
	if v := norm(300); v != 0.25 {
		t.Errorf("expected 0.25 at 3x scale, got %v", v)
	}
}

func TestNormalizeLinear(t *testing.T) {
	norm := NormalizeLinear(10, 20)

	if v := norm(10); v != 0.0 {
		t.Errorf("expected 0.0 at min, got %v", v)
	}
	if v := norm(20); v != 1.0 {
		t.Errorf("expected 1.0 at max, got %v", v)
	}
	if v := norm(15); v != 0.5 {
		t.Errorf("expected 0.5 at midpoint, got %v", v)
	}
	if v := norm(5); v != 0.0 {
		t.Errorf("expected 0.0 below min, got %v", v)
	}
	if v := norm(25); v != 1.0 {
		t.Errorf("expected 1.0 above max, got %v", v)
	}
}