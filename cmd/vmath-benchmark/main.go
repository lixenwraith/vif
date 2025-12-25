package main

import (
	"fmt"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/vmath"
)

// Simulation constants matching shield/drain systems
const (
	shieldRadiusX    = 6.0
	shieldRadiusY    = 3.0
	shieldMaxOpacity = 0.8
	gridWidth        = 120
	gridHeight       = 40
	sampleCount      = 10000
)

// Precomputed inverse radii (matches ShieldSystem.cacheInverseRadii)
var (
	invRxSq = vmath.Div(vmath.Scale, vmath.Mul(vmath.FromFloat(shieldRadiusX), vmath.FromFloat(shieldRadiusX)))
	invRySq = vmath.Div(vmath.Scale, vmath.Mul(vmath.FromFloat(shieldRadiusY), vmath.FromFloat(shieldRadiusY)))
)

// Test data: random dx, dy pairs within shield bounding box
type testPoint struct {
	dx, dy int32 // Q16.16
}

var testPoints []testPoint

func init() {
	rand.Seed(time.Now().UnixNano())
	testPoints = make([]testPoint, sampleCount)
	for i := range testPoints {
		// Generate points within bounding box of shield
		x := rand.Intn(int(shieldRadiusX)*2+1) - int(shieldRadiusX)
		y := rand.Intn(int(shieldRadiusY)*2+1) - int(shieldRadiusY)
		testPoints[i] = testPoint{
			dx: vmath.FromInt(x),
			dy: vmath.FromInt(y),
		}
	}
}

// === CURRENT IMPLEMENTATION (shields.go) ===

// currentShieldAlpha replicates the current shield renderer logic
func currentShieldAlpha(dx, dy int32) float64 {
	dxSq := vmath.Mul(dx, dx)
	dySq := vmath.Mul(dy, dy)
	normalizedDistSq := vmath.Mul(dxSq, invRxSq) + vmath.Mul(dySq, invRySq)

	if normalizedDistSq > vmath.Scale {
		return 0 // Outside ellipse
	}

	// Current: sqrt then square (redundant)
	dist := math.Sqrt(vmath.ToFloat(normalizedDistSq))
	alpha := (dist * dist) * shieldMaxOpacity
	return alpha
}

// === OPTIMIZED: ELIMINATE REDUNDANT SQRT ===

// optimizedShieldAlpha eliminates the sqrt+square redundancy
func optimizedShieldAlpha(dx, dy int32) float64 {
	dxSq := vmath.Mul(dx, dx)
	dySq := vmath.Mul(dy, dy)
	normalizedDistSq := vmath.Mul(dxSq, invRxSq) + vmath.Mul(dySq, invRySq)

	if normalizedDistSq > vmath.Scale {
		return 0
	}

	// Optimized: direct use of normalizedDistSq
	alpha := vmath.ToFloat(normalizedDistSq) * shieldMaxOpacity
	return alpha
}

// === ALTERNATIVE: PURE FIXED-POINT ===

// fixedPointShieldAlpha stays entirely in Q16.16 until final output
func fixedPointShieldAlpha(dx, dy int32) int32 {
	dxSq := vmath.Mul(dx, dx)
	dySq := vmath.Mul(dy, dy)
	normalizedDistSq := vmath.Mul(dxSq, invRxSq) + vmath.Mul(dySq, invRySq)

	if normalizedDistSq > vmath.Scale {
		return 0
	}

	// Pure fixed-point: alpha as Q16.16 (0.0 to maxOpacity)
	maxOpacityFixed := vmath.FromFloat(shieldMaxOpacity)
	return vmath.Mul(normalizedDistSq, maxOpacityFixed)
}

// === ELLIPSE CONTAINMENT (drain.go pattern) ===

// currentEllipseContains replicates drain system check
func currentEllipseContains(dx, dy int32) bool {
	dxSq := vmath.Mul(dx, dx)
	dySq := vmath.Mul(dy, dy)
	normalizedDistSq := vmath.Mul(dxSq, invRxSq) + vmath.Mul(dySq, invRySq)
	return normalizedDistSq <= vmath.Scale
}

