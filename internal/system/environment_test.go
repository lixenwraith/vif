package system

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/profile"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
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
	origin.startDrift(&event.DriftStartPayload{Direction: 2.6, Speed: 6})
	setEnvironmentDelta(originWorld, 1, 50*time.Millisecond)
	origin.Update()

	record, err := origin.SaveShared()
	if err != nil {
		t.Fatal(err)
	}
	streams := originWorld.Resources.Rand.SaveStreams(core.DomainShared)

	if origin.drift.Phase[component.GlyphBright] == ([2]int64{}) {
		t.Fatal("drift phase is whole at capture; the record must carry a part-cell remainder")
	}

	receiverWorld, receiver := newEnvironmentWorld(0xC0FFEE)
	receiverSwarm := addWindKinetic(receiverWorld, core.DomainShared)
	receiverWorld.Components.Swarm.SetComponent(receiverSwarm, component.SwarmComponent{})
	receiverGlyph := addDriftGlyph(receiverWorld, 40, 10, component.GlyphBright)
	if err := receiver.LoadShared(record); err != nil {
		t.Fatal(err)
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
// off the map, into a wall, around a wall's corner, or into a full cell.
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

	s.shiftLayer(component.GlyphBright, -1, 0)
	for _, e := range []core.Entity{edge, walled, crowded} {
		if w.HasEntity(e) {
			pos, _ := w.Positions.GetPosition(e)
			t.Fatalf("glyph at %v survived a step it could not take", pos)
		}
	}
	if pos, _ := w.Positions.GetPosition(behind); pos != (component.PositionComponent{X: 11, Y: 6}) {
		t.Fatalf("glyph behind the removed one at %v, want 11,6", pos)
	}

	s.shiftLayer(component.GlyphNormal, -1, 1)
	if w.HasEntity(cornered) {
		t.Fatal("a diagonal step cut between two walls")
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
	if s.drift.Speed != 2 || s.drift.Direction != vmath.NormalizeAngleF(math.Pi) {
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

// TestGlyphLinesEnterAtTheUpwindEdge: while glyphs drift, new lines are placed in
// the band beside the edge the drift comes from.
func TestGlyphLinesEnterAtTheUpwindEdge(t *testing.T) {
	type band func(cfg *engine.ConfigResource, p component.PositionComponent) bool
	right := func(cfg *engine.ConfigResource, p component.PositionComponent) bool {
		return p.X >= cfg.MapWidth-parameter.DriftEntryBand-2
	}
	top := func(_ *engine.ConfigResource, p component.PositionComponent) bool {
		return p.Y < parameter.DriftEntryBand
	}
	for _, tc := range []struct {
		name      string
		direction float64
		bands     []band
	}{
		{"left", math.Pi, []band{right}},
		{"down", math.Pi / 2, []band{top}},
		{"down-left", 3 * math.Pi / 4, []band{right, top}},
	} {
		w, _, _ := testCursorWorld(t)
		glyphs := NewGlyphSystem(w).(*GlyphSystem)
		w.Resources.Status.Bools.Get("environment.drift_active").Store(true)
		w.Resources.Status.Floats.Get("environment.drift_direction").Set(tc.direction)
		for range 20 {
			glyphs.placeLine("abc", component.GlyphBlue, component.GlyphNormal)
		}

		// Every glyph lies in a band, and each band holds one that lies in no other
		only := make([]int, len(tc.bands))
		for _, e := range w.Components.Glyph.Entities() {
			pos, _ := w.Positions.GetPosition(e)
			var in []int
			for i, b := range tc.bands {
				if b(w.Resources.Config, pos) {
					in = append(in, i)
				}
			}
			if len(in) == 0 {
				t.Fatalf("%s: glyph placed at %v, outside every entry band", tc.name, pos)
			}
			if len(in) == 1 {
				only[in[0]]++
			}
		}
		for i, n := range only {
			if n == 0 {
				t.Fatalf("%s: no line entered through band %d", tc.name, i)
			}
		}
	}
}
