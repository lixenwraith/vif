package system

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/status"
)

// MetaSystem handles meta-game commands like Reset, Debug, and Help
type MetaSystem struct {
	ctx *engine.GameContext

	world *engine.World
}

// NewMetaSystem creates a new meta system
func NewMetaSystem(ctx *engine.GameContext) engine.System {
	s := &MetaSystem{
		ctx:   ctx,
		world: ctx.World,
	}
	s.Init()
	return s
}

// Init resets session state for new game
func (s *MetaSystem) Init() {
	// No-op
}

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
//  2. GameState reset (counters, timers)
//  3. Cursor recreation
//  4. FSM reset (emits spawnLightning request, dispatched immediately)
//
// Other systems handle EventGameReset after this completes
func (s *MetaSystem) handleGameReset() {
	// 1. Pause and stop audio
	s.ctx.SetPaused(true)

	// 2. Synchronous World Cleanup
	// Already inside world.RunSafe from main -> DispatchEventsImmediately
	s.ctx.World.Clear()

	// 3. GameState reset (counters, NextID → 1)
	s.ctx.State.Reset()

	// 4. Cursor recreation (required before spawnLightning events)
	s.ctx.CreateCursorEntity()

	// 5. Reset mode and status
	s.ctx.SetMode(core.ModeNormal)
	s.ctx.SetCommandText("")
	s.ctx.SetSearchText("")
	s.ctx.SetStatusMessage("")
	s.ctx.SetOverlayContent(nil)

	// 5. Signal FSM reset - Non-blocking
	// On return from this function main releases the world lock and scheduler acquires it for reset
	select {
	case s.ctx.ResetChan <- struct{}{}:
	default:
	}
}

