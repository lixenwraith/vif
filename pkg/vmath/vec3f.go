package vmath

import (
	"math"
)

// Vec3F is a float64 3D vector for physics-heavy calculations.
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

// V3FDot returns the dot product
func V3FDot(a, b Vec3F) float64 {
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z
}

// V3FXY extracts X,Y components for 2D projection
func V3FXY(v Vec3F) (x, y float64) {
	return v.X, v.Y
}

// V3FFrom2D creates Vec3F from separate x,y with specified z
func V3FFrom2D(x, y, z float64) Vec3F {
	return Vec3F{X: x, Y: y, Z: z}
}

// V3FClampMagnitude limits vector magnitude
func V3FClampMagnitude(v Vec3F, maxMag float64) Vec3F {
	if V3FMagSq(v) <= maxMag*maxMag {
		return v
	}
	return V3FScale(V3FNormalize(v), maxMag)
}

// V3FDamp reduces vector magnitude by factor (1.0 = no damp, 0 = full damp)
func V3FDamp(v Vec3F, factor float64) Vec3F {
	return Vec3F{v.X * factor, v.Y * factor, v.Z * factor}
}

// V3FDampDt applies frame-rate independent damping: v * factor^dt
// Linear approximation v * (1 - (1-factor)*dt), valid for dt << 1 second
func V3FDampDt(v Vec3F, factor, dt float64) Vec3F {
	decay := ClampF(1.0-(1.0-factor)*dt, 0.0, 1.0)
	return Vec3F{v.X * decay, v.Y * decay, v.Z * decay}
}
