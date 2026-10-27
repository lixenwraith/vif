package system

import (
	"math"
	"testing"
	"time"

	"github.com/lixenwraith/color"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
)

func newParticleWorld(seed uint64) (*engine.World, *ParticleSystem) {
	w := engine.NewWorld()
	engine.NewGameContextWithClock(w, 20, 10, engine.NewManualClock())
	w.Resources.Rand = engine.NewRandResource(seed)
	return w, ready(NewParticleSystem(w)).(*ParticleSystem)
}

func TestParticleSpawnBranchesByBehavior(t *testing.T) {
	w, particles := newParticleWorld(0xC0111DE)

	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleDecay, X: 3, Y: 4, Char: 'd', SkipStartCell: true}, 0, 0)
	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleBlossom, X: 6, Y: 7, Char: 'b'}, 0, 0)
	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleNone, X: 1, Y: 1, Char: 'x'}, 0, 0)

	entities := w.Components.Particle.Entities()
	if len(entities) != 2 {
		t.Fatalf("particle count = %d, want 2 valid behaviors", len(entities))
	}

	for i, tc := range []struct {
		behavior                component.ParticleBehavior
		rune                    rune
		lastX, lastY            int
		wantNegativeY, wantPink bool
	}{
		{behavior: component.ParticleDecay, rune: 'd', lastX: 3, lastY: 4},
		{behavior: component.ParticleBlossom, rune: 'b', lastX: -1, lastY: -1, wantNegativeY: true, wantPink: true},
	} {
		entity := entities[i]
		if entity.Domain() != core.DomainPlayer {
			t.Errorf("%s domain = %s, want player", component.ParticleBehaviorNames[tc.behavior], entity.Domain())
		}
		particle, _ := w.Components.Particle.GetComponent(entity)
		if particle.Behavior != tc.behavior || particle.Rune != tc.rune ||
			particle.LastIntX != tc.lastX || particle.LastIntY != tc.lastY {
			t.Errorf("particle %d = %+v, want behavior=%v rune=%q last=(%d,%d)",
				i, particle, tc.behavior, tc.rune, tc.lastX, tc.lastY)
		}
		kinetic, _ := w.Components.Kinetic.GetComponent(entity)
		if (kinetic.VelY < 0) != tc.wantNegativeY || (kinetic.AccelY < 0) != tc.wantNegativeY {
			t.Errorf("%s vertical motion = vel %v accel %v", component.ParticleBehaviorNames[tc.behavior], kinetic.VelY, kinetic.AccelY)
		}
		sigil, _ := w.Components.Sigil.GetComponent(entity)
		wantColor := visual.RgbDecay
		if tc.wantPink {
			wantColor = visual.RgbBlossom
		}
		if sigil.Rune != tc.rune || sigil.Color != wantColor {
			t.Errorf("%s sigil = %+v, want rune %q color %+v", component.ParticleBehaviorNames[tc.behavior], sigil, tc.rune, wantColor)
		}
	}
}

func TestParticleWaveUsesBehaviorEdge(t *testing.T) {
	w, particles := newParticleWorld(0xA11CE)
	w.SetupLevel(4, 6, false, false, false)

	particles.HandleEvent(event.GameEvent{Type: event.EventParticleWave, Payload: &event.ParticleWavePayload{
		Behavior: component.ParticleDecay,
	}})
	particles.HandleEvent(event.GameEvent{Type: event.EventParticleWave, Payload: &event.ParticleWavePayload{
		Behavior: component.ParticleBlossom,
	}})

	var counts [component.ParticleBehaviorCount]int
	for _, entity := range w.Components.Particle.Entities() {
		particle, _ := w.Components.Particle.GetComponent(entity)
		position, _ := w.Positions.GetPosition(entity)
		kinetic, _ := w.Components.Kinetic.GetComponent(entity)
		counts[particle.Behavior]++
		switch particle.Behavior {
		case component.ParticleDecay:
			if position.Y != 0 || kinetic.VelY <= 0 || kinetic.AccelY <= 0 {
				t.Errorf("decay wave particle at %+v with vel=%v accel=%v", position, kinetic.VelY, kinetic.AccelY)
			}
		case component.ParticleBlossom:
			if position.Y != 5 || kinetic.VelY >= 0 || kinetic.AccelY >= 0 {
				t.Errorf("blossom wave particle at %+v with vel=%v accel=%v", position, kinetic.VelY, kinetic.AccelY)
			}
		}
	}
	if counts[component.ParticleDecay] != 4 || counts[component.ParticleBlossom] != 4 {
		t.Fatalf("wave counts = decay %d blossom %d, want 4 each",
			counts[component.ParticleDecay], counts[component.ParticleBlossom])
	}
}

