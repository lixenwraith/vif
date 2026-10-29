package engine

import (
	"github.com/lixenwraith/vif/internal/component"
)

// RockColumn is one map column of a rock field: its ceiling fills rows [0, Ceil)
// and its floor rows [Floor, map height)
type RockColumn struct {
	Ceil, Floor int
}

// Rock reports whether row y of the column is rock
func (c RockColumn) Rock(y int) bool { return y < c.Ceil || y >= c.Floor }

// RockField is rock no entity holds, a column for each map column from x 0. Its
// writer lays it from shared state (the wall system's cave from its record), so a
// capture carries that state and never the field. It blocks Mask as a wall does,
// and every wall query of Position reads it beside the wall entities.
type RockField struct {
	Mask component.WallBlockMask
	Look RockLook
	// Origin is the profile column at map column 0, which what draws the rock anchors to
	Origin int64
	cols   []RockColumn
	// The columns past the right end the next tick's scroll can bring in, for drawing
	// the field carried on between ticks; they block nothing
	ahead []RockColumn
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

// blocks reports whether the field's rock fills (x, y) for mask (0 = any wall);
// the caller has bounded y by the map
func (r *RockField) blocks(x, y int, mask component.WallBlockMask) bool {
	if x < 0 || x >= len(r.cols) || (mask != 0 && r.Mask&mask == 0) {
		return false
	}
	return r.cols[x].Rock(y)
}

// blocksArea reports whether the field's rock fills any cell of [x0, x1) × [y0, y1)
// for mask; the caller has bounded the rows by the map
func (r *RockField) blocksArea(x0, y0, x1, y1 int, mask component.WallBlockMask) bool {
	if (mask != 0 && r.Mask&mask == 0) || y0 >= y1 {
		return false
	}
	for x := max(x0, 0); x < min(x1, len(r.cols)); x++ {
		if c := r.cols[x]; y0 < c.Ceil || y1 > c.Floor {
			return true
		}
	}
	return false
}

// Rock returns the rock field, or nil when none stands. Its readers hold
// World.updateMutex and change it only through SetRock and ShiftRock.
func (p *Position) Rock() *RockField { return p.rock }

// SetRock lays cols as the rock field from map column 0, origin its first profile
// column; no columns lift the field
func (p *Position) SetRock(mask component.WallBlockMask, look RockLook, origin int64, cols []RockColumn) {
	p.wallEpoch++
	p.wallLog.lose()
	if len(cols) == 0 {
		p.rock = nil
		return
	}
	if p.rock == nil {
		p.rock = &RockField{}
	}
	p.rock.Mask, p.rock.Look, p.rock.Origin = mask, look, origin
	p.rock.cols = append(p.rock.cols[:0], cols...)
	p.rock.ahead = p.rock.ahead[:0]
}

// SetRockAhead keeps cols as the columns past the field's right end, in entry order
func (p *Position) SetRockAhead(cols []RockColumn) {
	if p.rock != nil {
		p.rock.ahead = append(p.rock.ahead[:0], cols...)
	}
}

// HasBlockingWallDrawnAt is HasBlockingWallAt as a frame draws the cave carry columns
// on: rock and shared walls ride its scroll, so cell (x, y) shows what x+carry holds
// now, and past the map's right edge the field's columns ahead
func (p *Position) HasBlockingWallDrawnAt(x, y, carry int, mask component.WallBlockMask) bool {
	if carry <= 0 || x < 0 {
		return p.HasBlockingWallAt(x, y, mask)
	}
	x += carry
	if r := p.rock; r != nil && x >= len(r.cols) && x < len(r.cols)+len(r.ahead) {
		return p.world != nil && y >= 0 && y < p.world.Resources.Config.MapHeight &&
			(mask == 0 || r.Mask&mask != 0) && r.ahead[x-len(r.cols)].Rock(y)
	}
	return p.HasBlockingWallAt(x, y, mask)
}

// ShiftRock moves the field a column left: map column 0 leaves it and enter stands
// at its right end. The wall log takes the cells each column's ends crossed.
func (p *Position) ShiftRock(enter RockColumn) {
	r := p.rock
	if r == nil {
		return
	}
	p.wallEpoch++
	w, h := p.wallBounds()
	for x := range min(len(r.cols), w) {
		was, now := r.cols[x], enter
		if x+1 < len(r.cols) {
			now = r.cols[x+1]
		}
		// A row neither end crossed is rock in both columns or in neither
		for _, ends := range [2][2]int{{was.Ceil, now.Ceil}, {was.Floor, now.Floor}} {
			for y := max(min(ends[0], ends[1]), 0); y < min(max(ends[0], ends[1]), h); y++ {
				p.wallLog.add(x, y, len(p.grid.Cells))
			}
		}
	}
	copy(r.cols, r.cols[1:])
	r.cols[len(r.cols)-1] = enter
	r.Origin++
}
