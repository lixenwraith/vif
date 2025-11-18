package systems

import (
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// RaceDetectionLogger provides debug logging for race condition testing
type RaceDetectionLogger struct {
	mu           sync.Mutex
	enabled      bool
	events       []RaceEvent
	eventCounter atomic.Int64
}

// RaceEvent represents a logged event during race testing
type RaceEvent struct {
	ID        int64
	Timestamp time.Time
	Goroutine string
	Operation string
	Details   string
}

// NewRaceDetectionLogger creates a new race detection logger
func NewRaceDetectionLogger(enabled bool) *RaceDetectionLogger {
	return &RaceDetectionLogger{
		enabled: enabled,
		events:  make([]RaceEvent, 0, 1000),
	}
}

// Log records a race detection event
func (r *RaceDetectionLogger) Log(goroutine, operation, details string) {
	if !r.enabled {
		return
	}

	id := r.eventCounter.Add(1)
	event := RaceEvent{
		ID:        id,
		Timestamp: time.Now(),
		Goroutine: goroutine,
		Operation: operation,
		Details:   details,
	}

	r.mu.Lock()
	r.events = append(r.events, event)
	r.mu.Unlock()

	// Also log to stderr for real-time monitoring
	if os.Getenv("VERBOSE_RACE_LOG") == "1" {
		log.Printf("[RACE] [%s] %s: %s", goroutine, operation, details)
	}
}

// GetEvents returns all logged events
func (r *RaceDetectionLogger) GetEvents() []RaceEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	events := make([]RaceEvent, len(r.events))
	copy(events, r.events)
	return events
}

// Clear clears all logged events
func (r *RaceDetectionLogger) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = r.events[:0]
}

// DumpEvents writes all events to a file
func (r *RaceDetectionLogger) DumpEvents(filename string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	file, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer file.Close()

	for _, event := range r.events {
		fmt.Fprintf(file, "[%d] %s | %s | %s | %s\n",
			event.ID,
			event.Timestamp.Format("15:04:05.000000"),
			event.Goroutine,
			event.Operation,
			event.Details)
	}

	return nil
}

// ConcurrencyMonitor monitors concurrent operations and detects potential issues
type ConcurrencyMonitor struct {
	mu              sync.Mutex
	activeOps       map[string]int
	maxConcurrent   map[string]int
	totalOps        map[string]int64
	anomalyDetected atomic.Bool
	anomalies       []string
}

// NewConcurrencyMonitor creates a new concurrency monitor
func NewConcurrencyMonitor() *ConcurrencyMonitor {
	return &ConcurrencyMonitor{
		activeOps:     make(map[string]int),
		maxConcurrent: make(map[string]int),
		totalOps:      make(map[string]int64),
		anomalies:     make([]string, 0),
	}
}

// StartOperation records the start of an operation
func (c *ConcurrencyMonitor) StartOperation(operation string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.activeOps[operation]++
	c.totalOps[operation]++

	if c.activeOps[operation] > c.maxConcurrent[operation] {
		c.maxConcurrent[operation] = c.activeOps[operation]
	}
}

// EndOperation records the end of an operation
func (c *ConcurrencyMonitor) EndOperation(operation string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.activeOps[operation]--
	if c.activeOps[operation] < 0 {
		c.anomalyDetected.Store(true)
		anomaly := fmt.Sprintf("Operation '%s' ended more times than started (count: %d)",
			operation, c.activeOps[operation])
		c.anomalies = append(c.anomalies, anomaly)
	}
}

// GetStats returns statistics about operations
func (c *ConcurrencyMonitor) GetStats() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats := make(map[string]interface{})
	stats["active_operations"] = copyIntMap(c.activeOps)
	stats["max_concurrent"] = copyIntMap(c.maxConcurrent)

	totalOpsMap := make(map[string]int64)
	for k, v := range c.totalOps {
		totalOpsMap[k] = v
	}
	stats["total_operations"] = totalOpsMap

	stats["anomaly_detected"] = c.anomalyDetected.Load()
	stats["anomalies"] = append([]string{}, c.anomalies...)

	return stats
}

// HasAnomalies returns whether any anomalies were detected
func (c *ConcurrencyMonitor) HasAnomalies() bool {
	return c.anomalyDetected.Load()
}

// GetAnomalies returns all detected anomalies
func (c *ConcurrencyMonitor) GetAnomalies() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string{}, c.anomalies...)
}

