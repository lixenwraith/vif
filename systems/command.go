package systems

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/events"
)

// TODO: rename to meta system
// CommandSystem handles meta-game commands like Reset, Debug, and Help
type CommandSystem struct {
	ctx *engine.GameContext
	res engine.CoreResources
}

// NewCommandSystem creates a new command system
func NewCommandSystem(ctx *engine.GameContext) *CommandSystem {
	return &CommandSystem{
		ctx: ctx,
		res: engine.GetCoreResources(ctx.World),
	}
}

// Priority returns the system's priority
func (s *CommandSystem) Priority() int {
	return constants.PriorityUI // Run with UI/Input logic
}

// EventTypes returns the event types CommandSystem handles
func (s *CommandSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventGameReset,
		events.EventDebugRequest,
		events.EventHelpRequest,
	}
}

// HandleEvent processes command events
func (s *CommandSystem) HandleEvent(world *engine.World, event events.GameEvent) {
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
func (s *CommandSystem) Update(world *engine.World, dt time.Duration) {
	// No tick-based logic
}

// handleGameReset resets the game state
func (s *CommandSystem) handleGameReset() {
	// Stop any playing audio
	s.ctx.StopAudio()

	// Despawn drain entities before clearing world
	drains := s.ctx.World.Drains.All()
	for _, e := range drains {
		s.ctx.World.DestroyEntity(e)
	}

	// Clear all entities from the world
	s.ctx.World.Clear()

	// No need to reset event queue due to potential race with input, the queued events will fail to no-op for invalid targets after world reset

	// Reset entire game state
	s.ctx.State.Reset(s.ctx.PausableClock.Now())

	// Recreate cursor entity
	s.ctx.CreateCursorEntity()
}

// handleDebugRequest shows debug information overlay
func (s *CommandSystem) handleDebugRequest() {
	// Query energy
	energyComp, _ := s.ctx.World.Energies.Get(s.ctx.CursorEntity)
	energyVal := energyComp.Current.Load()

	// Query heat
	heatVal := 0
	if hc, ok := s.ctx.World.Heats.Get(s.ctx.CursorEntity); ok {
		heatVal = int(hc.Current.Load())
	}

	// Query boost
	boost, ok := s.ctx.World.Boosts.Get(s.ctx.CursorEntity)
	boostActive := ok && boost.Active

	debugContent := []string{
		"=== DEBUG INFORMATION ===",
		"",
		fmt.Sprintf("Energy:        %d", energyVal),
		fmt.Sprintf("Heat:          %d / %d", heatVal, constants.MaxHeat),
		fmt.Sprintf("Game Ticks:    %d", s.ctx.State.GetGameTicks()),
		fmt.Sprintf("APM:           %d", s.ctx.State.GetAPM()),
		fmt.Sprintf("Frame Number:  %d", s.ctx.GetFrameNumber()),
		"",
		fmt.Sprintf("Screen Size:   %dx%d", s.ctx.Width, s.ctx.Height),
		fmt.Sprintf("Game Area:     %dx%d", s.ctx.GameWidth, s.ctx.GameHeight),
		fmt.Sprintf("Game Offset:   (%d, %d)", s.ctx.GameX, s.ctx.GameY),
		"",
		fmt.Sprintf("Boost Active:  %v", boostActive),
		fmt.Sprintf("Paused:        %v", s.ctx.IsPaused.Load()),
		"",
		"Entity Counts:",
		fmt.Sprintf("  Characters:  %d", s.ctx.World.Characters.Count()),
		fmt.Sprintf("  Nuggets:     %d", s.ctx.World.Nuggets.Count()),
		fmt.Sprintf("  Drains:      %d", s.ctx.World.Drains.Count()),
		fmt.Sprintf("  Cleaners:    %d", s.ctx.World.Cleaners.Count()),
		fmt.Sprintf("  Decays:      %d", s.ctx.World.Decays.Count()),
		"",
		"Press ESC or ENTER to close",
	}

	// Iterate registry - no reflection, type-safe
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

	// Set overlay state only (Mode set by input handler)
	s.ctx.SetOverlayState(true, " DEBUG ", debugContent, 0)
}

// handleHelpRequest shows help information overlay
func (s *CommandSystem) handleHelpRequest() {
	// Build help content
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
		"  :god      - Max heat + high energy",
		"  :spawn on/off - Enable/disable spawning",
		"  :d/:debug - Show debug info",
		"  :h/:help  - Show this help",
		"",
		"AUDIO:",
		"  Ctrl+S    - Toggle mute",
		"",
		"Press ESC or ENTER to close",
	}

	// Set overlay state only (Mode set by input handler)
	s.ctx.SetOverlayState(true, " HELP ", helpContent, 0)
}