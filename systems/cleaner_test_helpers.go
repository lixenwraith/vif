package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// Helper functions for cleaner system tests

func createCleanerTestContext() *engine.GameContext {
	ctx := engine.NewTestGameContext(80, 24, 100)
	// Inject required resources for migrated systems
	engine.AddResource(ctx.World.Resources, &engine.ConfigResource{
		GameWidth:    80,
		GameHeight:   24,
		ScreenWidth:  80,
		ScreenHeight: 24,
	})
	engine.AddResource(ctx.World.Resources, &engine.TimeResource{
		GameTime:  time.Now(),
		DeltaTime: 16 * time.Millisecond,
	})
	return ctx
}

func createRedCharacterAt(world *engine.World, x, y int) engine.Entity {
	entity := world.CreateEntity()

	world.Characters.Add(entity, components.CharacterComponent{
		Rune:  'R',
		Style: render.GetStyleForSequence(components.SequenceRed, components.LevelBright),
	})
	world.Sequences.Add(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceRed,
		Level: components.LevelBright,
	})

	// Use spatial transaction for atomic spawn (also adds PositionComponent)
	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, x, y)
	tx.Commit()
	return entity
}

func createBlueCharacterAt(world *engine.World, x, y int) engine.Entity {
	entity := world.CreateEntity()

	world.Characters.Add(entity, components.CharacterComponent{
		Rune:  'B',
		Style: render.GetStyleForSequence(components.SequenceBlue, components.LevelBright),
	})
	world.Sequences.Add(entity, components.SequenceComponent{
		ID:    2,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelBright,
	})

	// Use spatial transaction for atomic spawn (also adds PositionComponent)
	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, x, y)
	tx.Commit()
	return entity
}

func createGreenCharacterAt(world *engine.World, x, y int) engine.Entity {
	entity := world.CreateEntity()

	world.Characters.Add(entity, components.CharacterComponent{
		Rune:  'G',
		Style: render.GetStyleForSequence(components.SequenceGreen, components.LevelBright),
	})
	world.Sequences.Add(entity, components.SequenceComponent{
		ID:    3,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})

	// Use spatial transaction for atomic spawn (also adds PositionComponent)
	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, x, y)
	tx.Commit()
	return entity
}

func entityExists(world *engine.World, entity engine.Entity) bool {
	return world.HasAnyComponent(entity)
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