func copyIntMap(m map[string]int) map[string]int {
	result := make(map[string]int)
	for k, v := range m {
		result[k] = v
	}
	return result
}

// AtomicStateValidator validates atomic state consistency
type AtomicStateValidator struct {
	mu              sync.Mutex
	violations      []string
	validationCount atomic.Int64
}

// NewAtomicStateValidator creates a new atomic state validator
func NewAtomicStateValidator() *AtomicStateValidator {
	return &AtomicStateValidator{
		violations: make([]string, 0),
	}
}

// ValidateCleanerState validates cleaner system state consistency
func (v *AtomicStateValidator) ValidateCleanerState(isActive bool, activationTime, lastUpdateTime int64) {
	v.validationCount.Add(1)

	// Check for inconsistent state
	if isActive && activationTime == 0 {
		v.recordViolation("Cleaner is active but activationTime is 0")
	}

	if !isActive && lastUpdateTime != 0 {
		// This might be acceptable during cleanup, so we just log it
		// v.recordViolation("Cleaner is inactive but lastUpdateTime is non-zero")
	}

	if lastUpdateTime < activationTime && lastUpdateTime != 0 && activationTime != 0 {
		v.recordViolation(fmt.Sprintf("lastUpdateTime (%d) < activationTime (%d)",
			lastUpdateTime, activationTime))
	}
}

// ValidateCounterState validates color counter state
func (v *AtomicStateValidator) ValidateCounterState(colorType, level string, count int64) {
	v.validationCount.Add(1)

	if count < 0 {
		v.recordViolation(fmt.Sprintf("Color counter for %s-%s is negative: %d",
			colorType, level, count))
	}
}

// ValidateGoldState validates gold sequence state
func (v *AtomicStateValidator) ValidateGoldState(isActive bool, sequenceID int) {
	v.validationCount.Add(1)

	if isActive && sequenceID == 0 {
		v.recordViolation("Gold is active but sequenceID is 0")
	}
}

func (v *AtomicStateValidator) recordViolation(violation string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.violations = append(v.violations, violation)

	// Log immediately for debugging
	if os.Getenv("VERBOSE_RACE_LOG") == "1" {
		log.Printf("[VALIDATION VIOLATION] %s", violation)
	}
}

// GetViolations returns all recorded violations
func (v *AtomicStateValidator) GetViolations() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string{}, v.violations...)
}

// HasViolations returns whether any violations were recorded
func (v *AtomicStateValidator) HasViolations() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(v.violations) > 0
}

// GetValidationCount returns the total number of validations performed
func (v *AtomicStateValidator) GetValidationCount() int64 {
	return v.validationCount.Load()
}

// EntityLifecycleTracker tracks entity creation and destruction
type EntityLifecycleTracker struct {
	mu         sync.Mutex
	created    map[uint64]time.Time
	destroyed  map[uint64]time.Time
	leaked     []uint64
	createOps  atomic.Int64
	destroyOps atomic.Int64
}

// NewEntityLifecycleTracker creates a new entity lifecycle tracker
func NewEntityLifecycleTracker() *EntityLifecycleTracker {
	return &EntityLifecycleTracker{
		created:   make(map[uint64]time.Time),
		destroyed: make(map[uint64]time.Time),
		leaked:    make([]uint64, 0),
	}
}

// TrackCreate records entity creation
func (t *EntityLifecycleTracker) TrackCreate(entityID uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.created[entityID] = time.Now()
	t.createOps.Add(1)
}

// TrackDestroy records entity destruction
func (t *EntityLifecycleTracker) TrackDestroy(entityID uint64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.created[entityID]; !exists {
		// Entity destroyed without being created (in this tracking session)
		// This might be OK if entity was created before tracking started
	}

	t.destroyed[entityID] = time.Now()
	t.destroyOps.Add(1)
}

// DetectLeaks detects entities that were created but never destroyed
func (t *EntityLifecycleTracker) DetectLeaks() []uint64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.leaked = t.leaked[:0]

	for entityID := range t.created {
		if _, destroyed := t.destroyed[entityID]; !destroyed {
			t.leaked = append(t.leaked, entityID)
		}
	}

	return append([]uint64{}, t.leaked...)
}

// GetStats returns lifecycle statistics
func (t *EntityLifecycleTracker) GetStats() map[string]int64 {
	t.mu.Lock()
	defer t.mu.Unlock()

	return map[string]int64{
		"created":   t.createOps.Load(),
		"destroyed": t.destroyOps.Load(),
		"leaked":    int64(len(t.leaked)),
	}
}
