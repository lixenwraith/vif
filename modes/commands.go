package modes

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// ExecuteCommand executes a command and returns false if the game should exit
func ExecuteCommand(ctx *engine.GameContext, command string) bool {
	// Trim whitespace
	command = strings.TrimSpace(command)
	if command == "" {
		return true
	}

	// Parse command into parts (space-separated)
	parts := strings.Fields(command)
	cmd := parts[0]
	args := parts[1:]

	// Execute based on command
	switch cmd {
	case "q", "quit":
		return handleQuitCommand(ctx)
	case "n", "new":
		return handleNewCommand(ctx)
	case "energy":
		return handleEnergyCommand(ctx, args)
	case "heat":
		return handleHeatCommand(ctx, args)
	case "boost":
		return handleBoostCommand(ctx)
	case "spawn":
		return handleSpawnCommand(ctx, args)
	case "d", "debug":
		return handleDebugCommand(ctx)
	case "h", "help", "?":
		return handleHelpCommand(ctx)
	default:
		setCommandError(ctx, fmt.Sprintf("Unknown command: %s", cmd))
		return true
	}
}

// handleQuitCommand exits the game
func handleQuitCommand(ctx *engine.GameContext) bool {
	return false // Signal game exit
}

// handleNewCommand resets the game state
func handleNewCommand(ctx *engine.GameContext) bool {
	// Despawn drain entities before clearing world
	drains := ctx.World.Drains.All()
	for _, e := range drains {
		ctx.World.DestroyEntity(e)
	}

	// Clear all entities from the world
	clearAllEntities(ctx.World)

	// Clear any pending events from previous session
	ctx.ResetEventQueue()

	// Reset entire game state using unified initState() method (same as app start)
	ctx.State.Reset(ctx.PausableClock.Now())

	// Recreate cursor entity
	ctx.CursorEntity = ctx.World.CreateEntity()

	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
		X: ctx.GameWidth / 2,
		Y: ctx.GameHeight / 2,
	})

	ctx.World.Cursors.Add(ctx.CursorEntity, components.CursorComponent{})

	ctx.World.Protections.Add(ctx.CursorEntity, components.ProtectionComponent{
		Mask:      components.ProtectAll,
		ExpiresAt: 0,
	})

	ctx.LastCommand = ":new"
	return true
}

// handleEnergyCommand sets the energy to a specified value
func handleEnergyCommand(ctx *engine.GameContext, args []string) bool {
	if len(args) != 1 {
		setCommandError(ctx, "Invalid arguments for energy")
		return true
	}

	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid arguments for energy")
		return true
	}

	if value < 0 {
		setCommandError(ctx, "Value out of range for energy")
		return true
	}

	ctx.PushEvent(events.EventEnergySet, &events.EnergySetPayload{
		Value: value,
	}, ctx.PausableClock.Now())

	ctx.LastCommand = fmt.Sprintf(":energy %d", value)
	return true
}

// handleHeatCommand sets the heat to a specified value
func handleHeatCommand(ctx *engine.GameContext, args []string) bool {
	// 1. Argument Validation
	if len(args) != 1 {
		setCommandError(ctx, "Usage: :heat <0-100>")
		return true
	}

	// 2. Parse Value
	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid number format")
		return true
	}

	// 3. Logic Validation (0-MaxHeat)
	if value < 0 {
		value = 0
	}
	if value > constants.MaxHeat {
		value = constants.MaxHeat
	}

	// 4. Update Feedback first (visible immediately)
	ctx.LastCommand = fmt.Sprintf(":heat %d", value)

	// 5. Update State last (atomic, takes effect on next tick)
	ctx.State.SetHeat(value)

	return true
}

// handleBoostCommand enables boost for 10 seconds
func handleBoostCommand(ctx *engine.GameContext) bool {
	now := ctx.PausableClock.Now()
	endTime := now.Add(constants.BoostBaseDuration)

	// Maximize heat to ensure consistent gameplay state (Boost implies Max Heat)
	ctx.State.SetHeat(constants.MaxHeat)

	// CRITICAL: Set end time BEFORE enabling boost to prevent race condition
	// BoostSystem.UpdateBoostTimerAtomic() checks Enabled first, then reads EndTime
	// If we set Enabled=true before EndTime, the system may read stale EndTime
	// and immediately disable boost
	ctx.State.SetBoostEndTime(endTime)
	ctx.State.SetBoostColor(1) // Default to blue boost
	ctx.State.SetBoostEnabled(true)

	ctx.LastCommand = ":boost"
	return true
}

