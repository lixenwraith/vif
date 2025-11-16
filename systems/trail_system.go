package systems

import (
	"reflect"
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/engine"
)

const (
	trailLength  = 8
	trailDecayMs = 50
)

// TrailSystem manages trail effects
type TrailSystem struct{}

// NewTrailSystem creates a new trail system
func NewTrailSystem() *TrailSystem {
	return &TrailSystem{}
}

// Priority returns the system's priority
func (s *TrailSystem) Priority() int {
	return 20
}

// Update updates all trail particles
func (s *TrailSystem) Update(world *engine.World, dt time.Duration) {
	trailType := reflect.TypeOf(components.TrailComponent{})
	posType := reflect.TypeOf(components.PositionComponent{})

	entities := world.GetEntitiesWith(trailType, posType)
	now := time.Now()

	for _, entity := range entities {
		trailComp, _ := world.GetComponent(entity, trailType)
		trail := trailComp.(components.TrailComponent)

		// Check if trail should decay
		elapsed := now.Sub(trail.Timestamp).Seconds()
		if elapsed < 0 {
			// Future trail point - skip
			continue
		} else if elapsed >= 0.5 {
			// Trail expired - remove entity
			world.DestroyEntity(entity)
		} else {
			// Update intensity
			trail.Intensity *= (1.0 - elapsed*2)
			if trail.Intensity <= 0.05 {
				world.DestroyEntity(entity)
			} else {
				world.AddComponent(entity, trail)
			}
		}
	}
}

// AddTrail creates trail particles from one position to another
func AddTrail(world *engine.World, fromX, fromY, toX, toY int) {
	steps := trailLength
	dx := float64(toX - fromX)
	dy := float64(toY - fromY)

	for i := 1; i <= steps; i++ {
		progress := float64(i) / float64(steps)
		x := fromX + int(dx*progress)
		y := fromY + int(dy*progress)

		entity := world.CreateEntity()
		world.AddComponent(entity, components.PositionComponent{X: x, Y: y})
		world.AddComponent(entity, components.TrailComponent{
			Intensity: 1.0 - progress*0.8,
			Timestamp: time.Now().Add(time.Duration(i) * trailDecayMs * time.Millisecond),
		})
	}
}
