package component

import (
	"math"
	"time"

	"github.com/lixenwraith/vif/internal/core"
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

// SquareKindSpec is what one kind's alignment fires: its family lands on every
// target the combat matrix lets a tower strike with it, once as it fires, or, for
// a shield, each tick on those the shield covers while it holds
type SquareKindSpec struct {
	Name     string
	Attack   CombatAttackType
	Duration time.Duration // how long the effect holds once fired
	Recharge time.Duration // from its end until an alignment fires it again
	Bolt     bool          // a bolt flies from the aligned line to each target struck
	Shield   bool          // the blocks refuse every hit while it holds
	Halt     bool          // every gateway holds its spawns while it holds
}

// SquareKinds holds what each kind fires
var SquareKinds = [SquareKindCount]SquareKindSpec{
	SquareAttack:  {Name: "attack", Attack: CombatAttackStrike, Recharge: parameter.SquareStrikeRecharge, Bolt: true},
	SquareHold:    {Name: "hold", Attack: CombatAttackHold, Duration: parameter.SquareHoldDuration, Recharge: parameter.SquareHoldRecharge, Halt: true},
	SquareDefense: {Name: "defense", Attack: CombatAttackShield, Duration: parameter.SquareShieldDuration, Recharge: parameter.SquareShieldRecharge, Shield: true},
}

// SquareEffect is one kind's effect: holding, then recharging
type SquareEffect struct {
	Active   time.Duration
	Recharge time.Duration
}

// Ready reports whether an alignment fires it now
func (e SquareEffect) Ready() bool { return e.Active <= 0 && e.Recharge <= 0 }

// SquareComponent is a magic square, on an entity of its own: the block headers at
// its places, row by row, which a shift permutes, the block each roster slot has
// selected, the kind each line holds three of and each kind's effect
type SquareComponent struct {
	Places   [SquareBlocks]core.Entity
	Selected [parameter.MaxPlayers]core.Entity
	Lines    [SquareLines]uint8 // 1 + the kind a line holds three of, 0 for none
	Effects  [SquareKindCount]SquareEffect

	X, Y          int   // the top-left cell
	Width, Height int   // a block's cells
	Group         uint8 // the target group its standing blocks anchor
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

// ShieldRadii are the radii of the square's shield at a cell aspect: an ellipse
// through its corners, a margin wider
func (s *SquareComponent) ShieldRadii(aspect vmath.Aspect) (rx, ry float64) {
	w, h := s.Size()
	return float64(w)/2*math.Sqrt2 + parameter.SquareShieldMargin,
		float64(h)/2*math.Sqrt2 + aspect.Y(parameter.SquareShieldMargin/2.0)
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

// BlockComponent is a block of a magic square, on its composite's header. A fallen
// block has lost every cell: it still turns with its line and aligns, but nothing
// targets it
type BlockComponent struct {
	Square core.Entity
	Anchor core.Entity // the cell holding its target anchor, 0 for none
	Digit  uint8       // 1 to 9, the key that selects it
	Kind   SquareKind
	HP     int // what each cell started with
	Fallen bool
}
