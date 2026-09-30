package app

import (
	"testing"

	"github.com/lixenwraith/vif/internal/input"
)

// TestAMapCellPointerReachesOnlyWhatTheViewportShows: a bot's pointer names map
// cells, and like a mouse it moves the cursor onto a cell the viewport shows and
// onto no other.
func TestAMapCellPointerReachesOnlyWhatTheViewportShows(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 100, 40)
	defer a.Close()
	tickUntilCursor(t, a)
	if !a.SetupLevel(400, 120, true, false) {
		t.Fatal("a solo run refused a map larger than its viewport")
	}
	tickUntilCursor(t, a)

	var shownX, shownY, hiddenX, hiddenY int
	a.World().RunSafe(func() {
		cfg := a.World().Resources.Config
		pos, _ := a.World().LocalCursor()
		shownX, shownY = pos.X+1, pos.Y
		hiddenX, hiddenY = cfg.CameraX+cfg.ViewportWidth+5, pos.Y
		if _, _, ok := cfg.MapToViewport(shownX, shownY); !ok {
			t.Fatalf("the cell beside the cursor, %d,%d, is off screen", shownX, shownY)
		}
		if _, _, ok := cfg.MapToViewport(hiddenX, hiddenY); ok || hiddenX >= cfg.MapWidth {
			t.Fatalf("%d,%d is not a map cell off screen", hiddenX, hiddenY)
		}
	})
	point := func(x, y int) (atX, atY int) {
		a.handleIntent(&input.Intent{Type: input.IntentMouseMove, X: x, Y: y, MapCell: true})
		a.World().RunSafe(func() {
			pos, _ := a.World().LocalCursor()
			atX, atY = pos.X, pos.Y
		})
		return atX, atY
	}

	if x, y := point(hiddenX, hiddenY); x == hiddenX && y == hiddenY {
		t.Fatalf("the pointer reached %d,%d, which the viewport does not show", x, y)
	}
	if x, y := point(shownX, shownY); x != shownX || y != shownY {
		t.Fatalf("the pointer named %d,%d and the cursor is on %d,%d", shownX, shownY, x, y)
	}
}
