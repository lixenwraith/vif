package system

import (
	"encoding/json"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath"
)

// EnvironmentSystem applies gameplay effects whose source is global to the map.
// It is dual-domain because one shared wind acts on both player drains and shared
// species; each write is made inside an explicit domain scope.
type EnvironmentSystem struct {
	world *engine.World
	rng   *vmath.FastRand

	windActive    bool
	windApplied   bool
	windForce     float64
	windDirection float64
	windRemaining time.Duration

	statWindActive *atomic.Bool
	enabled        bool
}

// NewEnvironmentSystem creates the global gameplay environment system.
func NewEnvironmentSystem(world *engine.World) engine.System {
	s := &EnvironmentSystem{
		world: world,
	}

	s.statWindActive = world.Resources.Status.Bools.Get("environment.wind_active")
	s.Init()
	return s
}

func (s *EnvironmentSystem) Init() {
	if s.windApplied {
		s.clearPlanarWind()
	}
	s.rng = s.world.Rand(core.DomainShared, s.Name())
	s.windActive = false
	s.windApplied = false
	s.windForce = 0
	s.windDirection = 0
	s.windRemaining = 0
	s.enabled = true
	s.publishStatus()
}

// Name returns the system's name.
func (s *EnvironmentSystem) Name() string {
	return "environment"
}

func (s *EnvironmentSystem) Priority() int {
	return parameter.PriorityEnvironment
}

func (s *EnvironmentSystem) EventTypes() []event.EventType {
	return []event.EventType{
		event.EventWindStart,
		event.EventWindCancel,
		event.EventMetaSystemCommandRequest,
		event.EventGameResetRequest,
	}
}

func (s *EnvironmentSystem) HandleEvent(ev event.GameEvent) {
	if ev.Type == event.EventGameResetRequest {
		s.Init()
		return
	}

	if ev.Type == event.EventMetaSystemCommandRequest {
		if payload, ok := ev.Payload.(*event.MetaSystemCommandPayload); ok && payload.SystemName == s.Name() {
			s.enabled = payload.Enabled
			if !s.enabled {
				s.clearAppliedWind()
			}
			s.publishStatus()
		}
	}

	if !s.enabled {
		return
	}

	switch ev.Type {
	case event.EventWindStart:
		if payload, ok := ev.Payload.(*event.WindStartPayload); ok {
			s.startWind(payload)
		}
	case event.EventWindCancel:
		s.cancelWind()
	}
}

func (s *EnvironmentSystem) Update() {
	if !s.enabled || !s.windActive {
		s.clearAppliedWind()
		s.publishStatus()
		return
	}

	delta := s.world.Resources.Time.DeltaTime
	if delta <= 0 {
		s.publishStatus()
		return
	}

	// Every active tick consumes exactly two Shared-stream draws, independent of
	// entity counts or domains: one for force and one for direction.
	force, direction := s.sampleWind()
	windSeconds := min(delta, s.windRemaining).Seconds()
	motionSeconds := min(delta.Seconds(), parameter.MaxSimulationDeltaSeconds)
	if windSeconds > motionSeconds {
		windSeconds = motionSeconds
	}

	if motionSeconds > 0 && windSeconds > 0 {
		// A wind ending part-way through a tick contributes only that fraction to
		// the 2D integrators, which consume acceleration over motionSeconds.
		force *= windSeconds / motionSeconds
		forceX := force * vmath.CosF(direction)
		forceY := force * vmath.SinF(direction)

		s.applyPlanarWind(forceX, forceY)
		s.applyStormWind(forceX, forceY, motionSeconds)
		s.windApplied = true
	}

	s.windRemaining -= min(delta, s.windRemaining)
	if s.windRemaining <= 0 {
		// Leave this tick's acceleration in place for the species systems that run
		// after Environment. The next tick (or a cancel) clears it.
		s.windActive = false
		s.windForce = 0
		s.windDirection = 0
		s.windRemaining = 0
	}
	s.publishStatus()
}

func (s *EnvironmentSystem) startWind(payload *event.WindStartPayload) {
	if payload == nil || !finite(payload.Force) || payload.Force <= 0 ||
		!finite(payload.Direction) || payload.Duration <= 0 {
		return
	}

	// A new valid start replaces the previous wind completely.
	s.clearAppliedWind()
	s.windActive = true
	s.windForce = payload.Force
	s.windDirection = vmath.NormalizeAngleF(payload.Direction)
	s.windRemaining = payload.Duration
	s.publishStatus()
}

func (s *EnvironmentSystem) cancelWind() {
	s.clearAppliedWind()
	s.windActive = false
	s.windForce = 0
	s.windDirection = 0
	s.windRemaining = 0
	s.publishStatus()
}

func (s *EnvironmentSystem) sampleWind() (force, direction float64) {
	forceSpread := (s.rng.Float64()*2 - 1) * parameter.WindForceVariationRatio
	directionSpread := (s.rng.Float64()*2 - 1) * parameter.WindDirectionVariation
	return s.windForce * (1 + forceSpread), vmath.NormalizeAngleF(s.windDirection + directionSpread)
}

