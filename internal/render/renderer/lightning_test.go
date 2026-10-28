package renderer

import (
	"math/bits"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestBoltMeetsItsEndCellsOnTheSideItComesFrom: a bolt aims between cell centres, so
// it draws one quadrant of the cell it leaves and one of the cell it strikes, and along
// its axis that quadrant faces the other end, whichever way it is fired.
func TestBoltMeetsItsEndCellsOnTheSideItComesFrom(t *testing.T) {
	t.Parallel()
	r := &LightningRenderer{cellHits: make(map[uint64]uint8)}
	const ox, oy = 40, 40
	for _, d := range [][2]int{{0, -1}, {0, 1}, {-1, 0}, {1, 0}, {-1, -1}, {1, -1}, {-1, 1}, {1, 1}} {
		for _, reach := range []int{4, 9, 30} {
			tx, ty := ox+d[0]*reach, oy+d[1]*reach
			for seed := range uint64(64) {
				hits := r.traceBolt(r.generateFractalPath(ox, oy, tx, ty, vmath.NewFastRand(seed+1)))
				for _, end := range []struct{ x, y, dx, dy int }{{ox, oy, d[0], d[1]}, {tx, ty, -d[0], -d[1]}} {
					key, _ := quadrantHit(2*end.x, 2*end.y)
					got := hits[key]
					if bits.OnesCount8(got) != 1 {
						t.Fatalf("bolt (%d,%d)->(%d,%d) seed %d: cell (%d,%d) shows quadrants %04b, want one",
							ox, oy, tx, ty, seed, end.x, end.y, got)
					}
					q := bits.TrailingZeros8(got)
					qx, qy := q&1, q>>1
					// Facing +1 is the right column or bottom row; a diagonal's corner is the path's to pick
					axial := end.dx == 0 || end.dy == 0
					if axial && (end.dx != 0 && qx != (end.dx+1)/2 || end.dy != 0 && qy != (end.dy+1)/2) {
						t.Fatalf("bolt (%d,%d)->(%d,%d) seed %d: cell (%d,%d) shows quadrant %04b, not the side toward (%+d,%+d)",
							ox, oy, tx, ty, seed, end.x, end.y, got, end.dx, end.dy)
					}
				}
			}
		}
	}
}
