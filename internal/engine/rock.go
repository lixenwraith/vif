package engine

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/pkg/position"
)

// RockColumn is one map column of the rock: its ceiling fills rows [0, Ceil) and
// its floor rows [Floor, map height)
type RockColumn struct {
	Ceil, Floor int
}

// Rock reports whether row y of the column is rock
func (c RockColumn) Rock(y int) bool { return y < c.Ceil || y >= c.Floor }

// RockField is the cave's rock, two surfaces of the position index: the ceiling,
// solid above its height, and the floor, solid below, with one mask and one
// origin, the profile column at map column 0 that what draws the rock anchors to.
// Its writer lays it from its record, so a capture carries that and never the field.
type RockField struct {
	Mask   component.WallBlockMask
	Origin int64
	cols   []RockColumn
	// The columns past the right end the next tick's scroll can bring in, for drawing
	// the field carried on between ticks; they block nothing
	ahead          []RockColumn
	ceiling, floor *position.Surface
	heights        [2][]int // scratch: the columns as the surfaces take them
}

// RockLook is how a rock field draws: Visual in every cell, or with Stone set the art
// preset Art (a pattern.ArtPreset) from Seed, cell by cell
type RockLook struct {
	Visual component.WallVisualConfig
	Stone  bool
	Art    uint8
	Seed   uint64
}

// Width is how many map columns the field covers, from x 0
func (r *RockField) Width() int { return len(r.cols) }

// Columns is the field's columns from map column 0, to read and not to keep
func (r *RockField) Columns() []RockColumn { return r.cols }

// Ahead is the columns past the field's right end the next tick can bring in, to read
// and not to keep
func (r *RockField) Ahead() []RockColumn { return r.ahead }

// Column returns map column x's rock, and false outside the field
func (r *RockField) Column(x int) (RockColumn, bool) {
	if x < 0 || x >= len(r.cols) {
		return RockColumn{}, false
	}
	return r.cols[x], true
}

// split lays cols out as the ceiling's heights and the floor's
func (r *RockField) split(cols []RockColumn) (ceil, floor []int) {
	ceil, floor = r.heights[0][:0], r.heights[1][:0]
	for _, c := range cols {
		ceil, floor = append(ceil, c.Ceil), append(floor, c.Floor)
	}
	r.heights[0], r.heights[1] = ceil, floor
	return ceil, floor
}

// Rock returns the rock field, or nil when none stands. Its readers hold
// World.updateMutex and change it only through SetRock and ShiftRock.
func (p *Position) Rock() *RockField { return p.rock }

// RockLook is how the rock field standing draws
func (p *Position) RockLook() RockLook { return p.rockLook }

// SetRock lays cols as the rock field from map column 0, origin its first profile
// column, drawn in look; no columns lift the field
func (p *Position) SetRock(mask component.WallBlockMask, look RockLook, origin int64, cols []RockColumn) {
	p.rockLook = look
	if len(cols) == 0 {
		p.liftRock()
		return
	}
	r := p.rock
	if r == nil {
		r = &RockField{}
		p.rock = r
	}
	r.Mask, r.Origin = mask, origin
	r.cols = append(r.cols[:0], cols...)
	r.ahead = r.ahead[:0]
	ceil, floor := r.split(cols)
	if r.ceiling == nil {
		r.ceiling = p.index.AddSurface(mask, position.Above, origin, ceil)
		r.floor = p.index.AddSurface(mask, position.Below, origin, floor)
		return
	}
	r.ceiling.Mask, r.floor.Mask = mask, mask
	p.index.LaySurface(r.ceiling, origin, ceil)
	p.index.LaySurface(r.floor, origin, floor)
}

// liftRock takes the rock and its surfaces out
func (p *Position) liftRock() {
	if r := p.rock; r != nil {
		p.index.RemoveSurface(r.ceiling)
		p.index.RemoveSurface(r.floor)
		p.rock = nil
	}
}

// SetRockAhead keeps cols as the columns past the field's right end, in entry order
func (p *Position) SetRockAhead(cols []RockColumn) {
	r := p.rock
	if r == nil {
		return
	}
	r.ahead = append(r.ahead[:0], cols...)
	ceil, floor := r.split(cols)
	p.index.SetSurfaceAhead(r.ceiling, ceil)
	p.index.SetSurfaceAhead(r.floor, floor)
}

// HasBlockingWallDrawnAt is HasBlockingWallAt as a frame draws the cave carry columns
// on: rock and shared walls ride its scroll, so cell (x, y) shows what x+carry holds
// now, and past the map's right edge the field's columns ahead
func (p *Position) HasBlockingWallDrawnAt(x, y, carry int, mask component.WallBlockMask) bool {
	return p.index.BlocksCarried(x, y, carry, mask)
}

// ShiftRock moves the field a column left: map column 0 leaves it and enter stands
// at its right end. The wall log takes the cells each column's ends crossed.
func (p *Position) ShiftRock(enter RockColumn) {
	r := p.rock
	if r == nil {
		return
	}
	p.index.ShiftSurface(r.ceiling, enter.Ceil)
	p.index.ShiftSurface(r.floor, enter.Floor)
	copy(r.cols, r.cols[1:])
	r.cols[len(r.cols)-1] = enter
	r.Origin++
}