func TestParticleBehaviorsPreserveGlyphEffects(t *testing.T) {
	for _, tc := range []struct {
		name      string
		behavior  component.ParticleBehavior
		wantLevel component.GlyphLevel
	}{
		{name: "decay darkens", behavior: component.ParticleDecay, wantLevel: component.GlyphDark},
		{name: "blossom brightens", behavior: component.ParticleBlossom, wantLevel: component.GlyphBright},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, particles := newParticleWorld(0xEFFECC)
			glyphEntity := w.CreateEntity(core.DomainPlayer)
			w.Positions.SetPosition(glyphEntity, component.PositionComponent{X: 5, Y: 5})
			w.Components.Glyph.SetComponent(glyphEntity, component.GlyphComponent{
				Rune: 'g', Type: component.GlyphBlue, Level: component.GlyphNormal,
			})
			particles.spawnOne(event.ParticleSpawnEntry{Behavior: tc.behavior, X: 5, Y: 5, Char: 'p'}, 0, 0)

			particles.Update()

			glyph, _ := w.Components.Glyph.GetComponent(glyphEntity)
			if glyph.Level != tc.wantLevel || glyph.Type != component.GlyphBlue {
				t.Fatalf("glyph = %+v, want blue level %v", glyph, tc.wantLevel)
			}
			if got := w.Resources.Status.Ints.Get(component.ParticleBehaviorNames[tc.behavior] + ".applied").Load(); got != 1 {
				t.Fatalf("%s.applied = %d, want 1", component.ParticleBehaviorNames[tc.behavior], got)
			}
		})
	}
}

func TestDecayParticlePreservesGlyphTransitionChain(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   component.GlyphComponent
		want component.GlyphComponent
	}{
		{
			name: "level decreases",
			in:   component.GlyphComponent{Rune: 'x', Type: component.GlyphBlue, Level: component.GlyphBright},
			want: component.GlyphComponent{Rune: 'x', Type: component.GlyphBlue, Level: component.GlyphNormal},
		},
		{
			name: "dark blue becomes bright green",
			in:   component.GlyphComponent{Rune: 'x', Type: component.GlyphBlue, Level: component.GlyphDark},
			want: component.GlyphComponent{Rune: 'x', Type: component.GlyphGreen, Level: component.GlyphBright},
		},
		{
			name: "dark green becomes bright red",
			in:   component.GlyphComponent{Rune: 'x', Type: component.GlyphGreen, Level: component.GlyphDark},
			want: component.GlyphComponent{Rune: 'x', Type: component.GlyphRed, Level: component.GlyphBright},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, particles := newParticleWorld(1)
			entity := w.CreateEntity(core.DomainPlayer)
			w.Components.Glyph.SetComponent(entity, tc.in)
			particles.applyDecayToCharacter(&particles.behavior[component.ParticleDecay], entity)
			got, _ := w.Components.Glyph.GetComponent(entity)
			if got != tc.want {
				t.Fatalf("glyph = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParticleProtectionAndTerminalGlyphRules(t *testing.T) {
	w, particles := newParticleWorld(1)
	decayState := &particles.behavior[component.ParticleDecay]
	blossomState := &particles.behavior[component.ParticleBlossom]

	protected := w.CreateEntity(core.DomainPlayer)
	protectedGlyph := component.GlyphComponent{Rune: 'p', Type: component.GlyphRed, Level: component.GlyphDark}
	w.Components.Glyph.SetComponent(protected, protectedGlyph)
	w.Components.Protection.SetComponent(protected, component.ProtectionComponent{Mask: component.ProtectFromParticle})
	particles.applyDecayToCharacter(decayState, protected)
	if destroy := particles.applyBlossomToCharacter(blossomState, protected); destroy {
		t.Fatal("protected red glyph consumed blossom")
	}
	if got, _ := w.Components.Glyph.GetComponent(protected); got != protectedGlyph {
		t.Fatalf("protected glyph changed: %+v", got)
	}
	if got := decayState.statProtectedRejects.Load(); got != 1 {
		t.Errorf("decay protected rejects = %d, want 1", got)
	}
	if got := blossomState.statProtectedRejects.Load(); got != 1 {
		t.Errorf("blossom protected rejects = %d, want 1", got)
	}

	red := w.CreateEntity(core.DomainPlayer)
	w.Components.Glyph.SetComponent(red, component.GlyphComponent{Rune: 'r', Type: component.GlyphRed, Level: component.GlyphNormal})
	if destroy := particles.applyBlossomToCharacter(blossomState, red); !destroy {
		t.Fatal("unprotected red glyph did not consume blossom")
	}

	darkRed := w.CreateEntity(core.DomainPlayer)
	w.Components.Glyph.SetComponent(darkRed, component.GlyphComponent{Rune: 'd', Type: component.GlyphRed, Level: component.GlyphDark})
	if !particles.shouldDieByDecay(darkRed) {
		t.Fatal("dark red glyph was not terminal for decay")
	}
}

func TestParticleMutualAnnihilationIsImmediateAndSilent(t *testing.T) {
	w, particles := newParticleWorld(0xA6611A7E)
	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleDecay, X: 5, Y: 5, Char: 'd'}, 0, 0)
	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleBlossom, X: 5, Y: 5, Char: 'b'}, 0, 0)
	w.Resources.Time.DeltaTime = 0

	particles.Update()

	if got := w.Components.Particle.CountEntities(); got != 0 {
		t.Fatalf("particle count after mutual collision = %d, want 0", got)
	}
	if events := w.Resources.Event.Queue.Consume(); len(events) != 0 {
		t.Fatalf("mutual annihilation queued %d events, want direct removal", len(events))
	}
}

