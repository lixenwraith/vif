package systems

import (
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestSpawnSystemOnlyGeneratesBlueAndGreen verifies that spawn system never generates red sequences
func TestSpawnSystemOnlyGeneratesBlueAndGreen(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForMechanics(mockTime)
	world := ctx.World

	spawnSystem := NewSpawnSystem(ctx.GameWidth, ctx.GameHeight, 0, 0, ctx)

	// Add code blocks for spawning (required for new file-based system)
	spawnSystem.codeBlocks = []CodeBlock{
		{Lines: []string{"test line one", "test line two", "test line three"}},
		{Lines: []string{"test line four", "test line five", "test line six"}},
	}

	// Spawn many sequences to ensure statistical coverage
	for i := 0; i < 100; i++ {
		// Advance time to trigger spawn
		mockTime.Advance(3 * time.Second)
		spawnSystem.Update(world, 16*time.Millisecond)
	}

	// Check all spawned sequences
	seqType := reflect.TypeOf(components.SequenceComponent{})
	entities := world.GetEntitiesWith(seqType)

	redCount := 0
	blueCount := 0
	greenCount := 0

	for _, entity := range entities {
		seqComp, ok := world.GetComponent(entity, seqType)
		if !ok {
			continue
		}
		seq := seqComp.(components.SequenceComponent)

		switch seq.Type {
		case components.SequenceRed:
			redCount++
		case components.SequenceBlue:
			blueCount++
		case components.SequenceGreen:
			greenCount++
		}
	}

	// Verify no red sequences were spawned
	if redCount > 0 {
		t.Errorf("Spawn system should not generate red sequences, but found %d red sequences", redCount)
	}

	// Verify at least one type of sequence was spawned (Blue or Green)
	totalNonRed := blueCount + greenCount
	if totalNonRed == 0 {
		t.Error("Expected some blue or green sequences to be spawned")
	}

	// With the file-based system and 6-color limit, it's possible (though unlikely)
	// to get mostly one color due to random selection and the limit.
	// The important thing is that NO red sequences are spawned.
	t.Logf("Spawned %d blue and %d green sequences (%d total, 0 red sequences as expected)", blueCount, greenCount, totalNonRed)
}

// TestRedSequencesOnlyFromDecay verifies that red sequences only appear through decay
func TestRedSequencesOnlyFromDecay(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForMechanics(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)

	// Manually create a green sequence at LevelDark (ready to decay to red)
	entity := world.CreateEntity()

	world.AddComponent(entity, components.PositionComponent{
		X: 10,
		Y: 0,
	})

	world.AddComponent(entity, components.CharacterComponent{
		Rune: 'a',
	})

	world.AddComponent(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceGreen,
		Level: components.LevelDark,
	})

	world.UpdateSpatialIndex(entity, 10, 0)

	// Verify it's green initially
	seqType := reflect.TypeOf(components.SequenceComponent{})
	seqComp, _ := world.GetComponent(entity, seqType)
	seq := seqComp.(components.SequenceComponent)

	if seq.Type != components.SequenceGreen {
		t.Fatal("Initial sequence should be green")
	}

	// Trigger decay animation
	decaySystem.animating = true
	decaySystem.currentRow = 0
	decaySystem.startTime = mockTime.Now()
	decaySystem.applyDecayToRow(world, 0)

	// Verify it decayed to red
	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Entity should still exist after decay")
	}
	seq = seqComp.(components.SequenceComponent)

	if seq.Type != components.SequenceRed {
		t.Errorf("Green sequence at LevelDark should decay to Red, got %v", seq.Type)
	}

	if seq.Level != components.LevelBright {
		t.Errorf("Decayed red sequence should be at LevelBright, got %v", seq.Level)
	}
}

