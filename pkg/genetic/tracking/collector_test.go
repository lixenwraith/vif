package tracking

import (
	"testing"
	"time"
)

func TestStandardCollector_Accumulation(t *testing.T) {
	c := NewStandardCollector()

	c.Collect(MetricBundle{"distance": 10.0, "in_zone": 1.0}, 50*time.Millisecond)
	c.Collect(MetricBundle{"distance": 20.0, "in_zone": 0.0}, 50*time.Millisecond)
	c.Collect(MetricBundle{"distance": 30.0, "in_zone": 1.0}, 50*time.Millisecond)

	result := c.Finalize(MetricBundle{MetricDeathAtTarget: 1.0})

	if result[MetricTicksAlive] != 3 {
		t.Errorf("expected 3 ticks, got %v", result[MetricTicksAlive])
	}

	if result["avg_distance"] != 20.0 {
		t.Errorf("expected avg_distance 20.0, got %v", result["avg_distance"])
	}

	// in_zone was true for 2 ticks at 50ms each = 100ms = 0.1s
	if result["time_in_zone"] != 0.1 {
		t.Errorf("expected time_in_zone 0.1, got %v", result["time_in_zone"])
	}

	if result[MetricDeathAtTarget] != 1.0 {
		t.Errorf("expected death_at_target 1.0, got %v", result[MetricDeathAtTarget])
	}
}

func TestStandardCollector_MinMax(t *testing.T) {
	c := NewStandardCollector()

	c.Collect(MetricBundle{"value": 5.0}, time.Millisecond)
	c.Collect(MetricBundle{"value": 2.0}, time.Millisecond)
	c.Collect(MetricBundle{"value": 8.0}, time.Millisecond)

	result := c.Finalize(nil)

	if result["min_value"] != 2.0 {
		t.Errorf("expected min 2.0, got %v", result["min_value"])
	}
	if result["max_value"] != 8.0 {
		t.Errorf("expected max 8.0, got %v", result["max_value"])
	}
}

func TestStandardCollector_Reset(t *testing.T) {
	c := NewStandardCollector()

	c.Collect(MetricBundle{"x": 10.0}, time.Second)
	c.Reset()
	c.Collect(MetricBundle{"x": 5.0}, time.Second)

	result := c.Finalize(nil)

	if result[MetricTicksAlive] != 1 {
		t.Errorf("expected 1 tick after reset, got %v", result[MetricTicksAlive])
	}
	if result["avg_x"] != 5.0 {
		t.Errorf("expected avg_x 5.0 after reset, got %v", result["avg_x"])
	}
}

func TestCompositeCollector_MemberTracking(t *testing.T) {
	c := NewCompositeCollector()

	c.Collect(MetricBundle{MetricMemberCount: 4.0}, time.Second)
	c.Collect(MetricBundle{MetricMemberCount: 6.0}, time.Second)
	c.Collect(MetricBundle{MetricMemberCount: 3.0}, time.Second)

	result := c.Finalize(nil)

	if result[MetricPeakMemberCount] != 6.0 {
		t.Errorf("expected peak 6, got %v", result[MetricPeakMemberCount])
	}
	if result["final_member_count"] != 3.0 {
		t.Errorf("expected final 3, got %v", result["final_member_count"])
	}
	if result["member_retention"] != 0.5 {
		t.Errorf("expected retention 0.5, got %v", result["member_retention"])
	}
}

func TestCollectorPool_Reuse(t *testing.T) {
	pool := NewCollectorPool(2)

	c1 := pool.AcquireStandard()
	c1.Collect(MetricBundle{"x": 1.0}, time.Second)

	pool.ReleaseStandard(c1)

	c2 := pool.AcquireStandard()

	// Should be same instance, reset
	if c2 != c1 {
		t.Error("expected pooled collector to be reused")
	}

	result := c2.Finalize(nil)
	if result[MetricTicksAlive] != 0 {
		t.Error("expected collector to be reset on acquire")
	}
}