package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDrainSystem_CollisionWithBlueCharacter tests drain destroys blue characters
func TestDrainSystem_CollisionWithBlueCharacter(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain at position (5, 5)
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	// Get drain entity and position
	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	// Get drain's actual position
	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Create a blue character at the same position
	charEntity := world.CreateEntity()
	world.AddComponent(charEntity, components.PositionComponent{
		X: drain.X,
		Y: drain.Y,
	})
	world.AddComponent(charEntity, components.CharacterComponent{
		Rune:  'a',
		Style: tcell.StyleDefault,
	})
	world.AddComponent(charEntity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelNormal,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(charEntity, drain.X, drain.Y)
	tx.Commit()

	// Set color counter to 1
	// GameState mapping: 0=Blue, 1=Green
	ctx.State.AddColorCount(0, int(components.LevelNormal), 1)
	if ctx.State.BlueCountNormal.Load() != 1 {
		t.Fatal("Expected blue count to be 1")
	}

	// Update drain system (should handle collision)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify character was destroyed
	seqType := reflect.TypeOf(components.SequenceComponent{})
	if _, ok := world.GetComponent(charEntity, seqType); ok {
		t.Fatal("Expected character to be destroyed after collision")
	}

	// Verify color counter was decremented
	if ctx.State.BlueCountNormal.Load() != 0 {
		t.Fatalf("Expected blue count to be 0, got %d", ctx.State.BlueCountNormal.Load())
	}
}

// TestDrainSystem_CollisionWithGreenCharacter tests drain destroys green characters
func TestDrainSystem_CollisionWithGreenCharacter(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Create a green character at the same position
	charEntity := world.CreateEntity()
	world.AddComponent(charEntity, components.PositionComponent{
		X: drain.X,
		Y: drain.Y,
	})
	world.AddComponent(charEntity, components.CharacterComponent{
		Rune:  'b',
		Style: tcell.StyleDefault,
	})
	world.AddComponent(charEntity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelBright,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(charEntity, drain.X, drain.Y)
	tx.Commit()

	// Set color counter
	// GameState mapping: 0=Blue, 1=Green
	ctx.State.AddColorCount(1, int(components.LevelBright), 1)

	// Update drain system (should handle collision)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify character was destroyed
	seqType := reflect.TypeOf(components.SequenceComponent{})
	if _, ok := world.GetComponent(charEntity, seqType); ok {
		t.Fatal("Expected character to be destroyed after collision")
	}

	// Verify color counter was decremented
	if ctx.State.GreenCountBright.Load() != 0 {
		t.Fatalf("Expected green count to be 0, got %d", ctx.State.GreenCountBright.Load())
	}
}

// TestDrainSystem_CollisionWithRedCharacter tests drain destroys red characters
func TestDrainSystem_CollisionWithRedCharacter(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Create a red character at the same position
	charEntity := world.CreateEntity()
	world.AddComponent(charEntity, components.PositionComponent{
		X: drain.X,
		Y: drain.Y,
	})
	world.AddComponent(charEntity, components.CharacterComponent{
		Rune:  'c',
		Style: tcell.StyleDefault,
	})
	world.AddComponent(charEntity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceRed,
		Level: components.LevelDark,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(charEntity, drain.X, drain.Y)
	tx.Commit()

	// Update drain system (should handle collision)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify character was destroyed
	seqType := reflect.TypeOf(components.SequenceComponent{})
	if _, ok := world.GetComponent(charEntity, seqType); ok {
		t.Fatal("Expected character to be destroyed after collision")
	}
}

// TestDrainSystem_CollisionWithGoldSequence tests drain destroys entire gold sequence
func TestDrainSystem_CollisionWithGoldSequence(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain at position (0, 0)
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Activate gold sequence in GameState (simulate gold system spawn)
	sequenceID := 42
	ctx.State.ActivateGoldSequence(sequenceID, 5*time.Second)

	// Create a gold sequence with 10 characters
	goldEntities := make([]engine.Entity, 10)
	for i := 0; i < 10; i++ {
		entity := world.CreateEntity()
		goldEntities[i] = entity

		x := 10 + i
		y := 5

		world.AddComponent(entity, components.PositionComponent{
			X: x,
			Y: y,
		})
		world.AddComponent(entity, components.CharacterComponent{
			Rune:  rune('a' + i),
			Style: tcell.StyleDefault,
		})
		world.AddComponent(entity, components.SequenceComponent{
			ID:    sequenceID,
			Index: i,
			Type:  components.SequenceGold,
			Level: components.LevelBright,
		})

		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, x, y)
		tx.Commit()
	}

	// Move drain to collide with first gold character (10, 5)
	// Update drain component position
	drain.X = 10
	drain.Y = 5
	world.AddComponent(drainEntity, drain)

	// Update GameState atomics (this is what the collision check uses)
	ctx.State.SetDrainX(10)
	ctx.State.SetDrainY(5)

	// NOTE: We don't update the position component or spatial index
	// because the spatial index can only hold one entity per position.
	// The collision detection uses GetDrainX/GetDrainY from GameState,
	// not the spatial index lookup.

	// Update drain system (should destroy entire gold sequence)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify all gold characters were destroyed
	seqType := reflect.TypeOf(components.SequenceComponent{})
	for i, entity := range goldEntities {
		if _, ok := world.GetComponent(entity, seqType); ok {
			t.Fatalf("Expected gold character %d to be destroyed after collision", i)
		}
	}

	// Verify phase transition to PhaseGoldComplete
	phaseSnapshot := ctx.State.ReadPhaseState()
	if phaseSnapshot.Phase != engine.PhaseGoldComplete {
		t.Fatalf("Expected phase to be PhaseGoldComplete, got %v", phaseSnapshot.Phase)
	}

	// Verify gold is no longer active
	goldSnapshot := ctx.State.ReadGoldState()
	if goldSnapshot.Active {
		t.Fatal("Expected gold sequence to be inactive after collision")
	}
}

// TestDrainSystem_CollisionMultipleLevels tests collision with different brightness levels
func TestDrainSystem_CollisionMultipleLevels(t *testing.T) {
	levels := []struct {
		level components.SequenceLevel
		name  string
	}{
		{components.LevelDark, "Dark"},
		{components.LevelNormal, "Normal"},
		{components.LevelBright, "Bright"},
	}

	for _, tc := range levels {
		t.Run(tc.name, func(t *testing.T) {
			level := tc.level
			screen := tcell.NewSimulationScreen("UTF-8")
			screen.SetSize(80, 24)
			ctx := engine.NewGameContext(screen)
			world := engine.NewWorld()

			drainSys := NewDrainSystem(ctx)

			// Spawn drain
			ctx.State.SetScore(100)
			drainSys.Update(world, 16*time.Millisecond)

			drainEntityID := ctx.State.GetDrainEntity()
			drainEntity := engine.Entity(drainEntityID)

			drainType := reflect.TypeOf(components.DrainComponent{})
			drainComp, ok := world.GetComponent(drainEntity, drainType)
			if !ok {
				t.Fatal("Expected drain to have DrainComponent")
			}
			drain := drainComp.(components.DrainComponent)

			// Create a blue character with the specified level
			charEntity := world.CreateEntity()
			world.AddComponent(charEntity, components.PositionComponent{
				X: drain.X,
				Y: drain.Y,
			})
			world.AddComponent(charEntity, components.CharacterComponent{
				Rune:  'x',
				Style: tcell.StyleDefault,
			})
			world.AddComponent(charEntity, components.SequenceComponent{
				ID:    1,
				Index: 0,
				Type:  components.SequenceBlue,
				Level: level,
			})
		
	tx := world.BeginSpatialTransaction()
	tx.Spawn(charEntity, drain.X, drain.Y)
	tx.Commit()

			// Set color counter
			// GameState mapping: 0=Blue, 1=Green
			ctx.State.AddColorCount(0, int(level), 1)

			// Update drain system (should handle collision)
			drainSys.Update(world, 16*time.Millisecond)

			// Verify character was destroyed
			seqType := reflect.TypeOf(components.SequenceComponent{})
			if _, ok := world.GetComponent(charEntity, seqType); ok {
				t.Fatalf("Expected character with level %v to be destroyed", level)
			}

			// Verify color counter was decremented based on level
			var count int64
			switch level {
			case components.LevelDark:
				count = ctx.State.BlueCountDark.Load()
			case components.LevelNormal:
				count = ctx.State.BlueCountNormal.Load()
			case components.LevelBright:
				count = ctx.State.BlueCountBright.Load()
			}

			if count != 0 {
				t.Fatalf("Expected count to be 0 for level %v, got %d", level, count)
			}
		})
	}
}

// TestDrainSystem_NoCollisionWithNonSequenceEntity tests drain ignores non-sequence entities
func TestDrainSystem_NoCollisionWithNonSequenceEntity(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Create an entity without SequenceComponent at the same position
	otherEntity := world.CreateEntity()
	world.AddComponent(otherEntity, components.PositionComponent{
		X: drain.X,
		Y: drain.Y,
	})
	world.AddComponent(otherEntity, components.CharacterComponent{
		Rune:  '?',
		Style: tcell.StyleDefault,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(otherEntity, drain.X, drain.Y)
	tx.Commit()

	// Update drain system (should NOT destroy non-sequence entity)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify entity was NOT destroyed (still has CharacterComponent)
	charType := reflect.TypeOf(components.CharacterComponent{})
	if _, ok := world.GetComponent(otherEntity, charType); !ok {
		t.Fatal("Expected non-sequence entity to NOT be destroyed")
	}
}

// TestDrainSystem_NoSelfCollision tests drain doesn't collide with itself
func TestDrainSystem_NoSelfCollision(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to be active")
	}

	drainEntityID := ctx.State.GetDrainEntity()

	// Update multiple times (should not destroy itself)
	for i := 0; i < 10; i++ {
		drainSys.Update(world, 16*time.Millisecond)
	}

	// Verify drain is still active
	if !ctx.State.GetDrainActive() {
		t.Fatal("Expected drain to still be active after multiple updates")
	}

	// Verify same entity ID
	if ctx.State.GetDrainEntity() != drainEntityID {
		t.Fatal("Expected drain entity ID to remain the same")
	}
}

// TestDrainSystem_CollisionAtDifferentPositions tests collision works at various positions
func TestDrainSystem_CollisionAtDifferentPositions(t *testing.T) {
	positions := []struct {
		x, y int
	}{
		{0, 0},     // Top-left
		{10, 10},   // Middle
		{79, 23},   // Bottom-right (within bounds)
	}

	for _, pos := range positions {
		t.Run("Position", func(t *testing.T) {
			screen := tcell.NewSimulationScreen("UTF-8")
			screen.SetSize(80, 24)
			ctx := engine.NewGameContext(screen)
			world := engine.NewWorld()

			drainSys := NewDrainSystem(ctx)

			// Spawn drain manually at specific position
			ctx.State.SetScore(100)
			entity := world.CreateEntity()
			world.AddComponent(entity, components.PositionComponent{
				X: pos.x,
				Y: pos.y,
			})
			now := ctx.TimeProvider.Now()
			world.AddComponent(entity, components.DrainComponent{
				X:             pos.x,
				Y:             pos.y,
				LastMoveTime:  now,
				LastDrainTime: now,
				IsOnCursor:    false,
			})
	
  {
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, pos.x, pos.y)
		tx.Commit()
  }
			ctx.State.SetDrainActive(true)
			ctx.State.SetDrainEntity(uint64(entity))
			ctx.State.SetDrainX(pos.x)
			ctx.State.SetDrainY(pos.y)

			// Create a blue character at the same position
			charEntity := world.CreateEntity()
			world.AddComponent(charEntity, components.PositionComponent{
				X: pos.x,
				Y: pos.y,
			})
			world.AddComponent(charEntity, components.CharacterComponent{
				Rune:  'p',
				Style: tcell.StyleDefault,
			})
			world.AddComponent(charEntity, components.SequenceComponent{
				ID:    1,
				Index: 0,
				Type:  components.SequenceBlue,
				Level: components.LevelNormal,
			})
	
  {
		tx := world.BeginSpatialTransaction()
		tx.Spawn(charEntity, pos.x, pos.y)
		tx.Commit()
  }

			// Set color counter
			// GameState mapping: 0=Blue, 1=Green
			ctx.State.AddColorCount(0, int(components.LevelNormal), 1)

			// Update drain system
			drainSys.Update(world, 16*time.Millisecond)

			// Verify character was destroyed
			seqType := reflect.TypeOf(components.SequenceComponent{})
			if _, ok := world.GetComponent(charEntity, seqType); ok {
				t.Fatalf("Expected character at (%d, %d) to be destroyed", pos.x, pos.y)
			}

			// Verify color counter was decremented
			if ctx.State.BlueCountNormal.Load() != 0 {
				t.Fatalf("Expected blue count to be 0 at position (%d, %d)", pos.x, pos.y)
			}
		})
	}
}

// TestDrainSystem_CollisionWithNugget tests drain destroys nugget and clears active nugget
func TestDrainSystem_CollisionWithNugget(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	// Create nugget system and drain system
	nuggetSys := NewNuggetSystem(ctx)
	drainSys := NewDrainSystem(ctx)
	drainSys.SetNuggetSystem(nuggetSys)

	// Spawn drain at position (0, 0)
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Create a nugget at the same position as drain
	nuggetEntity := world.CreateEntity()
	world.AddComponent(nuggetEntity, components.PositionComponent{
		X: drain.X,
		Y: drain.Y,
	})
	world.AddComponent(nuggetEntity, components.CharacterComponent{
		Rune:  'N',
		Style: tcell.StyleDefault,
	})
	world.AddComponent(nuggetEntity, components.NuggetComponent{
		ID:        1,
		SpawnTime: ctx.TimeProvider.Now(),
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(nuggetEntity, drain.X, drain.Y)
	tx.Commit()

	// Set nugget as active in nugget system
	nuggetSys.activeNugget.Store(uint64(nuggetEntity))

	// Verify nugget is active before collision
	if nuggetSys.GetActiveNugget() != uint64(nuggetEntity) {
		t.Fatal("Expected nugget to be active before collision")
	}

	// Update drain system (should destroy nugget)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify nugget was destroyed
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if _, ok := world.GetComponent(nuggetEntity, nuggetType); ok {
		t.Fatal("Expected nugget to be destroyed after collision")
	}

	// Verify active nugget was cleared
	if nuggetSys.GetActiveNugget() != 0 {
		t.Fatalf("Expected active nugget to be cleared, got %d", nuggetSys.GetActiveNugget())
	}
}

// TestDrainSystem_NuggetCollisionWithoutSystem tests nugget collision without nugget system reference
func TestDrainSystem_NuggetCollisionWithoutSystem(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	// Create drain system WITHOUT setting nugget system
	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Create a nugget at the same position
	nuggetEntity := world.CreateEntity()
	world.AddComponent(nuggetEntity, components.PositionComponent{
		X: drain.X,
		Y: drain.Y,
	})
	world.AddComponent(nuggetEntity, components.CharacterComponent{
		Rune:  'N',
		Style: tcell.StyleDefault,
	})
	world.AddComponent(nuggetEntity, components.NuggetComponent{
		ID:        1,
		SpawnTime: ctx.TimeProvider.Now(),
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(nuggetEntity, drain.X, drain.Y)
	tx.Commit()

	// Update drain system (should still destroy nugget even without system reference)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify nugget was destroyed
	nuggetType := reflect.TypeOf(components.NuggetComponent{})
	if _, ok := world.GetComponent(nuggetEntity, nuggetType); ok {
		t.Fatal("Expected nugget to be destroyed after collision")
	}
}

// TestDrainSystem_GoldCollisionInactiveGold tests collision with inactive gold sequence
func TestDrainSystem_GoldCollisionInactiveGold(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Create a gold character WITHOUT activating gold in GameState
	goldEntity := world.CreateEntity()
	world.AddComponent(goldEntity, components.PositionComponent{
		X: drain.X,
		Y: drain.Y,
	})
	world.AddComponent(goldEntity, components.CharacterComponent{
		Rune:  'G',
		Style: tcell.StyleDefault,
	})
	world.AddComponent(goldEntity, components.SequenceComponent{
		ID:    88,
		Index: 0,
		Type:  components.SequenceGold,
		Level: components.LevelBright,
	})

	tx := world.BeginSpatialTransaction()
	tx.Spawn(goldEntity, drain.X, drain.Y)
	tx.Commit()

	// Update drain system (should NOT destroy gold if not active in GameState)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify gold was NOT destroyed (gold not active in GameState)
	seqType := reflect.TypeOf(components.SequenceComponent{})
	if _, ok := world.GetComponent(goldEntity, seqType); !ok {
		t.Fatal("Expected gold to NOT be destroyed when not active in GameState")
	}
}

// TestDrainSystem_CollisionWithFallingDecay tests drain destroys falling decay entities
func TestDrainSystem_CollisionWithFallingDecay(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain at position (0, 0)
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Create a falling decay entity at the same position
	decayEntity := world.CreateEntity()
	world.AddComponent(decayEntity, components.FallingDecayComponent{
		Column:        drain.X,
		YPosition:     float64(drain.Y),
		Speed:         5.0,
		Char:          'X',
		LastChangeRow: drain.Y,
	})
	// Note: FallingDecayComponent doesn't use PositionComponent, but we need
	// spatial index for collision detection

	tx := world.BeginSpatialTransaction()
	tx.Spawn(decayEntity, drain.X, drain.Y)
	tx.Commit()

	// Update drain system (should destroy falling decay entity)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify falling decay entity was destroyed
	fallingDecayType := reflect.TypeOf(components.FallingDecayComponent{})
	if _, ok := world.GetComponent(decayEntity, fallingDecayType); ok {
		t.Fatal("Expected falling decay entity to be destroyed after collision")
	}
}

// TestDrainSystem_CollisionWithMultipleFallingDecay tests drain collides with multiple decay entities
func TestDrainSystem_CollisionWithMultipleFallingDecay(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain at position (5, 5)
	ctx.State.SetScore(100)
	entity := world.CreateEntity()
	x, y := 5, 5
	world.AddComponent(entity, components.PositionComponent{
		X: x,
		Y: y,
	})
	now := ctx.TimeProvider.Now()
	world.AddComponent(entity, components.DrainComponent{
		X:             x,
		Y:             y,
		LastMoveTime:  now,
		LastDrainTime: now,
		IsOnCursor:    false,
	})

 {
	tx := world.BeginSpatialTransaction()
	tx.Spawn(entity, x, y)
	tx.Commit()
 }

	ctx.State.SetDrainActive(true)
	ctx.State.SetDrainEntity(uint64(entity))
	ctx.State.SetDrainX(x)
	ctx.State.SetDrainY(y)

	// Create multiple falling decay entities at different positions
	decayEntity1 := world.CreateEntity()
	world.AddComponent(decayEntity1, components.FallingDecayComponent{
		Column:        5,
		YPosition:     5.0,
		Speed:         5.0,
		Char:          'A',
		LastChangeRow: 5,
	})

 {
	tx := world.BeginSpatialTransaction()
	tx.Spawn(decayEntity1, 5, 5)
	tx.Commit()
 }

	decayEntity2 := world.CreateEntity()
	world.AddComponent(decayEntity2, components.FallingDecayComponent{
		Column:        10,
		YPosition:     10.0,
		Speed:         3.0,
		Char:          'B',
		LastChangeRow: 10,
	})

 {
	tx := world.BeginSpatialTransaction()
	tx.Spawn(decayEntity2, 10, 10)
	tx.Commit()
 }

	// Update drain system (should only destroy decay entity at drain position)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify first decay entity was destroyed (at drain position)
	fallingDecayType := reflect.TypeOf(components.FallingDecayComponent{})
	if _, ok := world.GetComponent(decayEntity1, fallingDecayType); ok {
		t.Fatal("Expected decay entity 1 to be destroyed (at drain position)")
	}

	// Verify second decay entity still exists (not at drain position)
	if _, ok := world.GetComponent(decayEntity2, fallingDecayType); !ok {
		t.Fatal("Expected decay entity 2 to still exist (not at drain position)")
	}
}

// TestDrainSystem_FallingDecayCollisionAtBoundary tests collision at screen boundaries
func TestDrainSystem_FallingDecayCollisionAtBoundary(t *testing.T) {
	positions := []struct {
		x, y int
		name string
	}{
		{0, 0, "TopLeft"},
		{79, 0, "TopRight"},
		{0, 23, "BottomLeft"},
		{40, 12, "Center"},
	}

	for _, pos := range positions {
		t.Run(pos.name, func(t *testing.T) {
			screen := tcell.NewSimulationScreen("UTF-8")
			screen.SetSize(80, 24)
			ctx := engine.NewGameContext(screen)
			world := engine.NewWorld()

			drainSys := NewDrainSystem(ctx)

			// Spawn drain at specific position
			ctx.State.SetScore(100)
			entity := world.CreateEntity()
			world.AddComponent(entity, components.PositionComponent{
				X: pos.x,
				Y: pos.y,
			})
			now := ctx.TimeProvider.Now()
			world.AddComponent(entity, components.DrainComponent{
				X:             pos.x,
				Y:             pos.y,
				LastMoveTime:  now,
				LastDrainTime: now,
				IsOnCursor:    false,
			})
	
  {
		tx := world.BeginSpatialTransaction()
		tx.Spawn(entity, pos.x, pos.y)
		tx.Commit()
  }
			ctx.State.SetDrainActive(true)
			ctx.State.SetDrainEntity(uint64(entity))
			ctx.State.SetDrainX(pos.x)
			ctx.State.SetDrainY(pos.y)

			// Create falling decay entity at same position
			decayEntity := world.CreateEntity()
			world.AddComponent(decayEntity, components.FallingDecayComponent{
				Column:        pos.x,
				YPosition:     float64(pos.y),
				Speed:         7.0,
				Char:          'Z',
				LastChangeRow: pos.y,
			})
	
  {
		tx := world.BeginSpatialTransaction()
		tx.Spawn(decayEntity, pos.x, pos.y)
		tx.Commit()
  }

			// Update drain system
			drainSys.Update(world, 16*time.Millisecond)

			// Verify decay entity was destroyed
			fallingDecayType := reflect.TypeOf(components.FallingDecayComponent{})
			if _, ok := world.GetComponent(decayEntity, fallingDecayType); ok {
				t.Fatalf("Expected decay entity to be destroyed at position (%d, %d)", pos.x, pos.y)
			}
		})
	}
}

// TestDrainSystem_DecayCollisionPriorityOverSequence tests decay collision happens before sequence collision
func TestDrainSystem_DecayCollisionPriorityOverSequence(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := engine.NewWorld()

	drainSys := NewDrainSystem(ctx)

	// Spawn drain
	ctx.State.SetScore(100)
	drainSys.Update(world, 16*time.Millisecond)

	drainEntityID := ctx.State.GetDrainEntity()
	drainEntity := engine.Entity(drainEntityID)

	drainType := reflect.TypeOf(components.DrainComponent{})
	drainComp, ok := world.GetComponent(drainEntity, drainType)
	if !ok {
		t.Fatal("Expected drain to have DrainComponent")
	}
	drain := drainComp.(components.DrainComponent)

	// Create a falling decay entity at drain position
	decayEntity := world.CreateEntity()
	world.AddComponent(decayEntity, components.FallingDecayComponent{
		Column:        drain.X,
		YPosition:     float64(drain.Y),
		Speed:         6.0,
		Char:          'D',
		LastChangeRow: drain.Y,
	})

 {
	tx := world.BeginSpatialTransaction()
	tx.Spawn(decayEntity, drain.X, drain.Y)
	tx.Commit()
 }

	// Create a blue character at different position
	charEntity := world.CreateEntity()
	world.AddComponent(charEntity, components.PositionComponent{
		X: drain.X + 1,
		Y: drain.Y,
	})
	world.AddComponent(charEntity, components.CharacterComponent{
		Rune:  'a',
		Style: tcell.StyleDefault,
	})
	world.AddComponent(charEntity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelNormal,
	})

 {
	tx := world.BeginSpatialTransaction()
	tx.Spawn(charEntity, drain.X+1, drain.Y)
	tx.Commit()
 }

	// Set color counter
	ctx.State.AddColorCount(0, int(components.LevelNormal), 1)

	// Update drain system (should only destroy decay entity at drain position)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify decay entity was destroyed
	fallingDecayType := reflect.TypeOf(components.FallingDecayComponent{})
	if _, ok := world.GetComponent(decayEntity, fallingDecayType); ok {
		t.Fatal("Expected decay entity to be destroyed")
	}

	// Verify blue character still exists (not at drain position)
	seqType := reflect.TypeOf(components.SequenceComponent{})
	if _, ok := world.GetComponent(charEntity, seqType); !ok {
		t.Fatal("Expected blue character to still exist")
	}
}

// TestDrainSystem_FallingDecayWithDifferentSpeeds tests collision with decay entities at various speeds
func TestDrainSystem_FallingDecayWithDifferentSpeeds(t *testing.T) {
	speeds := []float64{1.0, 5.0, 10.0, 15.0}

	for _, speed := range speeds {
		t.Run("Speed", func(t *testing.T) {
			screen := tcell.NewSimulationScreen("UTF-8")
			screen.SetSize(80, 24)
			ctx := engine.NewGameContext(screen)
			world := engine.NewWorld()

			drainSys := NewDrainSystem(ctx)

			// Spawn drain
			ctx.State.SetScore(100)
			drainSys.Update(world, 16*time.Millisecond)

			drainEntityID := ctx.State.GetDrainEntity()
			drainEntity := engine.Entity(drainEntityID)

			drainType := reflect.TypeOf(components.DrainComponent{})
			drainComp, ok := world.GetComponent(drainEntity, drainType)
			if !ok {
				t.Fatal("Expected drain to have DrainComponent")
			}
			drain := drainComp.(components.DrainComponent)

			// Create falling decay entity with specific speed
			decayEntity := world.CreateEntity()
			world.AddComponent(decayEntity, components.FallingDecayComponent{
				Column:        drain.X,
				YPosition:     float64(drain.Y),
				Speed:         speed,
				Char:          'S',
				LastChangeRow: drain.Y,
			})
		
	tx := world.BeginSpatialTransaction()
	tx.Spawn(decayEntity, drain.X, drain.Y)
	tx.Commit()

			// Update drain system
			drainSys.Update(world, 16*time.Millisecond)

			// Verify decay entity was destroyed regardless of speed
			fallingDecayType := reflect.TypeOf(components.FallingDecayComponent{})
			if _, ok := world.GetComponent(decayEntity, fallingDecayType); ok {
				t.Fatalf("Expected decay entity with speed %.1f to be destroyed", speed)
			}
		})
	}
}

