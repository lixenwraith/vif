package system

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/engine"
)

func TestWallDisplacementMaskUsesEntityCapabilities(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(*engine.World, core.Entity)
		want  component.WallBlockMask
	}{
		{
			name: "kinetic header is physical",
			setup: func(w *engine.World, e core.Entity) {
				w.Components.Kinetic.SetComponent(e, component.KineticComponent{})
				w.Components.Header.SetComponent(e, component.HeaderComponent{})
			},
			want: component.WallBlockKinetic,
		},
		{
			name: "plain composite header is an anchor",
			setup: func(w *engine.World, e core.Entity) {
				w.Components.Header.SetComponent(e, component.HeaderComponent{})
			},
			want: component.WallBlockNone,
		},
		{
			name: "particle",
			setup: func(w *engine.World, e core.Entity) {
				w.Components.Particle.SetComponent(e, component.ParticleComponent{Behavior: component.ParticleDecay})
			},
			want: component.WallBlockParticle,
		},
		{
			name:  "ordinary spawn blocker",
			setup: func(*engine.World, core.Entity) {},
			want:  component.WallBlockSpawn,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := engine.NewWorld()
			engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock())
			e := w.CreateEntity(core.DomainShared)
			tc.setup(w, e)
			walls := NewWallSystem(w).(*WallSystem)
			if got := walls.getMaskForEntity(e); got != tc.want {
				t.Fatalf("mask = %v, want %v", got, tc.want)
			}
		})
	}
}
