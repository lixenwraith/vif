package mode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// CommandResult represents the outcome of command execution
type CommandResult struct {
	Continue   bool // false = exit game
	KeepPaused bool // true = caller should not unpause
}

// ExecuteCommand parses and executes a command string
// Returns CommandResult indicating whether game should continue and pause state
func ExecuteCommand(ctx *engine.GameContext, command string) CommandResult {
	command = strings.TrimSpace(command)
	if command == "" {
		return CommandResult{Continue: true, KeepPaused: false}
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
		return CommandResult{Continue: true, KeepPaused: false}
	}
}

// setCommandError sets an error message in the status message
// This string will be cleared by InputHandler on the next keystroke
func setCommandError(ctx *engine.GameContext, message string) {
	ctx.SetStatusMessage(message)
}

// handleQuitCommand exits the game
func handleQuitCommand(ctx *engine.GameContext) CommandResult {
	return CommandResult{Continue: false, KeepPaused: true}
}

// handleNewCommand resets the game state via event
func handleNewCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventGameReset, nil)
	ctx.SetLastCommand(":new")
	return CommandResult{Continue: true, KeepPaused: true}
}

// handleEnergyCommand sets the energy to a specified value
func handleEnergyCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 1 {
		setCommandError(ctx, "Invalid arguments for energy")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid arguments for energy")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	ctx.PushEvent(event.EventEnergySet, &event.EnergySetPayload{
		Value: value,
	})

	ctx.SetLastCommand(fmt.Sprintf(":energy %d", value))
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleHeatCommand sets the heat to a specified value
func handleHeatCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 1 {
		setCommandError(ctx, "Usage: :heat <0-100>")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid number format")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	if value < 0 {
		value = 0
	}
	if value > constant.MaxHeat {
		value = constant.MaxHeat
	}

	ctx.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: value})
	ctx.SetLastCommand(fmt.Sprintf(":heat %d", value))

	return CommandResult{Continue: true, KeepPaused: false}
}

// handleBoostCommand triggers boost request event
func handleBoostCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventHeatSet, &event.HeatSetPayload{
		Value: constant.MaxHeat,
	})

	ctx.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
		Duration: constant.BoostBaseDuration,
	})

	ctx.SetLastCommand(":boost")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleGodCommand sets heat to max and energy to high value
func handleGodCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: constant.MaxHeat})
	ctx.PushEvent(event.EventEnergySet, &event.EnergySetPayload{Value: constant.GodEnergyAmount})
	ctx.SetLastCommand(":god")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleSpawnCommand enables or disables entity spawning via event
func handleSpawnCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 1 {
		setCommandError(ctx, "Invalid arguments for spawn")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	arg := strings.ToLower(args[0])
	switch arg {
	case "on":
		ctx.PushEvent(event.EventSpawnChange, &event.SpawnChangePayload{Enabled: true})
		ctx.SetLastCommand(":spawn on")
	case "off":
		ctx.PushEvent(event.EventSpawnChange, &event.SpawnChangePayload{Enabled: false})
		ctx.SetLastCommand(":spawn off")
	default:
		setCommandError(ctx, "Invalid arguments for spawn")
	}

	return CommandResult{Continue: true, KeepPaused: false}
}

// handleDebugCommand triggers debug overlay event
func handleDebugCommand(ctx *engine.GameContext) CommandResult {
	ctx.SetMode(core.ModeOverlay)
	ctx.PushEvent(event.EventDebugRequest, nil)
	return CommandResult{Continue: true, KeepPaused: true}
}

// handleHelpCommand triggers help overlay event
func handleHelpCommand(ctx *engine.GameContext) CommandResult {
	ctx.SetMode(core.ModeOverlay)
	ctx.PushEvent(event.EventHelpRequest, nil)
	return CommandResult{Continue: true, KeepPaused: true}
}