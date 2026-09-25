package navigation

import (
	"math/rand/v2"
	"testing"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// referenceCompute is Compute as it stood before its wall answers were memoized:
// every neighbour and both corners of every diagonal asked of isBlocked directly.
func referenceCompute(f *FlowField, targets []vmath.Point, isBlocked WallChecker) {
	if len(targets) == 0 {
		f.Valid = false
		return
	}
	w := f.Width
	f.CurrentGen++
	if f.CurrentGen == 0 {
		clear(f.VisitedGen)
		f.CurrentGen = 1
	}
	f.heap = f.heap[:0]
	for _, t := range targets {
		if t.X < 0 || t.Y < 0 || t.X >= f.Width || t.Y >= f.Height {
			continue
		}
		targetIdx := t.Y*w + t.X
		if !isBlocked(t.X, t.Y) {
			if f.VisitedGen[targetIdx] != f.CurrentGen {
				f.Distances[targetIdx] = 0
				f.VisitedGen[targetIdx] = f.CurrentGen
				f.Directions[targetIdx] = DirTarget
				f.heap.push(heapEntry{idx: targetIdx, dist: 0})
			}
		} else {
			f.seedVirtualTargets(t.X, t.Y, isBlocked)
		}
	}
	if len(f.heap) == 0 {
		f.Valid = false
		return
	}
	for len(f.heap) > 0 {
		entry := f.heap.pop()
		idx := entry.idx
		if f.VisitedGen[idx] == f.CurrentGen && entry.dist > f.Distances[idx] {
			continue
		}
		cx, cy := idx%w, idx/w
		for dirIdx := int8(0); dirIdx < DirCount; dirIdx++ {
			nx, ny := cx+DirVectors[dirIdx][0], cy+DirVectors[dirIdx][1]
			if nx < 0 || nx >= f.Width || ny < 0 || ny >= f.Height || isBlocked(nx, ny) {
				continue
			}
			dx, dy := DirVectors[dirIdx][0], DirVectors[dirIdx][1]
			if dx != 0 && dy != 0 && (isBlocked(cx+dx, cy) || isBlocked(cx, cy+dy)) {
				continue
			}
			nIdx := ny*w + nx
			newDist := entry.dist + dirCosts[dirIdx]
			if f.VisitedGen[nIdx] != f.CurrentGen || newDist < f.Distances[nIdx] {
				f.Distances[nIdx] = newDist
				f.VisitedGen[nIdx] = f.CurrentGen
				f.Directions[nIdx] = DirOpposite[dirIdx]
				f.heap.push(heapEntry{idx: nIdx, dist: newDist})
			}
		}
	}
	f.Valid = true
}

// FuzzComputeMatchesReference: on any wall layout — rooms, jagged edges left by
// destroyed cells, targets inside walls, either out-of-bounds convention — Compute
// gives every cell the reference's direction and distance, and a recompute after
// walls change sees the new walls.
func FuzzComputeMatchesReference(f *testing.F) {
	for seed := range uint64(16) {
		f.Add(seed, uint8(seed*7), uint8(seed*13))
	}
	f.Fuzz(func(t *testing.T, seed uint64, density, targets uint8) {
		rng := rand.New(rand.NewPCG(seed, uint64(density)<<8|uint64(targets)))
		w, h := 2+rng.IntN(40), 2+rng.IntN(20)
		walls := make([]bool, w*h)
		for range rng.IntN(8) { // rectangles, then cells carved out of them
			x0, y0 := rng.IntN(w), rng.IntN(h)
			for y := y0; y < min(h, y0+1+rng.IntN(6)); y++ {
				for x := x0; x < min(w, x0+1+rng.IntN(10)); x++ {
					walls[y*w+x] = true
				}
			}
		}
		for i := range walls {
			if rng.IntN(256) < int(density)/2 {
				walls[i] = !walls[i]
			}
		}
		oobBlocked := seed&1 == 1
		isBlocked := func(x, y int) bool {
			if x < 0 || y < 0 || x >= w || y >= h {
				return oobBlocked
			}
			return walls[y*w+x]
		}
		goals := make([]vmath.Point, 1+int(targets)%3)
		for i := range goals {
			goals[i] = vmath.Point{X: rng.IntN(w+2) - 1, Y: rng.IntN(h+2) - 1}
		}

		got := NewFlowField(w, h)
		for round := range 2 {
			if round == 1 { // a storm takes cells out of the walls between recomputes
				for i := range walls {
					if walls[i] && rng.IntN(4) == 0 {
						walls[i] = false
					}
				}
			}
			want := NewFlowField(w, h)
			referenceCompute(want, goals, isBlocked)
			got.Compute(goals, isBlocked)
			if got.Valid != want.Valid {
				t.Fatalf("round %d: valid %t, reference %t", round, got.Valid, want.Valid)
			}
			for y := range h {
				for x := range w {
					if gd, wd := got.GetDirection(x, y), want.GetDirection(x, y); gd != wd {
						t.Fatalf("round %d (%d,%d): direction %d, reference %d", round, x, y, gd, wd)
					}
					if gd, wd := got.GetDistance(x, y), want.GetDistance(x, y); gd != wd {
						t.Fatalf("round %d (%d,%d): distance %d, reference %d", round, x, y, gd, wd)
					}
				}
			}
		}
	})
}