// TestGoldSequenceRandomPosition verifies that gold sequences spawn at random positions
func TestGoldSequenceRandomPosition(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForMechanics(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight, 0, 0)

	positions := make(map[int]bool) // Track Y positions to verify randomness
	centerX := (ctx.GameWidth - 10) / 2
	nonCenterCount := 0

	// Spawn gold sequences multiple times
	for i := 0; i < 20; i++ {
		// Clear world
		world = engine.NewWorld()
		ctx.World = world

		// Trigger gold sequence spawn
		decaySystem.animating = true
		goldSystem.Update(world, 16*time.Millisecond)
		decaySystem.animating = false
		goldSystem.Update(world, 16*time.Millisecond)

		if !goldSystem.IsActive() {
			t.Fatal("Gold sequence should be active after spawning")
		}

		// Check position of first gold character
		seqType := reflect.TypeOf(components.SequenceComponent{})
		posType := reflect.TypeOf(components.PositionComponent{})
		entities := world.GetEntitiesWith(seqType, posType)

		for _, entity := range entities {
			seqComp, _ := world.GetComponent(entity, seqType)
			seq := seqComp.(components.SequenceComponent)

			if seq.Type == components.SequenceGold && seq.Index == 0 {
				posComp, _ := world.GetComponent(entity, posType)
				pos := posComp.(components.PositionComponent)

				positions[pos.Y] = true

				// Check if X position is not center (indicating randomness)
				if pos.X != centerX {
					nonCenterCount++
				}

				break
			}
		}

		// Remove gold sequence for next iteration
		goldSystem.removeGoldSequence(world)
	}

	// Verify that we got some variation in positions
	if len(positions) < 2 {
		t.Error("Gold sequences should spawn at different Y positions (random), but all spawned at same Y")
	}

	// Verify that at least some spawns were not at the old center position
	if nonCenterCount == 0 {
		t.Error("Expected at least some gold sequences to spawn at non-center X positions")
	}

	t.Logf("Gold sequences spawned at %d different Y positions, %d/%d at non-center X positions",
		len(positions), nonCenterCount, 20)
}

// TestDecayChainBlueToGreenToRed verifies the full decay chain
func TestDecayChainBlueToGreenToRed(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForMechanics(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)

	// Create a blue sequence at LevelDark
	entity := world.CreateEntity()

	world.AddComponent(entity, components.PositionComponent{
		X: 10,
		Y: 0,
	})

	world.AddComponent(entity, components.CharacterComponent{
		Rune: 'a',
	})

	world.AddComponent(entity, components.SequenceComponent{
		ID:    1,
		Index: 0,
		Type:  components.SequenceBlue,
		Level: components.LevelDark,
	})

	world.UpdateSpatialIndex(entity, 10, 0)

	seqType := reflect.TypeOf(components.SequenceComponent{})

	// Verify initial state (blue)
	seqComp, _ := world.GetComponent(entity, seqType)
	seq := seqComp.(components.SequenceComponent)
	if seq.Type != components.SequenceBlue || seq.Level != components.LevelDark {
		t.Fatal("Initial sequence should be Blue at LevelDark")
	}

	// First decay: Blue Dark → Green Bright
	decaySystem.applyDecayToRow(world, 0)

	seqComp, ok := world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Entity should exist after first decay")
	}
	seq = seqComp.(components.SequenceComponent)

	if seq.Type != components.SequenceGreen {
		t.Errorf("Blue Dark should decay to Green, got %v", seq.Type)
	}
	if seq.Level != components.LevelBright {
		t.Errorf("Decayed sequence should be Bright, got %v", seq.Level)
	}

	// Decay through brightness levels: Bright → Normal
	decaySystem.applyDecayToRow(world, 0)

	seqComp, _ = world.GetComponent(entity, seqType)
	seq = seqComp.(components.SequenceComponent)

	if seq.Type != components.SequenceGreen || seq.Level != components.LevelNormal {
		t.Errorf("Expected Green Normal, got %v %v", seq.Type, seq.Level)
	}

	// Decay: Normal → Dark
	decaySystem.applyDecayToRow(world, 0)

	seqComp, _ = world.GetComponent(entity, seqType)
	seq = seqComp.(components.SequenceComponent)

	if seq.Type != components.SequenceGreen || seq.Level != components.LevelDark {
		t.Errorf("Expected Green Dark, got %v %v", seq.Type, seq.Level)
	}

	// Next decay: Green Dark → Red Bright
	decaySystem.applyDecayToRow(world, 0)

	seqComp, ok = world.GetComponent(entity, seqType)
	if !ok {
		t.Fatal("Entity should exist after decaying to red")
	}
	seq = seqComp.(components.SequenceComponent)

	if seq.Type != components.SequenceRed {
		t.Errorf("Green Dark should decay to Red, got %v", seq.Type)
	}
	if seq.Level != components.LevelBright {
		t.Errorf("Decayed red sequence should be Bright, got %v", seq.Level)
	}

	t.Log("Successfully verified decay chain: Blue → Green → Red")
}

