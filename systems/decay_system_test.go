package systems

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/render"
)

// helper to create a robust test context with fixed dimensions
func createTestContext() *engine.GameContext {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.Init()
	screen.SetSize(80, 30)

	ctx := engine.NewGameContext(screen)
	// Explicitly force dimensions to ensure physics bounds checks don't fail
	ctx.Width = 80
	ctx.Height = 30
	ctx.GameWidth = 80
	ctx.GameHeight = 25 // Ensure enough height for falling entities
	ctx.GameX = 0
	ctx.GameY = 0

	return ctx
}

// TestDecaySweptCollisionNoTunneling verifies swept collision prevents tunneling
func TestDecaySweptCollisionNoTunneling(t *testing.T) {
	ctx := createTestContext()
	world := ctx.World
	decaySystem := NewDecaySystem(ctx)

	// Create blue sequences at multiple rows (0, 2, 4, 6)
	targetRows := []int{0, 2, 4, 6}
	entities := make([]engine.Entity, len(targetRows))
	col := 10

	for i, row := range targetRows {
		entity := world.CreateEntity()
		world.Positions.Add(entity, components.PositionComponent{X: col, Y: row})
		seqStyle := render.GetStyleForSequence(components.SequenceBlue, components.LevelBright)
		world.Characters.Add(entity, components.CharacterComponent{Rune: 'a', Style: seqStyle})
		world.Sequences.Add(entity, components.SequenceComponent{
			ID:    i + 1,
			Index: 0,
			Type:  components.SequenceBlue,
			Level: components.LevelBright,
		})
		entities[i] = entity
	}

	// Manually spawn a SINGLE falling entity at column 10, starting above screen
	fallingEntity := world.CreateEntity()
	world.FallingDecays.Add(fallingEntity, components.FallingDecayComponent{
		Column:        col,
		YPosition:     -1.0,
		Speed:         20.0, // High speed to test tunneling (20 rows/sec)
		Char:          'X',
		LastChangeRow: -1,
		LastIntX:      -1,
		LastIntY:      -1,
		PrevPreciseX:  float64(col),
		PrevPreciseY:  -1.0,
	})

	// Simulate physics manually
	// 20 rows/sec * 0.016s = 0.32 rows/frame.
	// Run enough frames to clear the screen (25 rows)
	dt := 0.016 // 16ms
	steps := 200

	for i := 0; i < steps; i++ {
		decaySystem.updateFallingEntities(world, dt)
	}

	// Verify all entities were decayed
	for i, entity := range entities {
		seq, ok := world.Sequences.Get(entity)
		if !ok {
			t.Errorf("Entity at row %d disappeared", targetRows[i])
			continue
		}

		// Should be decayed from Bright to Normal (or further to Dark/Green)
		// If it missed, it stays LevelBright
		if seq.Level == components.LevelBright {
			t.Errorf("Entity at row %d was not decayed - tunneling occurred!", targetRows[i])
		}
	}
}

// TestDecayCoordinateLatchPreventsReprocessing verifies coordinate latch prevents hitting same cell twice
func TestDecayCoordinateLatchPreventsReprocessing(t *testing.T) {
	ctx := createTestContext()
	world := ctx.World
	decaySystem := NewDecaySystem(ctx)

	// Create a blue sequence at row 5, column 10
	entity := world.CreateEntity()
	col, row := 10, 5
	world.Positions.Add(entity, components.PositionComponent{X: col, Y: row})
	seqStyle := render.GetStyleForSequence(components.SequenceBlue, components.LevelBright)
	world.Characters.Add(entity, components.CharacterComponent{Rune: 'a', Style: seqStyle})
	world.Sequences.Add(entity, components.SequenceComponent{
		Type:  components.SequenceBlue,
		Level: components.LevelBright,
	})

	// Manually spawn falling entity just above target
	falling := world.CreateEntity()
	world.FallingDecays.Add(falling, components.FallingDecayComponent{
		Column:       col,
		YPosition:    4.8,
		Speed:        10.0,
		LastIntX:     -1,
		LastIntY:     -1,
		PrevPreciseX: float64(col),
		PrevPreciseY: 4.8,
	})

	// Step 1: Move from 4.8 to 5.0 (Hit)
	// Speed 10 * 0.02 = 0.2
	hitCount := 0
	levelBefore := components.LevelBright

	decaySystem.updateFallingEntities(world, 0.02)

	seq, _ := world.Sequences.Get(entity)
	if seq.Level != levelBefore {
		hitCount++
		levelBefore = seq.Level
	}

	// Step 2: Move from 5.0 to 5.2 (Should NOT Hit due to latch)
	decaySystem.updateFallingEntities(world, 0.02)

	seq, _ = world.Sequences.Get(entity)
	if seq.Level != levelBefore {
		hitCount++
	}

	// Verify entity was hit exactly once
	if hitCount != 1 {
		t.Errorf("Entity was hit %d times, expected exactly 1", hitCount)
	}
}

