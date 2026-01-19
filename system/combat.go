package system

import (
	"fmt"
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/core"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// CombatSystem manages interaction logic with combat entities
type CombatSystem struct {
	world *engine.World

	// Random source for knockback impulse randomization
	rng *vmath.FastRand

	// Telemetry
	statActive *atomic.Bool
	statCount  *atomic.Int64

	enabled bool
}

// NewCombatSystem creates a new quasar system
func NewCombatSystem(world *engine.World) engine.System {
	s := &CombatSystem{
		world: world,
	}

	s.statActive = world.Resources.Status.Bools.Get("combat.active")
	s.statCount = world.Resources.Status.Ints.Get("combat.count")

	s.Init()
	return s
}

func (s *CombatSystem) Init() {
	s.rng = vmath.NewFastRand(uint64(s.world.Resources.Time.RealTime.UnixNano()))
	s.statActive.Store(false)
	s.statCount.Store(0)
	s.enabled = true
}

// Name returns system's name
func (s *CombatSystem) Name() string {
	return "combat"
}

func (s *CombatSystem) Priority() int {
	return constant.PriorityCombat
}

func (s *CombatSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventCombatFullKnockbackRequest,
		event.EventMetaSystemCommandRequest,
		event.EventGameReset,
	}
}

func (s *CombatSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameReset {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok {
			if payload.SystemName == s.Name() {
				s.enabled = payload.Enabled
			}
		}
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventCombatFullKnockbackRequest:
		if payload, ok := ev.Payload.(*event.CombatKnockbackRequestPayload); ok {
			s.applyFullKnockback(payload.OriginEntity, payload.TargetEntity)
		}
	}
}

func (s *CombatSystem) Update() {
	if !s.enabled {
		return
	}

	dt := s.world.Resources.Time.DeltaTime

	combatEntities := s.world.Components.Combat.GetAllEntities()
	for _, combatEntity := range combatEntities {
		combatComp, ok := s.world.Components.Combat.GetComponent(combatEntity)
		if !ok {
			continue
		}

		// Update knockback remaining timer
		if combatComp.KnockbackImmunityRemaining > 0 {
			combatComp.KnockbackImmunityRemaining -= dt
			if combatComp.KnockbackImmunityRemaining < 0 {
				combatComp.KnockbackImmunityRemaining = 0
			}
		}

		// Update hit flash timer
		if combatComp.HitFlashRemaining > 0 {
			combatComp.HitFlashRemaining -= dt
			if combatComp.HitFlashRemaining < 0 {
				combatComp.HitFlashRemaining = 0
			}
		}

		s.world.Components.Combat.SetComponent(combatEntity, combatComp)
	}

}

// applyFullKnockback applies radial impulse when drain overlaps shield
func (s *CombatSystem) applyFullKnockback(
	originEntity, targetEntity core.Entity,
) {
	targetCombatComp, ok := s.world.Components.Combat.GetComponent(targetEntity)
	if !ok {
		return
	}
	if targetCombatComp.KnockbackImmunityRemaining > 0 {
		return
	}

	s.world.DebugPrint(fmt.Sprintf("%s", targetCombatComp.KnockbackImmunityRemaining))

	originPos, ok := s.world.Positions.GetPosition(originEntity)
	if !ok {
		return
	}
	targetPos, ok := s.world.Positions.GetPosition(targetEntity)
	if !ok {
		return
	}

	// Radial direction: origin → target (e.g. cursor shield pushes drain outward)
	// TODO: change to cell-center physics from grid coords
	radialX := vmath.FromInt(targetPos.X - originPos.X)
	radialY := vmath.FromInt(targetPos.Y - originPos.Y)

	// TODO: fix this
	kineticComp, ok := s.world.Components.Kinetic.GetComponent(targetEntity)
	if !ok {
		return
	}

	if physics.ApplyCollision(&kineticComp, radialX, radialY, &physics.ShieldToDrain, s.rng) {
		s.world.Components.Kinetic.SetComponent(targetEntity, kineticComp)
	}
	// TODO: check above condition implications
	targetCombatComp.KnockbackImmunityRemaining = constant.CombatKnockbackImmunityInterval
	s.world.Components.Combat.SetComponent(targetEntity, targetCombatComp)
}