package renderer

import (
	"testing"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func TestEffectBoundsClipToVisibleMap(t *testing.T) {
	ctx := render.RenderContext{
		ViewportWidth:  20,
		ViewportHeight: 10,
		CameraX:        50,
		CameraY:        20,
		MapWidth:       200,
		MapHeight:      60,
	}

	startX, endX, startY, endY, ok := effectBounds(ctx, 55, 25, 30, 15)
	if !ok || startX != 50 || endX != 69 || startY != 20 || endY != 29 {
		t.Fatalf("clipped bounds = (%d..%d, %d..%d, %t), want (50..69, 20..29, true)",
			startX, endX, startY, endY, ok)
	}

	if _, _, _, _, ok := effectBounds(ctx, 5, 5, 4, 2); ok {
		t.Fatal("fully off-viewport effect reported visible bounds")
	}
}

// TestAHeldTurretShowsNoMuzzleFlash: the red's muzzle flash shows its turret
// firing, so a burst a stun holds, its mount disarmed, draws none
func TestAHeldTurretShowsNoMuzzleFlash(t *testing.T) {
	w := engine.NewWorld()
	r := NewStormRenderer(engine.NewGameContextWithClock(w, 40, 24, engine.NewManualClock()))
	ctx := render.RenderContext{ViewportWidth: 40, ViewportHeight: 24, MapWidth: 40, MapHeight: 24, CellAspect: vmath.RefAspect}
	circle := w.CreateEntity(core.DomainShared)
	w.Components.Mount.SetComponent(circle, component.MountComponent{AimX: 35, AimY: 12, HasAim: true})

	flash := func(armed bool) bool {
		mount, _ := w.Components.Mount.GetPtr(circle)
		mount.Armed = armed
		buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, 40, 24)
		r.renderRedMuzzleFlash(ctx, buf, &stormCircleRender{entity: circle, x: 15, y: 12, index: int(component.StormCircleRed)})
		var zero color.RGB
		for y := range 24 {
			for x := range 40 {
				if buf.CellAt(x, y).Bg != zero {
					return true
				}
			}
		}
		return false
	}
	if !flash(true) {
		t.Fatal("an armed turret drew no muzzle flash")
	}
	if flash(false) {
		t.Fatal("a held turret, disarmed, drew a muzzle flash")
	}
}
