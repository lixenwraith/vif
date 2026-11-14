package engine

import (
	"cmp"
	"math"
	"testing"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestMapOffsetAndItsInverseAgree pins the one definition the renderer and the
// mouse share. They are inverses: the viewport cell the renderer draws the map's
// origin at must be the cell a click there resolves to, or the pointer and the
// cursor disagree by half the centring margin.
func TestMapOffsetAndItsInverseAgree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		cfg      ConfigResource
		wantOffX int
		wantOffY int
	}{
		{
			name:     "map centred inside a larger viewport",
			cfg:      ConfigResource{MapWidth: 10, MapHeight: 6, ViewportWidth: 20, ViewportHeight: 12},
			wantOffX: 5, wantOffY: 3,
		},
		{
			name:     "odd margin floors, matching the renderer",
			cfg:      ConfigResource{MapWidth: 10, MapHeight: 6, ViewportWidth: 21, ViewportHeight: 13},
			wantOffX: 5, wantOffY: 3,
		},
		{
			name:     "camera crops a larger map",
			cfg:      ConfigResource{MapWidth: 40, MapHeight: 30, ViewportWidth: 20, ViewportHeight: 12, CameraX: 6, CameraY: 4},
			wantOffX: 0, wantOffY: 0,
		},
		{
			name:     "map exactly fills the viewport",
			cfg:      ConfigResource{MapWidth: 20, MapHeight: 12, ViewportWidth: 20, ViewportHeight: 12},
			wantOffX: 0, wantOffY: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			offX, offY := tt.cfg.MapOffset()
			if offX != tt.wantOffX || offY != tt.wantOffY {
				t.Fatalf("MapOffset() = (%d,%d), want (%d,%d)", offX, offY, tt.wantOffX, tt.wantOffY)
			}

			// Every map cell that the viewport shows round-trips through the
			// inverse, and nothing else in the viewport resolves to a map cell.
			resolved := 0
			for vy := range tt.cfg.ViewportHeight {
				for vx := range tt.cfg.ViewportWidth {
					mapX, mapY, ok := tt.cfg.ViewportToMap(vx, vy)
					if !ok {
						continue
					}
					resolved++
					if got := mapX - tt.cfg.CameraX + offX; got != vx {
						t.Fatalf("viewport %d round-tripped to %d", vx, got)
					}
					if got := mapY - tt.cfg.CameraY + offY; got != vy {
						t.Fatalf("viewport %d round-tripped to %d", vy, got)
					}
				}
			}
			want := min(tt.cfg.MapWidth, tt.cfg.ViewportWidth) * min(tt.cfg.MapHeight, tt.cfg.ViewportHeight)
			if resolved != want {
				t.Fatalf("%d viewport cells resolved to map cells, want %d", resolved, want)
			}
		})
	}
}

// TestViewportToMapRejectsTheMargin is the click that used to land on the wrong
// cell: a coordinate inside the viewport but outside the centred map.
func TestViewportToMapRejectsTheMargin(t *testing.T) {
	t.Parallel()
	cfg := ConfigResource{MapWidth: 10, MapHeight: 6, ViewportWidth: 20, ViewportHeight: 12}

	if _, _, ok := cfg.ViewportToMap(0, 0); ok {
		t.Fatal("the viewport's top-left corner resolved to a map cell")
	}
	// The map's own origin sits at the centring offset, not at the viewport's.
	x, y, ok := cfg.ViewportToMap(5, 3)
	if !ok || x != 0 || y != 0 {
		t.Fatalf("viewport (5,3) = (%d,%d) ok=%v, want the map origin", x, y, ok)
	}
}

// TestTheCameraEdgeIsAsFarFromEverySide: an edge between the extremes is the same
// distance in from every side of a wide or a tall view, whatever shape its cells,
// and keeps its share of the screen when cells shrink to show twice the map.
func TestTheCameraEdgeIsAsFarFromEverySide(t *testing.T) {
	t.Parallel()
	for _, v := range []struct {
		w, h   int
		aspect vmath.Aspect
	}{{212, 55, 0}, {155, 30, 0}, {67, 71, 0}, {120, 60, 0.6}} {
		c := ConfigResource{ViewportWidth: v.w, ViewportHeight: v.h, CellAspect: v.aspect}
		aspect := float64(cmp.Or(v.aspect, vmath.RefAspect))
		x, y := c.EdgeCells(15)
		// Columns against rows in cell widths, each rounded to its own cell
		if d := math.Abs(float64(x) - float64(y)/aspect); d > 0.5+0.5/aspect {
			t.Errorf("%dx%d: edge %dx%d is not as far from every side", v.w, v.h, x, y)
		}
		zoomed := ConfigResource{ViewportWidth: 2 * v.w, ViewportHeight: 2 * v.h, CellAspect: v.aspect}
		if zx, zy := zoomed.EdgeCells(15); max(zx-2*x, 2*x-zx) > 1 || max(zy-2*y, 2*y-zy) > 1 {
			t.Errorf("%dx%d: edge %dx%d at twice the cells, want about %dx%d", v.w, v.h, zx, zy, 2*x, 2*y)
		}
	}
}

// TestTheCameraEdgeRunsFromTheEdgeCellToEveryMove: at 0 the view moves once the
// cursor is on its edge row or column, which a pointer can reach; at the widest
// edge every move moves it, on views of even and odd size, wide or tall, and a
// second follow of the same cell never moves it back.
func TestTheCameraEdgeRunsFromTheEdgeCellToEveryMove(t *testing.T) {
	t.Parallel()
	for _, v := range [][2]int{{212, 55}, {211, 54}, {67, 71}, {40, 22}} {
		c := ConfigResource{MapWidth: 1000, MapHeight: 1000, ViewportWidth: v[0], ViewportHeight: v[1], CameraX: 400, CameraY: 400}
		follow := func(x, y, edge int) (int, int) {
			cx, cy := c.CameraX, c.CameraY
			c.FollowCamera(x, y, edge)
			return c.CameraX - cx, c.CameraY - cy
		}
		right, bottom := c.CameraX+v[0]-1, c.CameraY+v[1]-1
		if dx, dy := follow(right-1, bottom-1, 0); dx != 0 || dy != 0 {
			t.Errorf("%dx%d at 0: a cursor a cell inside the edge moved the view by (%d,%d)", v[0], v[1], dx, dy)
		}
		if dx, dy := follow(right, bottom, 0); dx != 1 || dy != 1 {
			t.Errorf("%dx%d at 0: a cursor on the edge moved the view by (%d,%d), want (1,1)", v[0], v[1], dx, dy)
		}
		x, y := c.CameraX+v[0]/2, c.CameraY+v[1]/2
		follow(x, y, parameter.CameraEdgeMost)
		for _, step := range [][2]int{{1, 0}, {1, 0}, {-1, 0}, {-1, 0}, {0, 1}, {0, 1}, {0, -1}, {0, -1}} {
			x, y = x+step[0], y+step[1]
			if dx, dy := follow(x, y, parameter.CameraEdgeMost); dx != step[0] || dy != step[1] {
				t.Errorf("%dx%d at the widest edge: a move by %v moved the view by (%d,%d)", v[0], v[1], step, dx, dy)
			}
			if dx, dy := follow(x, y, parameter.CameraEdgeMost); dx != 0 || dy != 0 {
				t.Errorf("%dx%d at the widest edge: a second follow moved the view by (%d,%d)", v[0], v[1], dx, dy)
			}
		}
	}
}
