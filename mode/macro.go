package mode

import (
	"time"

	"github.com/lixenwraith/vi-fighter/input"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// MacroRecorder handles macro recording and playback for a single register
// Designed for future multi-register expansion
type MacroRecorder struct {
	// Recording
	recording bool
	buffer    []input.Intent

	// Playback
	playing       bool
	playbackQueue []input.Intent
	playbackIndex int
	repeatTarget  int
	repeatCurrent int
	lastPlayTime  time.Time
}

// NewMacroRecorder creates an empty recorder
func NewMacroRecorder() *MacroRecorder {
	return &MacroRecorder{
		buffer:        make([]input.Intent, 0, 32),
		playbackQueue: make([]input.Intent, 0, 32),
	}
}

func (m *MacroRecorder) IsRecording() bool { return m.recording }
func (m *MacroRecorder) IsPlaying() bool   { return m.playing }

// StartRecording begins capturing intents, clears previous buffer
func (m *MacroRecorder) StartRecording() {
	m.recording = true
	m.buffer = m.buffer[:0]
}

// StopRecording ends capture
func (m *MacroRecorder) StopRecording() {
	m.recording = false
}

// Record appends intent to buffer (caller filters meta-intents)
func (m *MacroRecorder) Record(intent input.Intent) {
	if !m.recording {
		return
	}
	m.buffer = append(m.buffer, intent)
}

// StartPlayback begins playback with count repetitions
// Returns false if buffer empty (silent no-op)
func (m *MacroRecorder) StartPlayback(count int, now time.Time) bool {
	if len(m.buffer) == 0 {
		return false
	}
	m.playing = true
	m.playbackQueue = append(m.playbackQueue[:0], m.buffer...)
	m.playbackIndex = 0
	m.repeatTarget = count
	m.repeatCurrent = 0
	m.lastPlayTime = now.Add(-parameter.MacroPlaybackInterval) // Fire first immediately
	return true
}

// StopPlayback halts playback
func (m *MacroRecorder) StopPlayback() {
	m.playing = false
	m.playbackIndex = 0
	m.repeatCurrent = 0
}

// Tick returns next intent if interval elapsed, nil otherwise
func (m *MacroRecorder) Tick(now time.Time) *input.Intent {
	if !m.playing || len(m.playbackQueue) == 0 {
		return nil
	}

	if now.Sub(m.lastPlayTime) < parameter.MacroPlaybackInterval {
		return nil
	}
	m.lastPlayTime = now

	// Check for repeat boundary
	if m.playbackIndex >= len(m.playbackQueue) {
		m.repeatCurrent++
		if m.repeatCurrent >= m.repeatTarget {
			m.StopPlayback()
			return nil
		}
		m.playbackIndex = 0
	}

	intent := m.playbackQueue[m.playbackIndex]
	m.playbackIndex++
	return &intent
}

// Reset clears all state (called on game reset)
func (m *MacroRecorder) Reset() {
	m.recording = false
	m.playing = false
	m.buffer = m.buffer[:0]
	m.playbackQueue = m.playbackQueue[:0]
	m.playbackIndex = 0
	m.repeatTarget = 0
	m.repeatCurrent = 0
}