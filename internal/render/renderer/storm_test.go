package renderer

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/render"
)

func TestStormEffectBoundsClipToVisibleMap(t *testing.T) {
	ctx := render.RenderContext{
		ViewportWidth:  20,
		ViewportHeight: 10,
		CameraX:        50,
		CameraY:        20,
		MapWidth:       200,
		MapHeight:      60,
	}

	startX, endX, startY, endY, ok := stormEffectBounds(ctx, 55, 25, 30, 15)
	if !ok || startX != 50 || endX != 69 || startY != 20 || endY != 29 {
		t.Fatalf("clipped bounds = (%d..%d, %d..%d, %t), want (50..69, 20..29, true)",
			startX, endX, startY, endY, ok)
	}

	if _, _, _, _, ok := stormEffectBounds(ctx, 5, 5, 4, 2); ok {
		t.Fatal("fully off-viewport effect reported visible bounds")
	}
}
