package tracking

import (
	"maps"
	"time"
)

// derivedKeys caches the prefixed output names for one metric key
type derivedKeys struct {
	avg, min, max, dur string
}

// StandardCollector accumulates per-tick metrics for a single entity
type StandardCollector struct {
	totalTicks int
	sums       map[string]float64
	counts     map[string]int
	durations  map[string]time.Duration
	mins       map[string]float64
	maxs       map[string]float64
	seen       map[string]bool
	derived    map[string]derivedKeys
}

func NewStandardCollector() *StandardCollector {
	return &StandardCollector{
		sums:      make(map[string]float64, 8),
		counts:    make(map[string]int, 8),
		durations: make(map[string]time.Duration, 4),
		mins:      make(map[string]float64, 8),
		maxs:      make(map[string]float64, 8),
		seen:      make(map[string]bool, 8),
		derived:   make(map[string]derivedKeys, 8),
	}
}

// Tick advances the tick counter without recording a metric
func (c *StandardCollector) Tick() { c.totalTicks++ }

// Add records one metric sample; zero allocation after the first sighting of key
func (c *StandardCollector) Add(key string, value float64) {
	c.sums[key] += value
	c.counts[key]++

	if !c.seen[key] {
		c.seen[key] = true
		c.mins[key], c.maxs[key] = value, value
		if _, cached := c.derived[key]; !cached {
			c.derived[key] = derivedKeys{"avg_" + key, "min_" + key, "max_" + key, "time_" + key}
		}
		return
	}
	if value < c.mins[key] {
		c.mins[key] = value
	}
	if value > c.maxs[key] {
		c.maxs[key] = value
	}
}

// Flag accumulates active time for a boolean metric
func (c *StandardCollector) Flag(key string, active bool, dt time.Duration) {
	if active {
		c.durations[key] += dt
	}
}

// Collect records a bundle for one tick. Values above 0.5 also accumulate
// duration under the boolean-metric convention
func (c *StandardCollector) Collect(metrics MetricBundle, dt time.Duration) {
	c.totalTicks++
	for key, value := range metrics {
		c.Add(key, value)
		c.Flag(key, value > 0.5, dt)
	}
}

func (c *StandardCollector) Finalize(deathCondition MetricBundle) MetricBundle {
	result := make(MetricBundle, len(c.sums)*3+len(deathCondition)+1)
	result[MetricTicksAlive] = float64(c.totalTicks)

	for key := range c.seen {
		d := c.derived[key]
		if n := c.counts[key]; n > 0 {
			result[d.avg] = c.sums[key] / float64(n)
		}
		result[d.min] = c.mins[key]
		result[d.max] = c.maxs[key]
	}
	for key, dur := range c.durations {
		if d, ok := c.derived[key]; ok {
			result[d.dur] = dur.Seconds()
		}
	}
	maps.Copy(result, deathCondition)
	return result
}

// Reset clears accumulated state; the derived-name cache is retained for reuse
func (c *StandardCollector) Reset() {
	c.totalTicks = 0
	clear(c.sums)
	clear(c.counts)
	clear(c.durations)
	clear(c.mins)
	clear(c.maxs)
	clear(c.seen)
}
