package game

import (
	"math"
	"sync"
	"time"
)

// PlayerBehaviorModel tracks observable player actions for fitness context
type PlayerBehaviorModel struct {
	mu sync.RWMutex

	AvgReactionTime   time.Duration
	EnergyManagement  float64
	HeatManagement    float64
	TypingAccuracy    float64
	MovementFrequency float64
	AvgMoveDistance   float64

	sampleCount int
	emaAlpha    float64
}

// NewPlayerBehaviorModel creates a new player model with defaults
func NewPlayerBehaviorModel() *PlayerBehaviorModel {
	return &PlayerBehaviorModel{
		AvgReactionTime:  500 * time.Millisecond,
		EnergyManagement: 0.5,
		HeatManagement:   0.3,
		TypingAccuracy:   0.8,
		emaAlpha:         0.1,
	}
}

// Reset clears accumulated state for new game session
func (m *PlayerBehaviorModel) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.AvgReactionTime = 500 * time.Millisecond
	m.EnergyManagement = 0.5
	m.HeatManagement = 0.3
	m.TypingAccuracy = 0.8
	m.MovementFrequency = 0
	m.AvgMoveDistance = 0
	m.sampleCount = 0
}

// RecordReaction tracks reaction time to threats
func (m *PlayerBehaviorModel) RecordReaction(reactionTime time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sampleCount++
	m.AvgReactionTime = time.Duration(
		m.emaAlpha*float64(reactionTime) +
			(1-m.emaAlpha)*float64(m.AvgReactionTime),
	)
}

// RecordEnergyLevel tracks energy magnitude (handles polarity)
// High magnitude (positive or negative) indicates active resource management
func (m *PlayerBehaviorModel) RecordEnergyLevel(current int64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Track absolute magnitude normalized against reference range
	const referenceRange = 10000.0
	magnitude := math.Abs(float64(current))
	normalized := math.Min(magnitude/referenceRange, 1.0)

	m.EnergyManagement = m.emaAlpha*normalized + (1-m.emaAlpha)*m.EnergyManagement
}

// RecordHeatLevel tracks normalized heat level
func (m *PlayerBehaviorModel) RecordHeatLevel(current, max int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if max <= 0 {
		return
	}

	normalized := math.Max(0, math.Min(float64(current)/float64(max), 1.0))
	m.HeatManagement = m.emaAlpha*normalized + (1-m.emaAlpha)*m.HeatManagement
}

// RecordKeystroke tracks typing accuracy
func (m *PlayerBehaviorModel) RecordKeystroke(correct bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var v float64
	if correct {
		v = 1.0
	}
	m.TypingAccuracy = m.emaAlpha*v + (1-m.emaAlpha)*m.TypingAccuracy
}

// RecordMovement tracks cursor movement distance
func (m *PlayerBehaviorModel) RecordMovement(distance float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.AvgMoveDistance = m.emaAlpha*distance + (1-m.emaAlpha)*m.AvgMoveDistance
}

// PlayerBehaviorSnapshot is an immutable snapshot for fitness context
type PlayerBehaviorSnapshot struct {
	ReactionTime     time.Duration
	EnergyManagement float64
	HeatManagement   float64
	TypingAccuracy   float64
	MoveDistance     float64
	SampleCount      int
}

// Snapshot returns current state for fitness calculation
func (m *PlayerBehaviorModel) Snapshot() PlayerBehaviorSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return PlayerBehaviorSnapshot{
		ReactionTime:     m.AvgReactionTime,
		EnergyManagement: m.EnergyManagement,
		HeatManagement:   m.HeatManagement,
		TypingAccuracy:   m.TypingAccuracy,
		MoveDistance:     m.AvgMoveDistance,
		SampleCount:      m.sampleCount,
	}
}

// ThreatLevel estimates player skill (0-1, higher = more skilled)
func (s PlayerBehaviorSnapshot) ThreatLevel() float64 {
	reactionScore := 1.0 - float64(s.ReactionTime)/(2*float64(time.Second))
	if reactionScore < 0 {
		reactionScore = 0
	}

	return 0.3*reactionScore +
		0.3*s.TypingAccuracy +
		0.2*s.EnergyManagement +
		0.2*s.HeatManagement
}