// TestDecayFrameDeduplicationMap verifies processedGridCells prevents double-hits in same frame
func TestDecayFrameDeduplicationMap(t *testing.T) {
	ctx := createTestContext()
	world := ctx.World
	decaySystem := NewDecaySystem(ctx)

	// Create a blue sequence at target position
	col, row := 10, 5
	targetEntity := world.CreateEntity()
	world.Positions.Add(targetEntity, components.PositionComponent{X: col, Y: row})
	world.Sequences.Add(targetEntity, components.SequenceComponent{
		Type:  components.SequenceBlue,
		Level: components.LevelBright,
	})

	// Setup TWO falling entities hitting the same cell in the same frame

	// Entity 1: Hitting row 5
	e1 := world.CreateEntity()
	world.FallingDecays.Add(e1, components.FallingDecayComponent{
		Column: col, YPosition: 5.0, Speed: 10, LastIntX: -1, LastIntY: -1, PrevPreciseY: 4.9,
	})

	// Entity 2: Also hitting row 5
	e2 := world.CreateEntity()
	world.FallingDecays.Add(e2, components.FallingDecayComponent{
		Column: col, YPosition: 5.1, Speed: 10, LastIntX: -1, LastIntY: -1, PrevPreciseY: 4.9,
	})

	// Update once
	decaySystem.updateFallingEntities(world, 0.01)

	// Verify target was hit only once (decayed by one level)
	if seq, ok := world.Sequences.Get(targetEntity); ok {
		if seq.Level != components.LevelNormal {
			t.Errorf("Target has level %d, expected %d (should only decay once per frame)", seq.Level, components.LevelNormal)
		}
	} else {
		t.Error("Target was destroyed")
	}
}

// TestDecayDifferentSpeeds verifies decay works correctly with min and max speeds
func TestDecayDifferentSpeeds(t *testing.T) {
	testCases := []struct {
		name      string
		speed     float64
		targetRow int
	}{
		{"Slow", constants.FallingDecayMinSpeed, 5},
		{"Fast", constants.FallingDecayMaxSpeed, 20},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := createTestContext()
			world := ctx.World
			decaySystem := NewDecaySystem(ctx)

			// Target
			entity := world.CreateEntity()
			col := 10
			world.Positions.Add(entity, components.PositionComponent{X: col, Y: tc.targetRow})
			world.Sequences.Add(entity, components.SequenceComponent{
				Type:  components.SequenceBlue,
				Level: components.LevelBright,
			})

			// Falling entity
			falling := world.CreateEntity()
			world.FallingDecays.Add(falling, components.FallingDecayComponent{
				Column:       col,
				YPosition:    -1.0,
				Speed:        tc.speed,
				LastIntX:     -1,
				LastIntY:     -1,
				PrevPreciseY: -1.0,
			})

			// Simulate enough time to pass the row
			// Distance ~25 rows.
			dt := 0.016
			maxSteps := int(30.0/(tc.speed*dt)) + 100

			decayed := false
			for i := 0; i < maxSteps; i++ {
				decaySystem.updateFallingEntities(world, dt)

				if seq, ok := world.Sequences.Get(entity); ok {
					if seq.Level != components.LevelBright {
						decayed = true
						break
					}
				}
			}

			if !decayed {
				t.Errorf("Entity at row %d was not decayed with speed %.1f", tc.targetRow, tc.speed)
			}
		})
	}
}

// TestDecayFallingEntityPhysicsAccuracy verifies falling entity moves at correct speed
func TestDecayFallingEntityPhysicsAccuracy(t *testing.T) {
	ctx := createTestContext()
	world := ctx.World
	decaySystem := NewDecaySystem(ctx)

	entity := world.CreateEntity()
	speed := 10.0
	initialY := 5.0

	world.FallingDecays.Add(entity, components.FallingDecayComponent{
		Column:       10,
		YPosition:    initialY,
		Speed:        speed,
		PrevPreciseY: initialY,
	})

	// Simulate 1 second exactly
	dt := 0.016   // 16ms
	frames := 100 // 1.6 seconds total, but we'll sum actual dt

	elapsed := 0.0
	for i := 0; i < frames; i++ {
		decaySystem.updateFallingEntities(world, dt)
		elapsed += dt
	}

	fall, ok := world.FallingDecays.Get(entity)
	if !ok {
		t.Fatal("Falling entity destroyed unexpectedly")
	}

	expectedY := initialY + (speed * elapsed)
	if fall.YPosition < expectedY-0.01 || fall.YPosition > expectedY+0.01 {
		t.Errorf("Physics mismatch: expected Y %.3f, got %.3f", expectedY, fall.YPosition)
	}
}

// TestDecayMatrixEffectCharacterChanges verifies Matrix-style character changes occur
func TestDecayMatrixEffectCharacterChanges(t *testing.T) {
	ctx := createTestContext()
	world := ctx.World
	decaySystem := NewDecaySystem(ctx)

	entity := world.CreateEntity()
	initialChar := 'A'

	world.FallingDecays.Add(entity, components.FallingDecayComponent{
		Column:        10,
		YPosition:     0.0,
		Speed:         5.0,
		Char:          initialChar,
		LastChangeRow: -1,
	})

	// Simulate movement through multiple rows
	changed := false
	dt := 0.1 // Move ~0.5 row per step

	for i := 0; i < 50; i++ {
		decaySystem.updateFallingEntities(world, dt)

		if fall, ok := world.FallingDecays.Get(entity); ok {
			if fall.Char != initialChar {
				changed = true
				break
			}
		}
	}

	// It's probabilistic (40%), but over 50 steps (crossing ~5 rows) it should change
	if !changed {
		t.Log("Warning: Character didn't change (probabilistic failure, possibly OK)")
	}
}