package systems

import (
	"time"

	"github.com/lixenwraith/vi-fighter/components"
	"github.com/lixenwraith/vi-fighter/constants"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/events"
)

// MaterializeSystem manages materializer animations and triggering spawn completion
type MaterializeSystem struct {
	world *engine.World
	res   engine.CoreResources
}

// NewMaterializeSystem creates a new materialize system
func NewMaterializeSystem(world *engine.World) *MaterializeSystem {
	return &MaterializeSystem{
		world: world,
		res:   engine.GetCoreResources(world),
	}
}

// Priority returns the system's priority
// Must run before DrainSystem which listens to completion
func (s *MaterializeSystem) Priority() int {
	return constants.PriorityMaterializer
}

// EventTypes returns event types handled
func (s *MaterializeSystem) EventTypes() []events.EventType {
	return []events.EventType{
		events.EventMaterializeRequest,
	}
}

// HandleEvent processes requests to spawn visual effects
func (s *MaterializeSystem) HandleEvent(world *engine.World, event events.GameEvent) {
	if event.Type == events.EventMaterializeRequest {
		if payload, ok := event.Payload.(*events.MaterializeRequestPayload); ok {
			s.spawnMaterializers(world, payload.X, payload.Y, payload.Type)
		}
	}
}

// Update updates materialize spawner entities and triggers spawn completion events
func (s *MaterializeSystem) Update(world *engine.World, dt time.Duration) {
	dtSeconds := dt.Seconds()
	// Cap delta time to prevent tunneling on lag spikes
	if dtSeconds > 0.1 {
		dtSeconds = 0.1
	}

	entities := world.Materializers.All()
	if len(entities) == 0 {
		return
	}

	type targetState struct {
		entities   []core.Entity
		spawnType  components.SpawnType
		allArrived bool
	}
	// Group materializers by target position (4 entities per target)
	targets := make(map[uint64]*targetState)

	for _, entity := range entities {
		mat, ok := world.Materializers.Get(entity)
		if !ok {
			continue
		}

		// Read grid position from PositionStore
		oldPos, hasPos := world.Positions.Get(entity)
		if !hasPos {
			continue
		}

		// Group by target position
		key := uint64(mat.TargetX)<<32 | uint64(mat.TargetY)
		if targets[key] == nil {
			targets[key] = &targetState{
				entities:   make([]core.Entity, 0, 4),
				spawnType:  mat.Type,
				allArrived: true,
			}
		}

		state := targets[key]
		state.entities = append(state.entities, entity)

		if mat.Arrived {
			continue
		}

		// --- Physics Update ---
		mat.PreciseX += mat.VelocityX * dtSeconds
		mat.PreciseY += mat.VelocityY * dtSeconds

		// Check arrival based on direction
		arrived := false
		switch mat.Direction {
		case components.MaterializeFromTop:
			arrived = mat.PreciseY >= float64(mat.TargetY)
		case components.MaterializeFromBottom:
			arrived = mat.PreciseY <= float64(mat.TargetY)
		case components.MaterializeFromLeft:
			arrived = mat.PreciseX >= float64(mat.TargetX)
		case components.MaterializeFromRight:
			arrived = mat.PreciseX <= float64(mat.TargetX)
		}

		if arrived {
			mat.PreciseX = float64(mat.TargetX)
			mat.PreciseY = float64(mat.TargetY)
			mat.Arrived = true
		} else {
			state.allArrived = false
		}

		// --- Trail Update & Grid Sync ---
		newGridX := int(mat.PreciseX)
		newGridY := int(mat.PreciseY)

		if newGridX != oldPos.X || newGridY != oldPos.Y {
			mat.TrailHead = (mat.TrailHead + 1) % constants.MaterializeTrailLength
			mat.TrailRing[mat.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if mat.TrailLen < constants.MaterializeTrailLength {
				mat.TrailLen++
			}

			// Sync grid position to PositionStore
			world.Positions.Add(entity, components.PositionComponent{X: newGridX, Y: newGridY})
		}

		world.Materializers.Add(entity, mat)
	}

	// --- Target Completion Handling ---
	for key, state := range targets {
		if state.allArrived && len(state.entities) > 0 {
			// Destroy all materializers in this group
			for _, entity := range state.entities {
				world.DestroyEntity(entity)
			}

			// Emit completion event
			targetX := int(key >> 32)
			targetY := int(key & 0xFFFFFFFF)

			world.PushEvent(events.EventMaterializeComplete, &events.SpawnCompletePayload{
				X:    targetX,
				Y:    targetY,
				Type: state.spawnType,
			})
		}
	}
}

// spawnMaterializers creates the visual entities converging on the target
// Logic moved from DrainSystem
func (s *MaterializeSystem) spawnMaterializers(world *engine.World, targetX, targetY int, spawnType components.SpawnType) {
	config := s.res.Config

	// Clamp target coordinates
	if targetX < 0 {
		targetX = 0
	}
	if targetX >= config.GameWidth {
		targetX = config.GameWidth - 1
	}
	if targetY < 0 {
		targetY = 0
	}
	if targetY >= config.GameHeight {
		targetY = config.GameHeight - 1
	}

	gameWidth := float64(config.GameWidth)
	gameHeight := float64(config.GameHeight)
	tX := float64(targetX)
	tY := float64(targetY)
	duration := constants.MaterializeAnimationDuration.Seconds()

	type spawnerDef struct {
		startX, startY float64
		dir            components.MaterializeDirection
	}

	// Define 4 spawner entities converging from edges
	spawners := []spawnerDef{
		{tX, -1, components.MaterializeFromTop},
		{tX, gameHeight, components.MaterializeFromBottom},
		{-1, tY, components.MaterializeFromLeft},
		{gameWidth, tY, components.MaterializeFromRight},
	}

	for _, def := range spawners {
		velX := (tX - def.startX) / duration
		velY := (tY - def.startY) / duration

		startGridX := int(def.startX)
		startGridY := int(def.startY)

		var trailRing [constants.MaterializeTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := components.MaterializeComponent{
			PreciseX:  def.startX,
			PreciseY:  def.startY,
			VelocityX: velX,
			VelocityY: velY,
			TargetX:   targetX,
			TargetY:   targetY,
			TrailRing: trailRing,
			TrailHead: 0,
			TrailLen:  1,
			Direction: def.dir,
			Char:      constants.MaterializeChar,
			Arrived:   false,
			Type:      spawnType,
		}

		// Create Entity
		entity := world.CreateEntity()
		world.Positions.Add(entity, components.PositionComponent{X: startGridX, Y: startGridY})
		world.Materializers.Add(entity, comp)
		// Protect from cull/drain during animation
		world.Protections.Add(entity, components.ProtectionComponent{
			Mask: components.ProtectFromDrain | components.ProtectFromCull,
		})
	}
}