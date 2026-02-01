package tracking

import "time"

// MetricBundle is a generic container for named metrics
// Keys are metric names, values are float64 measurements
type MetricBundle map[string]float64

// Standard metric keys (conventions)
const (
	MetricTicksAlive      = "ticks_alive"
	MetricDistanceSquared = "distance_sq"
	MetricTimeInZone      = "time_in_zone"
	MetricDeathAtTarget   = "death_at_target"
	MetricMemberCount     = "member_count"
	MetricPeakMemberCount = "peak_member_count"
)

// Get returns metric value or default if not present
func (b MetricBundle) Get(key string, defaultVal float64) float64 {
	if v, ok := b[key]; ok {
		return v
	}
	return defaultVal
}

// Merge combines two bundles, other values override existing
func (b MetricBundle) Merge(other MetricBundle) MetricBundle {
	result := make(MetricBundle, len(b)+len(other))
	for k, v := range b {
		result[k] = v
	}
	for k, v := range other {
		result[k] = v
	}
	return result
}

// Clone creates a deep copy
func (b MetricBundle) Clone() MetricBundle {
	result := make(MetricBundle, len(b))
	for k, v := range b {
		result[k] = v
	}
	return result
}

// Collector accumulates metrics over an entity's lifetime
type Collector interface {
	// Collect records metrics for a single tick
	Collect(metrics MetricBundle, dt time.Duration)

	// Finalize returns accumulated metrics and prepares for reuse
	// deathCondition contains death-specific metrics
	Finalize(deathCondition MetricBundle) MetricBundle

	// Reset clears accumulated state for reuse
	Reset()
}