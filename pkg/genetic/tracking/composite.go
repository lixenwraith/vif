package tracking

import "time"

// CompositeCollector extends StandardCollector with member tracking
type CompositeCollector struct {
	StandardCollector
	peakMembers    int
	currentMembers int
}

// NewCompositeCollector creates a collector for composite entities
func NewCompositeCollector() *CompositeCollector {
	return &CompositeCollector{
		StandardCollector: *NewStandardCollector(),
	}
}

func (c *CompositeCollector) Collect(metrics MetricBundle, dt time.Duration) {
	c.StandardCollector.Collect(metrics, dt)

	if memberCount, ok := metrics[MetricMemberCount]; ok {
		c.currentMembers = int(memberCount)
		if c.currentMembers > c.peakMembers {
			c.peakMembers = c.currentMembers
		}
	}
}

func (c *CompositeCollector) Finalize(deathCondition MetricBundle) MetricBundle {
	result := c.StandardCollector.Finalize(deathCondition)

	result[MetricPeakMemberCount] = float64(c.peakMembers)
	result["final_member_count"] = float64(c.currentMembers)

	if c.peakMembers > 0 {
		result["member_retention"] = float64(c.currentMembers) / float64(c.peakMembers)
	}

	return result
}

func (c *CompositeCollector) Reset() {
	c.StandardCollector.Reset()
	c.peakMembers = 0
	c.currentMembers = 0
}