// applyPlanarWind writes acceleration only to the mobile species currently
// backed by the ordinary 2D integrators. In particular it deliberately does not
// range every Kinetic component, so cleaners, missiles, bullets, loot, weapon
// orbs, dust, decay, and blossom are excluded.
func (s *EnvironmentSystem) applyPlanarWind(forceX, forceY float64) {
	apply := func(entities []core.Entity, mass profile.Mass) {
		accelX := forceX / mass
		accelY := forceY / mass
		for _, entity := range entities {
			if kinetic, ok := s.world.Components.Kinetic.GetPtr(entity); ok {
				kinetic.AccelX = accelX
				kinetic.AccelY = accelY
			}
		}
	}

	s.world.WithDomain(core.DomainPlayer, func() {
		apply(s.world.Components.Drain.Entities(), profile.MassDrain)
	})
	s.world.WithDomain(core.DomainShared, func() {
		apply(s.world.Components.Swarm.Entities(), profile.MassSwarm)
		apply(s.world.Components.Eye.Entities(), profile.MassEye)
		apply(s.world.Components.SnakeHead.Entities(), profile.MassSnakeHead)
		apply(s.world.Components.Quasar.Entities(), profile.MassQuasar)
		// Pylons are a species and have a mass profile, but currently have no
		// Kinetic component; keeping them in the typed path makes that immobility
		// explicit and lets a future kinetic pylon inherit wind naturally.
		apply(s.world.Components.Pylon.Entities(), profile.MassPylon)
	})
}

// applyStormWind bridges the environment into StormSystem's authoritative 3D
// velocity. Storm folds this delta from Kinetic.Vel into Vel3D at the start of
// its update. A stunned circle skips that fold, so do not accumulate a hidden
// impulse while it is frozen.
func (s *EnvironmentSystem) applyStormWind(forceX, forceY, seconds float64) {
	s.world.WithDomain(core.DomainShared, func() {
		for _, entity := range s.world.Components.StormCircle.Entities() {
			if combat, ok := s.world.Components.Combat.GetComponent(entity); ok && combat.StunnedRemaining > 0 {
				continue
			}
			if kinetic, ok := s.world.Components.Kinetic.GetPtr(entity); ok {
				kinetic.VelX += forceX / profile.MassStorm * seconds
				kinetic.VelY += forceY / profile.MassStorm * seconds
			}
		}
	})
}

func (s *EnvironmentSystem) clearAppliedWind() {
	if !s.windApplied {
		return
	}
	s.clearPlanarWind()
	s.windApplied = false
}

func (s *EnvironmentSystem) clearPlanarWind() {
	s.applyPlanarWind(0, 0)
}

func (s *EnvironmentSystem) publishStatus() {
	s.statWindActive.Store(s.enabled && s.windActive)
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// environmentSnapshot is the D-19 continuation point. Shared capture already
// includes every shared Kinetic component and the environment RNG stream; this
// record carries the private base wind, remaining time, and whether one tick's
// acceleration still needs to be replaced or cleared.
type environmentSnapshot struct {
	Active    bool          `json:"active"`
	Applied   bool          `json:"applied"`
	Enabled   bool          `json:"enabled"`
	Force     float64       `json:"force"`
	Direction float64       `json:"direction"`
	Remaining time.Duration `json:"remaining"`
}

// SaveShared exports the global environment continuation state (D-19).
func (s *EnvironmentSystem) SaveShared() ([]byte, error) {
	return json.Marshal(environmentSnapshot{
		Active:    s.windActive,
		Applied:   s.windApplied,
		Enabled:   s.enabled,
		Force:     s.windForce,
		Direction: s.windDirection,
		Remaining: s.windRemaining,
	})
}

// LoadShared installs a captured environment continuation point. RNG position
// is restored by RandResource alongside this record, not duplicated here.
func (s *EnvironmentSystem) LoadShared(data []byte) error {
	var snap environmentSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return fmt.Errorf("environment: %w", err)
	}
	if !finite(snap.Force) || !finite(snap.Direction) || snap.Remaining < 0 {
		return fmt.Errorf("environment: invalid wind state")
	}
	if snap.Active && (snap.Force <= 0 || snap.Remaining <= 0) {
		return fmt.Errorf("environment: active wind requires positive force and duration")
	}

	s.clearAppliedWind()
	s.enabled = snap.Enabled
	s.windActive = snap.Active
	s.windApplied = snap.Applied
	s.windForce = snap.Force
	s.windDirection = vmath.NormalizeAngleF(snap.Direction)
	s.windRemaining = snap.Remaining
	if !s.windActive {
		s.windForce = 0
		s.windDirection = 0
		s.windRemaining = 0
	}
	s.publishStatus()
	return nil
}
