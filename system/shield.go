package system

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/component"
	"github.com/lixenwraith/vi-fighter/constant"
	"github.com/lixenwraith/vi-fighter/engine"
	"github.com/lixenwraith/vi-fighter/event"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// ShieldSystem owns shield activation state and processes drain events
type ShieldSystem struct {
	world *engine.World
	res   engine.Resources

	shieldStore *engine.Store[component.ShieldComponent]

	statActive *atomic.Bool
}

// NewShieldSystem creates a new shield system
func NewShieldSystem(world *engine.World) engine.System {
	res := engine.GetResources(world)
	s := &ShieldSystem{
		world: world,
		res:   res,

		shieldStore: engine.GetStore[component.ShieldComponent](world),

		statActive: res.Status.Bools.Get("shield.active"),
	}
	s.initLocked()
	return s
}

// Init resets session state for new game
func (s *ShieldSystem) Init() {
	s.initLocked()
}

// initLocked performs session state reset
func (s *ShieldSystem) initLocked() {
	s.statActive.Store(false)
	s.cacheInverseRadii()
}

// cacheInverseRadii precomputes Q16.16 inverse squared radii for ellipse checks
func (s *ShieldSystem) cacheInverseRadii() {
	cursorEntity := s.res.Cursor.Entity
	shield, ok := s.shieldStore.Get(cursorEntity)
	if !ok {
		panic(nil)
	}
	rx := vmath.FromFloat(constant.ShieldRadiusX)
	ry := vmath.FromFloat(constant.ShieldRadiusY)

	shield.RadiusX = rx
	shield.RadiusY = ry

	rxSq := vmath.Mul(rx, rx)
	rySq := vmath.Mul(ry, ry)
	shield.InvRxSq = vmath.Div(vmath.Scale, rxSq)
	shield.InvRySq = vmath.Div(vmath.Scale, rySq)

	s.shieldStore.Add(cursorEntity, shield)
}

// Priority returns the system's priority
func (s *ShieldSystem) Priority() int {
	return constant.PriorityShield
}

// EventTypes returns the event types ShieldSystem handles
func (s *ShieldSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventShieldActivate,
		event.EventShieldDeactivate,
		event.EventShieldDrain,
		event.EventGameReset,
	}
}

// HandleEvent processes shield-related events from the router
func (s *ShieldSystem) HandleEvent(ev event.GameEvent) {
	cursorEntity := s.res.Cursor.Entity

	switch ev.Type {
	case event.EventShieldActivate:
		shield, ok := s.shieldStore.Get(cursorEntity)
		if ok {
			shield.Active = true
			s.shieldStore.Add(cursorEntity, shield)
		}
		s.statActive.Store(true)

	case event.EventShieldDeactivate:
		shield, ok := s.shieldStore.Get(cursorEntity)
		if ok {
			shield.Active = false
			s.shieldStore.Add(cursorEntity, shield)
		}
		s.statActive.Store(false)

	case event.EventShieldDrain:
		if payload, ok := ev.Payload.(*event.ShieldDrainPayload); ok {
			s.applyConvergentDrain(payload.Amount)
		}

	case event.EventGameReset:
		s.Init()
	}
}

// Update handles passive shield drain
func (s *ShieldSystem) Update() {
	cursorEntity := s.res.Cursor.Entity

	shield, ok := s.shieldStore.Get(cursorEntity)
	if !ok || !shield.Active {
		return
	}

	now := s.res.Time.GameTime

	if now.Sub(shield.LastDrainTime) >= constant.ShieldPassiveDrainInterval {
		s.applyConvergentDrain(constant.ShieldPassiveDrainAmount)
		shield.LastDrainTime = now
		s.shieldStore.Add(cursorEntity, shield)
	}
}

// applyConvergentDrain reduces energy magnitude toward zero by amount, clamping at zero
func (s *ShieldSystem) applyConvergentDrain(amount int) {
	cursorEntity := s.res.Cursor.Entity

	energyStore := engine.GetStore[component.EnergyComponent](s.world)
	energyComp, ok := energyStore.Get(cursorEntity)
	if !ok {
		return
	}

	current := energyComp.Current.Load()
	if current == 0 {
		return
	}

	var delta int64
	if current > 0 {
		// Positive energy: subtract, clamp at 0
		if current > int64(amount) {
			delta = -int64(amount)
		} else {
			delta = -current // Clamp to exactly 0
		}
	} else {
		// Negative energy: add, clamp at 0
		if -current > int64(amount) {
			delta = int64(amount)
		} else {
			delta = -current // Clamp to exactly 0
		}
	}

	s.world.PushEvent(event.EventEnergyAdd, &event.EnergyAddPayload{
		Delta: int(delta),
	})
}