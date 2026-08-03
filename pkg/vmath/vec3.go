package vmath

import (
	"math"
)

// Vec3 is a 3D vector in Q32.32 fixed-point
// Used by storm system for 3D orbital physics
type Vec3 struct {
	X, Y, Z int64
}

func V3Add(a, b Vec3) Vec3 {
	return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z}
}

func V3Sub(a, b Vec3) Vec3 {
	return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z}
}

func V3Scale(v Vec3, s int64) Vec3 {
	return Vec3{Mul(v.X, s), Mul(v.Y, s), Mul(v.Z, s)}
}

func V3Dot(a, b Vec3) int64 {
	return Mul(a.X, b.X) + Mul(a.Y, b.Y) + Mul(a.Z, b.Z)
}

func V3MagSq(v Vec3) int64 {
	return Mul(v.X, v.X) + Mul(v.Y, v.Y) + Mul(v.Z, v.Z)
}

func V3Mag(v Vec3) int64 {
	return Sqrt(V3MagSq(v))
}

// V3Normalize normalizes a 3D vector
// Optimization: Calculates inverse magnitude once in float, multiplies 3 times
func V3Normalize(v Vec3) Vec3 {
	fx, fy, fz := float64(v.X), float64(v.Y), float64(v.Z)
	mag := math.Sqrt(fx*fx + fy*fy + fz*fz)

	if mag == 0 {
		return Vec3{}
	}

	// One division, three multiplies
	inv := ScaleF / mag
	return Vec3{
		int64(fx * inv),
		int64(fy * inv),
		int64(fz * inv),
	}
}

// V3XY extracts X,Y components as separate values for 2D projection
func V3XY(v Vec3) (x, y int64) {
	return v.X, v.Y
}

// V3From2D creates Vec3 from separate x,y with specified z
func V3From2D(x, y, z int64) Vec3 {
	return Vec3{X: x, Y: y, Z: z}
}

// V3ClampMagnitude limits vector magnitude
func V3ClampMagnitude(v Vec3, maxMag int64) Vec3 {
	magSq := V3MagSq(v)
	maxMagSq := Mul(maxMag, maxMag)
	if magSq <= maxMagSq {
		return v
	}
	return V3Scale(V3Normalize(v), maxMag)
}

// V3Damp reduces vector magnitude by factor (Scale = no damp, 0 = full damp)
func V3Damp(v Vec3, factor int64) Vec3 {
	return Vec3{Mul(v.X, factor), Mul(v.Y, factor), Mul(v.Z, factor)}
}

// V3DampDt applies frame-rate independent damping: v * factor^dt
// factor: decay rate per second (Q32.32, Scale = no decay)
// dt: delta time in Q32.32 seconds
// Uses linear approximation: v * (1 - (1-factor)*dt) for small dt
func V3DampDt(v Vec3, factor, dt int64) Vec3 {
	// Linear approximation valid for dt << 1 second
	decay := Scale - Mul(Scale-factor, dt)
	if decay < 0 {
		decay = 0
	}
	if decay > Scale {
		decay = Scale
	}
	return Vec3{Mul(v.X, decay), Mul(v.Y, decay), Mul(v.Z, decay)}
}