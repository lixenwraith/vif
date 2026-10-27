package component

import (
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/pkg/machine"
	"github.com/lixenwraith/vif/pkg/vmath/physics"
)

// BehaviorState is a data species' place in its program's machine; a program's
// states take their values in sorted-name order
type BehaviorState uint8

// BehaviorComponent is one data species instance: which program steps it and
// where it stands in it. Composite structure lives in HeaderComponent.
type BehaviorComponent struct {
	Program uint8 // index into Resources.Behaviors, the program's sorted-name order
	Machine machine.Instance[BehaviorState]

	PatternIndex   int           // glyph frame shown
	PatternElapsed time.Duration // time the frame has shown

	TargetX, TargetY int // the cell StoreTarget kept, which a charge or teleport goes to

	Shielded bool // Written from the machine's flags each step: damage and stun immunity
	Beaming  bool // Written likewise: a teleport line runs from the header to the stored cell
}

// BehaviorPrototype is what a program declares about its species' body: plain data
// the behavior system, the renderers and the readers of the species tables take a
// data species' values from
type BehaviorPrototype struct {
	Name    string      // the program's name, which keys its status rows
	Species SpeciesType // SpeciesDataFirst plus the program's index

	Width, Height    int // footprint in cells
	HeaderX, HeaderY int // phantom head offset from the top-left

	HitPoints int
	Combat    CombatEntityType      // the attack-matrix row it fights as
	Homing    physics.HomingProfile // what its homing steers by
	Mass      float64               // soft-collision mass; 0 takes no soft collision or flocking
	Drops     SpeciesType           // the code species whose drop tiers it rolls; SpeciesNone drops nothing
	Contact   CursorDamage          // what touching it costs a cursor
	Lifetime  time.Duration         // 0 = lives until killed or cancelled

	Frames  [][]rune      // glyph frames, each Width*Height runes row-major
	Pattern time.Duration // how long each frame shows
	Color   color.RGB
}
