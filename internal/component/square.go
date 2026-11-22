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

// SquareKindSpec is what a line of one kind fires each time its cooldown runs out:
// a shield over the whole square, any other kind its family on the swarms of the
// gateways the line faces, where the combat matrix lets a tower strike with it
type SquareKindSpec struct {
	Name     string
	Attack   CombatAttackType
	Duration time.Duration      // how long the effect holds once fired
	Cooldown time.Duration      // from a fire until the line fires again
	Bolt     LightningColorType // the bolt to each gateway and swarm a fire lands on
	Shield   bool               // the blocks refuse every hit and push off what it covers while it holds
	Halt     bool               // the gateways the line faces hold their spawns while it holds
}

// SquareKinds holds what each kind fires
var SquareKinds = [SquareKindCount]SquareKindSpec{
	SquareAttack:  {Name: "attack", Attack: CombatAttackStrike, Cooldown: parameter.SquareStrikeCooldown, Bolt: LightningRed},
	SquareHold:    {Name: "hold", Attack: CombatAttackHold, Duration: parameter.SquareHoldDuration, Cooldown: parameter.SquareHoldCooldown, Bolt: LightningGold, Halt: true},
	SquareDefense: {Name: "defense", Attack: CombatAttackShield, Duration: parameter.SquareShieldDuration, Cooldown: parameter.SquareShieldCooldown, Shield: true},
}

// SquareEffect is what is left of one kind's hold and cooldown, both counted from
// its last fire
type SquareEffect struct {
	Active   time.Duration
	Cooldown time.Duration
}

// Ready reports whether its line fires it now
func (e SquareEffect) Ready() bool { return e.Active <= 0 && e.Cooldown <= 0 }

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
