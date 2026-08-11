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

func BenchmarkMulFloat(b *testing.B) {
	for b.Loop() {
		sinkF = bfx * bfy
	}
}

func BenchmarkDivFloat(b *testing.B) {
	for b.Loop() {
		sinkF = bfx / bfy
	}
}

func BenchmarkSqrtFloat(b *testing.B) {
	for b.Loop() {
		sinkF = math.Sqrt(bfx)
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

func BenchmarkNormalize2DF(b *testing.B) {
	for b.Loop() {
		sinkF, _ = Normalize2DF(bfx, bfy)
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

func BenchmarkEllipseContainsF(b *testing.B) {
	invRx, invRy := EllipseInvRadiiSqF(5, 2.5)
	for b.Loop() {
		sinkB = EllipseContainsF(bfx, bfy, invRx, invRy)
	}
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

func BenchmarkExpDecayF(b *testing.B) {
	for b.Loop() {
		sinkF = ExpDecayF(137)
	}
}