// handleSpawnCommand enables or disables entity spawning
func handleSpawnCommand(ctx *engine.GameContext, args []string) bool {
	if len(args) != 1 {
		setCommandError(ctx, "Invalid arguments for spawn")
		return true
	}

	arg := strings.ToLower(args[0])
	switch arg {
	case "on":
		ctx.State.SetSpawnEnabled(true)
		ctx.LastCommand = ":spawn on"
	case "off":
		ctx.State.SetSpawnEnabled(false)
		ctx.LastCommand = ":spawn off"
	default:
		setCommandError(ctx, "Invalid arguments for spawn")
	}

	return true
}

// setCommandError sets an error message in the status message
// This string will be cleared by InputHandler on the next keystroke
func setCommandError(ctx *engine.GameContext, message string) {
	ctx.StatusMessage = message
}

// clearAllEntities removes all entities from the world
func clearAllEntities(world *engine.World) {
	// Use the world's Clear method to remove all entities
	world.Clear()
}

// handleDebugCommand shows debug information overlay
func handleDebugCommand(ctx *engine.GameContext) bool {
	// Gather debug stats

	energyComp, _ := ctx.World.Energies.Get(ctx.CursorEntity)
	energyVal := energyComp.Current.Load()

	debugContent := []string{
		"=== DEBUG INFORMATION ===",
		"",
		fmt.Sprintf("Energy:        %d", energyVal),
		fmt.Sprintf("Heat:          %d / %d", ctx.State.GetHeat(), constants.MaxHeat),
		fmt.Sprintf("FPS:           %d", ctx.State.GetGameTicks()), // Approximate
		fmt.Sprintf("Game Ticks:    %d", ctx.State.GetGameTicks()),
		fmt.Sprintf("APM:           %d", ctx.State.GetAPM()),
		fmt.Sprintf("Frame Number:  %d", ctx.GetFrameNumber()),
		"",
		fmt.Sprintf("Screen Size:   %dx%d", ctx.Width, ctx.Height),
		fmt.Sprintf("Game Area:     %dx%d", ctx.GameWidth, ctx.GameHeight),
		fmt.Sprintf("Game Offset:   (%d, %d)", ctx.GameX, ctx.GameY),
		"",
		fmt.Sprintf("Spawn Enabled: %v", ctx.State.GetSpawnEnabled()),
		fmt.Sprintf("Boost Active:  %v", ctx.State.GetBoostEnabled()),
		fmt.Sprintf("Paused:        %v", ctx.IsPaused.Load()),
		"",
		"Entity Counts:",
		fmt.Sprintf("  Characters:  %d", len(ctx.World.Characters.All())),
		fmt.Sprintf("  Nuggets:     %d", len(ctx.World.Nuggets.All())),
		fmt.Sprintf("  Drains:      %d", len(ctx.World.Drains.All())),
		fmt.Sprintf("  Cleaners:    %d", len(ctx.World.Cleaners.All())),
		fmt.Sprintf("  Decays:      %d", len(ctx.World.Decays.All())),
		"",
		"Press ESC or ENTER to close",
	}

	// Set overlay state
	ctx.OverlayActive = true
	ctx.OverlayTitle = " DEBUG "
	ctx.OverlayContent = debugContent
	ctx.OverlayScroll = 0

	// Switch to overlay mode
	ctx.Mode = engine.ModeOverlay

	return true
}

// handleHelpCommand shows help information overlay
func handleHelpCommand(ctx *engine.GameContext) bool {
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
		"  :spawn on/off - Enable/disable spawning",
		"  :d/:debug - Show debug info",
		"  :h/:help  - Show this help",
		"",
		"AUDIO:",
		"  Ctrl+S    - Toggle mute",
		"",
		"Press ESC or ENTER to close",
	}

	// Set overlay state
	ctx.OverlayActive = true
	ctx.OverlayTitle = " HELP "
	ctx.OverlayContent = helpContent
	ctx.OverlayScroll = 0

	// Switch to overlay mode
	ctx.Mode = engine.ModeOverlay

	return true
}