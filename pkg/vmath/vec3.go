package vmath

import (
	"math"
)

// Vec3 is a float64 3D vector for physics-heavy calculations.
type Vec3 struct {
	X, Y, Z float64
}

func V3Add(a, b Vec3) Vec3 {
	return Vec3{a.X + b.X, a.Y + b.Y, a.Z + b.Z}
}

func V3Sub(a, b Vec3) Vec3 {
	return Vec3{a.X - b.X, a.Y - b.Y, a.Z - b.Z}
}

func V3Scale(v Vec3, s float64) Vec3 {
	return Vec3{v.X * s, v.Y * s, v.Z * s}
}

func V3MagSq(v Vec3) float64 {
	return v.X*v.X + v.Y*v.Y + v.Z*v.Z
}

func V3Mag(v Vec3) float64 {
	return math.Sqrt(V3MagSq(v))
}

func V3Normalize(v Vec3) Vec3 {
	mag := V3Mag(v)
	if mag == 0 {
		return Vec3{}
	}
	inv := 1.0 / mag
	return Vec3{v.X * inv, v.Y * inv, v.Z * inv}
}

// V3Dot returns the dot product
func V3Dot(a, b Vec3) float64 {
	return a.X*b.X + a.Y*b.Y + a.Z*b.Z
}

// V3Cross returns the cross product: perpendicular to both by the right hand, its
// length the area they span, so x cross y is z
func V3Cross(a, b Vec3) Vec3 {
	return Vec3{a.Y*b.Z - a.Z*b.Y, a.Z*b.X - a.X*b.Z, a.X*b.Y - a.Y*b.X}
}

// V3Lerp performs linear interpolation between a and b, t in [0.0, 1.0]
func V3Lerp(a, b Vec3, t float64) Vec3 {
	return Vec3{a.X + (b.X-a.X)*t, a.Y + (b.Y-a.Y)*t, a.Z + (b.Z-a.Z)*t}
}

// V3Reflect returns velocity reflected off a surface with the given unit normal
func V3Reflect(v, normal Vec3) Vec3 {
	d := 2.0 * V3Dot(v, normal)
	return Vec3{v.X - d*normal.X, v.Y - d*normal.Y, v.Z - d*normal.Z}
}

// V3Rotate rotates v about a unit axis by angle in radians, by the right hand:
// about z it turns the plane as RotateVector does
func V3Rotate(v, axis Vec3, angleRad float64) Vec3 {
	cos, sin := Cos(angleRad), Sin(angleRad)
	c := V3Cross(axis, v)
	k := V3Dot(axis, v) * (1.0 - cos)
	return Vec3{v.X*cos + c.X*sin + axis.X*k, v.Y*cos + c.Y*sin + axis.Y*k, v.Z*cos + c.Z*sin + axis.Z*k}
}

// V3XY extracts X,Y components for 2D projection
func V3XY(v Vec3) (x, y float64) {
	return v.X, v.Y
}

// V3From2D creates Vec3 from separate x,y with specified z
func V3From2D(x, y, z float64) Vec3 {
	return Vec3{X: x, Y: y, Z: z}
}

// V3ClampMagnitude limits vector magnitude
func V3ClampMagnitude(v Vec3, maxMag float64) Vec3 {
	if V3MagSq(v) <= maxMag*maxMag {
		return v
	}
	return V3Scale(V3Normalize(v), maxMag)
}

// V3Damp reduces vector magnitude by factor (1.0 = no damp, 0 = full damp)
func V3Damp(v Vec3, factor float64) Vec3 {
	return Vec3{v.X * factor, v.Y * factor, v.Z * factor}
}

// V3DampDt applies frame-rate independent damping: v * factor^dt
// Linear approximation v * (1 - (1-factor)*dt), valid for dt << 1 second
func V3DampDt(v Vec3, factor, dt float64) Vec3 {
	decay := Clamp(1.0-(1.0-factor)*dt, 0.0, 1.0)
	return Vec3{v.X * decay, v.Y * decay, v.Z * decay}
}