func TestParticleBehaviorsShareOneRNGStream(t *testing.T) {
	w1, decayFirst := newParticleWorld(0x5EED)
	decayFirst.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleDecay, X: 1, Y: 1, Char: 'd'}, 0, 0)
	decayEntity := w1.Components.Particle.Entities()[0]
	decayKinetic, _ := w1.Components.Kinetic.GetComponent(decayEntity)

	w2, blossomFirst := newParticleWorld(0x5EED)
	blossomFirst.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleBlossom, X: 2, Y: 2, Char: 'b'}, 0, 0)
	blossomEntity := w2.Components.Particle.Entities()[0]
	blossomKinetic, _ := w2.Components.Kinetic.GetComponent(blossomEntity)
	if decayKinetic.VelY != -blossomKinetic.VelY {
		t.Fatalf("first draw differs by behavior: decay %v, blossom %v", decayKinetic.VelY, blossomKinetic.VelY)
	}

	blossomFirst.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleDecay, X: 1, Y: 1, Char: 'd'}, 0, 0)
	secondDecay := w2.Components.Particle.Entities()[1]
	secondDecayKinetic, _ := w2.Components.Kinetic.GetComponent(secondDecay)
	if decayKinetic.VelY == secondDecayKinetic.VelY {
		t.Fatalf("blossom spawn did not advance shared stream: both decay speeds are %v", decayKinetic.VelY)
	}
}

func TestFallingParticlesUseCommonWallDestruction(t *testing.T) {
	for _, behavior := range particleBehaviorOrder {
		if particleProfiles[behavior].bounce > 0 {
			continue
		}
		t.Run(component.ParticleBehaviorNames[behavior], func(t *testing.T) {
			w, particles := newParticleWorld(0x5EED)
			wall := w.CreateEntity(core.DomainShared)
			w.Positions.SetPosition(wall, component.PositionComponent{X: 5, Y: 5})
			w.Components.Wall.SetComponent(wall, component.WallComponent{BlockMask: component.WallBlockParticle})
			particles.spawnOne(event.ParticleSpawnEntry{Behavior: behavior, X: 5, Y: 5, Char: 'p'}, 0, 0)
			w.Resources.Time.DeltaTime = 0

			particles.Update()

			if got := w.Components.Particle.CountEntities(); got != 0 {
				t.Fatalf("particle count after wall collision = %d, want 0", got)
			}
			if got := w.Resources.Status.Ints.Get(component.ParticleBehaviorNames[behavior] + ".wall_collisions").Load(); got != 1 {
				t.Fatalf("wall collisions = %d, want 1", got)
			}
			if events := w.Resources.Event.Queue.Consume(); len(events) != 0 {
				t.Fatalf("wall collision queued %d events, want direct removal", len(events))
			}
		})
	}
}

func TestParticleBehaviorOrderCoversEveryProfile(t *testing.T) {
	seen := make(map[component.ParticleBehavior]bool, len(particleBehaviorOrder))
	for _, behavior := range particleBehaviorOrder {
		if seen[behavior] {
			t.Errorf("behavior %v appears more than once in update order", behavior)
		}
		seen[behavior] = true
		if particleProfiles[behavior].color == ([3]color.RGB{}) {
			t.Errorf("behavior %v has no profile", behavior)
		}
	}
	for behavior := component.ParticleDecay; behavior < component.ParticleBehaviorCount; behavior++ {
		if !seen[behavior] {
			t.Errorf("behavior %v has no update-order entry", behavior)
		}
	}
}