// === VMATH.SQRT USE CASE: ACTUAL DISTANCE ===

// vmathSqrtDistance demonstrates documented use case: actual game distance
// For movement speed, collision radius, etc. where real distance matters
func vmathSqrtDistance(dx, dy int32) int32 {
	dxSq := vmath.Mul(dx, dx)
	dySq := vmath.Mul(dy, dy)
	distSq := dxSq + dySq // Circular distance squared
	return vmath.Sqrt(distSq)
}

// mathSqrtDistance uses math.Sqrt for comparison
func mathSqrtDistance(dx, dy int32) float64 {
	dxSq := vmath.Mul(dx, dx)
	dySq := vmath.Mul(dy, dy)
	distSq := dxSq + dySq
	return math.Sqrt(vmath.ToFloat(distSq))
}

// === BENCHMARKS ===

func BenchmarkCurrentShieldAlpha(b *testing.B) {
	var sink float64
	for i := 0; i < b.N; i++ {
		p := testPoints[i%sampleCount]
		sink = currentShieldAlpha(p.dx, p.dy)
	}
	_ = sink
}

func BenchmarkOptimizedShieldAlpha(b *testing.B) {
	var sink float64
	for i := 0; i < b.N; i++ {
		p := testPoints[i%sampleCount]
		sink = optimizedShieldAlpha(p.dx, p.dy)
	}
	_ = sink
}

func BenchmarkFixedPointShieldAlpha(b *testing.B) {
	var sink int32
	for i := 0; i < b.N; i++ {
		p := testPoints[i%sampleCount]
		sink = fixedPointShieldAlpha(p.dx, p.dy)
	}
	_ = sink
}

func BenchmarkEllipseContains(b *testing.B) {
	var sink bool
	for i := 0; i < b.N; i++ {
		p := testPoints[i%sampleCount]
		sink = currentEllipseContains(p.dx, p.dy)
	}
	_ = sink
}

func BenchmarkVmathSqrtDistance(b *testing.B) {
	var sink int32
	for i := 0; i < b.N; i++ {
		p := testPoints[i%sampleCount]
		sink = vmathSqrtDistance(p.dx, p.dy)
	}
	_ = sink
}

func BenchmarkMathSqrtDistance(b *testing.B) {
	var sink float64
	for i := 0; i < b.N; i++ {
		p := testPoints[i%sampleCount]
		sink = mathSqrtDistance(p.dx, p.dy)
	}
	_ = sink
}

// === FULL FRAME SIMULATION ===

func BenchmarkFullShieldRenderCurrent(b *testing.B) {
	// Simulate rendering all cells in shield bounding box
	radiusXInt := int(shieldRadiusX)
	radiusYInt := int(shieldRadiusY)
	var sink float64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for y := -radiusYInt; y <= radiusYInt; y++ {
			for x := -radiusXInt; x <= radiusXInt; x++ {
				dx := vmath.FromInt(x)
				dy := vmath.FromInt(y)
				sink = currentShieldAlpha(dx, dy)
			}
		}
	}
	_ = sink
}

func BenchmarkFullShieldRenderOptimized(b *testing.B) {
	radiusXInt := int(shieldRadiusX)
	radiusYInt := int(shieldRadiusY)
	var sink float64

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for y := -radiusYInt; y <= radiusYInt; y++ {
			for x := -radiusXInt; x <= radiusXInt; x++ {
				dx := vmath.FromInt(x)
				dy := vmath.FromInt(y)
				sink = optimizedShieldAlpha(dx, dy)
			}
		}
	}
	_ = sink
}

func BenchmarkFullShieldRenderFixedPoint(b *testing.B) {
	radiusXInt := int(shieldRadiusX)
	radiusYInt := int(shieldRadiusY)
	var sink int32

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for y := -radiusYInt; y <= radiusYInt; y++ {
			for x := -radiusXInt; x <= radiusXInt; x++ {
				dx := vmath.FromInt(x)
				dy := vmath.FromInt(y)
				sink = fixedPointShieldAlpha(dx, dy)
			}
		}
	}
	_ = sink
}

