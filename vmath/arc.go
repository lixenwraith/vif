package vmath

// ArcSegment represents a contiguous unblocked arc on an ellipse
// Angles in Q32.32 where Scale = full rotation (2π)
type ArcSegment struct {
	StartAngle int64
	EndAngle   int64
	Length     int64
}

// EllipseSampleCount is points sampled for arc availability
const EllipseSampleCount = 64

// SampleEllipseGrid returns grid coordinates for N points along ellipse
// centerX, centerY: ellipse center (grid coords)
// radiusX, radiusY: semi-axes in Q32.32
func SampleEllipseGrid(centerX, centerY int, radiusX, radiusY int64, count int) [][2]int {
	points := make([][2]int, count)
	angleStep := Scale / int64(count)
	cx, cy := CenteredFromGrid(centerX, centerY)

	for i := 0; i < count; i++ {
		angle := int64(i) * angleStep
		px := cx + Mul(Cos(angle), radiusX)
		py := cy + Mul(Sin(angle), radiusY)
		points[i] = [2]int{ToInt(px), ToInt(py)}
	}
	return points
}

// FindUnblockedArcs converts blocked bitmap to contiguous unblocked segments
// Returns nil if fully blocked, single full-circle segment if fully free
func FindUnblockedArcs(blocked []bool) []ArcSegment {
	n := len(blocked)
	if n == 0 {
		return nil
	}

	angleStep := Scale / int64(n)

	// Quick check for uniform state
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
		return []ArcSegment{{StartAngle: 0, EndAngle: Scale, Length: Scale}}
	}

	// Find first blocked index as scan anchor
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

	for offset := 0; offset < n; offset++ {
		i := (firstBlocked + offset) % n

		if !blocked[i] && !inSegment {
			segStart = i
			inSegment = true
		} else if blocked[i] && inSegment {
			segments = append(segments, buildSegment(segStart, i, angleStep))
			inSegment = false
		}
	}

	// Handle wrap-around segment
	if inSegment {
		segments = append(segments, buildSegment(segStart, firstBlocked, angleStep))
	}

	return segments
}

func buildSegment(start, end int, angleStep int64) ArcSegment {
	startAngle := int64(start) * angleStep
	endAngle := int64(end) * angleStep
	length := endAngle - startAngle
	if length <= 0 {
		length += Scale
	}
	return ArcSegment{StartAngle: startAngle, EndAngle: endAngle, Length: length}
}

// TotalArcLength returns sum of all segment lengths
func TotalArcLength(segments []ArcSegment) int64 {
	total := int64(0)
	for _, s := range segments {
		total += s.Length
	}
	return total
}

// IsFullCircle returns true if segments cover entire orbit
func IsFullCircle(segments []ArcSegment) bool {
	return len(segments) == 1 && segments[0].Length == Scale
}

// DistributeAngles calculates N evenly-spaced angles within arc segments
// Returns angles in Q32.32; empty slice if no segments
func DistributeAngles(segments []ArcSegment, count int) []int64 {
	if count <= 0 || len(segments) == 0 {
		return nil
	}

	totalArc := TotalArcLength(segments)
	if totalArc == 0 {
		return make([]int64, count)
	}

	// Equal spacing with centering offset
	spacing := totalArc / int64(count)
	if spacing == 0 {
		spacing = 1
	}
	startOffset := spacing / 2

	angles := make([]int64, count)
	for i := 0; i < count; i++ {
		arcPos := (startOffset + int64(i)*spacing) % totalArc
		angles[i] = arcPositionToAngle(segments, arcPos)
	}
	return angles
}

// arcPositionToAngle converts linear position along combined arcs to angle
func arcPositionToAngle(segments []ArcSegment, pos int64) int64 {
	cumulative := int64(0)
	for _, seg := range segments {
		if pos < cumulative+seg.Length {
			offset := pos - cumulative
			angle := seg.StartAngle + offset
			if angle >= Scale {
				angle -= Scale
			}
			return angle
		}
		cumulative += seg.Length
	}
	// Fallback to first segment start
	if len(segments) > 0 {
		return segments[0].StartAngle
	}
	return 0
}

// AngleToEllipsePos converts angle to precise position on ellipse
// Returns (preciseX, preciseY) in Q32.32
func AngleToEllipsePos(angle, centerX, centerY, radiusX, radiusY int64) (int64, int64) {
	return centerX + Mul(Cos(angle), radiusX), centerY + Mul(Sin(angle), radiusY)
}

// AngleToGridPos converts angle to grid coordinates on ellipse
func AngleToGridPos(angle int64, centerX, centerY int, radiusX, radiusY int64) (int, int) {
	cx, cy := CenteredFromGrid(centerX, centerY)
	px, py := AngleToEllipsePos(angle, cx, cy, radiusX, radiusY)
	return ToInt(px), ToInt(py)
}

// NormalizeAngle wraps angle to [0, Scale)
func NormalizeAngle(angle int64) int64 {
	for angle >= Scale {
		angle -= Scale
	}
	for angle < 0 {
		angle += Scale
	}
	return angle
}

// AngleDiff returns shortest signed difference between angles
// Result in [-Scale/2, Scale/2]
func AngleDiff(from, to int64) int64 {
	diff := NormalizeAngle(to) - NormalizeAngle(from)
	if diff > Scale/2 {
		diff -= Scale
	} else if diff < -Scale/2 {
		diff += Scale
	}
	return diff
}