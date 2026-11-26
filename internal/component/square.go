package component

import (
	"math"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SquareKind is what a block of a magic square is; three of one kind along an edge
// fire the kind's effect
type SquareKind uint8

const (
	SquareAttack SquareKind = iota
	SquareTempest
	SquareDefense
	SquareKindCount
)

const (
	SquareSide   = 3
	SquareBlocks = SquareSide * SquareSide

	// A square's lines are its edges, the top row, the bottom row, the left column
	// and the right, each facing its side of the map, then its middle row and column
	SquareEdges = 4
	SquareLines = SquareEdges + 2
)

// SquareWind is the wind a kind blows about the square as it fires: its force, the
// turn of it from straight out, a quarter turn carrying bodies clockwise round the
// shield, how long it blows and the columns past the shield it fades over
type SquareWind struct {
	Force, Turn float64
	Duration    time.Duration
	Reach       float64
}

// SquareKindSpec is what a line of one kind fires as its cursor's each time its
// cooldown runs out: a family on what stands near the side of the map the line
// faces, which an edge alone does, a boost of the shield, a wind about the square,
// the square's dust shed; and what its blocks filling a lit spell fire: a family on
// every body within reach, all round, and the effect held SpellBoost long
type SquareKindSpec struct {
	Name       string
	Attack     CombatAttackType // CombatAttackNone lands nothing
	Duration   time.Duration    // how long the effect holds once fired
	Cooldown   time.Duration    // from a fire until the line fires again
	Boost      bool             // the shield takes no damage while it holds
	Shed       bool
	Wind       SquareWind // a zero force blows none
	Spell      CombatAttackType
	SpellBoost time.Duration
}

// SquareKinds holds what each kind fires
var SquareKinds = [SquareKindCount]SquareKindSpec{
	SquareAttack: {Name: "attack", Attack: CombatAttackStrike, Cooldown: parameter.SquareStrikeCooldown, Spell: CombatAttackStrike},
	SquareTempest: {Name: "tempest", Attack: CombatAttackNone, Duration: parameter.SquareTempestDuration, Cooldown: parameter.SquareTempestCooldown,
		Wind: SquareWind{Force: parameter.SquareTempestForce, Turn: math.Pi / 2, Duration: parameter.SquareTempestDuration, Reach: parameter.SquareTempestReach}, Spell: CombatAttackConfuse},
	SquareDefense: {Name: "defense", Attack: CombatAttackNone, Duration: parameter.SquareBoostDuration, Cooldown: parameter.SquareBoostCooldown,
		Boost: true, Shed: true, Wind: SquareWind{Force: parameter.SquareWindForce, Duration: parameter.SquareWindDuration, Reach: parameter.SquareWindReach},
		Spell: CombatAttackNone, SpellBoost: parameter.SquareSpellBoost},
}

// SquareEffect is what is left of one kind's hold and cooldown, both counted from
// its last fire, and whether that fire's family landed on anything
type SquareEffect struct {
	Active   time.Duration
	Cooldown time.Duration
	Struck   bool
}

// Ready reports whether its line fires it now
func (e SquareEffect) Ready() bool { return e.Active <= 0 && e.Cooldown <= 0 }

// SquareComponent is a magic square, on an entity of its own: the digit of the block
// at each place, row by row, which a turn permutes, each digit's kind, the digit each
// roster slot has selected, the kind each line holds three of and each kind's effect
type SquareComponent struct {
	Digits   [SquareBlocks]uint8
	Kinds    [SquareBlocks]SquareKind // by digit, less one
	Selected [parameter.MaxPlayers]uint8
	Lines    [SquareLines]uint8 // 1 + the kind a line holds three of and fires, 0 for none
	Effects  [SquareKindCount]SquareEffect
	Dust     time.Duration // its dust meter's charge, full at parameter.SquareDustFull

	// The spell: the places it lights, a bit each, whether they are lit, what is
	// left of its phase, the wait until they light or the time left to fill them,
	// and 1 + the kind whose blocks last filled it, 0 for none
	Spell      uint16
	SpellLit   bool
	SpellTimer time.Duration
	SpellCast  uint8

	X, Y          int // the top-left cell
	Width, Height int // a block's cells
	MaxHeight     int // the block's rows asked, of which Fit keeps what the map has room for

	ShieldStep int64 // the ShieldComponent.Step it gives its cursor's shield
}

// Place is the top-left cell of the block at place p
func (s *SquareComponent) Place(p int) (x, y int) {
	return s.X + p%SquareSide*(s.Width+parameter.SquareBlockGap), s.Y + p/SquareSide*(s.Height+parameter.SquareBlockGap)
}

// Size is the cells the square spans
func (s *SquareComponent) Size() (w, h int) {
	return SquareSide*s.Width + (SquareSide-1)*parameter.SquareBlockGap,
		SquareSide*s.Height + (SquareSide-1)*parameter.SquareBlockGap
}

// Center is the square's middle cell, where the cursor it fights through stands
func (s *SquareComponent) Center() (x, y int) {
	w, h := s.Size()
	return s.X + w/2, s.Y + h/2
}

// Legend is the cell a legend entry over column c of the square starts at: its
// name's row, its meter's under it, each a block wide
func (s *SquareComponent) Legend(c int) (x, y int) {
	x, _ = s.Place(c)
	return x, s.Y - 3
}

// KindAt is the kind of the block at place p
func (s *SquareComponent) KindAt(p int) SquareKind {
	if d := int(s.Digits[p]); d >= 1 && d <= SquareBlocks {
		return s.Kinds[d-1]
	}
	return SquareKindCount
}

// IsMagic reports a magic state: every row and column of digits summing alike, as
// the Lo Shu square's do and the turns that shift it whole, which line up no kind,
// or the digits standing in order row by row, 1 to 9 or 9 to 1
func (s *SquareComponent) IsMagic() bool {
	up, down := true, true
	for p, d := range s.Digits {
		up, down = up && int(d) == p+1, down && int(d) == SquareBlocks-p
	}
	var sums [2 * SquareSide]int
	for l := range sums {
		for _, p := range LinePlaces(l >= SquareSide, l%SquareSide) {
			sums[l] += int(s.Digits[p])
		}
		if sums[l] != sums[0] && !up && !down {
			return false
		}
	}
	return true
}

// ShieldRadius is the reach, in columns at a cell aspect, of the shield that covers
// the square: through its corners, a margin wider
func (s *SquareComponent) ShieldRadius(aspect vmath.Aspect) float64 {
	w, h := s.Size()
	return math.Hypot(float64(w)/2, aspect.Circular(float64(h)/2)) + parameter.SquareShieldMargin
}

// Line is the line that holds three of a kind, -1 when none does
func (s *SquareComponent) Line(kind SquareKind) int {
	for l, held := range s.Lines {
		if held == uint8(kind)+1 {
			return l
		}
	}
	return -1
}

// Fit sets the block's height to the most, down to one row, under which its
// cursor's orbs circle on a map of mapW by mapH about a middle at (cx, cy)
func (s *SquareComponent) Fit(cx, cy, mapW, mapH int, aspect vmath.Aspect) {
	for s.Height = max(s.MaxHeight, 1); s.Height > 1; s.Height-- {
		orbit := s.ShieldRadius(aspect) + parameter.OrbOrbitRadiusX - parameter.PlayerShieldRadius
		if orbit < room(cx, mapW)+vmath.CellCenter && aspect.FromCircular(orbit) < room(cy, mapH)+vmath.CellCenter {
			return
		}
	}
}

// room is the radius about cell c that stays on an axis of size cells
func room(c, size int) float64 { return float64(min(c, size-1-c)) }

// OrbAt is the cell on the square's outer orbit at an angle, 0 the right and a
// quarter turn the bottom; a map of mapW by mapH too small for the orbit flattens it
func (s *SquareComponent) OrbAt(angle float64, aspect vmath.Aspect, mapW, mapH int) vmath.Point {
	cx, cy := s.Center()
	rx := s.ShieldRadius(aspect) + parameter.SquareOrbGap
	ry := min(aspect.FromCircular(rx), room(cy, mapH))
	x, y := vmath.AngleToGridPos(angle, cx, cy, min(rx, room(cx, mapW)), ry)
	return vmath.Point{X: x, Y: y}
}

// OrbOf is the cell the orb of a kind that strikes stands at, at a game time: the
// middle of the side its line faces, or turning round the outer orbit once each
// SquareOrbIdlePeriod while no line holds the kind
func (s *SquareComponent) OrbOf(kind SquareKind, now time.Time, aspect vmath.Aspect, mapW, mapH int) vmath.Point {
	if l := s.Line(kind); l >= 0 {
		return s.Orb(l, aspect, mapW, mapH)
	}
	period := int64(parameter.SquareOrbIdlePeriod)
	return s.OrbAt(float64(now.UnixNano()%period)/float64(period)*vmath.TwoPi, aspect, mapW, mapH)
}

// Orb is the cell a line's orb stands at on the outer orbit, the middle of the side
// the line faces
func (s *SquareComponent) Orb(l int, aspect vmath.Aspect, mapW, mapH int) vmath.Point {
	// Quarter turns from the right: right, bottom, left, top
	return s.OrbAt(float64([SquareEdges]int{3, 1, 2, 0}[l])*math.Pi/2, aspect, mapW, mapH)
}

// Faces is the test of whether a cell lies in edge l's part of the map: the third
// on its side, past the edge of the middle block run on across the map
func (s *SquareComponent) Faces(l int) func(x, y int) bool {
	mx, my := s.Place(SquareBlocks / 2)
	side := l % 2 * 2
	if l >= 2 {
		return func(x, _ int) bool { return third(x, mx, s.Width) == side }
	}
	return func(_, y int) bool { return third(y, my, s.Height) == side }
}

// Strikes is the test of whether a cell lies where edge l's strike lands: in the
// part of the map it faces, within reach
func (s *SquareComponent) Strikes(l int, aspect vmath.Aspect) func(x, y int) bool {
	faces, within := s.Faces(l), s.Within(aspect)
	return func(x, y int) bool { return faces(x, y) && within(x, y) }
}

// Within is the test of whether a cell lies within reach of the square: inside
// SquareStrikeReach shield radii of its middle, all round
func (s *SquareComponent) Within(aspect vmath.Aspect) func(x, y int) bool {
	reach := parameter.SquareStrikeReach * s.ShieldRadius(aspect)
	cx, cy := s.Center()
	invRx, invRy := vmath.EllipseInvRadiiSq(reach, aspect.FromCircular(reach))
	return func(x, y int) bool { return vmath.EllipseContainsPoint(x, y, cx, cy, invRx, invRy) }
}

// SpellKind is the kind whose blocks fill every place lit, SquareKindCount when
// none does
func (s *SquareComponent) SpellKind(places uint16) SquareKind {
	kind := SquareKindCount
	for p := range SquareBlocks {
		if places&(1<<p) == 0 {
			continue
		}
		if at := s.KindAt(p); kind == SquareKindCount {
			kind = at
		} else if at != kind {
			return SquareKindCount
		}
	}
	return kind
}

// third is 0 for v before [start, start+size), 1 inside it and 2 past it
func third(v, start, size int) int {
	switch {
	case v < start:
		return 0
	case v >= start+size:
		return 2
	}
	return 1
}

// LinePlaces names the places along row i, or along column i
func LinePlaces(column bool, i int) (places [SquareSide]int) {
	for n := range places {
		if column {
			places[n] = n*SquareSide + i
		} else {
			places[n] = i*SquareSide + n
		}
	}
	return places
}

// PlacesOf names the places along line l
func PlacesOf(l int) [SquareSide]int {
	if l >= SquareEdges {
		return LinePlaces(l > SquareEdges, SquareSide/2)
	}
	return LinePlaces(l >= 2, l%2*(SquareSide-1))
}