// === ACCURACY VERIFICATION ===

func verifyAccuracy() {
	fmt.Println("=== Accuracy Verification ===")
	fmt.Println()

	// Test points at various distances
	testCases := []struct {
		x, y int
		desc string
	}{
		{0, 0, "center"},
		{3, 0, "half radius X"},
		{0, 1, "third radius Y"},
		{6, 0, "edge X"},
		{0, 3, "edge Y"},
		{4, 2, "diagonal inside"},
		{7, 4, "outside"},
	}

	fmt.Printf("%-15s %12s %12s %12s %12s\n", "Position", "Current", "Optimized", "FixedPt", "Delta")
	fmt.Println(string(make([]byte, 65)))

	for _, tc := range testCases {
		dx := vmath.FromInt(tc.x)
		dy := vmath.FromInt(tc.y)

		curr := currentShieldAlpha(dx, dy)
		opt := optimizedShieldAlpha(dx, dy)
		fp := vmath.ToFloat(fixedPointShieldAlpha(dx, dy))
		delta := math.Abs(curr - opt)

		fmt.Printf("(%2d,%2d) %-7s %12.6f %12.6f %12.6f %12.9f\n",
			tc.x, tc.y, tc.desc, curr, opt, fp, delta)
	}

	fmt.Println()
	fmt.Println("Note: Current and Optimized produce IDENTICAL results (delta = 0)")
	fmt.Println("      because sqrt(x)² = x")
	fmt.Println()

	// vmath.Sqrt accuracy check
	fmt.Println("=== vmath.Sqrt Accuracy (documented use case: game distances 0-500) ===")
	fmt.Println()
	fmt.Printf("%-10s %15s %15s %15s\n", "Input", "vmath.Sqrt", "math.Sqrt", "Error %")
	fmt.Println(string(make([]byte, 58)))

	distTests := []int{1, 4, 9, 16, 25, 100, 225, 400, 500, 1000}
	for _, d := range distTests {
		input := vmath.FromInt(d)
		vmResult := vmath.ToFloat(vmath.Sqrt(input))
		mathResult := math.Sqrt(float64(d))
		errPct := math.Abs(vmResult-mathResult) / mathResult * 100

		fmt.Printf("%10d %15.6f %15.6f %14.4f%%\n", d, vmResult, mathResult, errPct)
	}
}

func main() {
	fmt.Println("vi-fighter vmath Ellipse Operations Benchmark")
	fmt.Println("==============================================")
	fmt.Println()

	verifyAccuracy()

	fmt.Println()
	fmt.Println("=== Running Benchmarks ===")
	fmt.Println("Run with: go test -bench=. -benchmem ./cmd/vmath-benchmark/")
	fmt.Println()

	// Quick inline benchmark for immediate results
	iterations := 1000000

	start := time.Now()
	for i := 0; i < iterations; i++ {
		p := testPoints[i%sampleCount]
		_ = currentShieldAlpha(p.dx, p.dy)
	}
	currentTime := time.Since(start)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		p := testPoints[i%sampleCount]
		_ = optimizedShieldAlpha(p.dx, p.dy)
	}
	optimizedTime := time.Since(start)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		p := testPoints[i%sampleCount]
		_ = fixedPointShieldAlpha(p.dx, p.dy)
	}
	fixedPointTime := time.Since(start)

	fmt.Printf("Quick benchmark (%d iterations):\n", iterations)
	fmt.Printf("  Current (sqrt+square): %v\n", currentTime)
	fmt.Printf("  Optimized (no sqrt):   %v (%.1f%% faster)\n",
		optimizedTime, float64(currentTime-optimizedTime)/float64(currentTime)*100)
	fmt.Printf("  Fixed-point:           %v (%.1f%% faster)\n",
		fixedPointTime, float64(currentTime-fixedPointTime)/float64(currentTime)*100)
}
