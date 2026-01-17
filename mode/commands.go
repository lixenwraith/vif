package mode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/manifest"
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
	case "s", "system":
		return handleSystemCommand(ctx, args)
	case "energy":
		return handleEnergyCommand(ctx, args)
	case "heat":
		return handleHeatCommand(ctx, args)
	case "boost":
		return handleBoostCommand(ctx)
	case "god":
		return handleGodCommand(ctx)
	case "demon":
		return handleDemonCommand(ctx)
	case "blossom":
		return handleBlossomCommand(ctx)
	case "decay":
		return handleDecayCommand(ctx)
	case "cleaner":
		return handleCleanerCommand(ctx)
	case "dust":
		return handleDustCommand(ctx)
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

// handleSystemCommand sets the energy to a specified value
func handleSystemCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 2 {
		setCommandError(ctx, "Invalid arguments for system")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	validSystem := false
	for _, s := range manifest.ActiveSystems() {
		if args[0] == s {
			validSystem = true
			break
		}
	}
	if !validSystem {
		setCommandError(ctx, fmt.Sprintf("Invalid system: %s", args[0]))
		return CommandResult{Continue: true, KeepPaused: false}
	}

	enabledFlag := false
	switch args[1] {
	case "e", "enable", "enabled":
		enabledFlag = true
	case "d", "disable", "disabled":
		enabledFlag = false
	default:
		setCommandError(ctx, fmt.Sprintf("Invalid system: %s", args[0]))
		return CommandResult{Continue: true, KeepPaused: false}
	}

	ctx.PushEvent(event.EventMetaSystemCommandRequest, &event.MetaSystemCommandPayload{
		SystemName: args[0],
		Enabled:    enabledFlag,
	})

	ctx.SetLastCommand(fmt.Sprintf(":system %s %v", args[0], enabledFlag))
	return CommandResult{Continue: true, KeepPaused: false}
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

	ctx.PushEvent(event.EventEnergySetAmount, &event.EnergySetAmountPayload{
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
	ctx.PushEvent(event.EventEnergySetAmount, &event.EnergySetAmountPayload{Value: constant.GodEnergyAmount})
	ctx.SetLastCommand(":god")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleDemonCommand sets heat to max and energy to high value
func handleDemonCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventHeatSet, &event.HeatSetPayload{Value: constant.MaxHeat})
	ctx.PushEvent(event.EventEnergySetAmount, &event.EnergySetAmountPayload{Value: -constant.GodEnergyAmount})
	ctx.SetLastCommand(":demon")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleBlossomCommand triggers a blossom wave
func handleBlossomCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventBlossomWave, nil)
	ctx.SetLastCommand(":blossom")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleDecayCommand triggers a decay wave
func handleDecayCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventDecayWave, nil)
	ctx.SetLastCommand(":decay")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleCleanerCommand triggers sweeping cleaners
func handleCleanerCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventCleanerSweepingRequest, nil)
	ctx.SetLastCommand(":cleaner")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleDustCommand triggers glyph to dust transform
func handleDustCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventDustAll, nil)
	ctx.SetLastCommand(":dust")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleDebugCommand triggers debug overlay event
func handleDebugCommand(ctx *engine.GameContext) CommandResult {
	ctx.SetMode(core.ModeOverlay)
	ctx.PushEvent(event.EventMetaDebugRequest, nil)
	return CommandResult{Continue: true, KeepPaused: true}
}

// handleHelpCommand triggers help overlay event
func handleHelpCommand(ctx *engine.GameContext) CommandResult {
	ctx.SetMode(core.ModeOverlay)
	ctx.PushEvent(event.EventMetaHelpRequest, nil)
	return CommandResult{Continue: true, KeepPaused: true}
}