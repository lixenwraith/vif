package system

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/profile"
	"github.com/lixenwraith/vi-fighter/pkg/vmath/physics"
)

func newEnvironmentWorld(seed uint64) (*engine.World, *EnvironmentSystem) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	w.Resources.Rand = engine.NewRandResource(seed)
	return w, NewEnvironmentSystem(w).(*EnvironmentSystem)
}

func setEnvironmentDelta(w *engine.World, tick uint64, delta time.Duration) {
	now := engine.SimTime(tick, parameter.GameUpdateInterval)
	w.Resources.Time.Update(now, now, delta)
}

func addWindKinetic(w *engine.World, domain core.Domain) core.Entity {
	entity := w.CreateEntity(domain)
	w.Components.Kinetic.SetComponent(entity, component.KineticComponent{Kinetic: physics.Kinetic{
		PreciseX: 10.5,
		PreciseY: 10.5,
	}})
	return entity
}

func windAcceleration(w *engine.World, entity core.Entity) (float64, float64) {
	k, _ := w.Components.Kinetic.GetComponent(entity)
	return k.AccelX, k.AccelY
}

func TestWindAppliesBySpeciesMassAndExcludesOtherKinetics(t *testing.T) {
	w, s := newEnvironmentWorld(0x51A7E)

	drain := addWindKinetic(w, core.DomainPlayer)
	w.Components.Drain.SetComponent(drain, component.DrainComponent{})
	swarm := addWindKinetic(w, core.DomainShared)
	w.Components.Swarm.SetComponent(swarm, component.SwarmComponent{})
	eye := addWindKinetic(w, core.DomainShared)
	w.Components.Eye.SetComponent(eye, component.EyeComponent{})
	snake := addWindKinetic(w, core.DomainShared)
	w.Components.SnakeHead.SetComponent(snake, component.SnakeHeadComponent{})
	quasar := addWindKinetic(w, core.DomainShared)
	w.Components.Quasar.SetComponent(quasar, component.QuasarComponent{})
	pylon := addWindKinetic(w, core.DomainShared)
	w.Components.Pylon.SetComponent(pylon, component.PylonComponent{})
	storm := addWindKinetic(w, core.DomainShared)
	w.Components.StormCircle.SetComponent(storm, component.StormCircleComponent{})

	type excluded struct {
		name   string
		entity core.Entity
	}
	var excludedEntities []excluded
	addExcluded := func(name string, domain core.Domain, mark func(core.Entity)) {
		entity := addWindKinetic(w, domain)
		kinetic, _ := w.Components.Kinetic.GetPtr(entity)
		kinetic.VelX, kinetic.VelY = 4, -3
		kinetic.AccelX, kinetic.AccelY = 2, -1
		mark(entity)
		excludedEntities = append(excludedEntities, excluded{name: name, entity: entity})
	}
	addExcluded("cursor", core.DomainShared, func(e core.Entity) {
		w.Components.Cursor.SetComponent(e, component.CursorComponent{})
	})
	addExcluded("cleaner", core.DomainPlayer, func(e core.Entity) {
		w.Components.Cleaner.SetComponent(e, component.CleanerComponent{})
	})
	addExcluded("missile", core.DomainPlayer, func(e core.Entity) {
		w.Components.Missile.SetComponent(e, component.MissileComponent{})
	})
	addExcluded("bullet", core.DomainShared, func(e core.Entity) {
		w.Components.Bullet.SetComponent(e, component.BulletComponent{})
	})
	addExcluded("loot", core.DomainPlayer, func(e core.Entity) {
		w.Components.Loot.SetComponent(e, component.LootComponent{})
	})
	addExcluded("weapon orb", core.DomainPlayer, func(e core.Entity) {
		w.Components.Orb.SetComponent(e, component.OrbComponent{})
	})
	addExcluded("dust", core.DomainPlayer, func(e core.Entity) {
		w.Components.Dust.SetComponent(e, component.DustComponent{})
	})
	addExcluded("decay", core.DomainPlayer, func(e core.Entity) {
		w.Components.Particle.SetComponent(e, component.ParticleComponent{Behavior: component.ParticleDecay})
	})
	addExcluded("blossom", core.DomainPlayer, func(e core.Entity) {
		w.Components.Particle.SetComponent(e, component.ParticleComponent{Behavior: component.ParticleBlossom})
	})

	s.HandleEvent(event.GameEvent{Type: event.EventWindStart, Payload: &event.WindStartPayload{
		Force: 80, Direction: 0, Duration: time.Second,
	}})
	setEnvironmentDelta(w, 1, 50*time.Millisecond)
	s.Update()

	drainX, drainY := windAcceleration(w, drain)
	if drainX <= 0 {
		t.Fatalf("drain acceleration = (%v,%v), want wind generally toward +X", drainX, drainY)
	}
	wantForce := math.Hypot(drainX, drainY) * profile.MassDrain
	for _, tc := range []struct {
		name   string
		entity core.Entity
		mass   profile.Mass
	}{
		{"swarm", swarm, profile.MassSwarm},
		{"eye", eye, profile.MassEye},
		{"snake head", snake, profile.MassSnakeHead},
		{"quasar", quasar, profile.MassQuasar},
		{"pylon", pylon, profile.MassPylon},
	} {
		x, y := windAcceleration(w, tc.entity)
		if got := math.Hypot(x, y) * tc.mass; math.Abs(got-wantForce) > 1e-10 {
			t.Errorf("%s recovered force = %v, want %v", tc.name, got, wantForce)
		}
	}

	stormKinetic, _ := w.Components.Kinetic.GetComponent(storm)
	stormForce := math.Hypot(stormKinetic.VelX, stormKinetic.VelY) /
		(50 * time.Millisecond).Seconds() * profile.MassStorm
	if math.Abs(stormForce-wantForce) > 1e-10 {
		t.Errorf("storm recovered force = %v, want %v", stormForce, wantForce)
	}

	for _, tc := range excludedEntities {
		kinetic, _ := w.Components.Kinetic.GetComponent(tc.entity)
		if kinetic.VelX != 4 || kinetic.VelY != -3 || kinetic.AccelX != 2 || kinetic.AccelY != -1 {
			t.Errorf("%s kinetic changed: %+v", tc.name, kinetic.Kinetic)
		}
	}
}

