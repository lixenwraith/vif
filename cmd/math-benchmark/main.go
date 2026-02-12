package main

import (
	"fmt"
	"math"
	"time"

	"github.com/lixenwraith/vi-fighter/physics"
	"github.com/lixenwraith/vi-fighter/vmath"
)

const iterations = 10_000_000

func main() {
	fmt.Printf("vmath/physics Benchmark (%d iterations)\n", iterations)
	fmt.Println("══════════════════════════════════════════════════════════════")
	fmt.Printf("%-28s %14s %14s %10s\n", "Operation", "Q32.32", "float64", "Ratio")
	fmt.Println("──────────────────────────────────────────────────────────────")

	benchMagnitude()
	benchMagnitudeApprox()
	benchClampMagnitude()
	benchNormalize2D()
	benchV3Normalize()
	benchMulDiv()
	benchOrbitalDamp()
	benchCapSpeed()
	benchElasticCollision3D()

	fmt.Println("══════════════════════════════════════════════════════════════")
}

func benchMagnitude() {
	dx, dy := vmath.FromFloat(123.456), vmath.FromFloat(78.9)
	dxF, dyF := 123.456, 78.9

	start := time.Now()
	var rQ int64
	for i := 0; i < iterations; i++ {
		rQ = vmath.Magnitude(dx, dy)
	}
	q32Time := time.Since(start)

	start = time.Now()
	var rF float64
	for i := 0; i < iterations; i++ {
		rF = math.Sqrt(dxF*dxF + dyF*dyF)
	}
	floatTime := time.Since(start)

	printResult("Magnitude", q32Time, floatTime)
	_, _ = rQ, rF
}

func benchMagnitudeApprox() {
	dx, dy := vmath.FromFloat(123.456), vmath.FromFloat(78.9)
	dxF, dyF := 123.456, 78.9

	start := time.Now()
	var rQ int64
	for i := 0; i < iterations; i++ {
		rQ = vmath.MagnitudeApprox(dx, dy)
	}
	q32Time := time.Since(start)

	start = time.Now()
	var rF float64
	for i := 0; i < iterations; i++ {
		rF = math.Sqrt(dxF*dxF + dyF*dyF)
	}
	floatTime := time.Since(start)

	printResult("MagnitudeApprox", q32Time, floatTime)
	_, _ = rQ, rF
}

func benchClampMagnitude() {
	x, y := vmath.FromFloat(50.0), vmath.FromFloat(50.0)
	maxMag := vmath.FromFloat(30.0)
	xF, yF, maxMagF := 50.0, 50.0, 30.0

	start := time.Now()
	var rxQ, ryQ int64
	for i := 0; i < iterations; i++ {
		rxQ, ryQ = vmath.ClampMagnitude(x, y, maxMag)
	}
	q32Time := time.Since(start)

	start = time.Now()
	var rxF, ryF float64
	for i := 0; i < iterations; i++ {
		mag := math.Sqrt(xF*xF + yF*yF)
		if mag > maxMagF {
			scale := maxMagF / mag
			rxF, ryF = xF*scale, yF*scale
		} else {
			rxF, ryF = xF, yF
		}
	}
	floatTime := time.Since(start)

	printResult("ClampMagnitude", q32Time, floatTime)
	_, _, _, _ = rxQ, ryQ, rxF, ryF
}

func benchNormalize2D() {
	x, y := vmath.FromFloat(123.456), vmath.FromFloat(78.9)
	xF, yF := 123.456, 78.9

	start := time.Now()
	var nxQ, nyQ int64
	for i := 0; i < iterations; i++ {
		nxQ, nyQ = vmath.Normalize2D(x, y)
	}
	q32Time := time.Since(start)

	start = time.Now()
	var nxF, nyF float64
	for i := 0; i < iterations; i++ {
		inv := 1.0 / math.Sqrt(xF*xF+yF*yF)
		nxF, nyF = xF*inv, yF*inv
	}
	floatTime := time.Since(start)

	printResult("Normalize2D", q32Time, floatTime)
	_, _, _, _ = nxQ, nyQ, nxF, nyF
}

func benchV3Normalize() {
	v := vmath.Vec3{X: vmath.FromFloat(10), Y: vmath.FromFloat(20), Z: vmath.FromFloat(30)}
	xF, yF, zF := 10.0, 20.0, 30.0

	start := time.Now()
	var rQ vmath.Vec3
	for i := 0; i < iterations; i++ {
		rQ = vmath.V3Normalize(v)
	}
	q32Time := time.Since(start)

	start = time.Now()
	var rxF, ryF, rzF float64
	for i := 0; i < iterations; i++ {
		inv := 1.0 / math.Sqrt(xF*xF+yF*yF+zF*zF)
		rxF, ryF, rzF = xF*inv, yF*inv, zF*inv
	}
	floatTime := time.Since(start)

	printResult("V3Normalize", q32Time, floatTime)
	_, _, _, _ = rQ, rxF, ryF, rzF
}

func benchMulDiv() {
	a := vmath.FromFloat(123.456)
	b := vmath.FromFloat(78.9)
	c := vmath.FromFloat(45.67)
	aF, bF, cF := 123.456, 78.9, 45.67

	start := time.Now()
	var rQ int64
	for i := 0; i < iterations; i++ {
		rQ = vmath.MulDiv(a, b, c)
	}
	q32Time := time.Since(start)

	start = time.Now()
	var rF float64
	for i := 0; i < iterations; i++ {
		rF = (aF * bF) / cF
	}
	floatTime := time.Since(start)

	printResult("MulDiv", q32Time, floatTime)
	_, _ = rQ, rF
}

