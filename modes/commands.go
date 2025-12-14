package modes

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
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
	case "god":
		return handleGodCommand(ctx)
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

// handleNewCommand resets the game state via event
func handleNewCommand(ctx *engine.GameContext) bool {
	ctx.PushEvent(events.EventGameReset, nil, ctx.PausableClock.Now())
	ctx.SetLastCommand(":new")
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

	ctx.SetLastCommand(fmt.Sprintf(":energy %d", value))
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
	ctx.SetLastCommand(fmt.Sprintf(":heat %d", value))

	// 5. Push event for HeatSystem to process
	ctx.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: value}, ctx.PausableClock.Now())

	return true
}

// handleBoostCommand triggers boost request event
func handleBoostCommand(ctx *engine.GameContext) bool {
	now := ctx.PausableClock.Now()

	ctx.PushEvent(events.EventHeatSet, &events.HeatSetPayload{
		Value: constants.MaxHeat,
	}, now)

	ctx.PushEvent(events.EventBoostActivate, &events.BoostActivatePayload{
		Duration: constants.BoostBaseDuration,
	}, now)

	ctx.SetLastCommand(":boost")
	return true
}

// handleGodCommand sets heat to max and energy to high value
func handleGodCommand(ctx *engine.GameContext) bool {
	now := ctx.PausableClock.Now()
	ctx.PushEvent(events.EventHeatSet, &events.HeatSetPayload{Value: constants.MaxHeat}, now)
	ctx.PushEvent(events.EventEnergySet, &events.EnergySetPayload{Value: constants.GodEnergyAmount}, now)
	ctx.SetLastCommand(":god")
	return true
}

// handleSpawnCommand enables or disables entity spawning via event
func handleSpawnCommand(ctx *engine.GameContext, args []string) bool {
	if len(args) != 1 {
		setCommandError(ctx, "Invalid arguments for spawn")
		return true
	}

	arg := strings.ToLower(args[0])
	switch arg {
	case "on":
		ctx.PushEvent(events.EventSpawnChange, &events.SpawnChangePayload{Enabled: true}, ctx.PausableClock.Now())
		ctx.SetLastCommand(":spawn on")
	case "off":
		ctx.PushEvent(events.EventSpawnChange, &events.SpawnChangePayload{Enabled: false}, ctx.PausableClock.Now())
		ctx.SetLastCommand(":spawn off")
	default:
		setCommandError(ctx, "Invalid arguments for spawn")
	}

	return true
}

// setCommandError sets an error message in the status message
// This string will be cleared by InputHandler on the next keystroke
func setCommandError(ctx *engine.GameContext, message string) {
	ctx.SetStatusMessage(message)
}

// handleDebugCommand triggers debug overlay event
func handleDebugCommand(ctx *engine.GameContext) bool {
	// Synchronous mode switch to prevent InputHandler from reverting mode
	ctx.SetMode(core.ModeOverlay)
	ctx.PushEvent(events.EventDebugRequest, nil, ctx.PausableClock.Now())
	return true
}

// handleHelpCommand triggers help overlay event
func handleHelpCommand(ctx *engine.GameContext) bool {
	// Synchronous mode switch to prevent InputHandler from reverting mode
	ctx.SetMode(core.ModeOverlay)
	ctx.PushEvent(events.EventHelpRequest, nil, ctx.PausableClock.Now())
	return true
}