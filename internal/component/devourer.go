package component

import (
	"math"
	"slices"
	"time"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// DevourerComponent is a Devourer's state: its walls stand where its frame puts
// them, so its hitbox and its shading change together
type DevourerComponent struct {
	MaxHitPoints int           // the hit points it rose with, its health bar's full length
	Elapsed      time.Duration // since it rose
	Seed         uint64        // the field its walls swell by, its own
	NextOmen     time.Duration // until the next omen, counted while a gut's slot stands empty
	Omen         DevourerOmen
	Guts         [parameter.DevourerGutCap]core.Entity // each standing gut, zero for an empty slot
	Felt         [parameter.DevourerGutCap]int         // each gut's hit points as the body last felt them
}

// DevourerOmen is the ray lit where a gut forms once Remaining runs out, between the
// walls at From and To, angles about the map's center; zero Remaining is none
type DevourerOmen struct {
	Remaining time.Duration
	From, To  float64
}

// DevourerGutComponent is a gut spanning the open space between the walls at From
// and To. It formed at its Devourer's frame Formed, straight along its omen, and
// writhes from then on by its own Phase.
type DevourerGutComponent struct {
	Devourer core.Entity
	From, To float64
	Formed   int64
	Phase    float64
}

// DevourerRecede is a killed Devourer drawing back from how far it had Grown to the
// edges of a Width by Height map, Recede running from 0 as it dies to 1 as the last
// of it goes. Entity is the Devourer it was, whose return ends it.
type DevourerRecede struct {
	Entity        core.Entity
	Devourer      DevourerComponent
	Width, Height int
	Grown, Recede float64
}

// Frame is the frame its walls stand at
func (d *DevourerComponent) Frame() int64 { return int64(d.Elapsed / parameter.DevourerFrame) }

// Grown is how far in from the edges it has crept at its frame, 0 to 1
func (d *DevourerComponent) Grown() float64 { return DevourerGrown(d.Frame()) }

// DevourerGrown is how far in a Devourer has crept at a frame of its own
func DevourerGrown(frame int64) float64 {
	return min(1, float64(frame)*float64(parameter.DevourerFrame)/float64(parameter.DevourerCreep))
}

// DevourerShape is a Devourer's body at one frame: the bezel its edges grow, the
// rounded rectangle that leaves open, and the field that moves its walls sampled
// every step. Readings are in column units, a row two of them, so the walls keep
// their shape at the cell's aspect; nothing reads an angle's seam.
type DevourerShape struct {
	key                   devourerKey
	cx, cy, hx, hy, round float64
	reach                 float64 // the field's greatest magnitude
	step                  float64
	cols                  int
	wave                  []float64
}

// devourerKey is what a shape is built from
type devourerKey struct {
	seed          uint64
	frame         int64
	grown         float64
	width, height int
}

// Build lays the shape at a frame, grown in from the edges by grown, on a width by
// height map; built already, it stands as it is
func (s *DevourerShape) Build(seed uint64, frame int64, grown float64, width, height int) {
	if s.Built(seed, frame, grown, width, height) {
		return
	}
	s.key = devourerKey{seed, frame, grown, width, height}
	wp, hp := float64(width), 2*float64(height)
	bezel := parameter.DevourerBezel * min(wp, hp)
	// Eased out from the edge band, so its first frames already creep in
	eased := math.Sin(grown * math.Pi / 2)
	thick := parameter.DevourerEdge + max(bezel-parameter.DevourerEdge, 0)*eased
	s.cx, s.cy = wp/2, hp/2
	s.hx, s.hy = max(s.cx-thick, 0), max(s.cy-thick, 0)
	s.round = min(parameter.DevourerRound*thick, s.hx, s.hy)

	// The field grows in with the bezel, so a thin band keeps to the edges
	size := bezel * parameter.DevourerSwellSize
	s.step = size / 4
	s.cols = int(wp/s.step) + 2
	rows := int(hp/s.step) + 2
	s.wave = slices.Grow(s.wave[:0], s.cols*rows)[:s.cols*rows]
	t := float64(frame) * parameter.DevourerFrame.Seconds()
	reach := bezel * min(1, eased*1.4)
	s.reach = parameter.DevourerSwell + parameter.DevourerBreath
	for _, r := range parameter.DevourerRipples {
		s.reach += r.Amp
	}
	s.reach *= reach
	breath := parameter.DevourerBreath * math.Sin(parameter.DevourerBreathRate*t)
	for j := range rows {
		y := float64(j) * s.step
		for i := range s.cols {
			x := float64(i) * s.step
			theta := math.Atan2(y-s.cy, x-s.cx)
			w := parameter.DevourerSwell*DevourerField(seed, x/size, y/size, t*parameter.DevourerSwellRate, 2) + breath
			for _, r := range parameter.DevourerRipples {
				w += r.Amp * math.Sin(r.Count*theta+r.Speed*t+r.Phase)
			}
			s.wave[j*s.cols+i] = w * reach
		}
	}
}

// Built reports whether the shape stands laid from these already
func (s *DevourerShape) Built(seed uint64, frame int64, grown float64, width, height int) bool {
	return s.wave != nil && s.key == devourerKey{seed, frame, grown, width, height}
}

// Depth is how deep the point x, y lies in the flesh, negative in the open space:
// past the open rectangle's rounded edge, moved by the field
func (s *DevourerShape) Depth(x, y float64) float64 { return s.edge(x, y) + s.waveAt(x, y) }

// Depths fills dst with Depth at each of its points along the row at y, the first
// at x0 and each dx on, the field laid along the row once for them all in row,
// the caller's scratch, which it returns
func (s *DevourerShape) Depths(dst []float32, row []float64, x0, dx, y float64) []float64 {
	gy := max(y/s.step, 0)
	j := min(int(gy), len(s.wave)/s.cols-2)
	fy := min(gy-float64(j), 1)
	top, low := s.wave[j*s.cols:(j+1)*s.cols], s.wave[(j+1)*s.cols:(j+2)*s.cols]
	row = slices.Grow(row[:0], s.cols)[:s.cols]
	for i := range row {
		row[i] = top[i] + (low[i]-top[i])*fy
	}
	ax, qy := s.hx-s.round, math.Abs(y-s.cy)-(s.hy-s.round)
	for k := 0; k < len(dst); {
		// The points between two of the field's columns read one slope of it
		x := x0 + float64(k)*dx
		i := min(max(int(x/s.step), 0), s.cols-2)
		end := len(dst)
		if next := (float64(i+1)*s.step - x0) / dx; next < float64(end) && i < s.cols-2 {
			end = max(int(math.Ceil(next)), k+1)
		}
		slope := (row[i+1] - row[i]) / s.step
		base := row[i] - slope*float64(i)*s.step - s.round
		for ; k < end; k++ {
			x := x0 + float64(k)*dx
			edge := math.Abs(x-s.cx) - ax
			if qy > 0 && edge > 0 {
				edge = math.Sqrt(edge*edge + qy*qy)
			} else if qy > edge {
				edge = qy
			}
			dst[k] = float32(edge + base + slope*x)
		}
	}
	return row
}

// Covers reports whether the cell's center lies in the flesh; the field decides only
// within its reach of the edge
func (s *DevourerShape) Covers(x, y int) bool {
	px, py := float64(x)+vmath.CellCenter, 2*(float64(y)+vmath.CellCenter)
	edge := s.edge(px, py)
	if edge > s.reach || edge < -s.reach {
		return edge > 0
	}
	return edge+s.waveAt(px, py) > 0
}

// Span is the run of row y's cells, lo through hi, whose centers lie in the open
// space by more than margin whatever the field does; lo > hi where none does. The
// open rectangle drawn in by that much is its rounded inner offset.
func (s *DevourerShape) Span(y int, margin float64) (lo, hi int) {
	d := s.reach + margin
	ax, ay, r := s.hx-d, s.hy-d, max(s.round-d, 0)
	dy := math.Abs(2*(float64(y)+vmath.CellCenter) - s.cy)
	if ax <= 0 || dy >= ay {
		return 0, -1
	}
	half := ax
	if over := dy - (ay - r); over > 0 {
		half = ax - r + math.Sqrt(r*r-over*over)
	}
	half -= 1e-9 // a center the rounding puts on the edge is tested, not passed
	return int(math.Floor(s.cx-half-vmath.CellCenter)) + 1, int(math.Ceil(s.cx+half-vmath.CellCenter)) - 1
}

// Band is the run of row y's cells, lo through hi, past which every center lies in
// the flesh whatever the field does, by a column unit to spare
func (s *DevourerShape) Band(y int) (lo, hi int) { return s.Span(y, -2*s.reach-1) }

// edge is the distance past the open rectangle's rounded edge
func (s *DevourerShape) edge(x, y float64) float64 {
	qx, qy := math.Abs(x-s.cx)-(s.hx-s.round), math.Abs(y-s.cy)-(s.hy-s.round)
	edge := max(qx, qy)
	if qx > 0 && qy > 0 {
		edge = math.Sqrt(qx*qx + qy*qy)
	}
	return edge - s.round
}

// waveAt reads the field between its samples
func (s *DevourerShape) waveAt(x, y float64) float64 {
	gx, gy := max(x/s.step, 0), max(y/s.step, 0)
	i, j := min(int(gx), s.cols-2), min(int(gy), len(s.wave)/s.cols-2)
	fx, fy := min(gx-float64(i), 1), min(gy-float64(j), 1)
	k := j*s.cols + i
	a, b, c, d := s.wave[k], s.wave[k+1], s.wave[k+s.cols], s.wave[k+s.cols+1]
	return a + (b-a)*fx + (c-a+(d-c-b+a)*fx)*fy
}

// Wall is where the flesh begins on the ray from the center at theta
func (s *DevourerShape) Wall(theta float64) (x, y float64) {
	dx, dy := math.Cos(theta), math.Sin(theta)
	lo, hi := 0.0, math.Hypot(s.cx, s.cy)
	for range 24 {
		mid := (lo + hi) / 2
		if s.Depth(s.cx+dx*mid, s.cy+dy*mid) > 0 {
			hi = mid
		} else {
			lo = mid
		}
	}
	return s.cx + dx*hi, s.cy + dy*hi
}

// GutEnds are where a gut between the walls at from and to takes hold, a cell's
// width into each so it reads grown from them
func (s *DevourerShape) GutEnds(from, to float64) (x0, y0, x1, y1 float64) {
	x0, y0 = s.Wall(from)
	x1, y1 = s.Wall(to)
	return x0 + math.Cos(from), y0 + math.Sin(from), x1 + math.Cos(to), y1 + math.Sin(to)
}

// GutLine is a gut's centerline from its end at x0, y0 to its end at x1, y1, age
// seconds after it formed: straight as it forms, easing into a sag and a writhe of
// its own phase over its first second
type GutLine struct {
	x0, y0, dx, dy, nx, ny  float64
	ease, sway, lean, phase float64
}

func NewGutLine(x0, y0, x1, y1, age, phase float64) GutLine {
	l := GutLine{x0: x0, y0: y0, dx: x1 - x0, dy: y1 - y0, ease: min(age, 1), sway: math.Sin(0.9*age + phase), lean: 1.7 * age, phase: phase}
	if length := math.Hypot(l.dx, l.dy); length != 0 {
		l.nx, l.ny = l.dx/length, l.dy/length
	}
	return l
}

// Point is the centerline u of the way along
func (l *GutLine) Point(u float64) (x, y float64) {
	sag := l.ease * (parameter.DevourerGutSag*math.Sin(math.Pi*u)*l.sway +
		parameter.DevourerGutWrithe*math.Sin(3*math.Pi*u+l.lean+l.phase))
	return l.x0 + l.dx*u - l.ny*sag, l.y0 + l.dy*u + l.nx*sag
}

// DevourerField is value noise in [-1, 1] over octaves, z its third axis, so a field
// that runs z with time swells and sinks in place. Each octave's lattice turns, so
// no ridge of the sum runs along a row or a column.
func DevourerField(seed uint64, x, y, z float64, octaves int) float64 {
	const sin, cos = 0.6045, 0.7966 // 37.2 degrees
	sum, amp, norm := 0.0, 1.0, 0.0
	x, y = cos*x-sin*y, sin*x+cos*y
	for octave := range octaves {
		sum += amp * valueNoise(seed+uint64(octave), x, y, z)
		norm += amp
		amp *= 0.5
		x, y, z = 2.03*(cos*x-sin*y)+17.1, 2.03*(sin*x+cos*y)-9.3, 2.03*z
	}
	return sum / norm
}

func valueNoise(seed uint64, x, y, z float64) float64 {
	fx, fy, fz := math.Floor(x), math.Floor(y), math.Floor(z)
	ix, iy, iz := int64(fx), int64(fy), int64(fz)
	tx, ty, tz := x-fx, y-fy, z-fz
	tx, ty, tz = tx*tx*(3-2*tx), ty*ty*(3-2*ty), tz*tz*(3-2*tz)
	plane := func(iz int64) float64 {
		a, b := knot(seed, ix, iy, iz), knot(seed, ix+1, iy, iz)
		c, d := knot(seed, ix, iy+1, iz), knot(seed, ix+1, iy+1, iz)
		top, bottom := a+(b-a)*tx, c+(d-c)*tx
		return top + (bottom-top)*ty
	}
	near := plane(iz)
	return near + (plane(iz+1)-near)*tz
}

// knot is the noise's value at a lattice point, in [-1, 1)
func knot(seed uint64, x, y, z int64) float64 {
	h := vmath.Mix64(seed ^ uint64(x)*0x9e3779b97f4a7c15 ^ uint64(y)*0xc2b2ae3d27d4eb4f ^ uint64(z)*0x165667b19e3779f9)
	return float64(h>>11)/(1<<52) - 1
}
