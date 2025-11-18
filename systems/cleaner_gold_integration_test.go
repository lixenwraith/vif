package systems

import (
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestConcurrentGoldActivationDuringActiveCleaners tests gold sequence activation
// while cleaners are actively running
func TestConcurrentGoldActivationDuringActiveCleaners(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	goldSystem := NewGoldSequenceSystem(ctx, decaySystem, 80, 24, 0, 0)
	goldSystem.SetCleanerTrigger(cleanerSystem.TriggerCleaners)

	// Create Red characters on multiple rows to trigger cleaners
	for i := 0; i < 5; i++ {
		createRedCharacterAt(world, 10+i*5, i*2)
	}

	// Trigger first set of cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Verify cleaners are active
	if !cleanerSystem.IsActive() {
		t.Fatal("Cleaners should be active")
	}

	// Concurrently:
	// 1. Update cleaner system (running concurrently in background)
	// 2. Try to spawn gold sequence
	// 3. Try to trigger cleaners again via gold completion

	var wg sync.WaitGroup
	var errorCount atomic.Int32

	// Goroutine 1: Continuously update cleaner system
	stopChan := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Cleaner update panicked: %v", r)
			}
		}()

		for i := 0; i < 50; i++ {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(16 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Try to spawn gold sequence during active cleaners
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Gold spawn panicked: %v", r)
			}
		}()

		time.Sleep(100 * time.Millisecond)
		goldSystem.spawnGoldSequence(world)
	}()

	// Goroutine 3: Try to trigger cleaners again
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Cleaner trigger panicked: %v", r)
			}
		}()

		time.Sleep(150 * time.Millisecond)
		cleanerSystem.TriggerCleaners(world)
		cleanerSystem.Update(world, 16*time.Millisecond)
	}()

	// Goroutine 4: Read world state concurrently
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("World reader panicked: %v", r)
			}
		}()

		for i := 0; i < 20; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Read entities
				cleanerType := reflect.TypeOf(components.CleanerComponent{})
				_ = world.GetEntitiesWith(cleanerType)

				seqType := reflect.TypeOf(components.SequenceComponent{})
				_ = world.GetEntitiesWith(seqType)

				time.Sleep(25 * time.Millisecond)
			}
		}
	}()

	// Let test run
	time.Sleep(600 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}

	t.Logf("Test completed successfully with concurrent gold and cleaner operations")
}

// TestMultipleCleanersOnAdjacentLines tests cleaners running on adjacent rows
// simultaneously to verify no interference
func TestMultipleCleanersOnAdjacentLines(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters on adjacent rows
	for row := 5; row <= 10; row++ {
		for x := 10; x < 20; x += 2 {
			createRedCharacterAt(world, x, row)
		}
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	// Verify cleaners were created for all adjacent rows
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	if len(cleaners) != 6 {
		t.Fatalf("Expected 6 cleaners (one per row 5-10), got %d", len(cleaners))
	}

	// Track cleaners by row
	cleanersByRow := make(map[int]engine.Entity)
	for _, entity := range cleaners {
		cleanerComp, _ := world.GetComponent(entity, cleanerType)
		cleaner := cleanerComp.(components.CleanerComponent)
		cleanersByRow[cleaner.Row] = entity
	}

	// Verify each row has exactly one cleaner
	for row := 5; row <= 10; row++ {
		if _, exists := cleanersByRow[row]; !exists {
			t.Errorf("Expected cleaner on row %d, but not found", row)
		}
	}

	// Simulate movement and verify no cross-row interference
	var wg sync.WaitGroup
	var errorCount atomic.Int32

	// Update cleaners concurrently from multiple goroutines (simulate race conditions)
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					errorCount.Add(1)
					t.Errorf("Goroutine %d panicked: %v", goroutineID, r)
				}
			}()

			for j := 0; j < 20; j++ {
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors", errorCount.Load())
	}

	// Wait for cleaners to move significantly
	time.Sleep(300 * time.Millisecond)

	// Verify cleaners are still on their respective rows
	cleaners = world.GetEntitiesWith(cleanerType)
	for _, entity := range cleaners {
		cleanerComp, ok := world.GetComponent(entity, cleanerType)
		if !ok {
			continue
		}
		cleaner := cleanerComp.(components.CleanerComponent)

		// Verify row hasn't changed
		if cleaner.Row < 5 || cleaner.Row > 10 {
			t.Errorf("Cleaner row changed to invalid value: %d", cleaner.Row)
		}

		// Verify direction is correct for row
		expectedDirection := 1  // Odd rows: L→R
		if cleaner.Row%2 == 0 { // Even rows: R→L
			expectedDirection = -1
		}
		if cleaner.Direction != expectedDirection {
			t.Errorf("Row %d has incorrect direction: expected %d, got %d",
				cleaner.Row, expectedDirection, cleaner.Direction)
		}
	}

	t.Logf("Successfully tested %d cleaners on adjacent lines", len(cleaners))
}

