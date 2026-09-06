package engine

import "testing"

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