// TestGoldSequenceNotAtFixedCenter verifies gold sequence avoids fixed center-top position
func TestGoldSequenceNotAtFixedCenter(t *testing.T) {
	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx := createTestContextForMechanics(mockTime)
	world := ctx.World

	decaySystem := NewDecaySystem(ctx.GameWidth, ctx.GameHeight, ctx.Width, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, ctx.GameWidth, ctx.GameHeight, 0, 0)

	// Calculate old center position
	centerX := (ctx.GameWidth - 10) / 2
	centerY := 0

	atOldCenterCount := 0
	totalSpawns := 0

	// Spawn gold sequences multiple times
	for i := 0; i < 30; i++ {
		// Clear world
		world = engine.NewWorld()
		ctx.World = world

		// Trigger gold sequence spawn
		decaySystem.animating = true
		goldSystem.Update(world, 16*time.Millisecond)
		decaySystem.animating = false
		goldSystem.Update(world, 16*time.Millisecond)

		if !goldSystem.IsActive() {
			continue // Skip if couldn't spawn (rare)
		}

		totalSpawns++

		// Check position of first gold character
		seqType := reflect.TypeOf(components.SequenceComponent{})
		posType := reflect.TypeOf(components.PositionComponent{})
		entities := world.GetEntitiesWith(seqType, posType)

		for _, entity := range entities {
			seqComp, _ := world.GetComponent(entity, seqType)
			seq := seqComp.(components.SequenceComponent)

			if seq.Type == components.SequenceGold && seq.Index == 0 {
				posComp, _ := world.GetComponent(entity, posType)
				pos := posComp.(components.PositionComponent)

				if pos.X == centerX && pos.Y == centerY {
					atOldCenterCount++
				}

				break
			}
		}

		// Remove gold sequence for next iteration
		goldSystem.removeGoldSequence(world)
	}

	if totalSpawns == 0 {
		t.Fatal("Failed to spawn any gold sequences")
	}

	// With random positioning, the probability of ALL spawns being at the exact center
	// should be astronomically low (1/area)^30
	// We expect SOME spawns might be at center by chance, but not all
	percentAtCenter := float64(atOldCenterCount) / float64(totalSpawns) * 100

	t.Logf("%d/%d (%.1f%%) gold sequences spawned at old center position",
		atOldCenterCount, totalSpawns, percentAtCenter)

	// If more than 50% are at the old center, something is wrong with randomization
	if percentAtCenter > 50.0 {
		t.Errorf("Too many gold sequences at old center position (%.1f%%), expected random distribution",
			percentAtCenter)
	}
}

// Helper function to create a test context for mechanics tests
func createTestContextForMechanics(timeProvider engine.TimeProvider) *engine.GameContext {
	ctx := &engine.GameContext{
		World:        engine.NewWorld(),
		TimeProvider: timeProvider,
		Width:        100,
		Height:       30,
		GameWidth:    90,
		GameHeight:   26,
	}
	ctx.SetScore(0)
	ctx.SetScoreIncrement(0)
	return ctx
}