// handleDebugRequest shows debug information overlay with auto-discovered stats
func (s *MetaSystem) handleDebugRequest() {
	content := &core.OverlayContent{
		Title: "DEBUG",
	}

	// Card: Player GameState
	playerCard := core.OverlayCard{Title: "PLAYER"}
	energyComp, _ := s.world.Components.Energy.GetComponent(s.ctx.CursorEntity)
	playerCard.Entries = append(playerCard.Entries, core.CardEntry{
		Key: "Energy", Value: fmt.Sprintf("%d", energyComp.Current.Load()),
	})
	if hc, ok := s.world.Components.Heat.GetComponent(s.ctx.CursorEntity); ok {
		playerCard.Entries = append(playerCard.Entries, core.CardEntry{
			Key: "Heat", Value: fmt.Sprintf("%d/%d", hc.Current.Load(), constant.MaxHeat),
		})
	}
	if sc, ok := s.world.Components.Shield.GetComponent(s.ctx.CursorEntity); ok {
		playerCard.Entries = append(playerCard.Entries, core.CardEntry{
			Key: "Shield", Value: fmt.Sprintf("%v", sc.Active),
		})
	}
	content.Items = append(content.Items, playerCard)

	// Card: Engine (from context)
	engineCard := core.OverlayCard{Title: "ENGINE"}
	engineCard.Entries = append(engineCard.Entries,
		core.CardEntry{Key: "Frame", Value: fmt.Sprintf("%d", s.ctx.GetFrameNumber())},
		core.CardEntry{Key: "Screen", Value: fmt.Sprintf("%dx%d", s.ctx.Width, s.ctx.Height)},
		core.CardEntry{Key: "Game", Value: fmt.Sprintf("%dx%d", s.ctx.GameWidth, s.ctx.GameHeight)},
		core.CardEntry{Key: "Paused", Value: fmt.Sprintf("%v", s.ctx.IsPaused.Load())},
	)
	content.Items = append(content.Items, engineCard)

	// Cards from status registry, grouped by prefix
	reg := s.world.Resources.Status
	groups := make(map[string][]core.CardEntry)

	reg.Bools.Range(func(key string, ptr *atomic.Bool) {
		prefix, name := splitStatKey(key)
		groups[prefix] = append(groups[prefix], core.CardEntry{
			Key: name, Value: fmt.Sprintf("%v", ptr.Load()),
		})
	})

	reg.Ints.Range(func(key string, ptr *atomic.Int64) {
		val := ptr.Load()
		prefix, name := splitStatKey(key)
		var valStr string
		if strings.HasSuffix(key, ".timer") || strings.HasSuffix(key, ".duration") {
			valStr = time.Duration(val).String()
		} else {
			valStr = fmt.Sprintf("%d", val)
		}
		groups[prefix] = append(groups[prefix], core.CardEntry{Key: name, Value: valStr})
	})

	reg.Floats.Range(func(key string, ptr *status.AtomicFloat) {
		prefix, name := splitStatKey(key)
		groups[prefix] = append(groups[prefix], core.CardEntry{
			Key: name, Value: fmt.Sprintf("%.3f", ptr.Get()),
		})
	})

	reg.Strings.Range(func(key string, ptr *status.AtomicString) {
		prefix, name := splitStatKey(key)
		groups[prefix] = append(groups[prefix], core.CardEntry{Key: name, Value: ptr.Load()})
	})

	// Add registry groups as cards in deterministic order
	groupOrder := []string{"engine", "event", "entity", "spawnLightning", "boost", "gold", "decay"}
	seen := make(map[string]bool)

	for _, prefix := range groupOrder {
		if entries, ok := groups[prefix]; ok && len(entries) > 0 {
			content.Items = append(content.Items, core.OverlayCard{
				Title:   strings.ToUpper(prefix),
				Entries: entries,
			})
			seen[prefix] = true
		}
	}

	// Remaining groups not in predefined order
	for prefix, entries := range groups {
		if !seen[prefix] && len(entries) > 0 {
			content.Items = append(content.Items, core.OverlayCard{
				Title:   strings.ToUpper(prefix),
				Entries: entries,
			})
		}
	}

	s.ctx.SetOverlayContent(content)
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
	content := &core.OverlayContent{
		Title: "HELP",
	}

	// Card: Modes
	content.Items = append(content.Items, core.OverlayCard{
		Title: "MODES",
		Entries: []core.CardEntry{
			{Key: "i", Value: "Enter INSERT mode"},
			{Key: "ESC", Value: "Return to NORMAL / Show grid"},
			{Key: "/", Value: "Enter SEARCH mode"},
			{Key: ":", Value: "Enter COMMAND mode"},
		},
	})

	// Card: Movement
	content.Items = append(content.Items, core.OverlayCard{
		Title: "MOVEMENT",
		Entries: []core.CardEntry{
			{Key: "h/j/k/l", Value: "MoveEntity left/down/up/right"},
			{Key: "w/b", Value: "Word forward/backward"},
			{Key: "0/$", Value: "Line start/end"},
			{Key: "gg/G", Value: "Top/bottom of screen"},
			{Key: "f/F{c}", Value: "Find char forward/backward"},
			{Key: "t/T{c}", Value: "Till char forward/backward"},
			{Key: ";/,", Value: "Repeat find / reverse"},
		},
	})

	// Card: Delete
	content.Items = append(content.Items, core.OverlayCard{
		Title: "DELETE",
		Entries: []core.CardEntry{
			{Key: "d{motion}", Value: "Delete with motion"},
			{Key: "dd", Value: "Delete current line"},
			{Key: "D", Value: "Delete to end of line"},
			{Key: "x", Value: "Delete char at cursor"},
		},
	})

	// Card: Game
	content.Items = append(content.Items, core.OverlayCard{
		Title: "GAME",
		Entries: []core.CardEntry{
			{Key: "TAB", Value: "Jump to nugget (10 energy)"},
			{Key: "ENTER", Value: "Fire directional cleaners"},
			{Key: "Ctrl+S", Value: "Toggle audio mute"},
		},
	})

	// Card: Search
	content.Items = append(content.Items, core.OverlayCard{
		Title: "SEARCH",
		Entries: []core.CardEntry{
			{Key: "/text", Value: "Search for text"},
			{Key: "n/N", Value: "Next/previous match"},
		},
	})

	// Card: Commands
	content.Items = append(content.Items, core.OverlayCard{
		Title: "COMMANDS",
		Entries: []core.CardEntry{
			{Key: ":q", Value: "Quit game"},
			{Key: ":n", Value: "New game"},
			{Key: ":energy N", Value: "SetComponent energy"},
			{Key: ":heat N", Value: "SetComponent heat"},
			{Key: ":boost", Value: "Enable boost"},
			{Key: ":spawnLightning on/off", Value: "Toggle spawning"},
			{Key: ":d", Value: "Debug overlay"},
			{Key: ":h", Value: "This help"},
		},
	})

	s.ctx.SetOverlayContent(content)
}