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
	world.UpdateSpatialIndex(charEntity, drain.X, drain.Y)

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
	world.UpdateSpatialIndex(charEntity, drain.X, drain.Y)

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
	world.UpdateSpatialIndex(charEntity, drain.X, drain.Y)

	// Update drain system (should handle collision)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify character was destroyed
	seqType := reflect.TypeOf(components.SequenceComponent{})
	if _, ok := world.GetComponent(charEntity, seqType); ok {
		t.Fatal("Expected character to be destroyed after collision")
	}
}

// TestDrainSystem_NoCollisionWithGoldCharacter tests drain does NOT destroy gold characters (Part 6)
func TestDrainSystem_NoCollisionWithGoldCharacter(t *testing.T) {
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

	// Create a gold character at the same position
	charEntity := world.CreateEntity()
	world.AddComponent(charEntity, components.PositionComponent{
		X: drain.X,
		Y: drain.Y,
	})
	world.AddComponent(charEntity, components.CharacterComponent{
		Rune:  'g',
		Style: tcell.StyleDefault,
	})
	world.AddComponent(charEntity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGold,
		Level: components.LevelNormal,
	})
	world.UpdateSpatialIndex(charEntity, drain.X, drain.Y)

	// Update drain system (should NOT destroy gold character)
	drainSys.Update(world, 16*time.Millisecond)

	// Verify character was NOT destroyed
	seqType := reflect.TypeOf(components.SequenceComponent{})
	if _, ok := world.GetComponent(charEntity, seqType); !ok {
		t.Fatal("Expected gold character to NOT be destroyed (Part 6 will handle this)")
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
			world.UpdateSpatialIndex(charEntity, drain.X, drain.Y)

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
	world.UpdateSpatialIndex(otherEntity, drain.X, drain.Y)

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
			world.UpdateSpatialIndex(entity, pos.x, pos.y)
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
			world.UpdateSpatialIndex(charEntity, pos.x, pos.y)

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