func TestWindOverrideExpiryAndCancel(t *testing.T) {
	w, s := newEnvironmentWorld(0xA11CE)
	drain := addWindKinetic(w, core.DomainPlayer)
	w.Components.Drain.SetComponent(drain, component.DrainComponent{})

	s.HandleEvent(event.GameEvent{Type: event.EventWindStart, Payload: &event.WindStartPayload{
		Force: 10, Direction: -math.Pi / 2, Duration: 150 * time.Millisecond,
	}})
	if !s.windActive || !s.statWindActive.Load() || math.Abs(s.windDirection-3*math.Pi/2) > 1e-12 {
		t.Fatalf("start did not normalize and activate wind: active=%t direction=%v metric=%t",
			s.windActive, s.windDirection, s.statWindActive.Load())
	}

	setEnvironmentDelta(w, 1, 50*time.Millisecond)
	s.Update()
	if s.windRemaining != 100*time.Millisecond {
		t.Fatalf("remaining = %v, want 100ms", s.windRemaining)
	}
	if x, y := windAcceleration(w, drain); x == 0 && y == 0 {
		t.Fatal("active wind set no acceleration")
	}

	s.HandleEvent(event.GameEvent{Type: event.EventWindStart, Payload: &event.WindStartPayload{
		Force: 25, Direction: math.Pi, Duration: 20 * time.Millisecond,
	}})
	if s.windForce != 25 || s.windRemaining != 20*time.Millisecond {
		t.Fatalf("replacement state = force %v remaining %v", s.windForce, s.windRemaining)
	}
	if x, y := windAcceleration(w, drain); x != 0 || y != 0 {
		t.Fatalf("replacement left old acceleration (%v,%v)", x, y)
	}

	setEnvironmentDelta(w, 2, 50*time.Millisecond)
	s.Update()
	if s.windActive || s.windRemaining != 0 || s.statWindActive.Load() {
		t.Fatalf("expired wind stayed active: active=%t remaining=%v metric=%t",
			s.windActive, s.windRemaining, s.statWindActive.Load())
	}
	if x, y := windAcceleration(w, drain); x == 0 && y == 0 {
		t.Fatal("final partial tick was cleared before species integration")
	}

	setEnvironmentDelta(w, 3, 50*time.Millisecond)
	s.Update()
	if x, y := windAcceleration(w, drain); x != 0 || y != 0 {
		t.Fatalf("expired wind acceleration = (%v,%v), want zero", x, y)
	}

	s.HandleEvent(event.GameEvent{Type: event.EventWindStart, Payload: &event.WindStartPayload{
		Force: 12, Direction: 0, Duration: time.Second,
	}})
	s.Update()
	s.HandleEvent(event.GameEvent{Type: event.EventWindCancel})
	if s.windActive || s.windApplied || s.windForce != 0 || s.windRemaining != 0 || s.statWindActive.Load() {
		t.Fatalf("cancel left state: %+v", *s)
	}
	if x, y := windAcceleration(w, drain); x != 0 || y != 0 {
		t.Fatalf("cancelled wind acceleration = (%v,%v), want zero", x, y)
	}
}

