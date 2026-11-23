package component

import (
	"math"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// SquareKind is what a block of a magic square is; three of one kind along a row
// or a column fire the kind's effect
type SquareKind uint8

const (
	SquareAttack SquareKind = iota
	SquareHold
	SquareDefense
	SquareKindCount
)

const (
	SquareSide   = 3
	SquareBlocks = SquareSide * SquareSide
	SquareLines  = 2 * SquareSide // the rows top down, then the columns left to right
)

// SquareKindSpec is what a line of one kind fires each time its cooldown runs out: a
// boost of the shield over the square, any other kind its family from its orb on the
// bodies the gateways the line faces sent, where the combat matrix has a cursor's row
type SquareKindSpec struct {
	Name     string
	Attack   CombatAttackType
	Duration time.Duration      // how long the effect holds once fired
	Cooldown time.Duration      // from a fire until the line fires again
	Bolt     LightningColorType // the bolt its orb throws at what a fire lands on
	Boost    bool               // the shield takes no damage while it holds; a kind that does not boost aims
	Halt     bool               // the gateways the line faces hold their spawns while it holds
	Wind     float64            // the force of the gust out from the square as it fires, 0 for none
}

// SquareKinds holds what each kind fires
var SquareKinds = [SquareKindCount]SquareKindSpec{
	SquareAttack:  {Name: "attack", Attack: CombatAttackStrike, Cooldown: parameter.SquareStrikeCooldown, Bolt: LightningRed},
	SquareHold:    {Name: "hold", Attack: CombatAttackHold, Duration: parameter.SquareHoldDuration, Cooldown: parameter.SquareHoldCooldown, Bolt: LightningGold, Halt: true},
	SquareDefense: {Name: "defense", Attack: CombatAttackNone, Duration: parameter.SquareBoostDuration, Cooldown: parameter.SquareBoostCooldown, Boost: true, Wind: parameter.SquareWindForce},
}

// SquareEffect is what is left of one kind's hold and cooldown, both counted from
// its last fire
type SquareEffect struct {
	Active   time.Duration
	Cooldown time.Duration
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
	Lines    [SquareLines]uint8 // 1 + the kind a line holds three of, 0 for none
	Effects  [SquareKindCount]SquareEffect
	Magic    time.Duration // left of the wait before a magic state sheds dust again

	X, Y          int // the top-left cell
	Width, Height int // a block's cells

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

// KindAt is the kind of the block at place p
func (s *SquareComponent) KindAt(p int) SquareKind {
	if d := int(s.Digits[p]); d >= 1 && d <= SquareBlocks {
		return s.Kinds[d-1]
	}
	return SquareKindCount
}

// IsMagic reports a magic state: every row and column of digits summing alike, as
// the Lo Shu square's do and the turns that shift it whole, which line up no kind
func (s *SquareComponent) IsMagic() bool {
	var sums [SquareLines]int
	for l := range sums {
		for _, p := range LinePlaces(l) {
			sums[l] += int(s.Digits[p])
		}
		if sums[l] != sums[0] {
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

// OrbAt is the cell on the square's outer orbit at an angle, 0 the right and a
// quarter turn the bottom
func (s *SquareComponent) OrbAt(angle float64, aspect vmath.Aspect) vmath.Point {
	cx, cy := s.Center()
	rx := s.ShieldRadius(aspect) + parameter.SquareOrbGap
	x, y := vmath.AngleToGridPos(angle, cx, cy, rx, aspect.FromCircular(rx))
	return vmath.Point{X: x, Y: y}
}

// Orbs are the cells a line's orb stands at on the outer orbit, the middle of the
// side the line faces: one for a side's row or column, one at each end for the
// middle row and column, which face both
func (s *SquareComponent) Orbs(l int, aspect vmath.Aspect) (cells [2]vmath.Point, n int) {
	at := func(quarter int) vmath.Point { return s.OrbAt(float64(quarter)*math.Pi/2, aspect) }
	// Quarter turns from the right: right, bottom, left, top
	switch i, column := l%SquareSide, l >= SquareSide; {
	case i == 1 && column:
		return [2]vmath.Point{at(3), at(1)}, 2
	case i == 1:
		return [2]vmath.Point{at(2), at(0)}, 2
	case column:
		return [2]vmath.Point{at(2 - i)}, 1
	default:
		return [2]vmath.Point{at(3 - i)}, 1
	}
}

// Faces reports whether a cell lies in line l's third of the map, cut by the edges
// of the square's middle block run on across it: the right column faces every cell
// right of the middle column, the middle row every cell level with it
func (s *SquareComponent) Faces(l, x, y int) bool {
	mx, my := s.Place(SquareBlocks / 2)
	if l < SquareSide {
		return third(y, my, s.Height) == l
	}
	return third(x, mx, s.Width) == l-SquareSide
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

// LinePlaces names the places along line l
func LinePlaces(l int) (places [SquareSide]int) {
	for i := range places {
		if l < SquareSide {
			places[i] = l*SquareSide + i
		} else {
			places[i] = i*SquareSide + l - SquareSide
		}
	}
	return places
}
