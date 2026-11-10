package vmath

import (
	"math"
	"slices"
)

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
	return AppendEllipseGrid(make([][2]int, 0, count), centerX, centerY, radiusX, radiusY, count)
}

// AppendEllipseGrid is SampleEllipseGrid after what dst holds, in its storage: for
// a caller sampling every tick. A count dividing the sine table samples the
// table's own angles, so it reads their sines there.
func AppendEllipseGrid(dst [][2]int, centerX, centerY int, radiusX, radiusY float64, count int) [][2]int {
	if count <= 0 {
		return dst
	}
	cx, cy := (Point{X: centerX, Y: centerY}).Center()
	n := len(dst)
	dst = slices.Grow(dst, count)[:n+count]
	cells := dst[n:]
	stride, angleStep, table := LUTSize/count, TwoPi/float64(count), LUTSize%count == 0
	for i := range cells {
		var cos, sin float64
		if table {
			cos, sin = CosLUT[i*stride], SinLUT[i*stride]
		} else {
			cos, sin = math.Cos(float64(i)*angleStep), math.Sin(float64(i)*angleStep)
		}
		cells[i][0], cells[i][1] = int(math.Floor(cx+cos*radiusX)), int(math.Floor(cy+sin*radiusY))
	}
	return dst
}

// FindUnblockedArcs converts blocked bitmap to contiguous unblocked segments
func FindUnblockedArcs(blocked []bool) []ArcSegment { return AppendUnblockedArcs(nil, blocked) }

// AppendUnblockedArcs is FindUnblockedArcs into dst's storage
func AppendUnblockedArcs(dst []ArcSegment, blocked []bool) []ArcSegment {
	n := len(blocked)
	firstBlocked, free := -1, 0
	for i, b := range blocked {
		if !b {
			free++
		} else if firstBlocked < 0 {
			firstBlocked = i
		}
	}
	if free == 0 {
		return dst
	}
	if firstBlocked < 0 {
		return append(dst, ArcSegment{StartAngle: 0, EndAngle: TwoPi, Length: TwoPi})
	}

	// From the first blocked sample round, so no segment wraps the walk's end
	angleStep := TwoPi / float64(n)
	inSegment := false
	segStart := 0
	for offset := range n {
		i := firstBlocked + offset
		if i >= n {
			i -= n
		}
		if !blocked[i] && !inSegment {
			segStart = i
			inSegment = true
		} else if blocked[i] && inSegment {
			dst = append(dst, buildSegment(segStart, i, angleStep))
			inSegment = false
		}
	}
	if inSegment {
		dst = append(dst, buildSegment(segStart, firstBlocked, angleStep))
	}
	return dst
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
	return AppendDistributedAngles(make([]float64, 0, count), segments, count)
}

// AppendDistributedAngles is DistributeAngles into dst's storage, appending nothing
// for no segments
func AppendDistributedAngles(dst []float64, segments []ArcSegment, count int) []float64 {
	if len(segments) == 0 {
		return dst
	}
	totalArc := TotalArcLength(segments)
	spacing := totalArc / float64(count)
	startOffset := spacing / 2.0
	for i := range count {
		angle := 0.0
		if totalArc > 0 {
			angle = arcPositionToAngle(segments, math.Mod(startOffset+float64(i)*spacing, totalArc))
		}
		dst = append(dst, angle)
	}
	return dst
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
