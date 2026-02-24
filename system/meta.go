package system

import (
	"fmt"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/parameter"
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

// Name returns system's name
func (s *MetaSystem) Name() string {
	return "meta"
}

// Priority returns the system's priority
func (s *MetaSystem) Priority() int {
	return parameter.PriorityUI
}

// EventTypes returns the event types MetaSystem handles
func (s *MetaSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventDebugFlowToggle,
		event.EventDebugGraphToggle,
		event.EventMetaStatusMessageRequest,
		event.EventLevelSetup,
		event.EventMetaDebugRequest,
		event.EventMetaHelpRequest,
		event.EventMetaAboutRequest,
		event.EventGameReset,
	}
}

// HandleEvent processes command events
func (s *MetaSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameReset:
		s.handleGameReset()

	case event.EventMetaStatusMessageRequest:
		if payload, ok := ev.Payload.(*event.MetaStatusMessagePayload); ok {
			s.handleMessageRequest(payload)
		}

	case event.EventLevelSetup:
		if payload, ok := ev.Payload.(*event.LevelSetupPayload); ok {
			s.handleLevelSetup(payload)
		}

	case event.EventDebugFlowToggle:
		DebugShowFlow = !DebugShowFlow

	case event.EventDebugGraphToggle:
		DebugShowCompositeNav = !DebugShowCompositeNav

	case event.EventMetaDebugRequest:
		s.handleDebugRequest()

	case event.EventMetaHelpRequest:
		s.handleHelpRequest()

	case event.EventMetaAboutRequest:
		s.handleAboutRequest()
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
//  4. FSM reset (emits spawn request, dispatched immediately)
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

	// 4. Config reset (map dimensions to viewport)
	config := s.ctx.World.Resources.Config
	config.MapWidth = config.ViewportWidth
	config.MapHeight = config.ViewportHeight
	config.CameraX = 0
	config.CameraY = 0
	config.CropOnResize = true

	// 5. Cursor recreation
	s.ctx.World.CreateCursorEntity()

	// 6. Reset mode and status
	s.ctx.SetMode(core.ModeNormal)
	s.ctx.SetCommandText("")
	s.ctx.SetSearchText("")
	s.ctx.SetStatusMessage("", 0, false)
	s.ctx.SetOverlayContent(nil)

	// 7. Signal FSM reset - Non-blocking

	// On return from this function main releases the world lock and scheduler acquires it for reset
	select {
	case s.ctx.ResetChan <- struct{}{}:
	default:
	}
}

// handleMessageRequest displays a message in status bar
func (s *MetaSystem) handleMessageRequest(payload *event.MetaStatusMessagePayload) {
	if payload.Duration < 0 {
		payload.Duration = 0
	}
	s.ctx.SetStatusMessage(payload.Message, payload.Duration, payload.DurationOverride)
}

// handleLevelSetup reconfigures map dimensions and clears entities
func (s *MetaSystem) handleLevelSetup(payload *event.LevelSetupPayload) {
	width := payload.Width
	height := payload.Height
	cropOnResize := payload.CropOnResize

	// Zero dimensions = reset to viewport with crop enabled
	if width <= 0 || height <= 0 {
		width = s.world.Resources.Config.ViewportWidth
		height = s.world.Resources.Config.ViewportHeight
		cropOnResize = true
	}

	s.world.SetupLevel(width, height, payload.ClearEntities, cropOnResize)
}

// handleDebugRequest shows debug information overlay with auto-discovered stats
func (s *MetaSystem) handleDebugRequest() {
	content := &core.OverlayContent{
		Title: "DEBUG",
	}

	// Collect all stats into groups by prefix
	groups := make(map[string][]core.CardEntry)

	// Context stats
	groups["context"] = []core.CardEntry{
		{Key: "frame", Value: fmt.Sprintf("%d", s.ctx.GetFrameNumber())},
		{Key: "screen", Value: fmt.Sprintf("%dx%d", s.ctx.Width, s.ctx.Height)},
		{Key: "game", Value: fmt.Sprintf("%dx%d", s.ctx.World.Resources.Config.MapWidth, s.ctx.World.Resources.Config.MapHeight)},
		{Key: "paused", Value: fmt.Sprintf("%v", s.ctx.IsPaused.Load())},
	}

	// Player stats from cursor entity components
	cursorEntity := s.ctx.World.Resources.Player.Entity
	var playerEntries []core.CardEntry
	if ec, ok := s.world.Components.Energy.GetComponent(cursorEntity); ok {
		playerEntries = append(playerEntries, core.CardEntry{
			Key: "energy", Value: fmt.Sprintf("%d", ec.Current),
		})
	}
	if hc, ok := s.world.Components.Heat.GetComponent(cursorEntity); ok {
		playerEntries = append(playerEntries, core.CardEntry{
			Key: "heat", Value: fmt.Sprintf("%d/%d", hc.Current, parameter.HeatMax),
		})
	}
	if sc, ok := s.world.Components.Shield.GetComponent(cursorEntity); ok {
		playerEntries = append(playerEntries, core.CardEntry{
			Key: "shield", Value: fmt.Sprintf("%v", sc.Active),
		})
	}
	if len(playerEntries) > 0 {
		groups["player"] = playerEntries
	}

	// Status registry stats
	reg := s.world.Resources.Status

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

	// Sort prefixes alphabetically
	prefixes := make([]string, 0, len(groups))
	for prefix := range groups {
		prefixes = append(prefixes, prefix)
	}
	sort.Strings(prefixes)

	// Build cards in sorted order with sorted entries
	for _, prefix := range prefixes {
		entries := groups[prefix]
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].Key < entries[j].Key
		})
		content.Items = append(content.Items, core.OverlayCard{
			Title:   strings.ToUpper(prefix),
			Entries: entries,
		})
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
			{Key: ":energy N", Value: "Set energy"},
			{Key: ":heat N", Value: "Set heat"},
			{Key: ":boost", Value: "Enable boost"},
			{Key: ":spawn on/off", Value: "Toggle spawning"},
			{Key: ":d", Value: "Debug overlay"},
			{Key: ":h", Value: "This help"},
		},
	})

	s.ctx.SetOverlayContent(content)
}

// === About (placeholder) ===

// handleAboutRequest shows about information overlay
func (s *MetaSystem) handleAboutRequest() {
	content := &core.OverlayContent{
		Title:  "ABOUT",
		Custom: true,
	}

	// Store info as entries for the renderer to extract
	content.Items = append(content.Items, core.OverlayCard{
		Title: "VI-FIGHTER",
		Entries: []core.CardEntry{
			{Key: "desc", Value: "A terminal-based rouge-like action typing game with vi-style keybindings. Made with love for terminal, Go, VIM, and Games :)"},
			{Key: "version", Value: "0.1.0-alpha"},
			{Key: "engine", Value: "Custom ECS, Data-driven HFSM, Double-buffered ANSI renderer"},
			{Key: "go", Value: "1.25+"},
			{Key: "github", Value: "github.com/lixenwraith/vi-fighter"},
			{Key: "author", Value: "Lixen Wraith"},
			{Key: "website", Value: "lixen.com"},
			{Key: "license", Value: "BSD-3"},
		},
	})

	s.ctx.SetOverlayContent(content)
}