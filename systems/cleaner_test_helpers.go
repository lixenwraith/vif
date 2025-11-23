package systems

import (
	"reflect"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// Helper functions for cleaner system tests

func createCleanerTestContext() *engine.GameContext {
	return engine.NewTestGameContext(80, 24, 100)
}

func createRedCharacterAt(world *engine.World, x, y int) engine.Entity {
	entity := world.CreateEntity()

	world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'R',
		Style: render.GetStyleForSequence(components.SequenceRed, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceRed,
		Level: components.LevelBright,
	})

	// Use spatial transaction for atomic spawn
	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, x, y)
	tx.Commit()
	return entity
}

func createBlueCharacterAt(world *engine.World, x, y int) engine.Entity {
	entity := world.CreateEntity()

	world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'B',
		Style: render.GetStyleForSequence(components.SequenceBlue, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    2,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelBright,
	})

	// Use spatial transaction for atomic spawn
	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, x, y)
	tx.Commit()
	return entity
}

func createGreenCharacterAt(world *engine.World, x, y int) engine.Entity {
	entity := world.CreateEntity()

	world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
	world.AddComponent(entity, components.CharacterComponent{
		Rune:  'G',
		Style: render.GetStyleForSequence(components.SequenceGreen, components.LevelBright),
	})
	world.AddComponent(entity, components.SequenceComponent{
		ID:    3,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})

	// Use spatial transaction for atomic spawn
	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, x, y)
	tx.Commit()
	return entity
}

func entityExists(world *engine.World, entity engine.Entity) bool {
	posType := reflect.TypeOf(components.PositionComponent{})
	_, exists := world.GetComponent(entity, posType)
	return exists
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
