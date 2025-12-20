package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
)

// MaterializeSystem manages materializer animations and triggering spawn completion
type MaterializeSystem struct {
	world *engine.World
	res   engine.Resources

	matStore  *engine.Store[component.MaterializeComponent]
	protStore *engine.Store[component.ProtectionComponent]
}

// NewMaterializeSystem creates a new materialize system
func NewMaterializeSystem(world *engine.World) engine.System {
	return &MaterializeSystem{
		world: world,
		res:   engine.GetResources(world),

		matStore:  engine.GetStore[component.MaterializeComponent](world),
		protStore: engine.GetStore[component.ProtectionComponent](world),
	}
}

// Init
func (s *MaterializeSystem) Init() {}

// Priority returns the system's priority
// Must run before DrainSystem which listens to completion
func (s *MaterializeSystem) Priority() int {
	return constant.PriorityMaterialize
}

// EventTypes returns event types handled
func (s *MaterializeSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventMaterializeRequest,
	}
}

// HandleEvent processes requests to spawn visual effects
func (s *MaterializeSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventMaterializeRequest {
		if payload, ok := ev.Payload.(*event.MaterializeRequestPayload); ok {
			s.spawnMaterializers(payload.X, payload.Y, payload.Type)
		}
	}
}

// Update updates materialize spawner entities and triggers spawn completion events
func (s *MaterializeSystem) Update() {
	dtSeconds := s.res.Time.DeltaTime.Seconds()
	// Cap delta time to prevent tunneling on lag spikes
	if dtSeconds > 0.1 {
		dtSeconds = 0.1
	}

	entities := s.matStore.All()
	if len(entities) == 0 {
		return
	}

	type targetState struct {
		entities   []core.Entity
		spawnType  component.SpawnType
		allArrived bool
	}
	// Group materializers by target position (4 entities per target)
	targets := make(map[uint64]*targetState)

	for _, entity := range entities {
		mat, ok := s.matStore.Get(entity)
		if !ok {
			continue
		}

		// Read grid position from PositionStore
		oldPos, hasPos := s.world.Positions.Get(entity)
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
		case component.MaterializeFromTop:
			arrived = mat.PreciseY >= float64(mat.TargetY)
		case component.MaterializeFromBottom:
			arrived = mat.PreciseY <= float64(mat.TargetY)
		case component.MaterializeFromLeft:
			arrived = mat.PreciseX >= float64(mat.TargetX)
		case component.MaterializeFromRight:
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
			mat.TrailHead = (mat.TrailHead + 1) % constant.MaterializeTrailLength
			mat.TrailRing[mat.TrailHead] = core.Point{X: newGridX, Y: newGridY}
			if mat.TrailLen < constant.MaterializeTrailLength {
				mat.TrailLen++
			}

			// Sync grid position to PositionStore
			s.world.Positions.Add(entity, component.PositionComponent{X: newGridX, Y: newGridY})
		}

		s.matStore.Add(entity, mat)
	}

	// --- Target Completion Handling ---
	for key, state := range targets {
		if state.allArrived && len(state.entities) > 0 {
			// Destroy all materializers in this group
			for _, entity := range state.entities {
				s.world.DestroyEntity(entity)
			}

			// Emit completion event
			targetX := int(key >> 32)
			targetY := int(key & 0xFFFFFFFF)

			s.world.PushEvent(event.EventMaterializeComplete, &event.SpawnCompletePayload{
				X:    targetX,
				Y:    targetY,
				Type: state.spawnType,
			})
		}
	}
}

// spawnMaterializers creates the visual entities converging on the target
// Logic moved from DrainSystem
func (s *MaterializeSystem) spawnMaterializers(targetX, targetY int, spawnType component.SpawnType) {
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
	duration := constant.MaterializeAnimationDuration.Seconds()

	type spawnerDef struct {
		startX, startY float64
		dir            component.MaterializeDirection
	}

	// Define 4 spawner entities converging from edges
	spawners := []spawnerDef{
		{tX, -1, component.MaterializeFromTop},
		{tX, gameHeight, component.MaterializeFromBottom},
		{-1, tY, component.MaterializeFromLeft},
		{gameWidth, tY, component.MaterializeFromRight},
	}

	for _, def := range spawners {
		velX := (tX - def.startX) / duration
		velY := (tY - def.startY) / duration

		startGridX := int(def.startX)
		startGridY := int(def.startY)

		var trailRing [constant.MaterializeTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := component.MaterializeComponent{
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
			Char:      constant.MaterializeChar,
			Arrived:   false,
			Type:      spawnType,
		}

		// Create Entity
		entity := s.world.CreateEntity()
		s.world.Positions.Add(entity, component.PositionComponent{X: startGridX, Y: startGridY})
		s.matStore.Add(entity, comp)
		// Protect from cull/drain during animation
		s.protStore.Add(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromCull,
		})
	}
}