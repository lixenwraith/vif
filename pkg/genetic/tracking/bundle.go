// Package tracking accumulates one subject's metrics over its lifetime: a
// MetricBundle a tick, folded by a StandardCollector into averages, extremes
// and active time, by a CompositeCollector into member counts as well, and
// handed out of a CollectorPool that reuses them.
package tracking

import "time"

// MetricBundle is a generic container for named metrics
// Keys are metric names, values are float64 measurements
type MetricBundle map[string]float64

// The keys the collectors themselves read or write; a host names its own
const (
	MetricTicksAlive       = "ticks_alive"
	MetricMemberCount      = "member_count"
	MetricPeakMemberCount  = "peak_member_count"
	MetricFinalMemberCount = "final_member_count"
	MetricMemberRetention  = "member_retention"
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
