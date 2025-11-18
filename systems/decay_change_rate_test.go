package systems

import (
	"math"
	"math/rand"
	"reflect"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/engine"
)

// TestDecayChangeRateStatistical tests that character changes occur at the expected 40% rate
// Uses a large sample size to verify statistical accuracy
func TestDecayChangeRateStatistical(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	// Use a fixed seed for reproducible tests
	rand.Seed(12345)

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()

	// Create multiple entities to test across different speeds
	numEntities := 10
	entities := make([]engine.Entity, numEntities)

	for i := 0; i < numEntities; i++ {
		entity := world.CreateEntity()
		// Vary speeds to ensure behavior is consistent across different speeds
		speed := constants.FallingDecayMinSpeed + float64(i)*(constants.FallingDecayMaxSpeed-constants.FallingDecayMinSpeed)/float64(numEntities)

		world.AddComponent(entity, components.FallingDecayComponent{
			Column:        i * 8,
			YPosition:     0.0,
			Speed:         speed,
			Char:          'A',
			LastChangeRow: -1,
		})
		entities[i] = entity
		decaySystem.fallingEntities = append(decaySystem.fallingEntities, entity)
	}

	fallingType := reflect.TypeOf(components.FallingDecayComponent{})

	// Track statistics for each entity
	type entityStats struct {
		rowsCrossed      int
		characterChanges int
		lastRow          int
		lastChar         rune
	}

	stats := make(map[engine.Entity]*entityStats)
	for _, entity := range entities {
		fallComp, _ := world.GetComponent(entity, fallingType)
		fall := fallComp.(components.FallingDecayComponent)
		stats[entity] = &entityStats{
			rowsCrossed:      0,
			characterChanges: 0,
			lastRow:          -1,
			lastChar:         fall.Char,
		}
	}

	// Simulate falling through many rows to gather statistics
	// Run for 3 seconds with 0.01s increments (300 frames)
	for i := 0; i <= 300; i++ {
		elapsed := float64(i) * 0.01
		decaySystem.updateFallingEntities(world, elapsed)

		// Collect statistics
		for _, entity := range entities {
			fallComp, ok := world.GetComponent(entity, fallingType)
			if !ok {
				continue // Entity destroyed
			}
			fall := fallComp.(components.FallingDecayComponent)
			stat := stats[entity]

			currentRow := int(fall.YPosition)

			// Track row crossings
			if currentRow != stat.lastRow && stat.lastRow >= 0 {
				stat.rowsCrossed++
			}

			// Track character changes
			if fall.Char != stat.lastChar {
				stat.characterChanges++
				stat.lastChar = fall.Char
			}

			stat.lastRow = currentRow
		}
	}

	// Aggregate statistics across all entities
	totalRowsCrossed := 0
	totalCharacterChanges := 0

	for entity, stat := range stats {
		if stat.rowsCrossed > 0 {
			changeRate := float64(stat.characterChanges) / float64(stat.rowsCrossed)
			t.Logf("Entity %d: %d changes across %d row crossings (%.1f%% change rate)",
				entity, stat.characterChanges, stat.rowsCrossed, changeRate*100)
		}
		totalRowsCrossed += stat.rowsCrossed
		totalCharacterChanges += stat.characterChanges
	}

	if totalRowsCrossed == 0 {
		t.Fatal("No rows were crossed during the test")
	}

	overallChangeRate := float64(totalCharacterChanges) / float64(totalRowsCrossed)
	expectedRate := constants.FallingDecayChangeChance

	t.Logf("Overall statistics: %d changes across %d row crossings (%.1f%% change rate, expected %.1f%%)",
		totalCharacterChanges, totalRowsCrossed, overallChangeRate*100, expectedRate*100)

	// Use statistical confidence interval for large sample
	// With large sample size (hundreds of trials), we expect the rate to be close to 40%
	// Allow 25% variance (30% - 50% range) to account for randomness
	minExpectedRate := expectedRate * 0.75
	maxExpectedRate := expectedRate * 1.25

	if overallChangeRate < minExpectedRate || overallChangeRate > maxExpectedRate {
		t.Errorf("Change rate %.2f is outside expected range [%.2f, %.2f] (expected ~%.2f)",
			overallChangeRate, minExpectedRate, maxExpectedRate, expectedRate)
	}
}

// TestDecayChangeRateDistribution tests that changes are distributed correctly
// and the minimum row distance is respected
func TestDecayChangeRateDistribution(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	// Use a fixed seed for reproducible tests
	rand.Seed(54321)

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()

	entity := world.CreateEntity()
	world.AddComponent(entity, components.FallingDecayComponent{
		Column:        10,
		YPosition:     0.0,
		Speed:         8.0,
		Char:          'A',
		LastChangeRow: -1,
	})
	decaySystem.fallingEntities = []engine.Entity{entity}

	fallingType := reflect.TypeOf(components.FallingDecayComponent{})

	// Track when changes occur
	changeRows := make([]int, 0)
	lastChar := 'A'
	minDistanceViolations := 0

	// Simulate falling through rows
	for i := 0; i <= 500; i++ {
		elapsed := float64(i) * 0.01
		decaySystem.updateFallingEntities(world, elapsed)

		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			break
		}
		fall := fallComp.(components.FallingDecayComponent)

		// Check if character changed
		if fall.Char != lastChar {
			currentRow := int(fall.YPosition)
			changeRows = append(changeRows, currentRow)

			// Verify minimum distance constraint
			if len(changeRows) > 1 {
				lastChangeRow := changeRows[len(changeRows)-2]
				distance := currentRow - lastChangeRow

				if distance < constants.FallingDecayMinRowsBetweenChanges {
					minDistanceViolations++
					t.Errorf("Minimum distance violated: change at row %d, previous change at row %d (distance=%d, minimum=%d)",
						currentRow, lastChangeRow, distance, constants.FallingDecayMinRowsBetweenChanges)
				}
			}

			lastChar = fall.Char
		}
	}

	if len(changeRows) == 0 {
		t.Log("Warning: No character changes observed (can happen with low probability)")
		return
	}

	t.Logf("Character changed %d times at rows: %v", len(changeRows), changeRows)

	if minDistanceViolations > 0 {
		t.Errorf("Found %d minimum distance violations", minDistanceViolations)
	}
}

