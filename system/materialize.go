package system

import (
	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
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
	dtFixed := vmath.FromFloat(s.res.Time.DeltaTime.Seconds())
	// Cap delta time to prevent tunneling on lag spikes
	dtCap := vmath.FromFloat(0.1)
	if dtFixed > dtCap {
		dtFixed = dtCap
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

		// Group by target position (grid coords)
		tX := vmath.ToInt(mat.TargetX)
		tY := vmath.ToInt(mat.TargetY)
		key := uint64(tX)<<32 | uint64(tY)
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

		// Physics Update
		mat.Integrate(dtFixed)

		// Check if spawner reached target based on direction
		arrived := false
		switch mat.Direction {
		case component.MaterializeFromTop:
			arrived = mat.PreciseY >= mat.TargetY
		case component.MaterializeFromBottom:
			arrived = mat.PreciseY <= mat.TargetY
		case component.MaterializeFromLeft:
			arrived = mat.PreciseX >= mat.TargetX
		case component.MaterializeFromRight:
			arrived = mat.PreciseX <= mat.TargetX
		}

		// If materializer arrived at target, clamp position to target and zeroes velocity
		if arrived {
			mat.PreciseX = mat.TargetX
			mat.PreciseY = mat.TargetY
			mat.VelX = 0
			mat.VelY = 0
			mat.Arrived = true
		} else {
			state.allArrived = false
		}

		// Trail Update & Grid Sync
		newGridX := vmath.ToInt(mat.PreciseX)
		newGridY := vmath.ToInt(mat.PreciseY)

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

	// Target Completion Handling
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

	targetXFixed := vmath.FromInt(targetX)
	targetYFixed := vmath.FromInt(targetY)
	gameWidthFixed := vmath.FromInt(config.GameWidth)
	gameHeightFixed := vmath.FromInt(config.GameHeight)
	negOne := vmath.FromInt(-1)
	durationFixed := vmath.FromFloat(constant.MaterializeAnimationDuration.Seconds())

	type spawnerDef struct {
		startX, startY int32
		dir            component.MaterializeDirection
	}

	// Define 4 spawner entities converging from edges
	spawners := []spawnerDef{
		{targetXFixed, negOne, component.MaterializeFromTop},
		{targetXFixed, gameHeightFixed, component.MaterializeFromBottom},
		{negOne, targetYFixed, component.MaterializeFromLeft},
		{gameWidthFixed, targetYFixed, component.MaterializeFromRight},
	}

	for _, def := range spawners {
		velX := vmath.Div(targetXFixed-def.startX, durationFixed)
		velY := vmath.Div(targetYFixed-def.startY, durationFixed)

		startGridX := vmath.ToInt(def.startX)
		startGridY := vmath.ToInt(def.startY)

		var trailRing [constant.MaterializeTrailLength]core.Point
		trailRing[0] = core.Point{X: startGridX, Y: startGridY}

		comp := component.MaterializeComponent{
			KineticState: component.KineticState{
				PreciseX: def.startX,
				PreciseY: def.startY,
				VelX:     velX,
				VelY:     velY,
			},
			TargetX:   targetXFixed,
			TargetY:   targetYFixed,
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
		// Protect from death/drain during animation
		s.protStore.Add(entity, component.ProtectionComponent{
			Mask: component.ProtectFromDrain | component.ProtectFromDeath,
		})
	}
}