// Dust lives by its glyph level and fades a level as its life runs down; the
// falling behaviors carry no timer, so they live until they collide or leave.
func TestParticleLifetimeFollowsProfile(t *testing.T) {
	w, particles := newParticleWorld(0x71FE)
	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleDecay, X: 3, Y: 4, Char: 'd'}, 0, 0)
	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleBlossom, X: 6, Y: 7, Char: 'b'}, 0, 0)
	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleDust, X: 10, Y: 5, Char: 'u',
		Level: component.GlyphBright, SkipStartCell: true}, 10, 5)

	entities := w.Components.Particle.Entities()
	for _, e := range entities[:2] {
		if w.Components.Timer.HasEntity(e) {
			t.Fatalf("falling particle %d has a timer", e)
		}
	}
	dust := entities[2]
	timer, ok := w.Components.Timer.GetPtr(dust)
	if !ok || timer.Slots[component.TimerLifetime].Remaining != parameter.DustTimerBright {
		t.Fatalf("bright dust timer = %+v, want %v", timer, parameter.DustTimerBright)
	}

	timer.Slots[component.TimerLifetime].Remaining = parameter.DustTimerNormal - time.Millisecond
	w.Resources.Time.DeltaTime = 0
	particles.Update()
	if sigil, _ := w.Components.Sigil.GetComponent(dust); sigil.Color != visual.RgbDustNormal {
		t.Fatalf("dust colour = %+v, want the normal level once below its lifetime", sigil.Color)
	}
}

func TestDustBouncesOffWalls(t *testing.T) {
	w, particles := newParticleWorld(0xB0)
	wall := w.CreateEntity(core.DomainShared)
	w.Positions.SetPosition(wall, component.PositionComponent{X: 6, Y: 5})
	w.Components.Wall.SetComponent(wall, component.WallComponent{BlockMask: component.WallBlockParticle})
	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleDust, X: 5, Y: 5, Char: 'u',
		Level: component.GlyphNormal, SkipStartCell: true}, 5, 5)
	dust := w.Components.Particle.Entities()[0]
	kinetic, _ := w.Components.Kinetic.GetComponent(dust)
	kinetic.VelX, kinetic.VelY = 20, 0
	w.Components.Kinetic.SetComponent(dust, kinetic)
	w.Resources.Time.DeltaTime = 100 * time.Millisecond

	particles.Update()

	kinetic, ok := w.Components.Kinetic.GetComponent(dust)
	if pos, _ := w.Positions.GetPosition(dust); !ok || pos.X != 5 || kinetic.VelX >= 0 {
		t.Fatalf("dust at %+v with velocity %v, want it held at 5 and reflected", pos, kinetic.VelX)
	}
	if got := w.Resources.Status.Ints.Get("dust.wall_collisions").Load(); got != 1 {
		t.Fatalf("dust wall collisions = %d, want 1", got)
	}
}

// Every decay particle on the map orbits a spinning or charging Kraken, entering
// with a kick in its spin, and drops straight down once the Kraken stands still.
func TestDecayOrbitsKrakenOnlyWhileItSpinsOrCharges(t *testing.T) {
	w, _, kraken, _ := krakenFixture(t)
	particles := ready(NewParticleSystem(w)).(*ParticleSystem)
	k, _ := w.Components.Kraken.GetPtr(kraken)
	k.State, k.TurnDir = component.KrakenSpin, 1
	body, _ := w.Components.Kinetic.GetComponent(kraken)
	particles.spawnOne(event.ParticleSpawnEntry{Behavior: component.ParticleDecay, X: 90, Y: 10, Char: 'd', SkipStartCell: true}, 0, 0)
	decay := w.Components.Particle.Entities()[0]
	w.Resources.Time.DeltaTime = 50 * time.Millisecond
	distance := func() float64 {
		kinetic, _ := w.Components.Kinetic.GetComponent(decay)
		return math.Hypot(kinetic.PreciseX-body.PreciseX, 2*(kinetic.PreciseY-body.PreciseY))
	}
	start := distance()

	particles.Update()
	particle, _ := w.Components.Particle.GetComponent(decay)
	kinetic, _ := w.Components.Kinetic.GetComponent(decay)
	if !particle.Orbiting || particle.OrbitRadius < parameter.DecayOrbitRadiusMin || kinetic.VelX <= 0 {
		t.Fatalf("decay above a spinning Kraken = %+v, velocity %v; want it orbiting with the spin", particle, kinetic.VelX)
	}
	for range 40 {
		particles.Update()
	}
	if d := distance(); d > start/2 || !w.Components.Particle.HasEntity(decay) {
		t.Fatalf("decay at %.1f cells from the Kraken after 2 s, started at %.1f", d, start)
	}

	k.State = component.KrakenIdle
	particles.Update()
	particle, _ = w.Components.Particle.GetComponent(decay)
	kinetic, _ = w.Components.Kinetic.GetComponent(decay)
	if particle.Orbiting || kinetic.VelX != 0 || kinetic.VelY < parameter.ParticleMinSpeed {
		t.Fatalf("decay beside an idle Kraken = %+v, velocity (%v, %v); want a straight fall", particle, kinetic.VelX, kinetic.VelY)
	}
}
