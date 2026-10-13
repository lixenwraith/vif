package system

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/internal/status"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// EnvironmentSystem applies gameplay effects whose source is global to the map.
// It is dual-domain because one shared wind acts on both player drains and shared
// species, and one shared drift moves every instance's own glyphs; each write is
// made inside an explicit domain scope.
type EnvironmentSystem struct {
	world *engine.World
	rng   *vmath.FastRand

	windActive    bool
	windApplied   bool
	windForce     float64
	windDirection float64
	windRemaining time.Duration

	drift driftState
	// Velocity per level and axis in microcells/second, derived from drift
	driftVelocity [driftLevels][2]int64
	driftLayer    []core.Entity
	driftDoomed   []core.Entity

	statWindActive     *atomic.Bool
	statDriftActive    *atomic.Bool
	statDriftDirection *status.AtomicFloat
	toggle
}

const driftLevels = len(parameter.DriftLayerFactor)

// driftCell is one cell of phase: velocity in microcells/second times elapsed
// nanoseconds, kept in integers so every instance crosses on the same tick.
const driftCell = int64(1e6) * int64(time.Second)

// driftState is the whole of a drift, and its D-19 record. Phase is each level's
// sub-cell remainder per axis, so a layer steps on the same tick everywhere.
type driftState struct {
	Active    bool                  `json:"active"`
	Endless   bool                  `json:"endless"`
	Speed     float64               `json:"speed"`
	Direction float64               `json:"direction"`
	Remaining time.Duration         `json:"remaining"`
	Phase     [driftLevels][2]int64 `json:"phase"`
}

// NewEnvironmentSystem creates the global gameplay environment system.
func NewEnvironmentSystem(world *engine.World) engine.System {
	s := &EnvironmentSystem{
		world: world,
	}

	s.statWindActive = world.Resources.Status.Bools.Get("environment.wind_active")
	s.statDriftActive = world.Resources.Status.Bools.Get("environment.drift_active")
	s.statDriftDirection = world.Resources.Status.Floats.Get("environment.drift_direction")
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
	s.endDrift()
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
		event.EventDriftStart,
		event.EventDriftCancel,
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
	case event.EventDriftStart:
		if payload, ok := ev.Payload.(*event.DriftStartPayload); ok {
			s.startDrift(payload)
		}
	case event.EventDriftCancel:
		s.endDrift()
		s.publishStatus()
	}
}

func (s *EnvironmentSystem) Update() {
	if s.enabled {
		delta := s.world.Resources.Time.DeltaTime
		s.updateWind(delta)
		s.updateDrift(delta)
	} else {
		s.clearAppliedWind()
	}
	s.publishStatus()
}