func TestInvalidWindDoesNotReplaceActiveWind(t *testing.T) {
	_, s := newEnvironmentWorld(7)
	s.startWind(&event.WindStartPayload{Force: 8, Direction: 1, Duration: time.Second})
	s.startWind(&event.WindStartPayload{Force: math.NaN(), Direction: 2, Duration: 2 * time.Second})
	if !s.windActive || s.windForce != 8 || s.windDirection != 1 || s.windRemaining != time.Second {
		t.Fatalf("invalid event replaced active wind: active=%t force=%v direction=%v remaining=%v",
			s.windActive, s.windForce, s.windDirection, s.windRemaining)
	}
}

func TestWindSamplingIsIndependentOfPlayerEntityCount(t *testing.T) {
	build := func(extraDrains int) (*engine.World, *EnvironmentSystem, core.Entity) {
		w, s := newEnvironmentWorld(0xD371)
		swarm := addWindKinetic(w, core.DomainShared)
		w.Components.Swarm.SetComponent(swarm, component.SwarmComponent{})
		for range extraDrains {
			drain := addWindKinetic(w, core.DomainPlayer)
			w.Components.Drain.SetComponent(drain, component.DrainComponent{})
		}
		s.startWind(&event.WindStartPayload{Force: 30, Direction: 0.75, Duration: time.Second})
		return w, s, swarm
	}

	wA, a, swarmA := build(0)
	wB, b, swarmB := build(7)
	for tick := uint64(1); tick <= 6; tick++ {
		setEnvironmentDelta(wA, tick, 50*time.Millisecond)
		setEnvironmentDelta(wB, tick, 50*time.Millisecond)
		a.Update()
		b.Update()
		ax, ay := windAcceleration(wA, swarmA)
		bx, by := windAcceleration(wB, swarmB)
		if ax != bx || ay != by {
			t.Fatalf("tick %d acceleration differs: (%v,%v) != (%v,%v)", tick, ax, ay, bx, by)
		}
	}
	if a.rng.State() != b.rng.State() {
		t.Fatalf("entity count shifted shared wind RNG: %x != %x", a.rng.State(), b.rng.State())
	}
	aState, _ := a.SaveShared()
	bState, _ := b.SaveShared()
	if !reflect.DeepEqual(aState, bState) {
		t.Fatalf("environment states differ:\nA %s\nB %s", aState, bState)
	}
}

func TestWindSnapshotAndRNGContinueTogether(t *testing.T) {
	originWorld, origin := newEnvironmentWorld(0xC0FFEE)
	originSwarm := addWindKinetic(originWorld, core.DomainShared)
	originWorld.Components.Swarm.SetComponent(originSwarm, component.SwarmComponent{})
	origin.startWind(&event.WindStartPayload{Force: 45, Direction: 2.5, Duration: 400 * time.Millisecond})
	setEnvironmentDelta(originWorld, 1, 50*time.Millisecond)
	origin.Update()

	record, err := origin.SaveShared()
	if err != nil {
		t.Fatal(err)
	}
	streams := originWorld.Resources.Rand.SaveStreams()

	receiverWorld, receiver := newEnvironmentWorld(0xC0FFEE)
	receiverSwarm := addWindKinetic(receiverWorld, core.DomainShared)
	receiverWorld.Components.Swarm.SetComponent(receiverSwarm, component.SwarmComponent{})
	if err := receiver.LoadShared(record); err != nil {
		t.Fatal(err)
	}
	if unknown := receiverWorld.Resources.Rand.LoadStreams(streams); len(unknown) != 0 {
		t.Fatalf("unknown RNG streams: %v", unknown)
	}

	for tick := uint64(2); tick <= 5; tick++ {
		setEnvironmentDelta(originWorld, tick, 50*time.Millisecond)
		setEnvironmentDelta(receiverWorld, tick, 50*time.Millisecond)
		origin.Update()
		receiver.Update()
		ox, oy := windAcceleration(originWorld, originSwarm)
		rx, ry := windAcceleration(receiverWorld, receiverSwarm)
		if ox != rx || oy != ry {
			t.Fatalf("tick %d continuation differs: origin=(%v,%v) receiver=(%v,%v)", tick, ox, oy, rx, ry)
		}
	}

	originState, _ := origin.SaveShared()
	receiverState, _ := receiver.SaveShared()
	if !reflect.DeepEqual(originState, receiverState) || origin.rng.State() != receiver.rng.State() {
		t.Fatalf("continued snapshot/RNG state diverged:\norigin=%s/%x\nreceiver=%s/%x",
			originState, origin.rng.State(), receiverState, receiver.rng.State())
	}
}