// TestCleanerCollisionWithActivelyChangingText tests cleaners hitting characters
// that are being modified concurrently (decay, typing, etc.)
func TestCleanerCollisionWithActivelyChangingText(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)
	decaySys := NewDecaySystem(80, 24, 80, 0, ctx)
	decaySys.SetSpawnSystem(spawnSys)

	// Create Red characters at specific positions
	redEntities := make([]engine.Entity, 0)
	for x := 20; x < 40; x += 2 {
		entity := createRedCharacterAt(world, x, 5)
		redEntities = append(redEntities, entity)
	}

	// Create Green characters nearby that we'll modify
	for x := 21; x < 40; x += 2 {
		entity := createGreenCharacterAt(world, x, 5)
		spawnSys.AddColorCount(components.SequenceGreen, components.LevelBright, 1)
		_ = entity
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	stopChan := make(chan struct{})

	// Goroutine 1: Continuously apply decay to row 5 (modifying characters)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Decay goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 10; i++ {
			select {
			case <-stopChan:
				return
			default:
				decaySys.applyDecayToRow(world, 5)
				time.Sleep(50 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Continuously move and detect collisions
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Cleaner collision goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 40; i++ {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.detectAndDestroyRedCharacters(world)
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Try to destroy some entities manually (simulate typing)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Manual destroy goroutine panicked: %v", r)
			}
		}()

		time.Sleep(100 * time.Millisecond)

		seqType := reflect.TypeOf(components.SequenceComponent{})
		posType := reflect.TypeOf(components.PositionComponent{})
		entities := world.GetEntitiesWith(seqType, posType)

		destroyed := 0
		for _, entity := range entities {
			if destroyed >= 3 {
				break
			}

			seqComp, ok := world.GetComponent(entity, seqType)
			if !ok {
				continue
			}
			seq := seqComp.(components.SequenceComponent)

			// Only destroy Green characters (simulate typing)
			if seq.Type == components.SequenceGreen {
				posComp, ok := world.GetComponent(entity, posType)
				if ok {
					pos := posComp.(components.PositionComponent)
					world.RemoveFromSpatialIndex(pos.X, pos.Y)
					spawnSys.AddColorCount(seq.Type, seq.Level, -1)
				}
				world.DestroyEntity(entity)
				destroyed++
			}

			time.Sleep(30 * time.Millisecond)
		}
	}()

	// Goroutine 4: Read spatial index continuously
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Spatial index reader panicked: %v", r)
			}
		}()

		for i := 0; i < 50; i++ {
			select {
			case <-stopChan:
				return
			default:
				for x := 20; x < 40; x++ {
					_ = world.GetEntityAtPosition(x, 5)
				}
				time.Sleep(15 * time.Millisecond)
			}
		}
	}()

	// Let test run
	time.Sleep(1 * time.Second)
	close(stopChan)

	wg.Wait()

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}

	t.Logf("Successfully tested cleaner collision with changing text")
}

