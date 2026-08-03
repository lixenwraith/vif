package tracking

import "time"

// StandardCollector implements Collector for single entities
type StandardCollector struct {
	totalTicks int
	sums       map[string]float64
	counts     map[string]int
	durations  map[string]time.Duration
	mins       map[string]float64
	maxs       map[string]float64
	minSet     map[string]bool
}

// NewStandardCollector creates a reusable collector
func NewStandardCollector() *StandardCollector {
	return &StandardCollector{
		sums:      make(map[string]float64),
		counts:    make(map[string]int),
		durations: make(map[string]time.Duration),
		mins:      make(map[string]float64),
		maxs:      make(map[string]float64),
		minSet:    make(map[string]bool),
	}
}

func (c *StandardCollector) Collect(metrics MetricBundle, dt time.Duration) {
	c.totalTicks++

	for key, value := range metrics {
		c.sums[key] += value
		c.counts[key]++

		if !c.minSet[key] || value < c.mins[key] {
			c.mins[key] = value
			c.minSet[key] = true
		}
		if value > c.maxs[key] {
			c.maxs[key] = value
		}

		// Boolean metrics: accumulate time when > 0.5
		if value > 0.5 {
			c.durations[key] += dt
		}
	}
}

func (c *StandardCollector) Finalize(deathCondition MetricBundle) MetricBundle {
	result := make(MetricBundle)

	result[MetricTicksAlive] = float64(c.totalTicks)

	for key, sum := range c.sums {
		if count := c.counts[key]; count > 0 {
			result["avg_"+key] = sum / float64(count)
		}
	}

	for key, dur := range c.durations {
		result["time_"+key] = dur.Seconds()
	}

	for key, val := range c.mins {
		result["min_"+key] = val
	}
	for key, val := range c.maxs {
		result["max_"+key] = val
	}

	for key, val := range deathCondition {
		result[key] = val
	}

	return result
}

func (c *StandardCollector) Reset() {
	c.totalTicks = 0
	clear(c.sums)
	clear(c.counts)
	clear(c.durations)
	clear(c.mins)
	clear(c.maxs)
	clear(c.minSet)
}