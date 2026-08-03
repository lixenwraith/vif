package vmath

import (
	"math"
)

// Vec3F is a float64 3D vector for physics-heavy calculations
// Avoids int64↔float64 conversion overhead in hot paths
type Vec3F struct {
	X, Y, Z float64
}

func V3FAdd(a, b Vec3F) Vec3F {
	return Vec3F{a.X + b.X, a.Y + b.Y, a.Z + b.Z}
}

func V3FSub(a, b Vec3F) Vec3F {
	return Vec3F{a.X - b.X, a.Y - b.Y, a.Z - b.Z}
}

func V3FScale(v Vec3F, s float64) Vec3F {
	return Vec3F{v.X * s, v.Y * s, v.Z * s}
}

func V3FMagSq(v Vec3F) float64 {
	return v.X*v.X + v.Y*v.Y + v.Z*v.Z
}

func V3FMag(v Vec3F) float64 {
	return math.Sqrt(V3FMagSq(v))
}

func V3FNormalize(v Vec3F) Vec3F {
	mag := V3FMag(v)
	if mag == 0 {
		return Vec3F{}
	}
	inv := 1.0 / mag
	return Vec3F{v.X * inv, v.Y * inv, v.Z * inv}
}

// V3FToQ32 converts to Q32.32 Vec3
func V3FToQ32(v Vec3F) Vec3 {
	return Vec3{
		X: FromFloat(v.X),
		Y: FromFloat(v.Y),
		Z: FromFloat(v.Z),
	}
}

// V3ToFloat converts Q32.32 Vec3 to Vec3F
func V3ToFloat(v Vec3) Vec3F {
	return Vec3F{
		X: ToFloat(v.X),
		Y: ToFloat(v.Y),
		Z: ToFloat(v.Z),
	}
}