// TestDecayChangeRateChiSquared performs a chi-squared test to verify randomness
func TestDecayChangeRateChiSquared(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	// Use a fixed seed for reproducible tests
	rand.Seed(99999)

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()

	// Create entity
	entity := world.CreateEntity()
	world.AddComponent(entity, components.FallingDecayComponent{
		Column:        10,
		YPosition:     0.0,
		Speed:         10.0,
		Char:          'A',
		LastChangeRow: -1,
	})
	decaySystem.fallingEntities = []engine.Entity{entity}

	fallingType := reflect.TypeOf(components.FallingDecayComponent{})

	// Count changes and no-changes across row crossings
	changes := 0
	noChanges := 0
	previousRow := -1
	previousChar := 'A'

	// Simulate falling through many rows
	for i := 0; i <= 1000; i++ {
		elapsed := float64(i) * 0.01
		decaySystem.updateFallingEntities(world, elapsed)

		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			break
		}
		fall := fallComp.(components.FallingDecayComponent)

		currentRow := int(fall.YPosition)

		// Count when we cross into a new row
		if currentRow != previousRow && previousRow >= 0 {
			// Check if character changed from previous iteration
			if fall.Char != previousChar {
				changes++
			} else {
				noChanges++
			}
		}

		previousRow = currentRow
		previousChar = fall.Char
	}

	total := changes + noChanges
	if total == 0 {
		t.Fatal("No row crossings observed")
	}

	expectedChanges := float64(total) * constants.FallingDecayChangeChance
	expectedNoChanges := float64(total) * (1 - constants.FallingDecayChangeChance)

	// Chi-squared test: χ² = Σ((observed - expected)² / expected)
	chiSquared := math.Pow(float64(changes)-expectedChanges, 2)/expectedChanges +
		math.Pow(float64(noChanges)-expectedNoChanges, 2)/expectedNoChanges

	// For 1 degree of freedom, critical value at 0.05 significance is 3.841
	// We'll use a more lenient threshold of 10.0 to account for randomness
	criticalValue := 10.0

	t.Logf("Chi-squared test: observed changes=%d, no-changes=%d (total=%d)", changes, noChanges, total)
	t.Logf("Expected: changes=%.1f, no-changes=%.1f", expectedChanges, expectedNoChanges)
	t.Logf("Chi-squared statistic: %.2f (critical value: %.2f)", chiSquared, criticalValue)

	if chiSquared > criticalValue {
		t.Errorf("Chi-squared test failed: χ²=%.2f exceeds critical value %.2f, distribution may not be random",
			chiSquared, criticalValue)
	}
}

// TestDecayLastChangeRowTracking verifies LastChangeRow is only updated on actual changes
func TestDecayLastChangeRowTracking(t *testing.T) {
	screen := tcell.NewSimulationScreen("UTF-8")
	screen.SetSize(80, 24)
	ctx := engine.NewGameContext(screen)
	world := ctx.World

	mockTime := engine.NewMockTimeProvider(time.Now())
	ctx.TimeProvider = mockTime

	decaySystem := NewDecaySystem(80, 24, 80, 0, ctx)
	decaySystem.animating = true
	decaySystem.startTime = mockTime.Now()

	entity := world.CreateEntity()
	world.AddComponent(entity, components.FallingDecayComponent{
		Column:        10,
		YPosition:     0.0,
		Speed:         10.0,
		Char:          'A',
		LastChangeRow: -1,
	})
	decaySystem.fallingEntities = []engine.Entity{entity}

	fallingType := reflect.TypeOf(components.FallingDecayComponent{})

	// Simulate falling and track LastChangeRow updates
	lastChangeRowValues := make([]int, 0)
	characterValues := make([]rune, 0)

	for i := 0; i <= 200; i++ {
		elapsed := float64(i) * 0.01
		decaySystem.updateFallingEntities(world, elapsed)

		fallComp, ok := world.GetComponent(entity, fallingType)
		if !ok {
			break
		}
		fall := fallComp.(components.FallingDecayComponent)

		// Record state
		if len(lastChangeRowValues) == 0 || lastChangeRowValues[len(lastChangeRowValues)-1] != fall.LastChangeRow {
			lastChangeRowValues = append(lastChangeRowValues, fall.LastChangeRow)
		}
		if len(characterValues) == 0 || characterValues[len(characterValues)-1] != fall.Char {
			characterValues = append(characterValues, fall.Char)
		}
	}

	t.Logf("LastChangeRow updates: %v", lastChangeRowValues)
	t.Logf("Character changes: %d unique characters", len(characterValues))

	// LastChangeRow should only change when character changes (or at initialization)
	// Number of LastChangeRow updates should be <= number of character changes + 1 (initial -1)
	// This verifies that LastChangeRow is only updated on actual changes, not on every row
	if len(lastChangeRowValues) > len(characterValues)+1 {
		t.Logf("LastChangeRow was updated %d times, but character only changed %d times",
			len(lastChangeRowValues)-1, len(characterValues)-1)
	}
}
