package mode

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/manifest"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/toml"
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
	case "flow":
		ctx.World.PushEvent(event.EventDebugFlowToggle, nil)
		return CommandResult{Continue: true, KeepPaused: false}
	case "graph":
		ctx.World.PushEvent(event.EventDebugGraphToggle, nil)
		return CommandResult{Continue: true, KeepPaused: false}
	case "q", "quit":
		return handleQuitCommand(ctx)
	case "n", "new":
		return handleNewCommand(ctx)
	case "s", "system":
		return handleSystemCommand(ctx, args)
	case "m", "mouse":
		return handleMouseCommand(ctx, args)
	case "e", "emit", "event":
		return handleEmitCommand(ctx, args)
	case "d", "debug":
		return handleDebugCommand(ctx)
	case "h", "help", "?":
		return handleHelpCommand(ctx)
	case "a", "about":
		return handleAboutCommand(ctx)
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
	default:
		setCommandError(ctx, fmt.Sprintf("Unknown command: %s", cmd))
		return CommandResult{Continue: true, KeepPaused: false}
	}
}

// setCommandError sets an error message in the status message
// This string will be cleared by InputHandler on the next keystroke
func setCommandError(ctx *engine.GameContext, message string) {
	ctx.SetStatusMessage(message, 0, false)
}

// handleQuitCommand exits the game
func handleQuitCommand(ctx *engine.GameContext) CommandResult {
	return CommandResult{Continue: false, KeepPaused: true}
}

// handleNewCommand resets the game state via event
func handleNewCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventGameReset, nil)
	ctx.SetLastCommand(":new")
	ctx.MacroClearFlag.Store(true) // Signal macro reset
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

func handleMouseCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) != 1 {
		setCommandError(ctx, "Usage: :mouse free|auto|enable|disable")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	var msg string
	switch args[0] {
	case "free":
		newState := !ctx.MouseFreeMode.Load()
		ctx.MouseFreeMode.Store(newState)
		if newState {
			msg = "Mouse free mode enabled"
		} else {
			msg = "Mouse free mode disabled"
		}

	case "auto":
		newState := !ctx.MouseAutoMode.Load()
		ctx.MouseAutoMode.Store(newState)
		if newState {
			msg = "Mouse auto-fire enabled"
		} else {
			msg = "Mouse auto-fire disabled"
		}

	case "enable":
		if ctx.MouseDisabled.Load() {
			ctx.MouseDisabled.Store(false)
			msg = "Mouse enabled"
		} else {
			msg = "Mouse already enabled"
		}

	case "disable":
		if !ctx.MouseDisabled.Load() {
			ctx.MouseDisabled.Store(true)
			msg = "Mouse disabled"
		} else {
			msg = "Mouse already disabled"
		}

	default:
		setCommandError(ctx, "Usage: :mouse free|auto|enable|disable")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	ctx.SetStatusMessage(msg, parameter.StatusMessageDefaultTimeout, false)
	ctx.SetLastCommand(":mouse " + args[0])
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleEmitCommand emits an event by name with optional TOML payload (debug/testing)
// Usage: :emit EventName
// Usage: :emit EventName { field = value, nested = { x = 1 } }
func handleEmitCommand(ctx *engine.GameContext, args []string) CommandResult {
	if len(args) < 1 {
		setCommandError(ctx, "Usage: :emit <EventName> [{ payload }]")
		return CommandResult{Continue: true, KeepPaused: false}
	}

	name := args[0]

	// Normalize: add "Event" prefix if missing
	if !strings.HasPrefix(name, "Event") {
		name = "Event" + name
	}

	eventType, ok := event.GetEventType(name)
	if !ok {
		setCommandError(ctx, fmt.Sprintf("Unknown event: %s", name))
		return CommandResult{Continue: true, KeepPaused: false}
	}

	// Parse payload if provided
	var payload any
	if len(args) > 1 {
		payloadStr := strings.Join(args[1:], " ")
		var err error
		payload, err = parseEventPayload(eventType, payloadStr)
		if err != nil {
			setCommandError(ctx, fmt.Sprintf("Payload error: %v", err))
			return CommandResult{Continue: true, KeepPaused: false}
		}
	}

	ctx.PushEvent(eventType, payload)
	ctx.SetLastCommand(fmt.Sprintf(":emit %s", strings.Join(args, " ")))

	return CommandResult{Continue: true, KeepPaused: false}
}

// parseEventPayload parses an inline TOML table string into the typed payload struct
// Input: "{ field = value, ... }" or empty string
// Returns: typed payload pointer or nil
func parseEventPayload(et event.EventType, raw string) (any, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}

	// Get typed payload struct for this event
	payload := event.NewPayloadStruct(et)
	if payload == nil {
		return nil, fmt.Errorf("event does not accept payload")
	}

	// Wrap inline table as TOML key-value for parser compatibility
	wrapped := "_p = " + raw

	p := toml.NewParser([]byte(wrapped))
	parsed, err := p.Parse()
	if err != nil {
		return nil, err
	}

	payloadMap, ok := parsed["_p"]
	if !ok {
		return nil, fmt.Errorf("malformed payload")
	}

	if err := toml.Decode(payloadMap, payload); err != nil {
		return nil, err
	}

	return payload, nil
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

// handleAboutCommand triggers about overlay event
func handleAboutCommand(ctx *engine.GameContext) CommandResult {
	ctx.SetMode(core.ModeOverlay)
	ctx.PushEvent(event.EventMetaAboutRequest, nil)
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

	ctx.PushEvent(event.EventEnergySetRequest, &event.EnergySetPayload{
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
	if value > parameter.HeatMax {
		value = parameter.HeatMax
	}

	ctx.PushEvent(event.EventHeatSetRequest, &event.HeatSetRequestPayload{Value: value})
	ctx.SetLastCommand(fmt.Sprintf(":heat %d", value))

	return CommandResult{Continue: true, KeepPaused: false}
}

// handleBoostCommand triggers boost request event
func handleBoostCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventHeatSetRequest, &event.HeatSetRequestPayload{
		Value: parameter.HeatMax,
	})

	ctx.PushEvent(event.EventBoostActivate, &event.BoostActivatePayload{
		Duration: parameter.BoostBaseDuration,
	})

	ctx.SetLastCommand(":boost")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleGodCommand sets heat to max and energy to high value
func handleGodCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventHeatSetRequest, &event.HeatSetRequestPayload{Value: parameter.HeatMax})
	ctx.PushEvent(event.EventEnergySetRequest, &event.EnergySetPayload{Value: parameter.GodEnergyAmount})
	ctx.PushEvent(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Weapon: component.WeaponRod})
	ctx.PushEvent(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Weapon: component.WeaponLauncher})
	ctx.PushEvent(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Weapon: component.WeaponDisruptor})
	ctx.SetLastCommand(":god")
	return CommandResult{Continue: true, KeepPaused: false}
}

// handleDemonCommand sets heat to max and energy to high value
func handleDemonCommand(ctx *engine.GameContext) CommandResult {
	ctx.PushEvent(event.EventHeatSetRequest, &event.HeatSetRequestPayload{Value: parameter.HeatMax})
	ctx.PushEvent(event.EventEnergySetRequest, &event.EnergySetPayload{Value: -parameter.GodEnergyAmount})
	ctx.PushEvent(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Weapon: component.WeaponRod})
	ctx.PushEvent(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Weapon: component.WeaponLauncher})
	ctx.PushEvent(event.EventWeaponAddRequest, &event.WeaponAddRequestPayload{Weapon: component.WeaponDisruptor})
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
	ctx.PushEvent(event.EventDustAllRequest, nil)
	ctx.SetLastCommand(":dust")
	return CommandResult{Continue: true, KeepPaused: false}
}