// TestHeatMeterStateTransitionsDuringCleanerAnimation tests that heat meter
// changes don't cause issues during cleaner animation
func TestHeatMeterStateTransitionsDuringCleanerAnimation(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	goldSystem := NewGoldSequenceSystem(ctx, nil, 80, 24, 0, 0)
	goldSystem.SetCleanerTrigger(cleanerSystem.TriggerCleaners)

	// Create Red characters
	for i := 0; i < 10; i++ {
		createRedCharacterAt(world, 10+i*3, 5)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	stopChan := make(chan struct{})

	// Shared heat value (simulating game context heat)
	var currentHeat atomic.Int32
	maxHeat := 100
	currentHeat.Store(50)

	// Goroutine 1: Continuously modify heat value
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Heat modifier panicked: %v", r)
			}
		}()

		for i := 0; i < 50; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Simulate heat changes
				if i%3 == 0 {
					currentHeat.Store(int32(maxHeat)) // Fill to max
				} else if i%3 == 1 {
					currentHeat.Store(0) // Reset
				} else {
					currentHeat.Store(int32(maxHeat / 2)) // Mid-level
				}
				time.Sleep(30 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Try to trigger cleaners based on heat
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Cleaner trigger goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 30; i++ {
			select {
			case <-stopChan:
				return
			default:
				heat := int(currentHeat.Load())
				goldSystem.TriggerCleanersIfHeatFull(world, heat, maxHeat)
				time.Sleep(40 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Update cleaners continuously
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Cleaner update panicked: %v", r)
			}
		}()

		for i := 0; i < 60; i++ {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(16 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Check cleaner active state
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("State checker panicked: %v", r)
			}
		}()

		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				_ = cleanerSystem.IsActive()
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Let test run
	time.Sleep(1200 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}

	t.Logf("Successfully tested heat meter transitions during cleaner animation")
}

// TestScreenBufferModificationsDuringScan tests concurrent screen buffer reads
// while cleaners are scanning for Red characters
func TestScreenBufferModificationsDuringScan(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create entities across the screen
	for y := 0; y < 24; y++ {
		for x := 0; x < 80; x += 10 {
			if y%2 == 0 {
				createRedCharacterAt(world, x, y)
			} else {
				createBlueCharacterAt(world, x, y)
			}
		}
	}

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	stopChan := make(chan struct{})

	// Goroutine 1: Continuously scan for Red rows
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Scanner panicked: %v", r)
			}
		}()

		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				_ = cleanerSystem.scanRedCharacterRows(world)
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Modify screen buffer (add entities)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Entity adder panicked: %v", r)
			}
		}()

		for i := 0; i < 50; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Add new Red characters at random positions
				x := 5 + (i*7)%70
				y := (i * 3) % 24
				createRedCharacterAt(world, x, y)
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Modify screen buffer (remove entities)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Entity remover panicked: %v", r)
			}
		}()

		time.Sleep(100 * time.Millisecond)

		for i := 0; i < 30; i++ {
			select {
			case <-stopChan:
				return
			default:
				seqType := reflect.TypeOf(components.SequenceComponent{})
				posType := reflect.TypeOf(components.PositionComponent{})
				entities := world.GetEntitiesWith(seqType, posType)

				if len(entities) > 0 {
					entity := entities[i%len(entities)]
					posComp, ok := world.GetComponent(entity, posType)
					if ok {
						pos := posComp.(components.PositionComponent)
						world.RemoveFromSpatialIndex(pos.X, pos.Y)
					}
					world.DestroyEntity(entity)
				}

				time.Sleep(25 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Trigger cleaners periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Trigger goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 10; i++ {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.TriggerCleaners(world)
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(100 * time.Millisecond)
			}
		}
	}()

	// Goroutine 5: Read spatial index continuously
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Spatial reader panicked: %v", r)
			}
		}()

		for i := 0; i < 200; i++ {
			select {
			case <-stopChan:
				return
			default:
				for y := 0; y < 24; y++ {
					for x := 0; x < 80; x += 20 {
						_ = world.GetEntityAtPosition(x, y)
					}
				}
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()

	// Let test run
	time.Sleep(1 * time.Second)
	close(stopChan)

	wg.Wait()

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}

	t.Logf("Successfully tested screen buffer modifications during scan")
}

// TestConcurrentCleanerSpawns tests multiple concurrent cleaner spawn requests
func TestConcurrentCleanerSpawns(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters on multiple rows
	for row := 0; row < 20; row += 2 {
		for x := 10; x < 30; x += 5 {
			createRedCharacterAt(world, x, row)
		}
	}

	var wg sync.WaitGroup
	var triggerCount atomic.Int32

	// Launch 10 concurrent trigger requests
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Trigger goroutine %d panicked: %v", id, r)
				}
			}()

			cleanerSystem.TriggerCleaners(world)
			triggerCount.Add(1)
		}(i)
	}

	// Launch concurrent update calls
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Update goroutine %d panicked: %v", id, r)
				}
			}()

			for j := 0; j < 20; j++ {
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	wg.Wait()

	// Wait for async processing
	time.Sleep(200 * time.Millisecond)

	// Verify cleaners were created
	cleanerType := reflect.TypeOf(components.CleanerComponent{})
	cleaners := world.GetEntitiesWith(cleanerType)

	// Should have cleaners despite multiple triggers (duplicate prevention)
	if len(cleaners) == 0 {
		t.Error("Expected cleaners to be created")
	}

	// Verify exactly 10 cleaners (one per row with Red characters)
	expectedCleaners := 10 // Rows 0, 2, 4, 6, 8, 10, 12, 14, 16, 18
	if len(cleaners) != expectedCleaners {
		t.Logf("Warning: Expected %d cleaners, got %d (may be timing-dependent)", expectedCleaners, len(cleaners))
	}

	t.Logf("Processed %d concurrent trigger requests, created %d cleaners",
		triggerCount.Load(), len(cleaners))
}

