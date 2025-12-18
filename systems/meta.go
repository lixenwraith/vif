package systems

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/events"
)

// MetaSystem handles meta-game commands like Reset, Debug, and Help
type MetaSystem struct {
	ctx *engine.GameContext
	res engine.CoreResources

	// Cached stores for debug display and reset
	drainStore   *engine.Store[components.DrainComponent]
	energyStore  *engine.Store[components.EnergyComponent]
	heatStore    *engine.Store[components.HeatComponent]
	shieldStore  *engine.Store[components.ShieldComponent]
	charStore    *engine.Store[components.CharacterComponent]
	nuggetStore  *engine.Store[components.NuggetComponent]
	cleanerStore *engine.Store[components.CleanerComponent]
	decayStore   *engine.Store[components.DecayComponent]
}

// NewMetaSystem creates a new meta system
func NewMetaSystem(ctx *engine.GameContext) engine.System {
	world := ctx.World
	return &MetaSystem{
		ctx:          ctx,
		res:          engine.GetCoreResources(world),
		drainStore:   engine.GetStore[components.DrainComponent](world),
		energyStore:  engine.GetStore[components.EnergyComponent](world),
		heatStore:    engine.GetStore[components.HeatComponent](world),
		shieldStore:  engine.GetStore[components.ShieldComponent](world),
		charStore:    engine.GetStore[components.CharacterComponent](world),
		nuggetStore:  engine.GetStore[components.NuggetComponent](world),
		cleanerStore: engine.GetStore[components.CleanerComponent](world),
		decayStore:   engine.GetStore[components.DecayComponent](world),
	}
}

// Init
func (s *MetaSystem) Init() {}

// Priority returns the system's priority
func (s *MetaSystem) Priority() int {
	return constants.PriorityUI
}

// EventTypes returns the event types MetaSystem handles
func (s *MetaSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventGameReset,
		events.EventDebugRequest,
		events.EventHelpRequest,
	}
}

// HandleEvent processes command events
func (s *MetaSystem) HandleEvent(event events.GameEvent) {
	switch event.Type {
	case events.EventGameReset:
		s.handleGameReset()
	case events.EventDebugRequest:
		s.handleDebugRequest()
	case events.EventHelpRequest:
		s.handleHelpRequest()
	}
}

// Update implements System interface
func (s *MetaSystem) Update() {
	// No tick-based logic
}

// handleGameReset performs full game reset with deterministic ordering
// Execution sequence (race-free):
//  1. Entity cleanup (drains, world entities)
//  2. State reset (counters, timers)
//  3. Cursor recreation
//  4. FSM reset (emits spawn request, dispatched immediately)
//
// Other systems handle EventGameReset after this completes
func (s *MetaSystem) handleGameReset() {
	// Phase 1: Stop audio
	s.ctx.StopAudio()

	// Phase 2: Synchronous World Cleanup
	// Already inside world.RunSafe from main -> DispatchEventsImmediately
	s.ctx.World.Clear()

	// Phase 3: State reset (counters, NextSeqID → 1)
	s.ctx.State.Reset(s.ctx.PausableClock.Now())

	// Phase 4: Cursor recreation (required before spawn events)
	s.ctx.CreateCursorEntity()

	// Phase 5: Signal FSM reset - Non-blocking
	// On return from this function main releases the world lock and scheduler acquires it for reset
	select {
	case s.ctx.ResetChan <- struct{}{}:
	default:
	}
}

