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

const (
	bx = int64(7)<<32 | 0x4000_0000 // 7.25
	by = int64(3)<<32 | 0x8000_0000 // 3.5
)

var (
	bfx = ToFloat(bx)
	bfy = ToFloat(by)
)

func BenchmarkMul(b *testing.B) {
	for b.Loop() {
		sinkI = Mul(bx, by)
	}
}

func BenchmarkMulFloat(b *testing.B) {
	for b.Loop() {
		sinkF = bfx * bfy
	}
}

func BenchmarkDiv(b *testing.B) {
	for b.Loop() {
		sinkI = Div(bx, by)
	}
}

func BenchmarkDivFloat(b *testing.B) {
	for b.Loop() {
		sinkF = bfx / bfy
	}
}

func BenchmarkSqrt(b *testing.B) {
	for b.Loop() {
		sinkI = Sqrt(bx)
	}
}

func BenchmarkSqrtFloat(b *testing.B) {
	for b.Loop() {
		sinkF = math.Sqrt(bfx)
	}
}

func BenchmarkMagnitude(b *testing.B) {
	for b.Loop() {
		sinkI = Magnitude(bx, by)
	}
}

func BenchmarkMagnitudeF(b *testing.B) {
	for b.Loop() {
		sinkF = MagnitudeF(bfx, bfy)
	}
}

func BenchmarkMagnitudeHypot(b *testing.B) {
	for b.Loop() {
		sinkF = math.Hypot(bfx, bfy)
	}
}

func BenchmarkNormalize2D(b *testing.B) {
	for b.Loop() {
		sinkI, _ = Normalize2D(bx, by)
	}
}

func BenchmarkNormalize2DF(b *testing.B) {
	for b.Loop() {
		sinkF, _ = Normalize2DF(bfx, bfy)
	}
}

func BenchmarkSin(b *testing.B) {
	for b.Loop() {
		sinkI = Sin(bx)
	}
}

func BenchmarkSinF(b *testing.B) {
	for b.Loop() {
		sinkF = SinF(bfx)
	}
}

func BenchmarkSinStdlib(b *testing.B) {
	for b.Loop() {
		sinkF = math.Sin(bfx)
	}
}

func BenchmarkAtan2F(b *testing.B) {
	for b.Loop() {
		sinkF = Atan2F(bfy, bfx)
	}
}

func BenchmarkAtan2Stdlib(b *testing.B) {
	for b.Loop() {
		sinkF = math.Atan2(bfy, bfx)
	}
}

func BenchmarkEllipseContains(b *testing.B) {
	invRx, invRy := EllipseInvRadiiSq(FromFloat(5), FromFloat(2.5))
	for b.Loop() {
		sinkB = EllipseContains(bx, by, invRx, invRy)
	}
}

func BenchmarkEllipseContainsF(b *testing.B) {
	invRx, invRy := EllipseInvRadiiSqF(5, 2.5)
	for b.Loop() {
		sinkB = EllipseContainsF(bfx, bfy, invRx, invRy)
	}
}

func BenchmarkGridTraverser(b *testing.B) {
	x1, y1 := FromFloat(2.5), FromFloat(3.5)
	x2, y2 := FromFloat(40.5), FromFloat(28.5)
	n := 0
	for b.Loop() {
		tr := NewGridTraverser(x1, y1, x2, y2)
		for tr.Next() {
			n++
		}
	}
	sinkI = int64(n)
}

func BenchmarkGridTraverserF(b *testing.B) {
	n := 0
	for b.Loop() {
		tr := NewGridTraverserF(2.5, 3.5, 40.5, 28.5)
		for tr.Next() {
			n++
		}
	}
	sinkI = int64(n)
}

func BenchmarkExpDecay(b *testing.B) {
	for b.Loop() {
		sinkI = ExpDecay(137)
	}
}

func BenchmarkExpDecayF(b *testing.B) {
	for b.Loop() {
		sinkF = ExpDecayF(137)
	}
}
