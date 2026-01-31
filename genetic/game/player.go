package game

import (
	"sync"
	"time"
)

type PlayerBehaviorModel struct {
	mu sync.RWMutex

	AvgReactionTime   time.Duration
	ShieldUsageRate   float64
	ShieldActivations int
	MovementFrequency float64
	AvgMoveDistance   float64
	TypingAccuracy    float64
	KillRate          float64

	sampleCount int
	emaAlpha    float64
}

func NewPlayerBehaviorModel() *PlayerBehaviorModel {
	return &PlayerBehaviorModel{
		AvgReactionTime: 500 * time.Millisecond,
		ShieldUsageRate: 0.3,
		TypingAccuracy:  0.8,
		emaAlpha:        0.1,
	}
}

func (m *PlayerBehaviorModel) RecordReaction(reactionTime time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.sampleCount++
	m.AvgReactionTime = time.Duration(
		m.emaAlpha*float64(reactionTime) +
			(1-m.emaAlpha)*float64(m.AvgReactionTime),
	)
}

func (m *PlayerBehaviorModel) RecordShieldState(active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var v float64
	if active {
		v = 1.0
	}
	m.ShieldUsageRate = m.emaAlpha*v + (1-m.emaAlpha)*m.ShieldUsageRate
}

func (m *PlayerBehaviorModel) RecordKeystroke(correct bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var v float64
	if correct {
		v = 1.0
	}
	m.TypingAccuracy = m.emaAlpha*v + (1-m.emaAlpha)*m.TypingAccuracy
}

func (m *PlayerBehaviorModel) RecordMovement(distance float64) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.AvgMoveDistance = m.emaAlpha*distance + (1-m.emaAlpha)*m.AvgMoveDistance
}

type PlayerBehaviorSnapshot struct {
	ReactionTime   time.Duration
	ShieldUsage    float64
	TypingAccuracy float64
	MoveDistance   float64
	SampleCount    int
}

func (m *PlayerBehaviorModel) Snapshot() PlayerBehaviorSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()

	return PlayerBehaviorSnapshot{
		ReactionTime:   m.AvgReactionTime,
		ShieldUsage:    m.ShieldUsageRate,
		TypingAccuracy: m.TypingAccuracy,
		MoveDistance:   m.AvgMoveDistance,
		SampleCount:    m.sampleCount,
	}
}

func (s PlayerBehaviorSnapshot) ThreatLevel() float64 {
	reactionScore := 1.0 - float64(s.ReactionTime)/(2*float64(time.Second))
	if reactionScore < 0 {
		reactionScore = 0
	}

	return 0.4*reactionScore + 0.3*s.TypingAccuracy + 0.3*s.ShieldUsage
}