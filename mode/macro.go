package mode

import (
	"time"

	"github.com/lixenwraith/vi-fighter/input"
	"github.com/lixenwraith/vi-fighter/parameter"
)

// `qa` ... `q` : Record to 'a'
// `qa` `q` : Clear macro 'a' (empty recording)
// `@a` : Play 'a' once
// `3@a` : Play 'a' 3 times
// `@@a` : Play 'a' infinitely
// `@@@` : Play all recorded macros infinitely
// `q@` : Stop all playback
// `Ctrl+@` : Stop all playback (alternative)

// PlaybackState tracks a single macro's playback progress
type PlaybackState struct {
	label         rune
	index         int
	repeatTarget  int // 0 = infinite
	repeatCurrent int
	lastPlayTime  time.Time
	startOrder    int // FIFO ordering
}

// MacroManager handles multi-macro recording and concurrent playback
type MacroManager struct {
	// Storage
	buffers map[rune][]input.Intent

	// Recording state
	recording   bool
	recordLabel rune

	// Playback state
	active       map[rune]*PlaybackState
	startCounter int // Monotonic counter for FIFO ordering
}

// NewMacroManager creates an empty manager
func NewMacroManager() *MacroManager {
	return &MacroManager{
		buffers: make(map[rune][]input.Intent),
		active:  make(map[rune]*PlaybackState),
	}
}

func (m *MacroManager) IsRecording() bool    { return m.recording }
func (m *MacroManager) RecordingLabel() rune { return m.recordLabel }
func (m *MacroManager) IsPlaying() bool      { return len(m.active) > 0 }
func (m *MacroManager) IsLabelPlaying(r rune) bool {
	_, ok := m.active[r]
	return ok
}

// StartRecording begins capturing intents to specified label
// Stops any playback of that label first
func (m *MacroManager) StartRecording(label rune) {
	// Stop playback of this label if running
	delete(m.active, label)

	m.recording = true
	m.recordLabel = label
	m.buffers[label] = make([]input.Intent, 0, 32)
}

// StopRecording ends capture
func (m *MacroManager) StopRecording() {
	m.recording = false
	m.recordLabel = 0
}

// Record appends intent to current recording buffer
// Ignores if not recording or if intent originated from playback
func (m *MacroManager) Record(intent input.Intent) {
	if !m.recording {
		return
	}
	m.buffers[m.recordLabel] = append(m.buffers[m.recordLabel], intent)
}

// StartPlayback begins playback of label with count repetitions (0 = infinite)
// Returns false if buffer empty or already playing (no-op)
func (m *MacroManager) StartPlayback(label rune, count int, now time.Time) bool {
	// No-op if already playing this label
	if _, playing := m.active[label]; playing {
		return false
	}

	buffer, exists := m.buffers[label]
	if !exists || len(buffer) == 0 {
		return false
	}

	m.startCounter++
	m.active[label] = &PlaybackState{
		label:         label,
		index:         0,
		repeatTarget:  count,
		repeatCurrent: 0,
		lastPlayTime:  now.Add(-parameter.MacroPlaybackInterval), // Fire first immediately
		startOrder:    m.startCounter,
	}
	return true
}

// StartAllPlayback begins infinite playback of all non-empty macros
// Returns count of macros started
func (m *MacroManager) StartAllPlayback(now time.Time) int {
	started := 0
	for label, buffer := range m.buffers {
		if len(buffer) == 0 {
			continue
		}
		// Skip if already playing
		if _, playing := m.active[label]; playing {
			continue
		}
		m.startCounter++
		m.active[label] = &PlaybackState{
			label:         label,
			index:         0,
			repeatTarget:  0, // Infinite
			repeatCurrent: 0,
			lastPlayTime:  now.Add(-parameter.MacroPlaybackInterval),
			startOrder:    m.startCounter,
		}
		started++
	}
	return started
}

// StopPlayback halts playback of specific label
func (m *MacroManager) StopPlayback(label rune) {
	delete(m.active, label)
}

// StopAllPlayback halts all macro playback
func (m *MacroManager) StopAllPlayback() {
	m.active = make(map[rune]*PlaybackState)
}

// Tick returns intents ready for execution from all active macros
// Returns slice ordered by start time (FIFO)
func (m *MacroManager) Tick(now time.Time) []*input.Intent {
	if len(m.active) == 0 {
		return nil
	}

	// Collect ready states in FIFO order
	type readyItem struct {
		state  *PlaybackState
		intent *input.Intent
	}
	var ready []readyItem

	for label, state := range m.active {
		if now.Sub(state.lastPlayTime) < parameter.MacroPlaybackInterval {
			continue
		}

		buffer := m.buffers[label]
		if len(buffer) == 0 {
			delete(m.active, label)
			continue
		}

		// Check repeat boundary
		if state.index >= len(buffer) {
			state.repeatCurrent++
			// 0 = infinite
			if state.repeatTarget > 0 && state.repeatCurrent >= state.repeatTarget {
				delete(m.active, label)
				continue
			}
			state.index = 0
		}

		// Copy intent and mark as playback-originated
		intent := buffer[state.index]
		intent.MacroPlayback = true
		state.index++
		state.lastPlayTime = now

		ready = append(ready, readyItem{state: state, intent: &intent})
	}

	if len(ready) == 0 {
		return nil
	}

	// Sort by start order (FIFO)
	for i := 0; i < len(ready)-1; i++ {
		for j := i + 1; j < len(ready); j++ {
			if ready[j].state.startOrder < ready[i].state.startOrder {
				ready[i], ready[j] = ready[j], ready[i]
			}
		}
	}

	result := make([]*input.Intent, len(ready))
	for i, item := range ready {
		result[i] = item.intent
	}
	return result
}

// ActiveLabels returns currently playing macro labels (for UI)
func (m *MacroManager) ActiveLabels() []rune {
	if len(m.active) == 0 {
		return nil
	}

	// Collect and sort by start order
	type item struct {
		label rune
		order int
	}
	items := make([]item, 0, len(m.active))
	for label, state := range m.active {
		items = append(items, item{label, state.startOrder})
	}
	for i := 0; i < len(items)-1; i++ {
		for j := i + 1; j < len(items); j++ {
			if items[j].order < items[i].order {
				items[i], items[j] = items[j], items[i]
			}
		}
	}

	result := make([]rune, len(items))
	for i, it := range items {
		result[i] = it.label
	}
	return result
}

// Reset clears all state (called on game reset)
func (m *MacroManager) Reset() {
	m.recording = false
	m.recordLabel = 0
	m.buffers = make(map[rune][]input.Intent)
	m.active = make(map[rune]*PlaybackState)
	m.startCounter = 0
}