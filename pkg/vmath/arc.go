package vmath

import "math"

// ArcSegment represents a contiguous unblocked arc on an ellipse in radians
type ArcSegment struct {
	StartAngle float64
	EndAngle   float64
	Length     float64
}

// SampleEllipseGrid returns grid coordinates for N points along an ellipse
func SampleEllipseGrid(centerX, centerY int, radiusX, radiusY float64, count int) [][2]int {
	if count <= 0 {
		return nil
	}

	points := make([][2]int, count)
	angleStep := TwoPi / float64(count)
	cx, cy := (Point{X: centerX, Y: centerY}).Center()

	for i := range count {
		angle := float64(i) * angleStep
		px := cx + math.Cos(angle)*radiusX
		py := cy + math.Sin(angle)*radiusY
		p := PointAt(px, py)
		points[i] = [2]int{p.X, p.Y}
	}
	return points
}

// FindUnblockedArcs converts blocked bitmap to contiguous unblocked segments
func FindUnblockedArcs(blocked []bool) []ArcSegment {
	n := len(blocked)
	if n == 0 {
		return nil
	}

	angleStep := TwoPi / float64(n)

	allBlocked, allFree := true, true
	for _, b := range blocked {
		if b {
			allFree = false
		} else {
			allBlocked = false
		}
	}

	if allBlocked {
		return nil
	}
	if allFree {
		return []ArcSegment{{StartAngle: 0, EndAngle: TwoPi, Length: TwoPi}}
	}

	firstBlocked := 0
	for i, b := range blocked {
		if b {
			firstBlocked = i
			break
		}
	}

	var segments []ArcSegment
	inSegment := false
	segStart := 0

	for offset := range n {
		i := (firstBlocked + offset) % n

		if !blocked[i] && !inSegment {
			segStart = i
			inSegment = true
		} else if blocked[i] && inSegment {
			segments = append(segments, buildSegment(segStart, i, angleStep))
			inSegment = false
		}
	}

	if inSegment {
		segments = append(segments, buildSegment(segStart, firstBlocked, angleStep))
	}

	return segments
}

func buildSegment(start, end int, angleStep float64) ArcSegment {
	startAngle := float64(start) * angleStep
	endAngle := float64(end) * angleStep
	length := endAngle - startAngle
	if length <= 0 {
		length += TwoPi
	}
	return ArcSegment{StartAngle: startAngle, EndAngle: endAngle, Length: length}
}

// TotalArcLength returns sum of all segment lengths in radians
func TotalArcLength(segments []ArcSegment) float64 {
	var total float64
	for _, s := range segments {
		total += s.Length
	}
	return total
}

// IsFullCircle returns true if segments cover entire orbit
func IsFullCircle(segments []ArcSegment) bool {
	// Tolerance for float inaccuracies
	return len(segments) == 1 && segments[0].Length >= TwoPi-0.0001
}

// NormalizeAngle wraps angle to [0, 2π)
func NormalizeAngle(angle float64) float64 {
	angle = math.Mod(angle, TwoPi)
	if angle < 0 {
		angle += TwoPi
	}
	return angle
}

// AngleDiff returns shortest signed difference between angles in [-π, π]
func AngleDiff(from, to float64) float64 {
	diff := NormalizeAngle(to) - NormalizeAngle(from)
	if diff > math.Pi {
		diff -= TwoPi
	} else if diff < -math.Pi {
		diff += TwoPi
	}
	return diff
}

// DistributeAngles calculates N evenly-spaced angles within arc segments [0, 2π)
func DistributeAngles(segments []ArcSegment, count int) []float64 {
	if count <= 0 || len(segments) == 0 {
		return nil
	}

	totalArc := TotalArcLength(segments)
	if totalArc <= 0 {
		return make([]float64, count)
	}

	spacing := totalArc / float64(count)
	startOffset := spacing / 2.0

	angles := make([]float64, count)
	for i := range count {
		arcPos := math.Mod(startOffset+float64(i)*spacing, totalArc)
		angles[i] = arcPositionToAngle(segments, arcPos)
	}
	return angles
}

func arcPositionToAngle(segments []ArcSegment, pos float64) float64 {
	var cumulative float64
	for _, seg := range segments {
		if pos <= cumulative+seg.Length {
			offset := pos - cumulative
			return NormalizeAngle(seg.StartAngle + offset)
		}
		cumulative += seg.Length
	}
	if len(segments) > 0 {
		return segments[0].StartAngle
	}
	return 0.0
}

// AngleToEllipsePos converts angle to precise position on ellipse in float64
func AngleToEllipsePos(angle, centerX, centerY, radiusX, radiusY float64) (float64, float64) {
	return centerX + math.Cos(angle)*radiusX, centerY + math.Sin(angle)*radiusY
}

// AngleToGridPos converts angle to grid coordinates on ellipse
func AngleToGridPos(angle float64, centerX, centerY int, radiusX, radiusY float64) (int, int) {
	cx, cy := (Point{X: centerX, Y: centerY}).Center()
	px, py := AngleToEllipsePos(angle, cx, cy, radiusX, radiusY)
	p := PointAt(px, py)
	return p.X, p.Y
}
