package system

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/position"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

func newEnvironmentWorld(seed uint64) (*engine.World, *EnvironmentSystem) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 80, 40, engine.NewManualClock())
	w.Resources.Rand = engine.NewRandResource(seed)
	return w, ready(NewEnvironmentSystem(w)).(*EnvironmentSystem)
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
	addExcluded("tracer", core.DomainPlayer, func(e core.Entity) {
		w.Components.Tracer.SetComponent(e, component.TracerComponent{})
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
		w.Components.Particle.SetComponent(e, component.ParticleComponent{Behavior: component.ParticleDust})
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

func TestEnvironmentIsIndependentOfPlayerEntityCount(t *testing.T) {
	build := func(extra int) (*engine.World, *EnvironmentSystem, core.Entity) {
		w, s := newEnvironmentWorld(0xD371)
		swarm := addWindKinetic(w, core.DomainShared)
		w.Components.Swarm.SetComponent(swarm, component.SwarmComponent{})
		for i := range extra {
			drain := addWindKinetic(w, core.DomainPlayer)
			w.Components.Drain.SetComponent(drain, component.DrainComponent{})
			addDriftGlyph(w, 1+i, 3, component.GlyphLevel(i%driftLevels))
		}
		s.startWind(&event.WindStartPayload{Force: 30, Direction: 0.75, Duration: time.Second})
		s.startDrift(&event.DriftStartPayload{Direction: 3.5, Speed: 11})
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

func TestEnvironmentSnapshotAndRNGContinueTogether(t *testing.T) {
	originWorld, origin := newEnvironmentWorld(0xC0FFEE)
	originSwarm := addWindKinetic(originWorld, core.DomainShared)
	originWorld.Components.Swarm.SetComponent(originSwarm, component.SwarmComponent{})
	originGlyph := addDriftGlyph(originWorld, 40, 10, component.GlyphBright)
	origin.startWind(&event.WindStartPayload{Force: 45, Direction: 2.5, Duration: 400 * time.Millisecond})
	origin.startDrift(&event.DriftStartPayload{Direction: 2.6, Speed: 6, Scenery: true})
	setEnvironmentDelta(originWorld, 1, 50*time.Millisecond)
	origin.Update()
	origin.startDrift(&event.DriftStartPayload{Direction: 2.2, Speed: 9, EaseMs: 300, Scenery: true})
	setEnvironmentDelta(originWorld, 1, 50*time.Millisecond)
	origin.Update()

	record, err := origin.SaveShared()
	if err != nil {
		t.Fatal(err)
	}
	streams := originWorld.Resources.Rand.SaveStreams(core.DomainShared)

	if origin.drift.Phase[component.GlyphBright] == ([2]int64{}) || origin.drift.Done == 0 {
		t.Fatal("drift phase is whole or its ease done at capture; the record must carry both part way")
	}

	receiverWorld, receiver := newEnvironmentWorld(0xC0FFEE)
	receiverSwarm := addWindKinetic(receiverWorld, core.DomainShared)
	receiverWorld.Components.Swarm.SetComponent(receiverSwarm, component.SwarmComponent{})
	receiverGlyph := addDriftGlyph(receiverWorld, 40, 10, component.GlyphBright)
	if err := receiver.LoadShared(record); err != nil {
		t.Fatal(err)
	}
	if !receiver.statDriftScenery.Load() {
		t.Fatal("the record lost the drift's scenery")
	}
	if unknown := receiverWorld.Resources.Rand.LoadStreams(core.DomainShared, streams); len(unknown) != 0 {
		t.Fatalf("unknown RNG streams: %v", unknown)
	}

	for tick := uint64(2); tick <= 12; tick++ {
		setEnvironmentDelta(originWorld, tick, 50*time.Millisecond)
		setEnvironmentDelta(receiverWorld, tick, 50*time.Millisecond)
		origin.Update()
		receiver.Update()
		ox, oy := windAcceleration(originWorld, originSwarm)
		rx, ry := windAcceleration(receiverWorld, receiverSwarm)
		if ox != rx || oy != ry {
			t.Fatalf("tick %d continuation differs: origin=(%v,%v) receiver=(%v,%v)", tick, ox, oy, rx, ry)
		}
		op, _ := originWorld.Positions.GetPosition(originGlyph)
		rp, _ := receiverWorld.Positions.GetPosition(receiverGlyph)
		if op != rp {
			t.Fatalf("tick %d drift stepped differently: origin=%v receiver=%v", tick, op, rp)
		}
	}
	if p, _ := originWorld.Positions.GetPosition(originGlyph); p == (component.PositionComponent{X: 40, Y: 10}) {
		t.Fatal("drift never stepped during the continuation")
	}

	originState, _ := origin.SaveShared()
	receiverState, _ := receiver.SaveShared()
	if !reflect.DeepEqual(originState, receiverState) || origin.rng.State() != receiver.rng.State() {
		t.Fatalf("continued snapshot/RNG state diverged:\norigin=%s/%x\nreceiver=%s/%x",
			originState, origin.rng.State(), receiverState, receiver.rng.State())
	}
}

func addDriftGlyph(w *engine.World, x, y int, level component.GlyphLevel) core.Entity {
	e := w.CreateEntity(core.DomainPlayer)
	w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
	w.Components.Glyph.SetComponent(e, component.GlyphComponent{Rune: 'g', Type: component.GlyphBlue, Level: level})
	return e
}

func runEnvironment(w *engine.World, s *EnvironmentSystem, from, ticks uint64) uint64 {
	for tick := from; tick < from+ticks; tick++ {
		setEnvironmentDelta(w, tick, 50*time.Millisecond)
		s.Update()
	}
	return from + ticks
}

// TestDriftStepsEachLevelAtItsParallaxFraction: in one second at four cells a
// second, bright text moves four cells, normal two and dark one; rows move at half
// that for the cell aspect. Shared (gold) glyphs stay put.
func TestDriftStepsEachLevelAtItsParallaxFraction(t *testing.T) {
	for _, tc := range []struct {
		name          string
		direction     float64
		dx, dy        int
		bright, other int // cells moved by bright and by normal
	}{
		{"left", math.Pi, -1, 0, 4, 2},
		{"down", math.Pi / 2, 0, 1, 2, 1},
	} {
		w, s := newEnvironmentWorld(1)
		levels := []core.Entity{
			addDriftGlyph(w, 40, 10, component.GlyphDark),
			addDriftGlyph(w, 40, 11, component.GlyphNormal),
			addDriftGlyph(w, 40, 12, component.GlyphBright),
		}
		gold := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(gold, component.PositionComponent{X: 40, Y: 13})
		w.Components.Glyph.SetComponent(gold, component.GlyphComponent{Rune: 'g', Type: component.GlyphGold, Level: component.GlyphBright})
		w.Components.Member.SetComponent(gold, component.MemberComponent{})

		s.startDrift(&event.DriftStartPayload{Direction: tc.direction, Speed: 4})
		runEnvironment(w, s, 1, 20)

		for i, moved := range []int{tc.other / 2, tc.other, tc.bright} {
			pos, _ := w.Positions.GetPosition(levels[i])
			if want := (component.PositionComponent{X: 40 + tc.dx*moved, Y: 10 + i + tc.dy*moved}); pos != want {
				t.Fatalf("%s: level %d at %v, want %v", tc.name, i, pos, want)
			}
		}
		if pos, _ := w.Positions.GetPosition(gold); pos != (component.PositionComponent{X: 40, Y: 13}) {
			t.Fatalf("%s: shared glyph drifted to %v", tc.name, pos)
		}
	}
}

// TestDriftRemovesGlyphsThatCannotStep: a glyph leaves play rather than stepping
// off the map, into or through a wall, around a wall's corner, or into a full cell.
func TestDriftRemovesGlyphsThatCannotStep(t *testing.T) {
	w, s := newEnvironmentWorld(1)
	wall := func(x, y int) {
		e := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(e, component.PositionComponent{X: x, Y: y})
		w.Components.Wall.SetComponent(e, component.WallComponent{BlockMask: component.WallBlockAll})
	}
	edge := addDriftGlyph(w, 0, 5, component.GlyphBright)
	wall(10, 6)
	walled := addDriftGlyph(w, 11, 6, component.GlyphBright)
	behind := addDriftGlyph(w, 12, 6, component.GlyphBright)
	for range parameter.ReservedPlayerPerCell {
		w.Positions.SetPosition(w.CreateEntity(core.DomainPlayer), component.PositionComponent{X: 20, Y: 8})
	}
	crowded := addDriftGlyph(w, 21, 8, component.GlyphBright)
	wall(29, 10)
	wall(30, 11)
	cornered := addDriftGlyph(w, 30, 10, component.GlyphNormal)
	wall(40, 14)
	through := addDriftGlyph(w, 42, 14, component.GlyphDark)

	s.shiftLayers([driftLevels][2]int{component.GlyphBright: {-1, 0}})
	for _, e := range []core.Entity{edge, walled, crowded} {
		if w.HasEntity(e) {
			pos, _ := w.Positions.GetPosition(e)
			t.Fatalf("glyph at %v survived a step it could not take", pos)
		}
	}
	if pos, _ := w.Positions.GetPosition(behind); pos != (component.PositionComponent{X: 11, Y: 6}) {
		t.Fatalf("glyph behind the removed one at %v, want 11,6", pos)
	}

	s.shiftLayers([driftLevels][2]int{component.GlyphNormal: {-1, 1}, component.GlyphDark: {-3, 0}})
	if w.HasEntity(cornered) {
		t.Fatal("a diagonal step cut between two walls")
	}
	if w.HasEntity(through) {
		t.Fatal("a step of three cells passed through the wall on its way")
	}
}

// TestSceneryDriftsIntoWallsAndLeavesOnlyOffTheMap: under a scenery drift, scenery
// steps into a wall and out beyond it where a plain glyph would leave play, and
// still leaves at the map's edge
func TestSceneryDriftsIntoWallsAndLeavesOnlyOffTheMap(t *testing.T) {
	w, s := newEnvironmentWorld(1)
	s.startDrift(&event.DriftStartPayload{Direction: math.Pi, Speed: 4, Scenery: true})
	if !s.statDriftScenery.Load() {
		t.Fatal("a scenery drift did not publish its kind")
	}
	scenery := func(x, y int) core.Entity {
		e := addDriftGlyph(w, x, y, component.GlyphBright)
		w.Components.Protection.SetComponent(e, component.ProtectionComponent{Mask: component.ProtectScenery})
		return e
	}
	for y := 6; y <= 7; y++ {
		wall := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(wall, component.PositionComponent{X: 10, Y: y})
		w.Components.Wall.SetComponent(wall, component.WallComponent{BlockMask: component.WallBlockAll})
	}
	plain := addDriftGlyph(w, 11, 6, component.GlyphBright)
	behind := scenery(11, 7)
	edge := scenery(1, 5)

	s.shiftLayers([driftLevels][2]int{component.GlyphBright: {-2, 0}})
	if w.HasEntity(plain) || w.HasEntity(edge) {
		t.Fatal("a plain glyph entered the wall or scenery left the map")
	}
	if pos, _ := w.Positions.GetPosition(behind); !w.HasEntity(behind) || pos != (component.PositionComponent{X: 9, Y: 7}) {
		t.Fatalf("scenery at %v, want it through the wall at 9,7", pos)
	}
}

func TestDriftExpiryReplacementAndCancel(t *testing.T) {
	w, s := newEnvironmentWorld(1)
	toggle := func(enabled bool) {
		s.HandleEvent(event.GameEvent{Type: event.EventMetaSystemCommandRequest,
			Payload: &event.MetaSystemCommandPayload{SystemName: s.Name(), Enabled: enabled}})
	}

	s.HandleEvent(event.GameEvent{Type: event.EventDriftStart,
		Payload: &event.DriftStartPayload{Direction: math.Pi, Speed: 4, DurationMs: 1000}})
	tick := runEnvironment(w, s, 1, 19)
	if !s.drift.Active || !s.statDriftActive.Load() {
		t.Fatal("drift ended before its duration")
	}
	tick = runEnvironment(w, s, tick, 1)
	if s.drift != (driftState{}) || s.statDriftActive.Load() {
		t.Fatalf("expiry left state: %+v", s.drift)
	}

	s.startDrift(&event.DriftStartPayload{Direction: math.Pi, Speed: 4})
	tick = runEnvironment(w, s, tick, 3)
	phase := s.drift.Phase
	s.startDrift(&event.DriftStartPayload{Direction: math.Pi, Speed: 2})
	if !s.drift.Endless || s.drift.Speed != 2 || s.drift.Phase != phase {
		t.Fatalf("replacement = %+v, want speed 2 until cancelled with phase %v kept", s.drift, phase)
	}
	s.startDrift(&event.DriftStartPayload{Direction: 0, Speed: 2})
	if s.drift.Phase != ([driftLevels][2]int64{}) {
		t.Fatalf("reversal kept phase %v, want the old part-cell dropped", s.drift.Phase)
	}
	s.startDrift(&event.DriftStartPayload{Direction: math.Pi, Speed: 2})
	s.startDrift(&event.DriftStartPayload{Direction: math.NaN(), Speed: 3})
	s.startDrift(&event.DriftStartPayload{Direction: 1, Speed: 0})
	s.startDrift(&event.DriftStartPayload{Direction: 1, Speed: 3, DurationMs: -1})
	if s.drift.Speed != 2 || s.drift.Direction != vmath.NormalizeAngle(math.Pi) {
		t.Fatalf("invalid start replaced the drift: %+v", s.drift)
	}

	toggle(false)
	held := s.drift
	runEnvironment(w, s, tick, 40)
	if s.drift != held || s.statDriftActive.Load() {
		t.Fatalf("a disabled environment advanced or published its drift: %+v", s.drift)
	}
	toggle(true)
	s.HandleEvent(event.GameEvent{Type: event.EventDriftCancel})
	if s.drift != (driftState{}) || s.statDriftActive.Load() {
		t.Fatalf("cancel left state: %+v", s.drift)
	}
}

// An eased start ramps each layer from how it drifts now, covering the area under the
// ramp, and an eased cancel brings the layers to rest before the drift ends: bright
// text easing from rest to four cells a second over a second moves two cells, six
// more easing on to eight, and two more easing to rest over half a second
func TestADriftEasesFromItsPaceAndAnEasedCancelEndsAtRest(t *testing.T) {
	w, s := newEnvironmentWorld(1)
	bright := addDriftGlyph(w, 40, 10, component.GlyphBright)
	at := func(want int) {
		t.Helper()
		if pos, _ := w.Positions.GetPosition(bright); pos.X != want {
			t.Fatalf("bright text at column %d, want %d; drift %+v", pos.X, want, s.drift)
		}
	}
	ease := func(speed float64) {
		s.HandleEvent(event.GameEvent{Type: event.EventDriftStart, Payload: &event.DriftStartPayload{Direction: math.Pi, Speed: speed, EaseMs: 1000}})
	}
	ease(4)
	tick := runEnvironment(w, s, 1, 20)
	at(38)
	ease(8)
	tick = runEnvironment(w, s, tick, 20)
	at(32)
	s.HandleEvent(event.GameEvent{Type: event.EventDriftCancel, Payload: &event.DriftCancelPayload{EaseMs: 500}})
	tick = runEnvironment(w, s, tick, 9)
	if !s.statDriftActive.Load() {
		t.Fatal("the drift ended before its ease to rest")
	}
	runEnvironment(w, s, tick, 20)
	at(30)
	if s.drift != (driftState{}) || s.statDriftActive.Load() {
		t.Fatalf("an eased cancel left the drift %+v", s.drift)
	}
}

// TestALayersCarryOverATickIsTheStepItTakesNext: the drift a tick leaves for the
// renderer carries each layer, over the next tick, exactly as far as that tick steps
// it, part-cells and a diagonal included; a disabled drift carries nothing
func TestALayersCarryOverATickIsTheStepItTakesNext(t *testing.T) {
	w, s := newEnvironmentWorld(1)
	levels := []core.Entity{
		addDriftGlyph(w, 60, 6, component.GlyphDark),
		addDriftGlyph(w, 60, 12, component.GlyphNormal),
		addDriftGlyph(w, 60, 18, component.GlyphBright),
	}
	s.startDrift(&event.DriftStartPayload{Direction: 5 * math.Pi / 6, Speed: 30})
	tick := uint64(1)
	for range 12 {
		var carry [3][2]int
		var before [3]component.PositionComponent
		for level, e := range levels {
			before[level], _ = w.Positions.GetPosition(e)
			carry[level][0], carry[level][1] = w.Resources.Drift.Carry(level, 50*time.Millisecond)
		}
		tick = runEnvironment(w, s, tick, 1)
		for level, e := range levels {
			if pos, _ := w.Positions.GetPosition(e); pos.X-before[level].X != carry[level][0] || pos.Y-before[level].Y != carry[level][1] {
				t.Fatalf("tick %d: level %d stepped from %v to %v, carry %v", tick, level, before[level], pos, carry[level])
			}
		}
	}
	s.HandleEvent(event.GameEvent{Type: event.EventMetaSystemCommandRequest,
		Payload: &event.MetaSystemCommandPayload{SystemName: s.Name(), Enabled: false}})
	for level := range levels {
		if dx, dy := w.Resources.Drift.Carry(level, 50*time.Millisecond); dx != 0 || dy != 0 {
			t.Fatalf("a disabled drift carries level %d by (%d,%d)", level, dx, dy)
		}
	}
}

// TestTheEnvironmentPublishesEachLayersStep: each tick leaves the cells every layer
// stepped for the glyph spawner, none for a layer that held still or a drift switched
// off
func TestTheEnvironmentPublishesEachLayersStep(t *testing.T) {
	w, s := newEnvironmentWorld(1)
	levels := []core.Entity{
		addDriftGlyph(w, 60, 6, component.GlyphDark),
		addDriftGlyph(w, 60, 12, component.GlyphNormal),
		addDriftGlyph(w, 60, 18, component.GlyphBright),
	}
	s.startDrift(&event.DriftStartPayload{Direction: 5 * math.Pi / 6, Speed: 30})
	tick := uint64(1)
	for range 12 {
		var before [3]component.PositionComponent
		for level, e := range levels {
			before[level], _ = w.Positions.GetPosition(e)
		}
		tick = runEnvironment(w, s, tick, 1)
		for level, e := range levels {
			pos, _ := w.Positions.GetPosition(e)
			if step := w.Resources.Drift.Step[level]; pos.X-before[level].X != step[0] || pos.Y-before[level].Y != step[1] {
				t.Fatalf("tick %d: level %d stepped from %v to %v, published %v", tick, level, before[level], pos, step)
			}
		}
	}
	s.HandleEvent(event.GameEvent{Type: event.EventMetaSystemCommandRequest,
		Payload: &event.MetaSystemCommandPayload{SystemName: s.Name(), Enabled: false}})
	runEnvironment(w, s, tick, 1)
	if step := w.Resources.Drift.Step; step != ([driftLevels][2]int{}) {
		t.Fatalf("a drift switched off published steps %v", step)
	}
}

// TestAFastDriftSpawnsSoonerInProportion: the spawn interval shrinks by how far the
// drift outpaces DriftFillSpeed, so its text keeps its density in passing, and a
// slower drift spawns at the plain interval
func TestAFastDriftSpawnsSoonerInProportion(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	glyphs := ready(NewGlyphSystem(w)).(*GlyphSystem)
	speed := w.Resources.Status.Floats.Get("environment.drift_speed")
	plain := glyphs.calculateNextSpawn()
	for _, tc := range []struct {
		speed float64
		want  time.Duration
	}{
		{parameter.DriftFillSpeed / 2, plain},
		{parameter.DriftFillSpeed * 8, plain / 8},
	} {
		speed.Set(tc.speed)
		if got := glyphs.calculateNextSpawn(); got != tc.want {
			t.Fatalf("a drift at %v spawns every %v, want %v", tc.speed, got, tc.want)
		}
	}
}

// entryWorld is a cursor world whose glyphs drift, with the spawner and the
// environment run in their order each tick
type entryWorld struct {
	w      *engine.World
	env    *EnvironmentSystem
	glyphs *GlyphSystem
	tick   uint64
}

func newEntryWorld(t *testing.T, direction, speed float64, scenery bool) *entryWorld {
	w, _, _ := testCursorWorld(t)
	ew := &entryWorld{w: w, env: ready(NewEnvironmentSystem(w)).(*EnvironmentSystem),
		glyphs: ready(NewGlyphSystem(w)).(*GlyphSystem), tick: 1}
	ew.env.startDrift(&event.DriftStartPayload{Direction: direction, Speed: speed, Scenery: scenery})
	return ew
}

// step runs a tick, starting text entering inside the spawner's turn as a spawn would
func (ew *entryWorld) step(t *testing.T, enter ...string) {
	t.Helper()
	setEnvironmentDelta(ew.w, ew.tick, 50*time.Millisecond)
	ew.glyphs.Update()
	for _, text := range enter {
		ew.start(t, text, component.GlyphBright)
	}
	ew.env.Update()
	ew.tick++
}

// start starts a line entering, retrying the rows a cursor or a line of its level holds
func (ew *entryWorld) start(t *testing.T, text string, level component.GlyphLevel) {
	t.Helper()
	for range 20 {
		if ew.glyphs.placeLine(text, component.GlyphBlue, level) {
			return
		}
	}
	t.Fatalf("no row took %q", text)
}

// shown is what a frame right after the tick draws of the lines entering and the text
// already on the map: each glyph in its cell, and each rune still entering a step on
// from where the spawner left it, as the glyph renderer draws them
func (ew *entryWorld) shown() map[component.PositionComponent]rune {
	cells := map[component.PositionComponent]rune{}
	ew.w.Components.Glyph.Each(func(e core.Entity, g *component.GlyphComponent) bool {
		pos, _ := ew.w.Positions.GetPosition(e)
		cells[pos] = g.Rune
		return true
	})
	for _, line := range ew.glyphs.entering {
		step := ew.w.Resources.Drift.Step[line.Level]
		x, y := line.X+step[0], line.Y+step[1]
		for _, r := range line.Text {
			if !ew.w.Positions.IsOutOfBounds(x, y) {
				cells[component.PositionComponent{X: x, Y: y}] = r
			}
			x += line.Dir
		}
	}
	return cells
}

// readRow reads cells on one row as a run of neighbouring columns
func readRow(t *testing.T, cells map[component.PositionComponent]rune) (text string, row, lo, hi int) {
	t.Helper()
	row, lo, hi = -1, math.MaxInt, math.MinInt
	for pos := range cells {
		if row >= 0 && pos.Y != row {
			t.Fatalf("one line stands on rows %d and %d", row, pos.Y)
		}
		row, lo, hi = pos.Y, min(lo, pos.X), max(hi, pos.X)
	}
	runes := make([]rune, 0, len(cells))
	for x := lo; x <= hi; x++ {
		r, ok := cells[component.PositionComponent{X: x, Y: row}]
		if !ok {
			t.Fatalf("a gap at column %d in %v", x, cells)
		}
		runes = append(runes, r)
	}
	return string(runes), row, lo, hi
}

// TestALineEntersAcrossTheUpwindEdgeAsItsLayerMakesRoom: under a drift a line comes
// on from past the upwind edge, a rune at a time across a side, the last first from
// the left, and as a whole row across an end. A frame after each tick shows it in
// order with no gap, its runes still to place drawn up to the edge; its first frame
// shows it at the edge, and in the end the whole line is on the map.
func TestALineEntersAcrossTheUpwindEdgeAsItsLayerMakesRoom(t *testing.T) {
	const text = "abcdefghij"
	for _, tc := range []struct {
		name      string
		direction float64
		side      int // +1 enters across the right edge, -1 the left, 0 an end
	}{
		{"left", math.Pi, 1},
		{"right", 0, -1},
		{"down", math.Pi / 2, 0},
		{"up", 3 * math.Pi / 2, 0},
	} {
		ew := newEntryWorld(t, tc.direction, 80, false)
		width, height := ew.w.Resources.Config.MapWidth, ew.w.Resources.Config.MapHeight
		ew.step(t)
		ew.step(t, text)
		first := true
		for range 6 {
			if cells := ew.shown(); len(cells) > 0 {
				shown, row, lo, hi := readRow(t, cells)
				inOrder := map[int]bool{1: strings.HasPrefix(text, shown), -1: strings.HasSuffix(text, shown), 0: shown == text}[tc.side]
				atEdge := map[int]bool{1: hi == width-1, -1: lo == 0, 0: row == 0 || row == height-1}[tc.side]
				if !inOrder || shown != text && !atEdge {
					t.Fatalf("%s: a frame shows %q on row %d, columns %d to %d", tc.name, shown, row, lo, hi)
				}
				if first && (!atEdge || tc.side != 0 && len(shown) != 1) {
					t.Fatalf("%s: the line first shows as %q on row %d, columns %d to %d", tc.name, shown, row, lo, hi)
				}
				first = false
			}
			ew.step(t)
		}
		if n := ew.w.Components.Glyph.CountEntities(); n != len(text) || len(ew.glyphs.entering) > 0 {
			t.Fatalf("%s: %d runes on the map and %d lines entering, want the whole line on", tc.name, n, len(ew.glyphs.entering))
		}
	}
}

// TestAnEnteringRuneTakesItsCellAsTheDriftCarriesAGlyph: a rune enters its cell past
// another glyph there, and past rock or a wall that blocks spawns as scenery, where
// typeable text leaves a hole; a space leaves its cell empty
func TestAnEnteringRuneTakesItsCellAsTheDriftCarriesAGlyph(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	glyphs := ready(NewGlyphSystem(w)).(*GlyphSystem)
	cfg := w.Resources.Config
	cols := make([]engine.RockColumn, cfg.MapWidth)
	for x := range cols {
		cols[x] = engine.RockColumn{Ceil: 0, Floor: cfg.MapHeight}
	}
	cols[31] = engine.RockColumn{Ceil: cfg.MapHeight, Floor: cfg.MapHeight}
	w.Positions.SetRock(component.WallBlockAll, engine.RockLook{}, 0, cols)
	for _, y := range []int{12, 16} {
		wall := w.CreateEntity(core.DomainShared)
		w.Positions.SetPosition(wall, component.PositionComponent{X: 33, Y: y})
		w.Components.Wall.SetComponent(wall, component.WallComponent{BlockMask: component.WallBlockAll})
		addDriftGlyph(w, 35, y, component.GlyphDark)
	}

	for _, tc := range []struct {
		scenery bool
		y       int
		want    string // columns 30 to 36: a rock column, a wall, a glyph of another depth
	}{
		{false, 12, "a.c.efg"},
		{true, 16, "abc.efg"},
	} {
		line := engine.EntryLine{X: 30, Y: tc.y, Dir: 1, Text: "abc efg", Type: component.GlyphBlue,
			Level: component.GlyphBright, Scenery: tc.scenery}
		if glyphs.feed(&line, [2]int{-1, 0}) {
			t.Fatalf("scenery %v: a line on the map is still entering", tc.scenery)
		}
		got := []rune(".......")
		for x := range got {
			for _, e := range w.Positions.EntitiesAt(30+x, tc.y, position.ScopeAll) {
				if g, ok := w.Components.Glyph.GetComponent(e); ok && g.Level == component.GlyphBright {
					got[x] = g.Rune
				}
			}
		}
		if string(got) != tc.want {
			t.Fatalf("scenery %v: entered as %q, want %q", tc.scenery, string(got), tc.want)
		}
	}
}

// TestNoTwoLinesOfALevelEnterOnOneRow: a line never starts on a row where a line of
// its level is still entering, which would lay one text over the other, while lines
// of two levels share rows as their layers pass
func TestNoTwoLinesOfALevelEnterOnOneRow(t *testing.T) {
	ew := newEntryWorld(t, math.Pi, 80, true)
	ew.step(t)
	for range 40 {
		for _, level := range []component.GlyphLevel{component.GlyphNormal, component.GlyphBright} {
			ew.glyphs.placeLine("abcdefghij", component.GlyphBlue, level)
		}
	}
	rows := map[component.GlyphLevel]map[int]bool{component.GlyphNormal: {}, component.GlyphBright: {}}
	for _, line := range ew.glyphs.entering {
		if rows[line.Level][line.Y] {
			t.Fatalf("two lines of level %d enter on row %d", line.Level, line.Y)
		}
		rows[line.Level][line.Y] = true
	}
	shared := 0
	for row := range rows[component.GlyphBright] {
		if rows[component.GlyphNormal][row] {
			shared++
		}
	}
	if shared == 0 {
		t.Fatalf("lines of two levels never shared a row: %v", rows)
	}
}

// TestALineStartsEnteringAtAFreeCell: a line starts only where its first cells on the
// map stand empty, as text is placed on no other entity
func TestALineStartsEnteringAtAFreeCell(t *testing.T) {
	ew := newEntryWorld(t, math.Pi, 80, true)
	ew.step(t)
	width, height := ew.w.Resources.Config.MapWidth, ew.w.Resources.Config.MapHeight
	free := height / 2
	for y := range height {
		if y != free {
			addDriftGlyph(ew.w, width-1, y, component.GlyphDark)
		}
	}
	for range 200 {
		ew.glyphs.placeLine("abcdefghij", component.GlyphBlue, component.GlyphBright)
	}
	if len(ew.glyphs.entering) != 1 || ew.glyphs.entering[0].Y != free {
		t.Fatalf("lines entering %+v, want one on row %d", ew.glyphs.entering, free)
	}
}

// TestEnteringTextIsDroppedOnceNothingCarriesItIn: what is still to enter is dropped
// once the drift stops, turns away from its edge or the spawner is switched off,
// rather than standing past the edge; no more of it comes on than a frame had shown
func TestEnteringTextIsDroppedOnceNothingCarriesItIn(t *testing.T) {
	for _, tc := range []struct {
		name string
		stop func(ew *entryWorld)
	}{
		{"stops", func(ew *entryWorld) { ew.env.HandleEvent(event.GameEvent{Type: event.EventDriftCancel}) }},
		{"turns", func(ew *entryWorld) { ew.env.startDrift(&event.DriftStartPayload{Direction: 0, Speed: 80}) }},
		{"spawner off", func(ew *entryWorld) {
			ew.glyphs.HandleEvent(event.GameEvent{Type: event.EventMetaSystemCommandRequest,
				Payload: &event.MetaSystemCommandPayload{SystemName: ew.glyphs.Name(), Enabled: false}})
		}},
	} {
		ew := newEntryWorld(t, math.Pi, 80, true)
		ew.step(t)
		ew.step(t, "abcdefghij")
		ew.step(t)
		shown := len(ew.shown())
		if len(ew.glyphs.entering) == 0 {
			t.Fatalf("%s: nothing was entering", tc.name)
		}
		tc.stop(ew)
		ew.step(t)
		if n := ew.w.Components.Glyph.CountEntities(); len(ew.glyphs.entering) > 0 || len(ew.w.Resources.Entry.Lines) > 0 || n > shown {
			t.Fatalf("%s: %d lines entering, %d published, %d runes on against %d shown before", tc.name,
				len(ew.glyphs.entering), len(ew.w.Resources.Entry.Lines), n, shown)
		}
	}
}

// TestEnteringTextCarriesOverACopy: a copy of the spawner whose environment holds no
// step yet goes on feeding its lines exactly as the source does
func TestEnteringTextCarriesOverACopy(t *testing.T) {
	source, copied := newEntryWorld(t, math.Pi, 80, true), newEntryWorld(t, math.Pi, 80, true)
	for _, ew := range []*entryWorld{source, copied} {
		ew.step(t)
		ew.step(t, "abcdefghijklmnopqrstuvwxyz", "0123456789")
	}
	data, err := copied.glyphs.SaveLocal()
	if err != nil {
		t.Fatal(err)
	}
	record, err := copied.env.SaveShared()
	if err != nil {
		t.Fatal(err)
	}
	if err := copied.env.LoadShared(record); err != nil {
		t.Fatal(err)
	}
	if err := copied.glyphs.LoadLocal(data); err != nil {
		t.Fatal(err)
	}
	if len(copied.glyphs.entering) == 0 {
		t.Fatal("no line was still entering at the copy")
	}
	for tick := range 12 {
		if got, want := copied.shown(), source.shown(); !reflect.DeepEqual(got, want) {
			t.Fatalf("tick %d: the copy shows %v, the source %v", tick, got, want)
		}
		source.step(t)
		copied.step(t)
	}
}

// TestTextPlacedUnderASceneryDriftIsSceneryCountedApart: text placed while the drift
// is scenery carries ProtectScenery and other text carries none, and the spawner's
// density and census count only the kind it places that a player sees, so neither
// kind crowds out the other and scenery behind rock or a wall fills nothing
func TestTextPlacedUnderASceneryDriftIsSceneryCountedApart(t *testing.T) {
	w, _, _ := testCursorWorld(t)
	glyphs := ready(NewGlyphSystem(w)).(*GlyphSystem)
	drift := w.Resources.Status.Bools.Get("environment.drift_scenery")
	place := func(scenery bool, glyphType component.GlyphType, level component.GlyphLevel, lines int) []core.Entity {
		drift.Store(scenery)
		before := len(w.Components.Glyph.Entities())
		for range lines {
			if !glyphs.placeLine("abc", glyphType, level) {
				t.Fatal("no room for a line")
			}
		}
		return w.Components.Glyph.Entities()[before:]
	}
	scenery := place(true, component.GlyphBlue, component.GlyphNormal, 4)
	typeable := place(false, component.GlyphGreen, component.GlyphBright, 1)

	for _, e := range scenery {
		if prot, _ := w.Components.Protection.GetComponent(e); prot.Mask != component.ProtectScenery {
			t.Fatalf("text placed under a scenery drift carries %v, want ProtectScenery", prot.Mask)
		}
	}
	for _, e := range typeable {
		if w.Components.Protection.HasEntity(e) {
			t.Fatal("text placed under no scenery drift carries a protection")
		}
	}
	glyphs.updateCensus()
	blue, green := censusSlot(component.GlyphBlue, component.GlyphNormal), censusSlot(component.GlyphGreen, component.GlyphBright)
	if n := glyphs.countPlacedKind(); n != len(typeable) || glyphs.census[blue] != 0 || glyphs.census[green] != len(typeable) {
		t.Fatalf("typeable count %d, census blue %d green %d, want %d and scenery left out", n, glyphs.census[blue], glyphs.census[green], len(typeable))
	}
	drift.Store(true)
	if n := glyphs.countPlacedKind(); n != len(scenery) {
		t.Fatalf("scenery count %d, want %d", n, len(scenery))
	}

	// Rock over one scenery glyph's column and a wall on another's cell hide them
	hidden := map[core.Entity]bool{scenery[0]: true, scenery[len(scenery)-1]: true}
	rock, _ := w.Positions.GetPosition(scenery[0])
	cols := make([]engine.RockColumn, w.Resources.Config.MapWidth)
	for x := range cols {
		cols[x] = engine.RockColumn{Ceil: 0, Floor: w.Resources.Config.MapHeight}
	}
	cols[rock.X] = engine.RockColumn{Ceil: rock.Y + 1, Floor: w.Resources.Config.MapHeight}
	w.Positions.SetRock(component.WallBlockAll, engine.RockLook{}, 0, cols)
	wall := w.CreateEntity(core.DomainShared)
	walled, _ := w.Positions.GetPosition(scenery[len(scenery)-1])
	w.Positions.SetPosition(wall, walled)
	w.Components.Wall.SetComponent(wall, component.WallComponent{BlockMask: component.WallBlockAll})
	for _, e := range scenery {
		if pos, _ := w.Positions.GetPosition(e); !hidden[e] && pos.X == rock.X && pos.Y <= rock.Y {
			hidden[e] = true
		}
	}
	glyphs.updateCensus()
	shown := 0
	for _, n := range glyphs.census {
		shown += n
	}
	if n, want := glyphs.countPlacedKind(), len(scenery)-len(hidden); n != want || shown != want {
		t.Fatalf("scenery count %d and census %d with %d behind rock and a wall, want %d", n, shown, len(hidden), want)
	}
}
