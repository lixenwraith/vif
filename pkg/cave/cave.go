// Package cave shapes a side-scrolling cave as a profile of columns: how many rows of
// ceiling and of floor each column holds. A column is a pure function of its index and
// the seed, in integer arithmetic, so any instance derives any column on its own and
// every platform rounds it alike.
package cave

import (
	"errors"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// Config shapes one cave; every length is in cells.
type Config struct {
	Seed   uint64 `json:"seed"`
	Height int    `json:"height"` // rows a column spans

	MinGap  int `json:"min_gap"`  // fewest open rows between ceiling and floor
	MaxGap  int `json:"max_gap"`  // most open rows
	MinWall int `json:"min_wall"` // fewest rows the ceiling and the floor each keep

	// Wavelength is the columns between the profile's knots: longer is gentler, a
	// wall's steepest climb near 1.5·R/Wavelength rows a column for R rows of travel
	Wavelength int `json:"wavelength"`
	Swing      int `json:"swing"`  // rows the channel strays either side of the middle; 0 is all the walls leave
	Detail     int `json:"detail"` // rows of rough rock laid over the smooth profile
	Ramp       int `json:"ramp"`   // columns closing in from a mouth of MaxGap centred rows
}

// Column is one column's rock: Top rows down from row 0, Bottom rows up from the last.
type Column struct {
	Top, Bottom int
}

// MaxWavelength keeps the smoothstep numerator times a knot step inside int64
const MaxWavelength = 1 << 12

// unit is fixed-point one: a knot is a value in [0, unit)
const unit = 1 << 16

// minSwing is the least a centre knot stands off the middle, as a fraction of unit:
// knots alternate sides, so each bend of the channel travels at least twice this
const minSwing = unit / 4

// roughWavelength is the columns between rough-rock knots: short, so it reads as
// texture, and smoothed, so an edge never flickers column to column
const roughWavelength = 4

// Each noise source draws its own knots
const (
	channelGap uint64 = iota + 1
	channelCentre
	channelRoughTop
	channelRoughBottom
)

// Validate reports a configuration whose rows cannot hold its gaps and walls
func (c Config) Validate() error {
	switch {
	case c.MinWall < 0 || c.Swing < 0 || c.Detail < 0 || c.Ramp < 0:
		return errors.New("cave: negative wall, swing, detail or ramp")
	case c.MinGap < 1 || c.MaxGap < c.MinGap+2*c.Detail:
		return errors.New("cave: max gap must hold the min gap and two rows of detail")
	case c.Height < 2*c.MinWall+c.MaxGap:
		return errors.New("cave: height must hold both walls and the max gap")
	case c.Wavelength < 1 || c.Wavelength > MaxWavelength:
		return errors.New("cave: wavelength out of range")
	}
	return nil
}

// At returns column i's rock, counted from the mouth at 0; a valid configuration
// always leaves MinGap open rows and MinWall rows of each wall.
func (c Config) At(i int64) Column {
	i = max(i, 0)
	h, w := int64(c.Height), int64(c.Wavelength)
	detail := int64(c.Detail)

	// The smooth gap keeps room for detail on both walls; the channel snakes through
	// the middle of the rows it leaves, its knots half a wavelength off the gap's
	gap := int64(c.MinGap) + 2*detail + int64(c.MaxGap-c.MinGap-2*c.Detail)*c.smooth(channelGap, i, w)/unit
	slack := h - 2*int64(c.MinWall) - gap
	travel := slack
	if c.Swing > 0 {
		travel = min(slack, 2*int64(c.Swing))
	}
	top := int64(c.MinWall) + (slack-travel)/2 + travel*c.snake(i+w/2, w)/unit
	bottom := h - gap - top
	if detail > 0 {
		top += (detail + 1) * c.smooth(channelRoughTop, i, roughWavelength) / unit
		bottom += (detail + 1) * c.smooth(channelRoughBottom, i, roughWavelength) / unit
	}

	// Within the ramp, a weighted mean with the mouth: both terms are valid columns
	// and flooring each wall only widens the gap
	if ramp := int64(c.Ramp); i < ramp {
		mouthTop := (h - int64(c.MaxGap)) / 2
		mouthBottom := h - int64(c.MaxGap) - mouthTop
		top = (mouthTop*(ramp-i) + top*i) / ramp
		bottom = (mouthBottom*(ramp-i) + bottom*i) / ramp
	}
	return Column{Top: int(top), Bottom: int(bottom)}
}

// smooth is one channel's value noise at column i: a knot every w columns, eased
// between by smoothstep, whose numerator u²(3w−2u) never exceeds w³
func (c Config) smooth(channel uint64, i, w int64) int64 {
	k := i / w
	u := i - k*w
	a, b := c.knot(channel, k), c.knot(channel, k+1)
	return a + (b-a)*(u*u*(3*w-2*u))/(w*w*w)
}

// snake is the centre channel: smooth noise whose knots alternate above and below
// the middle, each at least minSwing off it
func (c Config) snake(i, w int64) int64 {
	k := i / w
	u := i - k*w
	a, b := c.side(k), c.side(k+1)
	return a + (b-a)*(u*u*(3*w-2*u))/(w*w*w)
}

// side is centre knot k: the upper half on even knots, the lower on odd ones
func (c Config) side(k int64) int64 {
	off := minSwing + c.knot(channelCentre, k)*(unit/2-minSwing)/unit
	if k%2 == 0 {
		return unit/2 + off - 1
	}
	return unit/2 - off
}

// knot is channel's value at knot k, the top 16 bits of a mix of the three
func (c Config) knot(channel uint64, k int64) int64 {
	return int64(vmath.MixChain(c.Seed, channel, uint64(k)) >> 48)
}
