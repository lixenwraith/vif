package modes

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
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
	case "score":
		return handleScoreCommand(ctx, args)
	case "heat":
		return handleHeatCommand(ctx, args)
	case "boost":
		return handleBoostCommand(ctx)
	case "spawn":
		return handleSpawnCommand(ctx, args)
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
	// Reset score and heat
	ctx.State.SetScore(0)
	ctx.State.SetHeat(0)

	// Clear all entities from the world
	clearAllEntities(ctx.World)

	// Use Entity Builder for singleton CursorEntity creation
	// 1. Create new entity ID
	newCursorID := ctx.World.NewEntity().Build()
	ctx.CursorEntity = newCursorID

	// 2. Add Components
	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
		X: ctx.GameWidth / 2,
		Y: ctx.GameHeight / 2,
	})

	ctx.World.Cursors.Add(ctx.CursorEntity, components.CursorComponent{})

	// 3. Restore Protection (Critical for DestroyEntity checks)
	ctx.World.Protections.Add(ctx.CursorEntity, components.ProtectionComponent{
		Mask:      components.ProtectAll,
		ExpiresAt: 0,
	})

	// Reset boost state
	ctx.State.SetBoostEnabled(false)
	ctx.State.SetBoostEndTime(time.Time{})
	ctx.State.SetBoostColor(0)

	// Reset drain state
	ctx.State.SetDrainActive(false)
	ctx.State.SetDrainEntity(0)

	// Reset visual feedback
	ctx.State.SetCursorError(false)
	ctx.State.SetScoreBlinkActive(false)

	// Reset game lifecycle flags
	ctx.State.SetInitialSpawnComplete()

	// Display success message
	ctx.LastCommand = ":new"
	return true
}

// handleScoreCommand sets the score to a specified value
func handleScoreCommand(ctx *engine.GameContext, args []string) bool {
	if len(args) != 1 {
		setCommandError(ctx, "Invalid arguments for score")
		return true
	}

	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid arguments for score")
		return true
	}

	if value < 0 {
		setCommandError(ctx, "Value out of range for score")
		return true
	}

	ctx.State.SetScore(value)
	ctx.LastCommand = fmt.Sprintf(":score %d", value)
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
	// We allow setting it up to GameWidth because that is the trigger for Boost.
	if value < 0 {
		value = 0
	}
	if value > constants.MaxHeat {
		value = constants.MaxHeat
	}

	// 4. Update State
	ctx.State.SetHeat(value)

	// 5. Update Feedback
	ctx.LastCommand = fmt.Sprintf(":heat %d", value)
	return true
}

// handleBoostCommand enables boost for 10 seconds
func handleBoostCommand(ctx *engine.GameContext) bool {
	now := ctx.TimeProvider.Now()
	endTime := now.Add(constants.BoostBaseDuration)

	ctx.State.SetBoostEnabled(true)
	ctx.State.SetBoostEndTime(endTime)
	// Default to blue boost (1)
	ctx.State.SetBoostColor(1)

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
// This string will be cleared by InputHandler on the next keystroke.
func setCommandError(ctx *engine.GameContext, message string) {
	ctx.StatusMessage = message
}

// clearAllEntities removes all entities from the world
func clearAllEntities(world *engine.World) {
	// Use the world's Clear method to remove all entities
	world.Clear()
}