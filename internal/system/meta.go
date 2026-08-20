package system

import (
	"slices"
	"strings"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/help"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/status"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// MetaSystem handles meta-game commands like Reset, Debug, and Help
type MetaSystem struct {
	ctx *engine.GameContext

	world *engine.World

	// Context and player telemetry, published for the debug overlay and HUD
	statMapW    *atomic.Int64
	statMapH    *atomic.Int64
	statCameraX *atomic.Int64
	statCameraY *atomic.Int64
	// statPlayerX *atomic.Int64
	// statPlayerY *atomic.Int64
	statPlayerX *status.PlayerInt
	statPlayerY *status.PlayerInt

	// Kill counters gate FSM region transitions, so they live in a system with
	// no enable/disable toggle
	statKills           [component.SpeciesCount]*atomic.Int64
	statKillsTotal      *atomic.Int64
	statKillsUncredited *atomic.Int64
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

// Init caches the telemetry pointers; runs at construction, before Freeze
func (s *MetaSystem) Init() {
	reg := s.world.Resources.Status
	s.statMapW = reg.Ints.Get("context.map_w")
	s.statMapH = reg.Ints.Get("context.map_h")
	s.statCameraX = reg.Ints.Get("context.camera_x")
	s.statCameraY = reg.Ints.Get("context.camera_y")
	s.statPlayerX = status.NewPlayerInt(reg, parameter.MaxPlayers, "x", "player.x")
	s.statPlayerY = status.NewPlayerInt(reg, parameter.MaxPlayers, "y", "player.y")
	// s.statPlayerX = reg.Ints.Get("player.x")
	// s.statPlayerY = reg.Ints.Get("player.y")
	for i := component.SpeciesType(1); i < component.SpeciesCount; i++ {
		s.statKills[i] = reg.Ints.Get("kills." + component.SpeciesNames[i])
	}
	s.statKillsTotal = reg.Ints.Get("kills.total")
	s.statKillsUncredited = reg.Ints.Get("kills.uncredited")
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
		event.EventModeChanged,
		event.EventLevelSetup,
		event.EventScreenResize,
		event.EventMetaDebugRequest,
		event.EventMetaHelpRequest,
		event.EventMetaAboutRequest,
		event.EventGamePauseRequest,
		event.EventGameSpeedRequest,
		event.EventGameStepRequest,
		event.EventEnemyKilled, // TODO: move kill telemetry to combat, not a meta concept, all happens in world
		event.EventGameResetRequest,
	}
}

// HandleEvent processes command events
func (s *MetaSystem) HandleEvent(ev event.GameEvent) {
	switch ev.Type {
	case event.EventGameResetRequest:
		p, _ := ev.Payload.(*event.GameResetPayload)
		s.handleGameReset(p != nil && p.Purge)

	case event.EventMetaStatusMessageRequest:
		if payload, ok := ev.Payload.(*event.MetaStatusMessagePayload); ok {
			s.handleMessageRequest(payload)
		}

	case event.EventModeChanged:
		p, ok := ev.Payload.(*event.ModeChangedPayload)
		if !ok || int(p.Mode) >= len(core.ModeNames) {
			return // absent or out-of-range mode from an external stream
		}
		s.ctx.SetMode(p.Mode)
		s.ctx.World.UpdateBoundsRadius()

	case event.EventLevelSetup:
		if payload, ok := ev.Payload.(*event.LevelSetupPayload); ok {
			s.handleLevelSetup(payload)
		}

	case event.EventScreenResize:
		if p, ok := ev.Payload.(*event.ScreenResizePayload); ok {
			s.handleScreenResize(p)
		}

	case event.EventDebugFlowToggle:
		if payload, ok := ev.Payload.(*event.DebugFlowGroupPayload); ok {
			DebugFlowGroupID = payload.GroupID
			DebugShowFlow = true
		} else {
			DebugShowFlow = !DebugShowFlow
		}

	case event.EventDebugGraphToggle:
		if payload, ok := ev.Payload.(*event.DebugFlowGroupPayload); ok {
			DebugFlowGroupID = payload.GroupID
			DebugShowCompositeNav = true
		} else {
			DebugShowCompositeNav = !DebugShowCompositeNav
		}

	case event.EventMetaDebugRequest:
		s.handleDebugRequest()

	case event.EventMetaHelpRequest:
		s.handleHelpRequest()

	case event.EventMetaAboutRequest:
		s.handleAboutRequest()

	case event.EventGamePauseRequest:
		if p, ok := ev.Payload.(*event.GamePausePayload); ok {
			s.handlePauseRequest(p.Paused)
		}

	case event.EventGameSpeedRequest:
		p, _ := ev.Payload.(*event.GameSpeedPayload)
		s.handleSpeedRequest(p)

	case event.EventGameStepRequest:
		p, _ := ev.Payload.(*event.GameStepPayload)
		s.handleStepRequest(p)

	case event.EventEnemyKilled:
		if p, ok := ev.Payload.(*event.EnemyKilledPayload); ok {
			s.recordKill(p)
		}
	}
}

// Update publishes context and player telemetry; every read is world state
// already guarded by the update mutex
func (s *MetaSystem) Update() {
	cfg := s.world.Resources.Config
	s.statMapW.Store(int64(cfg.MapWidth))
	s.statMapH.Store(int64(cfg.MapHeight))
	s.statCameraX.Store(int64(cfg.CameraX))
	s.statCameraY.Store(int64(cfg.CameraY))

	// Per-slot placement; -1 marks an empty slot
	roster := s.world.Resources.Player
	for i := range parameter.MaxPlayers {
		slot := uint8(i)
		x, y := int64(-1), int64(-1)
		if pos, ok := s.world.Positions.GetPosition(roster.Slot(slot)); ok {
			x, y = int64(pos.X), int64(pos.Y)
		}
		s.statPlayerX.Store(slot, x)
		s.statPlayerY.Store(slot, y)
	}
}

// recordKill counts one enemy death per species, tracking deaths no cursor is credited with
func (s *MetaSystem) recordKill(p *event.EnemyKilledPayload) {
	if p.Species <= component.SpeciesNone || p.Species >= component.SpeciesCount {
		return
	}
	s.statKills[p.Species].Add(1)
	s.statKillsTotal.Add(1)
	if s.world.ResolveCursor(p.KillerEntity) == 0 {
		s.statKillsUncredited.Add(1)
	}
}

// resetKills zeroes every species counter for a new game
func (s *MetaSystem) resetKills() {
	for i := component.SpeciesType(1); i < component.SpeciesCount; i++ {
		s.statKills[i].Store(0)
	}
	s.statKillsTotal.Store(0)
	s.statKillsUncredited.Store(0)
}

// handleGameReset rebuilds world state; purge additionally clears operator session state
// Execution sequence (race-free):
//  1. Entity cleanup (drains, world entities)
//  2. GameState reset (counters, timers)
//  3. Cursor recreation
//  4. FSM reset (emits spawn request, dispatched immediately)
//
// Other systems handle EventGameResetRequest after this completes
func (s *MetaSystem) handleGameReset(purge bool) {
	// 1. Pause and stop audio
	s.ctx.SetPaused(true)

	// 2. Synchronous World Cleanup
	// Already inside world.RunSafe from main -> DispatchEventsImmediately
	s.ctx.World.Clear()

	// 3. GameState reset (counters, NextID → 1)
	s.ctx.State.Reset()
	s.resetKills()

	// 4. Journal run advances with the tick counter it re-bases; both are world-lock state,
	// so no producer can observe one without the other
	run := s.world.Resources.Event.Queue.NextRun()
	vlog.SetRun(run)

	// 5. Config reset (map dimensions to viewport)
	config := s.ctx.World.Resources.Config
	config.MapWidth = config.ViewportWidth
	config.MapHeight = config.ViewportHeight
	config.CameraX = 0
	config.CameraY = 0
	config.CropOnResize = true

	// Grid tracks map dimensions; a level-sized grid would outlive the level
	s.ctx.World.Positions.ResizeGrid(config.MapWidth, config.MapHeight)

	// 6. Reset mode and status; the FSM reset below spawns the cursor
	s.ctx.SetMode(core.ModeNormal)
	s.ctx.SetCommandText("")
	s.ctx.SetSearchText("")
	s.ctx.SetStatusMessage("", 0, false)
	s.ctx.SetOverlayContent(nil)

	// 7. Cancel pending step requests; the rate itself is operator-owned and survives
	s.ctx.TimeCtl.CancelBreak()

	// 8. Signal FSM reset - Non-blocking

	// On return from this function main releases the world lock and scheduler acquires it for reset
	select {
	case s.ctx.ResetChan <- struct{}{}:
	default:
	}

	// 9. Purge operator session state; last, so it wins over anything reset restored
	if purge {
		s.ctx.ResetSessionState()
		vlog.Info("app", "msg", "session purge")
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

// handleScreenResize applies terminal dimensions and reflows the geometry. Sole
// writer of ctx.Width/Height, live and headless alike, so the main loop and this
// handler cannot race over them.
// A report that would leave the game area below one cell is dropped rather than
// clamped: the clamp in updateGameArea is exactly where screen and viewport stop
// being mutually derivable, which would desync ScreenSize and the journal anchor.
func (s *MetaSystem) handleScreenResize(p *event.ScreenResizePayload) {
	if !engine.ViewportFits(p.Width, p.Height) {
		return
	}
	if p.Width == s.ctx.Width && p.Height == s.ctx.Height {
		return
	}
	s.ctx.Width = p.Width
	s.ctx.Height = p.Height
	s.ctx.HandleResizeLocked()
}

// handleDebugRequest shows the debug overlay, pinned groups first
func (s *MetaSystem) handleDebugRequest() {
	reg := s.world.Resources.Status
	views := reg.Views()
	pins := s.ctx.OverlayPins()

	content := &core.OverlayContent{
		Title: "DEBUG",
		Items: make([]core.OverlayItem, 0, len(views)),
	}

	// Pinned groups lead in pin order; the rest keep the registry's sorted order
	for _, key := range pins {
		if v, ok := reg.GroupView(key); ok {
			content.Items = append(content.Items, debugCard(v, true))
		}
	}
	for i := range views {
		if slices.Contains(pins, views[i].Name()) {
			continue
		}
		content.Items = append(content.Items, debugCard(views[i], false))
	}

	s.ctx.SetOverlayContent(content)
}

// debugCard projects one metric group into an overlay card
func debugCard(v status.GroupView, pinned bool) core.OverlayCard {
	entries := make([]core.CardEntry, v.Len())
	for i := range entries {
		entries[i] = core.CardEntry{Key: v.MetricName(i), Value: v.Value(i)}
	}
	return core.OverlayCard{
		Key:     v.Name(),
		Title:   strings.ToUpper(v.Name()),
		Entries: entries,
		Pinned:  pinned,
	}
}

// handleHelpRequest projects the help topics against the active key bindings
func (s *MetaSystem) handleHelpRequest() {
	topics := help.Topics(help.KeyTable())

	content := &core.OverlayContent{
		Title:  "HELP",
		Layout: core.OverlayLayoutDoc,
		Items:  make([]core.OverlayItem, 0, len(topics)),
	}

	for _, t := range topics {
		entries := make([]core.CardEntry, len(t.Entries))
		for i, e := range t.Entries {
			entries[i] = core.CardEntry{Key: e.Keys, Value: e.Desc}
		}
		content.Items = append(content.Items, core.OverlayCard{
			Key: t.Key, Title: t.Title, Entries: entries,
		})
	}

	s.ctx.SetOverlayContent(content)
}

// === About (placeholder) ===

// handleAboutRequest shows about information overlay
func (s *MetaSystem) handleAboutRequest() {
	content := &core.OverlayContent{
		Title:  "ABOUT",
		Layout: core.OverlayLayoutAbout,
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

// === Pause ===

// handlePauseRequest applies pause to game state and clock, then announces
// the change; each system applies it to its own domain (audio → AudioSystem)
func (s *MetaSystem) handlePauseRequest(paused bool) {
	if !s.ctx.TimeCtl.SetPaused(paused) {
		return
	}
	s.ctx.PushEvent(event.EventGamePauseChanged, &event.GamePausePayload{Paused: paused})
}

// handleSpeedRequest applies the time scale through its single owner, then
// announces it; a nil or non-positive payload restores real time
func (s *MetaSystem) handleSpeedRequest(p *event.GameSpeedPayload) {
	scale := engine.ScaleNormal
	if p != nil && p.Num > 0 && p.Den > 0 {
		scale = engine.TimeScale{Num: p.Num, Den: p.Den}
	}
	// if s.ctx.TimeCtl.Scale() == scale {
	if s.ctx.TimeCtl.Scale() == scale && s.ctx.TimeCtl.Armed() == nil {
		return
	}
	s.ctx.TimeCtl.SetScale(scale)
	vlog.Info("app", "msg", "time scale", "scale", scale.String())
	s.ctx.PushEvent(event.EventGameSpeedChanged, &event.GameSpeedPayload{Num: scale.Num, Den: scale.Den})
}

// handleStepRequest arms a tick allowance or a run-until breakpoint; pause and
// rate move through their single owner here
func (s *MetaSystem) handleStepRequest(p *event.GameStepPayload) {
	if p == nil || p.Off {
		s.handleSpeedRequest(nil) // restores 1x and disarms
		return
	}

	if p.Ticks > 0 {
		n := min(p.Ticks, int64(parameter.StepBurstMax))
		s.handlePauseRequest(true)
		s.ctx.TimeCtl.StepTicks(n)
		vlog.Info("app", "msg", "step", "ticks", n)
		return
	}

	cur := s.ctx.TimeCtl.Scale()
	run := cur
	if p.Num > 0 && p.Den > 0 {
		run = engine.TimeScale{Num: p.Num, Den: p.Den}
	}

	bs := &engine.BreakState{
		Restore: cur,
		Pause:   p.Pause,
		Expiry:  s.world.Resources.Game.State.GetGameTicks() + parameter.StepRunMaxTicks,
	}

	switch strings.ToLower(p.Mode) {
	case "fsm":
		bs.Mode = engine.StepFSM
		bs.Region = p.Region
		bs.Label = "fsm:" + regionLabel(p.Region)
	case "event", "ev":
		et, ok := event.GetEventType(p.Event)
		if !ok || et == event.EventNone {
			s.ctx.SetStatusMessage("Unknown event: "+p.Event, 0, true)
			return
		}
		bs.Mode = engine.StepEvent
		bs.Event = et
		bs.Label = "ev:" + strings.TrimPrefix(event.GetEventName(et), "Event")
	default:
		return
	}
	if bs.Pause {
		bs.Label += "!"
	}
	s.ctx.TimeCtl.Arm(bs, run)
	vlog.Info("app", "msg", "break armed", "on", bs.Label, "scale", run.String(), "expiry", bs.Expiry)
	s.ctx.SetStatusMessage("Run until "+bs.Label, 0, true)
}

// regionLabel renders an empty region filter as the wildcard it is
func regionLabel(r string) string {
	if r == "" {
		return "any"
	}
	return r
}