// handleDebugRequest shows debug information overlay
func (s *MetaSystem) handleDebugRequest() {
	// Query energy
	energyComp, _ := s.energyStore.Get(s.ctx.CursorEntity)
	energyVal := energyComp.Current.Load()

	// Query heat
	heatVal := 0
	if hc, ok := s.heatStore.Get(s.ctx.CursorEntity); ok {
		heatVal = int(hc.Current.Load())
	}

	// Query shield
	shieldActive := false
	if sc, ok := s.shieldStore.Get(s.ctx.CursorEntity); ok {
		shieldActive = sc.Active
	}

	// Build debug content
	debugContent := []string{
		"=== DEBUG INFORMATION ===",
		"",
		fmt.Sprintf("Energy:        %d", energyVal),
		fmt.Sprintf("Heat:          %d / %d", heatVal, constants.MaxHeat),
		fmt.Sprintf("Shield Active: %v", shieldActive),
		fmt.Sprintf("Game Ticks:    %d", s.ctx.State.GetGameTicks()),
		fmt.Sprintf("APM:           %d", s.ctx.State.GetAPM()),
		fmt.Sprintf("Frame Number:  %d", s.ctx.GetFrameNumber()),
		"",
		fmt.Sprintf("Screen Size:   %dx%d", s.ctx.Width, s.ctx.Height),
		fmt.Sprintf("Game Area:     %dx%d", s.ctx.GameWidth, s.ctx.GameHeight),
		fmt.Sprintf("Game Offset:   (%d, %d)", s.ctx.GameX, s.ctx.GameY),
		"",
		fmt.Sprintf("Spawn Enabled: %v", s.res.Status.Bools.Get("spawn.enabled").Load()),
		fmt.Sprintf("Boost Active:  %v", s.res.Status.Bools.Get("boost.active").Load()),
		fmt.Sprintf("Paused:        %v", s.ctx.IsPaused.Load()),
		"",
		"Entity Counts:",
		fmt.Sprintf("  Characters:  %d", s.charStore.Count()),
		fmt.Sprintf("  Nuggets:     %d", s.nuggetStore.Count()),
		fmt.Sprintf("  Drains:      %d", s.drainStore.Count()),
		fmt.Sprintf("  Cleaners:    %d", s.cleanerStore.Count()),
		fmt.Sprintf("  Decays:      %d", s.decayStore.Count()),
		"",
		"Status Registry:",
	}

	// Add status registry values
	reg := s.res.Status
	reg.Bools.Range(func(key string, ptr *atomic.Bool) {
		debugContent = append(debugContent, fmt.Sprintf("  %s: %v", key, ptr.Load()))
	})

	reg.Ints.Range(func(key string, ptr *atomic.Int64) {
		val := ptr.Load()
		// Format durations nicely
		if strings.HasSuffix(key, ".timer") {
			debugContent = append(debugContent, fmt.Sprintf("  %s: %s", key, time.Duration(val).String()))
		} else {
			debugContent = append(debugContent, fmt.Sprintf("  %s: %d", key, val))
		}
	})

	reg.Floats.Range(func(key string, ptr *status.AtomicFloat) {
		debugContent = append(debugContent, fmt.Sprintf("  %s: %.3f", key, ptr.Get()))
	})

	debugContent = append(debugContent, "", "Press ESC or ENTER to close")

	// Set overlay state
	s.ctx.SetOverlayState(true, " DEBUG ", debugContent, 0)
}

// handleHelpRequest shows help information overlay
func (s *MetaSystem) handleHelpRequest() {
	helpContent := []string{
		"=== VI-FIGHTER HELP ===",
		"",
		"MODES:",
		"  i         - Enter INSERT mode",
		"  ESC       - Return to NORMAL mode / Show grid",
		"  /         - Enter SEARCH mode",
		"  :         - Enter COMMAND mode",
		"",
		"MOVEMENT (Normal Mode):",
		"  h/j/k/l   - Move left/down/up/right",
		"  w/b       - Move forward/backward by word",
		"  0/$       - Move to start/end of line",
		"  gg        - Go to top",
		"  G         - Go to bottom",
		"  f{char}   - Find character forward",
		"  F{char}   - Find character backward",
		"  t{char}   - Till character forward",
		"  T{char}   - Till character backward",
		"  ;         - Repeat last find/till",
		"  ,         - Repeat last find/till (reverse)",
		"",
		"DELETE (Normal Mode):",
		"  d{motion} - Delete with motion (dw, d$, etc.)",
		"  dd        - Delete current line",
		"  D         - Delete to end of line",
		"",
		"GAME MECHANICS:",
		"  TAB       - Jump to nugget (costs 10 energy)",
		"  ENTER     - Fire directional cleaners (costs 10 heat)",
		"",
		"SEARCH:",
		"  /text     - Search for text",
		"  n         - Next match",
		"  N         - Previous match",
		"",
		"COMMANDS:",
		"  :q        - Quit game",
		"  :n        - New game",
		"  :energy N - Set energy to N",
		"  :heat N   - Set heat to N",
		"  :boost    - Enable boost for 10s",
		"  :spawn on/off - Enable/disable spawning",
		"  :d/:debug - Show debug info",
		"  :h/:help  - Show this help",
		"",
		"AUDIO:",
		"  Ctrl+S    - Toggle mute",
		"",
		"Press ESC or ENTER to close",
	}

	s.ctx.SetOverlayState(true, " HELP ", helpContent, 0)
}