func (s *EnvironmentSystem) updateWind(delta time.Duration) {
	if !s.windActive {
		s.clearAppliedWind()
		return
	}
	if delta <= 0 {
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
// range every Kinetic component, so tracers, missiles, bullets, loot, weapon
// orbs, dust, and particle behaviors are excluded.
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
			if combat, ok := s.world.Components.Combat.GetComponent(entity); ok && combat.Stunned() {
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

// startDrift replaces any drift but keeps each layer's phase, so a change of pace
// does not jolt text already on its way to the next cell.
func (s *EnvironmentSystem) startDrift(payload *event.DriftStartPayload) {
	if payload == nil || !finite(payload.Direction) || !finite(payload.Speed) || payload.Speed <= 0 ||
		payload.DurationMs < 0 || payload.DurationMs > int64(math.MaxInt64/time.Millisecond) {
		return
	}
	s.drift.Active = true
	s.drift.Endless = payload.DurationMs == 0
	s.drift.Speed = min(payload.Speed, parameter.DriftMaxSpeed)
	s.drift.Direction = vmath.NormalizeAngleF(payload.Direction)
	s.drift.Remaining = time.Duration(payload.DurationMs) * time.Millisecond
	s.deriveDriftVelocity()
	s.publishStatus()
}

// endDrift leaves the one inactive state every path that stops a drift encodes alike
func (s *EnvironmentSystem) endDrift() {
	s.drift = driftState{}
	s.driftVelocity = [driftLevels][2]int64{}
}

// deriveDriftVelocity uses exact trigonometry rather than the lookup tables: a
// table bin leaves a cardinal drift a stray cross component that steps a row
// every minute or so. Rounding to whole microcells absorbs the last bit.
func (s *EnvironmentSystem) deriveDriftVelocity() {
	sin, cos := math.Sincos(s.drift.Direction)
	for level, factor := range parameter.DriftLayerFactor {
		micro := s.drift.Speed * factor * 1e6
		s.driftVelocity[level] = [2]int64{
			int64(math.Round(micro * cos)),
			int64(math.Round(micro * sin * parameter.DriftAspectY)),
		}
	}
}

func (s *EnvironmentSystem) updateDrift(delta time.Duration) {
	if !s.drift.Active || delta <= 0 {
		return
	}
	elapsed := min(delta, time.Duration(parameter.MaxSimulationDeltaSeconds*float64(time.Second)))
	if !s.drift.Endless {
		elapsed = min(elapsed, s.drift.Remaining)
	}

	// Phases advance whatever glyphs this instance holds, keeping the record shared
	s.world.WithDomain(core.DomainPlayer, func() {
		for level := range driftLevels {
			phase := &s.drift.Phase[level]
			phase[0] += s.driftVelocity[level][0] * int64(elapsed)
			phase[1] += s.driftVelocity[level][1] * int64(elapsed)
			stepX, stepY := phase[0]/driftCell, phase[1]/driftCell
			phase[0] -= stepX * driftCell
			phase[1] -= stepY * driftCell
			if stepX != 0 || stepY != 0 {
				s.shiftLayer(component.GlyphLevel(level), int(stepX), int(stepY))
			}
		}
	})

	if !s.drift.Endless {
		s.drift.Remaining -= min(delta, s.drift.Remaining)
		if s.drift.Remaining <= 0 {
			s.endDrift()
		}
	}
}

// shiftLayer moves every loose glyph of one level, a cell and an axis at a time so
// no step passes through a wall or across a corner. The layer is collected in
// dense order, which a replay reproduces.
func (s *EnvironmentSystem) shiftLayer(level component.GlyphLevel, stepX, stepY int) {
	s.driftLayer = s.driftLayer[:0]
	s.world.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
		if g.Level == level && e.Domain() == core.DomainPlayer && s.world.Positions.HasPosition(e) &&
			!s.world.Components.Member.HasEntity(e) && !s.world.Components.Death.HasEntity(e) {
			s.driftLayer = append(s.driftLayer, e)
		}
		return true
	})
	dx, dy := 1, 1
	if stepX < 0 {
		dx = -1
	}
	if stepY < 0 {
		dy = -1
	}
	for range vmath.IntAbs(stepX) {
		s.stepLayer(dx, 0)
	}
	for range vmath.IntAbs(stepY) {
		s.stepLayer(0, dy)
	}
}

// stepLayer moves the collected layer one cell, removing outright the glyphs that
// would leave the map, enter a wall, or find their cell full: drift is scenery
// passing by, so leaving play costs and rewards nothing.
func (s *EnvironmentSystem) stepLayer(dx, dy int) {
	positions := s.world.Positions
	movers := s.driftLayer[:0]
	s.driftDoomed = s.driftDoomed[:0]
	for _, e := range s.driftLayer {
		pos, _ := positions.GetPosition(e)
		if positions.IsBlocked(pos.X+dx, pos.Y+dy, component.WallBlockSpawn) {
			s.driftDoomed = append(s.driftDoomed, e)
		} else {
			movers = append(movers, e)
		}
	}
	s.world.DestroyEntitiesBatch(s.driftDoomed)

	s.driftDoomed = positions.Shift(movers, dx, dy, s.driftDoomed[:0])
	if len(s.driftDoomed) > 0 {
		s.world.DestroyEntitiesBatch(s.driftDoomed)
		movers = slices.DeleteFunc(movers, func(e core.Entity) bool { return !positions.HasPosition(e) })
	}
	s.driftLayer = movers
}

func (s *EnvironmentSystem) publishStatus() {
	s.statWindActive.Store(s.enabled && s.windActive)
	s.statDriftActive.Store(s.enabled && s.drift.Active)
	s.statDriftDirection.Set(s.drift.Direction)
}

func finite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// environmentSnapshot is the D-19 continuation point. Shared capture already
// includes every shared Kinetic component and the environment RNG stream; this
// record carries the private base wind, remaining time, whether one tick's
// acceleration still needs to be replaced or cleared, and the drift.
type environmentSnapshot struct {
	Active    bool          `json:"active"`
	Applied   bool          `json:"applied"`
	Enabled   bool          `json:"enabled"`
	Force     float64       `json:"force"`
	Direction float64       `json:"direction"`
	Remaining time.Duration `json:"remaining"`
	Drift     driftState    `json:"drift"`
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
		Drift:     s.drift,
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
	if err := snap.Drift.validate(); err != nil {
		return err
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
	// A record moves no glyph: this instance's glyphs keep their own history
	s.endDrift()
	if snap.Drift.Active {
		s.drift = snap.Drift
		s.deriveDriftVelocity()
	}
	s.publishStatus()
	return nil
}

// validate accepts exactly the drift states Update can leave behind
func (d driftState) validate() error {
	if !d.Active {
		if d != (driftState{}) {
			return fmt.Errorf("environment: inactive drift carries state")
		}
		return nil
	}
	if !finite(d.Speed) || d.Speed <= 0 || d.Speed > parameter.DriftMaxSpeed || !finite(d.Direction) ||
		d.Remaining < 0 || d.Endless != (d.Remaining == 0) {
		return fmt.Errorf("environment: invalid drift state")
	}
	for _, phase := range d.Phase {
		if phase[0] <= -driftCell || phase[0] >= driftCell || phase[1] <= -driftCell || phase[1] >= driftCell {
			return fmt.Errorf("environment: drift phase beyond one cell")
		}
	}
	return nil
}