func benchOrbitalDamp() {
	vx, vy := vmath.FromFloat(10.0), vmath.FromFloat(5.0)
	dx, dy := vmath.FromFloat(20.0), vmath.FromFloat(15.0)
	damping := vmath.FromFloat(0.5)
	dt := vmath.FromFloat(0.016)

	vxF, vyF := 10.0, 5.0
	dxF, dyF := 20.0, 15.0
	dampingF := 0.5
	dtF := 0.016

	start := time.Now()
	var nvxQ, nvyQ int64
	for i := 0; i < iterations; i++ {
		nvxQ, nvyQ = physics.OrbitalDamp(vx, vy, dx, dy, damping, dt)
	}
	q32Time := time.Since(start)

	start = time.Now()
	var nvxF, nvyF float64
	for i := 0; i < iterations; i++ {
		// Mutate input slightly to prevent compiler deleting the loop
		dxF += 0.00001

		dist := math.Sqrt(dxF*dxF + dyF*dyF)
		rx, ry := dxF/dist, dyF/dist
		radialSpeed := vxF*rx + vyF*ry
		dampFactor := 1.0 - dampingF*dtF
		newRadial := radialSpeed * dampFactor
		delta := newRadial - radialSpeed
		nvxF, nvyF = vxF+delta*rx, vyF+delta*ry

		// Prevent dxF from growing to infinity if needed, or just let it float
		if dxF > 1000.0 {
			dxF = 20.0
		}
	}
	floatTime := time.Since(start)

	printResult("OrbitalDamp", q32Time, floatTime)
	_, _, _, _ = nvxQ, nvyQ, nvxF, nvyF
}

func benchCapSpeed() {
	maxSpeed := vmath.FromFloat(50.0)
	maxSpeedF := 50.0

	start := time.Now()
	for i := 0; i < iterations; i++ {
		vx, vy := vmath.FromFloat(100.0), vmath.FromFloat(100.0)
		vx, vy = physics.CapSpeed(vx, vy, maxSpeed)
	}
	q32Time := time.Since(start)

	start = time.Now()
	for i := 0; i < iterations; i++ {
		vxF, vyF := 100.0, 100.0
		magSq := vxF*vxF + vyF*vyF
		maxSq := maxSpeedF * maxSpeedF
		if magSq > maxSq {
			scale := maxSpeedF / math.Sqrt(magSq)
			vxF *= scale
			vyF *= scale
		}
		_, _ = vxF, vyF
	}
	floatTime := time.Since(start)

	printResult("CapSpeed", q32Time, floatTime)
}

func benchElasticCollision3D() {
	posABase := vmath.Vec3{X: 0, Y: 0, Z: 0}
	posBBase := vmath.Vec3{X: vmath.FromFloat(2), Y: 0, Z: 0}
	velABase := vmath.Vec3{X: vmath.FromFloat(5), Y: 0, Z: 0}
	velBBase := vmath.Vec3{X: vmath.FromFloat(-3), Y: 0, Z: 0}
	massA, massB := vmath.Scale, vmath.Scale*2
	restitution := vmath.FromFloat(0.8)

	posAxF, posAyF, posAzF := 0.0, 0.0, 0.0
	posBxF, posByF, posBzF := 2.0, 0.0, 0.0
	velAxF, velAyF, velAzF := 5.0, 0.0, 0.0
	velBxF, velByF, velBzF := -3.0, 0.0, 0.0
	massAF, massBF := 1.0, 2.0
	restitutionF := 0.8

	iters := iterations / 10

	start := time.Now()
	var collidedQ bool
	for i := 0; i < iters; i++ {
		// Copy base values each iteration (simulates real usage)
		posA, posB := posABase, posBBase
		velA, velB := velABase, velBBase
		posB.X += int64(i & 0xFF) // Vary to prevent optimization
		collidedQ = physics.ElasticCollision3DInPlace(
			&posA, &posB,
			&velA, &velB,
			massA, massB, restitution,
		)
	}
	q32Time := time.Since(start)

	start = time.Now()
	var collidedF bool
	for i := 0; i < iters; i++ {
		posBxVar := posBxF + float64(i&0xFF)*0.001

		deltaX := posBxVar - posAxF
		deltaY := posByF - posAyF
		deltaZ := posBzF - posAzF
		dist := math.Sqrt(deltaX*deltaX + deltaY*deltaY + deltaZ*deltaZ)
		if dist == 0 {
			continue
		}
		nx, ny, nz := deltaX/dist, deltaY/dist, deltaZ/dist

		relVx := velAxF - velBxF
		relVy := velAyF - velByF
		relVz := velAzF - velBzF
		vn := relVx*nx + relVy*ny + relVz*nz

		if vn <= 0 {
			collidedF = false
			continue
		}

		invA, invB := 1.0/massAF, 1.0/massBF
		j := (1.0 + restitutionF) * vn / (invA + invB)

		velAxF -= j * invA * nx
		velAyF -= j * invA * ny
		velAzF -= j * invA * nz
		velBxF += j * invB * nx
		velByF += j * invB * ny
		velBzF += j * invB * nz
		collidedF = true
	}
	floatTime := time.Since(start)

	q32Time = time.Duration(float64(q32Time) * 10)
	floatTime = time.Duration(float64(floatTime) * 10)

	printResult("ElasticCollision3D", q32Time, floatTime)
	_, _ = collidedQ, collidedF
}

func printResult(name string, q32Time, floatTime time.Duration) {
	ratio := float64(q32Time) / float64(floatTime)
	fmt.Printf("%-28s %14s %14s %9.2fx\n", name, q32Time, floatTime, ratio)
}