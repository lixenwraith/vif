package modes

import (
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// OpMove updates cursor position based on motion result
// Handles consecutive move penalty (heat reset)
func OpMove(ctx *engine.GameContext, result MotionResult, cmd rune) {
	if !result.Valid {
		return
	}

	ctx.World.Positions.Add(ctx.CursorEntity, components.PositionComponent{
		X: result.EndX,
		Y: result.EndY,
	})
}

// OpDelete destroys entities in range, returns true if green/blue deleted
func OpDelete(ctx *engine.GameContext, result MotionResult) bool {
	if !result.Valid {
		return false
	}

	if result.Type == RangeLine {
		return deleteLineRange(ctx, result.StartY, result.EndY)
	}

	// Normalize direction
	startX, endX := result.StartX, result.EndX
	if startX > endX {
		startX, endX = endX, startX
	}

	// Adjust for exclusive motions (endpoint not included)
	if result.Style == StyleExclusive && endX > startX {
		endX--
	}

	return deleteRange(ctx, startX, endX, result.StartY)
}

// deleteLineRange deletes all entities on lines from startY to endY inclusive
func deleteLineRange(ctx *engine.GameContext, startY, endY int) bool {
	if startY > endY {
		startY, endY = endY, startY
	}
	deleted := false
	for y := startY; y <= endY; y++ {
		if deleteAllOnLine(ctx, y) {
			deleted = true
		}
	}
	return deleted
}

// deleteAllOnLine deletes all interactable entities on a line
func deleteAllOnLine(ctx *engine.GameContext, y int) bool {
	entities := ctx.World.Query().With(ctx.World.Positions).Execute()

	deletedGreenOrBlue := false
	entitiesToDelete := make([]engine.Entity, 0)

	for _, entity := range entities {
		pos, _ := ctx.World.Positions.Get(entity)
		if pos.Y != y {
			continue
		}
		if !engine.IsInteractable(ctx.World, entity) {
			continue
		}
		if prot, ok := ctx.World.Protections.Get(entity); ok {
			if prot.Mask.Has(components.ProtectFromDelete) || prot.Mask == components.ProtectAll {
				continue
			}
		}
		if seq, ok := ctx.World.Sequences.Get(entity); ok {
			if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
				deletedGreenOrBlue = true
			}
		}
		entitiesToDelete = append(entitiesToDelete, entity)
	}

	for _, entity := range entitiesToDelete {
		ctx.World.DestroyEntity(entity)
	}

	return deletedGreenOrBlue
}

// deleteRange deletes all interactable entities in a range on a line
func deleteRange(ctx *engine.GameContext, startX, endX, y int) bool {
	deletedGreenOrBlue := false
	entitiesToDelete := make([]engine.Entity, 0)

	if startX > endX {
		startX, endX = endX, startX
	}

	for x := startX; x <= endX; x++ {
		entities := ctx.World.Positions.GetAllAt(x, y)
		for _, entity := range entities {
			if !engine.IsInteractable(ctx.World, entity) {
				continue
			}
			if prot, ok := ctx.World.Protections.Get(entity); ok {
				if prot.Mask.Has(components.ProtectFromDelete) || prot.Mask == components.ProtectAll {
					continue
				}
			}
			if seq, ok := ctx.World.Sequences.Get(entity); ok {
				if seq.Type == components.SequenceGreen || seq.Type == components.SequenceBlue {
					deletedGreenOrBlue = true
				}
			}
			entitiesToDelete = append(entitiesToDelete, entity)
		}
	}

	for _, entity := range entitiesToDelete {
		ctx.World.DestroyEntity(entity)
	}

	return deletedGreenOrBlue
}