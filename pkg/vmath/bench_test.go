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

func BenchmarkExpDecay(b *testing.B) {
	for b.Loop() {
		sinkF = ExpDecay(137)
	}
}
