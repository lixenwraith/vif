package system

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/engine/status"
	"github.com/lixenwraith/vi-fighter/event"
)

// MetaSystem handles meta-game commands like Reset, Debug, and Help
type MetaSystem struct {
	ctx *engine.GameContext
	res engine.Resources

	// Cached stores for debug display and reset
	drainStore   *engine.Store[component.DrainComponent]
	energyStore  *engine.Store[component.EnergyComponent]
	heatStore    *engine.Store[component.HeatComponent]
	shieldStore  *engine.Store[component.ShieldComponent]
	charStore    *engine.Store[component.CharacterComponent]
	nuggetStore  *engine.Store[component.NuggetComponent]
	cleanerStore *engine.Store[component.CleanerComponent]
	decayStore   *engine.Store[component.DecayComponent]
}

// NewMetaSystem creates a new meta system
func NewMetaSystem(ctx *engine.GameContext) engine.System {
	world := ctx.World
	return &MetaSystem{
		ctx:          ctx,
		res:          engine.GetResources(world),
		drainStore:   engine.GetStore[component.DrainComponent](world),
		energyStore:  engine.GetStore[component.EnergyComponent](world),
		heatStore:    engine.GetStore[component.HeatComponent](world),
		shieldStore:  engine.GetStore[component.ShieldComponent](world),
		charStore:    engine.GetStore[component.CharacterComponent](world),
		nuggetStore:  engine.GetStore[component.NuggetComponent](world),
		cleanerStore: engine.GetStore[component.CleanerComponent](world),
		decayStore:   engine.GetStore[component.DecayComponent](world),
	}
}

// Init
func (s *MetaSystem) Init() {}

// Priority returns the system's priority
func (s *MetaSystem) Priority() int {
	return constant.PriorityUI
}

// EventTypes returns the event types MetaSystem handles
func (s *MetaSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventGameReset,
		event.EventDebugRequest,
		event.EventHelpRequest,
	}
}

// HandleEvent processes command events
func (s *MetaSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameReset:
		s.handleGameReset()
	case event.EventDebugRequest:
		s.handleDebugRequest()
	case event.EventHelpRequest:
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

	// Phase 3: State reset (counters, NextID → 1)
	s.ctx.State.Reset()

	// Phase 4: Cursor recreation (required before spawn events)
	s.ctx.CreateCursorEntity()

	// Phase 5: Signal FSM reset - Non-blocking
	// On return from this function main releases the world lock and scheduler acquires it for reset
	select {
	case s.ctx.ResetChan <- struct{}{}:
	default:
	}
}

// handleDebugRequest shows debug information overlay with auto-discovered stats
func (s *MetaSystem) handleDebugRequest() {
	var lines []string

	// Section: Player State (cursor-specific, always relevant)
	lines = append(lines, "§PLAYER STATE")

	energyComp, _ := s.energyStore.Get(s.ctx.CursorEntity)
	lines = append(lines, fmt.Sprintf("Energy|%d", energyComp.Current.Load()))

	heatVal := int64(0)
	if hc, ok := s.heatStore.Get(s.ctx.CursorEntity); ok {
		heatVal = hc.Current.Load()
	}
	lines = append(lines, fmt.Sprintf("Heat|%d / %d", heatVal, constant.MaxHeat))

	shieldActive := false
	if sc, ok := s.shieldStore.Get(s.ctx.CursorEntity); ok {
		shieldActive = sc.Active
	}
	lines = append(lines, fmt.Sprintf("Shield|%v", shieldActive))

	// Section: Engine
	lines = append(lines, "§ENGINE")
	lines = append(lines, fmt.Sprintf("Frame|%d", s.ctx.GetFrameNumber()))
	lines = append(lines, fmt.Sprintf("Screen|%dx%d", s.ctx.Width, s.ctx.Height))
	lines = append(lines, fmt.Sprintf("Game Area|%dx%d", s.ctx.GameWidth, s.ctx.GameHeight))
	lines = append(lines, fmt.Sprintf("Paused|%v", s.ctx.IsPaused.Load()))

	// Section: Auto-discovered Registry Stats
	reg := s.res.Status

	// Collect and group by prefix
	groups := make(map[string][][2]string) // prefix -> []{"key", "value"}

	reg.Bools.Range(func(key string, ptr *atomic.Bool) {
		prefix, name := splitStatKey(key)
		groups[prefix] = append(groups[prefix], [2]string{name, fmt.Sprintf("%v", ptr.Load())})
	})

	reg.Ints.Range(func(key string, ptr *atomic.Int64) {
		val := ptr.Load()
		prefix, name := splitStatKey(key)

		// Format durations nicely
		var valStr string
		if strings.HasSuffix(key, ".timer") || strings.HasSuffix(key, ".duration") {
			valStr = time.Duration(val).String()
		} else {
			valStr = fmt.Sprintf("%d", val)
		}
		groups[prefix] = append(groups[prefix], [2]string{name, valStr})
	})

	reg.Floats.Range(func(key string, ptr *status.AtomicFloat) {
		prefix, name := splitStatKey(key)
		groups[prefix] = append(groups[prefix], [2]string{name, fmt.Sprintf("%.3f", ptr.Get())})
	})

	// Output groups in deterministic order
	groupOrder := []string{"engine", "event", "entity", "spawn", "boost", "gold", "decay"}
	seen := make(map[string]bool)

	for _, prefix := range groupOrder {
		if stats, ok := groups[prefix]; ok {
			lines = append(lines, "§"+strings.ToUpper(prefix))
			for _, kv := range stats {
				lines = append(lines, fmt.Sprintf("%s|%s", kv[0], kv[1]))
			}
			seen[prefix] = true
		}
	}

	// Any remaining groups not in predefined order
	for prefix, stats := range groups {
		if seen[prefix] {
			continue
		}
		lines = append(lines, "§"+strings.ToUpper(prefix))
		for _, kv := range stats {
			lines = append(lines, fmt.Sprintf("%s|%s", kv[0], kv[1]))
		}
	}

	lines = append(lines, "")
	lines = append(lines, "~ESC or ENTER to close")

	s.ctx.SetOverlayState(true, " DEBUG ", lines, 0)
}

// splitStatKey splits "prefix.name" into prefix and name
// Returns ("misc", key) if no dot found
func splitStatKey(key string) (prefix, name string) {
	idx := strings.Index(key, ".")
	if idx < 0 {
		return "misc", key
	}
	return key[:idx], key[idx+1:]
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