package vmath

import "math"

const TwoPi = 2.0 * math.Pi

// ArcSegmentF represents a contiguous unblocked arc on an ellipse in radians
type ArcSegmentF struct {
	StartAngle float64
	EndAngle   float64
	Length     float64
}

// SampleEllipseGridF returns grid coordinates for N points along an ellipse
func SampleEllipseGridF(centerX, centerY int, radiusX, radiusY float64, count int) [][2]int {
	points := make([][2]int, count)
	angleStep := TwoPi / float64(count)
	cx, cy := CenteredFromGridF(centerX, centerY)

	for i := range count {
		angle := float64(i) * angleStep
		px := cx + math.Cos(angle)*radiusX
		py := cy + math.Sin(angle)*radiusY
		points[i] = [2]int{int(math.Floor(px)), int(math.Floor(py))}
	}
	return points
}

// FindUnblockedArcsF converts blocked bitmap to contiguous unblocked segments
func FindUnblockedArcsF(blocked []bool) []ArcSegmentF {
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
		return []ArcSegmentF{{StartAngle: 0, EndAngle: TwoPi, Length: TwoPi}}
	}

	firstBlocked := 0
	for i, b := range blocked {
		if b {
			firstBlocked = i
			break
		}
	}

	var segments []ArcSegmentF
	inSegment := false
	segStart := 0

	for offset := range n {
		i := (firstBlocked + offset) % n

		if !blocked[i] && !inSegment {
			segStart = i
			inSegment = true
		} else if blocked[i] && inSegment {
			segments = append(segments, buildSegmentF(segStart, i, angleStep))
			inSegment = false
		}
	}

	if inSegment {
		segments = append(segments, buildSegmentF(segStart, firstBlocked, angleStep))
	}

	return segments
}

func buildSegmentF(start, end int, angleStep float64) ArcSegmentF {
	startAngle := float64(start) * angleStep
	endAngle := float64(end) * angleStep
	length := endAngle - startAngle
	if length <= 0 {
		length += TwoPi
	}
	return ArcSegmentF{StartAngle: startAngle, EndAngle: endAngle, Length: length}
}

// TotalArcLengthF returns sum of all segment lengths in radians
func TotalArcLengthF(segments []ArcSegmentF) float64 {
	var total float64
	for _, s := range segments {
		total += s.Length
	}
	return total
}

// IsFullCircleF returns true if segments cover entire orbit
func IsFullCircleF(segments []ArcSegmentF) bool {
	// Tolerance for float inaccuracies
	return len(segments) == 1 && segments[0].Length >= TwoPi-0.0001
}

// NormalizeAngleF wraps angle to [0, 2π)
func NormalizeAngleF(angle float64) float64 {
	angle = math.Mod(angle, TwoPi)
	if angle < 0 {
		angle += TwoPi
	}
	return angle
}

// AngleDiffF returns shortest signed difference between angles in [-π, π]
func AngleDiffF(from, to float64) float64 {
	diff := NormalizeAngleF(to) - NormalizeAngleF(from)
	if diff > math.Pi {
		diff -= TwoPi
	} else if diff < -math.Pi {
		diff += TwoPi
	}
	return diff
}

// DistributeAnglesF calculates N evenly-spaced angles within arc segments [0, 2π)
func DistributeAnglesF(segments []ArcSegmentF, count int) []float64 {
	if count <= 0 || len(segments) == 0 {
		return nil
	}

	totalArc := TotalArcLengthF(segments)
	if totalArc <= 0 {
		return make([]float64, count)
	}

	spacing := totalArc / float64(count)
	startOffset := spacing / 2.0

	angles := make([]float64, count)
	for i := range count {
		arcPos := math.Mod(startOffset+float64(i)*spacing, totalArc)
		angles[i] = arcPositionToAngleF(segments, arcPos)
	}
	return angles
}

func arcPositionToAngleF(segments []ArcSegmentF, pos float64) float64 {
	var cumulative float64
	for _, seg := range segments {
		if pos <= cumulative+seg.Length {
			offset := pos - cumulative
			return NormalizeAngleF(seg.StartAngle + offset)
		}
		cumulative += seg.Length
	}
	if len(segments) > 0 {
		return segments[0].StartAngle
	}
	return 0.0
}

// AngleToEllipsePosF converts angle to precise position on ellipse in float64
func AngleToEllipsePosF(angle, centerX, centerY, radiusX, radiusY float64) (float64, float64) {
	return centerX + math.Cos(angle)*radiusX, centerY + math.Sin(angle)*radiusY
}

// AngleToGridPosF converts angle to grid coordinates on ellipse
func AngleToGridPosF(angle float64, centerX, centerY int, radiusX, radiusY float64) (int, int) {
	cx, cy := CenteredFromGridF(centerX, centerY)
	px, py := AngleToEllipsePosF(angle, cx, cy, radiusX, radiusY)
	return int(math.Floor(px)), int(math.Floor(py))
}
