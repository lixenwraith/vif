package engine

import (
	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/pkg/position"
)

// The rock field is the index's; the game reads it by these names.
type (
	RockField  = position.Rock
	RockColumn = position.Column
)

// RockLook is how a rock field draws: Visual in every cell, or with Stone set the art
// preset Art (a pattern.ArtPreset) from Seed, cell by cell
type RockLook struct {
	Visual component.WallVisualConfig
	Stone  bool
	Art    uint8
	Seed   uint64
}

// Rock returns the rock field, or nil when none stands. Its readers hold
// World.updateMutex and change it only through SetRock and ShiftRock.
func (p *Position) Rock() *RockField { return p.index.Rock() }

// RockLook is how the rock field standing draws
func (p *Position) RockLook() RockLook { return p.rockLook }

// SetRock lays cols as the rock field from map column 0, origin its first profile
// column, drawn in look; no columns lift the field
func (p *Position) SetRock(mask component.WallBlockMask, look RockLook, origin int64, cols []RockColumn) {
	p.rockLook = look
	p.index.SetRock(position.Mask(mask), origin, cols)
}

// SetRockAhead keeps cols as the columns past the field's right end, in entry order
func (p *Position) SetRockAhead(cols []RockColumn) { p.index.SetRockAhead(cols) }

// HasBlockingWallDrawnAt is HasBlockingWallAt as a frame draws the cave carry columns
// on: rock and shared walls ride its scroll, so cell (x, y) shows what x+carry holds
// now, and past the map's right edge the field's columns ahead
func (p *Position) HasBlockingWallDrawnAt(x, y, carry int, mask component.WallBlockMask) bool {
	return p.index.BlocksDrawn(x, y, carry, position.Mask(mask))
}

// ShiftRock moves the field a column left: map column 0 leaves it and enter stands
// at its right end. The wall log takes the cells each column's ends crossed.
func (p *Position) ShiftRock(enter RockColumn) { p.index.ShiftRock(enter) }
