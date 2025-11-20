package modes

import (
	"fmt"
	"reflect"
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

	// Reset cursor position to center
	ctx.CursorX = ctx.GameWidth / 2
	ctx.CursorY = ctx.GameHeight / 2
	ctx.State.SetCursorX(ctx.CursorX)
	ctx.State.SetCursorY(ctx.CursorY)

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
	if len(args) != 1 {
		setCommandError(ctx, "Invalid arguments for heat")
		return true
	}

	value, err := strconv.Atoi(args[0])
	if err != nil {
		setCommandError(ctx, "Invalid arguments for heat")
		return true
	}

	if value < 0 || value > 100 {
		setCommandError(ctx, "Value out of range for heat")
		return true
	}

	ctx.State.SetHeat(value)
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
func setCommandError(ctx *engine.GameContext, message string) {
	ctx.StatusMessage = message
	// Note: The status message timeout will be handled by the renderer
	// which should check the message timestamp and clear it after 2 seconds
}

// clearAllEntities removes all entities from the world
func clearAllEntities(world *engine.World) {
	// Collect all unique entity IDs from different component queries
	// Use a map to avoid duplicates since some entities have multiple components
	entitiesToDelete := make(map[engine.Entity]bool)

	// Query for all entities with PositionComponent (most game entities)
	posType := reflect.TypeOf(components.PositionComponent{})
	posEntities := world.GetEntitiesWith(posType)
	for _, entity := range posEntities {
		entitiesToDelete[entity] = true
	}

	// Query for entities with SequenceComponent (characters, sequences)
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqEntities := world.GetEntitiesWith(seqType)
	for _, entity := range seqEntities {
		entitiesToDelete[entity] = true
	}

	// Query for entities with FallingDecayComponent (falling decay)
	fallingType := reflect.TypeOf(components.FallingDecayComponent{})
	fallingEntities := world.GetEntitiesWith(fallingType)
	for _, entity := range fallingEntities {
		entitiesToDelete[entity] = true
	}

	// Query for cleaners
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleanerEntities := world.GetEntitiesWith(cleanerType)
	for _, entity := range cleanerEntities {
		entitiesToDelete[entity] = true
	}

	// Query for nuggets
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	nuggetEntities := world.GetEntitiesWith(nuggetType)
	for _, entity := range nuggetEntities {
		entitiesToDelete[entity] = true
	}

	// Query for drain entities
	drainType := reflect.TypeOf(components.DrainComponent{})
	drainEntities := world.GetEntitiesWith(drainType)
	for _, entity := range drainEntities {
		entitiesToDelete[entity] = true
	}

	// Query for removal flash effects
	flashType := reflect.TypeOf(components.RemovalFlashComponent{})
	flashEntities := world.GetEntitiesWith(flashType)
	for _, entity := range flashEntities {
		entitiesToDelete[entity] = true
	}

	// Now delete all collected entities
	for entity := range entitiesToDelete {
		world.SafeDestroyEntity(entity)
	}
}
