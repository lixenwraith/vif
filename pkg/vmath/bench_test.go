package vmath

import (
	"math"
	"testing"
)

// sink prevents dead-code elimination of pure arithmetic benchmarks
var (
	sinkI int64
	sinkF float64
	sinkB bool
)

const bfx, bfy = 7.25, 3.5

func BenchmarkMagnitude(b *testing.B) {
	for b.Loop() {
		sinkF = Magnitude(bfx, bfy)
	}
}

func BenchmarkMagnitudeHypot(b *testing.B) {
	for b.Loop() {
		sinkF = math.Hypot(bfx, bfy)
	}
}

func BenchmarkNormalize2D(b *testing.B) {
	for b.Loop() {
		sinkF, _ = Normalize2D(bfx, bfy)
	}
}

func BenchmarkSin(b *testing.B) {
	for b.Loop() {
		sinkF = Sin(bfx)
	}
}

func BenchmarkSinStdlib(b *testing.B) {
	for b.Loop() {
		sinkF = math.Sin(bfx)
	}
}

func BenchmarkAtan2(b *testing.B) {
	for b.Loop() {
		sinkF = Atan2(bfy, bfx)
	}
}

func BenchmarkAtan2Stdlib(b *testing.B) {
	for b.Loop() {
		sinkF = math.Atan2(bfy, bfx)
	}
}

func BenchmarkEllipseContains(b *testing.B) {
	invRx, invRy := EllipseInvRadiiSq(5, 2.5)
	for b.Loop() {
		sinkB = EllipseContains(bfx, bfy, invRx, invRy)
	}
}

func BenchmarkGridTraverser(b *testing.B) {
	n := 0
	for b.Loop() {
		tr := NewGridTraverser(2.5, 3.5, 40.5, 28.5)
		for tr.Next() {
			n++
		}
	}
	sinkI = int64(n)
}

func BenchmarkGridTraverser3(b *testing.B) {
	n := 0
	for b.Loop() {
		tr := NewGridTraverser3(Vec3{2.5, 3.5, 1.5}, Vec3{40.5, 28.5, 9.5})
		for tr.Next() {
			n++
		}
	}
	sinkI = int64(n)
}

// BenchmarkOrbitArcs is what an orb update samples: an orbit's cells, the free
// arcs among them and three angles spread over those, into fresh storage and
// into a caller's
func BenchmarkOrbitArcs(b *testing.B) {
	blocked := make([]bool, EllipseSampleCount)
	for i := range blocked {
		blocked[i] = i%11 < 3
	}
	b.Run("fresh", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			cells := SampleEllipseGrid(40, 20, 6, 3, EllipseSampleCount)
			sinkI = int64(len(cells) + len(DistributeAngles(FindUnblockedArcs(blocked), 3)))
		}
	})
	b.Run("reused", func(b *testing.B) {
		var cells [][2]int
		var arcs []ArcSegment
		var angles []float64
		b.ReportAllocs()
		for b.Loop() {
			cells = AppendEllipseGrid(cells[:0], 40, 20, 6, 3, EllipseSampleCount)
			arcs = AppendUnblockedArcs(arcs[:0], blocked)
			angles = AppendDistributedAngles(angles[:0], arcs, 3)
			sinkI = int64(len(cells) + len(angles))
		}
	})
}

func BenchmarkExpDecay(b *testing.B) {
	for b.Loop() {
		sinkF = ExpDecay(137)
	}
}