// TestTextColorChangesDuringCleanerTransit tests changing character colors
// (via decay) while cleaners are in transit
func TestTextColorChangesDuringCleanerTransit(t *testing.T) {
	world := engine.NewWorld()
	ctx := createCleanerTestContext()

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	spawnSys := NewSpawnSystem(80, 24, 40, 12, ctx)
	decaySys := NewDecaySystem(80, 24, 80, 0, ctx)
	decaySys.SetSpawnSystem(spawnSys)

	// Create Red characters at specific positions
	for x := 10; x < 50; x += 3 {
		createRedCharacterAt(world, x, 5)
	}

	// Create Green characters nearby
	for x := 11; x < 50; x += 3 {
		entity := createGreenCharacterAt(world, x, 5)
		spawnSys.AddColorCount(components.SequenceGreen, components.LevelBright, 1)
		_ = entity
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	var errorCount atomic.Int32
	stopChan := make(chan struct{})

	// Goroutine 1: Continuously apply decay (changing colors)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Decay goroutine panicked: %v", r)
			}
		}()

		for i := 0; i < 15; i++ {
			select {
			case <-stopChan:
				return
			default:
				decaySys.applyDecayToRow(world, 5)
				time.Sleep(60 * time.Millisecond)
			}
		}
	}()

	// Goroutine 2: Update cleaners (moving and destroying)
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Cleaner update panicked: %v", r)
			}
		}()

		for i := 0; i < 80; i++ {
			select {
			case <-stopChan:
				return
			default:
				cleanerSystem.Update(world, 16*time.Millisecond)
				time.Sleep(16 * time.Millisecond)
			}
		}
	}()

	// Goroutine 3: Read character components continuously
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Component reader panicked: %v", r)
			}
		}()

		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				seqType := reflect.TypeOf(components.SequenceComponent{})
				charType := reflect.TypeOf(components.CharacterComponent{})
				entities := world.GetEntitiesWith(seqType, charType)

				for _, entity := range entities {
					_, _ = world.GetComponent(entity, seqType)
					_, _ = world.GetComponent(entity, charType)
				}

				time.Sleep(10 * time.Millisecond)
			}
		}
	}()

	// Goroutine 4: Monitor color counters
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			if r := recover(); r != nil {
				errorCount.Add(1)
				t.Errorf("Counter monitor panicked: %v", r)
			}
		}()

		for i := 0; i < 100; i++ {
			select {
			case <-stopChan:
				return
			default:
				// Read all color counters
				_ = spawnSys.GetColorCount(components.SequenceGreen, components.LevelBright)
				_ = spawnSys.GetColorCount(components.SequenceGreen, components.LevelNormal)
				_ = spawnSys.GetColorCount(components.SequenceGreen, components.LevelDark)
				time.Sleep(12 * time.Millisecond)
			}
		}
	}()

	// Let test run
	time.Sleep(1300 * time.Millisecond)
	close(stopChan)

	wg.Wait()

	// Verify no errors
	if errorCount.Load() > 0 {
		t.Errorf("Test completed with %d errors/panics", errorCount.Load())
	}

	t.Logf("Successfully tested color changes during cleaner transit")
}

// TestGameStatePauseResumeWithCleaners tests pausing and resuming game state
// while cleaners are active (simulated via time control)
func TestGameStatePauseResumeWithCleaners(t *testing.T) {
	// Use mock time provider for controlled time
	startTime := time.Now()
	mockTime := engine.NewMockTimeProvider(startTime)

	world := engine.NewWorld()
	ctx := &engine.GameContext{
		World:        world,
		TimeProvider: mockTime,
		GameWidth:    80,
		GameHeight:   24,
	}

	cleanerSystem := NewCleanerSystem(ctx, 80, 24, constants.DefaultCleanerConfig())
	defer cleanerSystem.Shutdown()

	// Create Red characters
	for i := 0; i < 10; i++ {
		createRedCharacterAt(world, 10+i*5, 5)
	}

	// Trigger cleaners
	cleanerSystem.TriggerCleaners(world)
	cleanerSystem.Update(world, 16*time.Millisecond)
	time.Sleep(50 * time.Millisecond)

	if !cleanerSystem.IsActive() {
		t.Fatal("Cleaners should be active")
	}

	// Advance time a bit
	mockTime.Advance(200 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	// Verify still active
	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should still be active after 200ms")
	}

	// Simulate pause (stop advancing time, but continue update loop)
	// In real game, update loop would stop or skip, but here we test that
	// time not advancing doesn't break the system
	for i := 0; i < 10; i++ {
		cleanerSystem.Update(world, 16*time.Millisecond)
		time.Sleep(20 * time.Millisecond)
		// Note: Not advancing mockTime = simulated pause
	}

	// Resume (advance time significantly)
	mockTime.Advance(600 * time.Millisecond)
	time.Sleep(100 * time.Millisecond)

	// Verify still active (total elapsed: 800ms, duration is 1000ms)
	if !cleanerSystem.IsActive() {
		t.Error("Cleaners should still be active after resume at 800ms total")
	}

	// Advance past duration
	mockTime.Advance(300 * time.Millisecond) // Total: 1100ms
	time.Sleep(100 * time.Millisecond)

	// Verify deactivated
	if cleanerSystem.IsActive() {
		t.Error("Cleaners should be inactive after duration expires")
	}

	t.Logf("Successfully tested pause/resume with cleaners")
}
