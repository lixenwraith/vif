// Package cave shapes a side-scrolling cave as a profile of columns: how many rows of
// ceiling and of floor each column holds. A column is a pure function of its index and
// the seed, in integer arithmetic, so any instance derives any column on its own and
// every platform rounds it alike. A cave's channel snakes, or each of its walls is a
// jagged fractal of its own, and an opening widens it from a column on.
package cave

import (
	"errors"
	"slices"

	"github.com/lixenwraith/vif/pkg/vmath"
)

// Config shapes one cave; every length is in cells.
type Config struct {
	Seed   uint64 `json:"seed"`
	Height int    `json:"height"` // rows a column spans

	MinGap  int `json:"min_gap"`  // fewest open rows between ceiling and floor
	MaxGap  int `json:"max_gap"`  // most open rows; a jagged cave's at most all the walls leave
	MinWall int `json:"min_wall"` // fewest rows the ceiling and the floor each keep

	// Wavelength is the columns between the profile's knots: longer is gentler, a
	// wall's steepest climb near 1.5·R/Wavelength rows a column for R rows of travel
	Wavelength int `json:"wavelength"`
	Swing      int `json:"swing"`  // rows the channel strays either side of the middle; 0 is all the walls leave
	Detail     int `json:"detail"` // rows of rough rock laid over the smooth profile
	Ramp       int `json:"ramp"`   // columns closing in from a mouth of MaxGap centred rows

	// Octaves, when set, makes the cave jagged: each wall reaches into the gap by its
	// own fractal noise of that many octaves, from Wavelength halving down
	Octaves int `json:"octaves,omitempty"`

	// Open, when set, widens the cave from one column on
	Open Opening `json:"open,omitzero"`
}

// Opening widens a cave past column At, over Ramp columns, to Gap open rows centred,
// all the walls leave when 0, and holds it open from there.
type Opening struct {
	At   int64 `json:"at"`
	Gap  int   `json:"gap,omitempty"`
	Ramp int   `json:"ramp"`
}

// Column is one column's rock: Top rows down from row 0, Bottom rows up from the last.
type Column struct {
	Top, Bottom int
}

// MaxWavelength keeps the smoothstep numerator times a knot step inside int64
const MaxWavelength = 1 << 12

// MaxOctaves is the most a jagged wall sums; past it an octave is under a row
const MaxOctaves = 8

// A jagged wall's octave keeps jaggedGain percent of the last one's amplitude, and the
// sum's spread about its middle is stretched by jaggedStretch percent and squared, so
// a wall keeps near its side and now and then reaches far in
const (
	jaggedGain    = 60
	jaggedStretch = 170
)

// jaggedMedian is the columns either side whose median a jagged wall takes, so no
// bump or notch of that many columns or fewer stands out of its edge
const jaggedMedian = 2

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
	channelWallTop
	channelWallBottom
)

// Validate reports a configuration whose rows cannot hold its gaps and walls
func (c Config) Validate() error {
	switch {
	case c.MinWall < 0 || c.Swing < 0 || c.Detail < 0 || c.Ramp < 0:
		return errors.New("cave: negative wall, swing, detail or ramp")
	case c.MinGap < 1 || c.MaxGap < c.MinGap+2*c.Detail:
		return errors.New("cave: max gap must hold the min gap and two rows of detail")
	case c.Octaves < 0 || c.Octaves > MaxOctaves:
		return errors.New("cave: octaves out of range")
	case c.Octaves == 0 && c.Height < 2*c.MinWall+c.MaxGap:
		return errors.New("cave: height must hold both walls and the max gap")
	case c.widest() < c.MinGap+2*c.Detail:
		return errors.New("cave: height must hold both walls, the min gap and two rows of detail")
	case c.Wavelength < 1 || c.Wavelength > MaxWavelength:
		return errors.New("cave: wavelength out of range")
	case c.Open != (Opening{}) && (c.Open.At < 0 || c.Open.Ramp < 1 || c.Open.Gap < 0 || c.Open.Gap > 0 && c.Open.Gap < c.MinGap):
		return errors.New("cave: an opening needs a column, a ramp, and a gap of all the walls leave or at least the min gap")
	}
	return nil
}

// widest is the most open rows a column holds: MaxGap, or in a jagged cave as many
// of them as the walls leave
func (c Config) widest() int {
	return min(c.MaxGap, c.Height-2*c.MinWall)
}

// At returns column i's rock, counted from the mouth at 0; a valid configuration
// always leaves MinGap open rows and MinWall rows of each wall.
func (c Config) At(i int64) Column {
	if c.Octaves == 0 {
		return c.opened(i, c.column(i))
	}
	// A jagged wall's edge is the median of its own around it. Of 2r+1 columns, r+1
	// keep each wall at least its median, so one column keeps both and the medians
	// leave at least its gap
	var tops, bottoms [2*jaggedMedian + 1]int
	for k := range tops {
		col := c.column(i + int64(k-jaggedMedian))
		tops[k], bottoms[k] = col.Top, col.Bottom
	}
	slices.Sort(tops[:])
	slices.Sort(bottoms[:])
	return c.opened(i, Column{Top: tops[jaggedMedian], Bottom: bottoms[jaggedMedian]})
}

// opened eases column i out to the opening past its column, a weighted mean as the
// mouth's ramp takes. It follows a jagged cave's median, so every column up to the
// opening's keeps the rock it had before the opening was set.
func (c Config) opened(i int64, col Column) Column {
	o := c.Open
	if o.Ramp == 0 || i <= o.At {
		return col
	}
	gap := c.Height - 2*c.MinWall
	if o.Gap > 0 {
		gap = min(o.Gap, gap)
	}
	top := int64(c.Height-gap) / 2
	bottom := int64(c.Height-gap) - top
	k, r := min(i-o.At, int64(o.Ramp)), int64(o.Ramp)
	return Column{
		Top:    int((int64(col.Top)*(r-k) + top*k) / r),
		Bottom: int((int64(col.Bottom)*(r-k) + bottom*k) / r),
	}
}

// column is column i's rock before a jagged cave's median
func (c Config) column(i int64) Column {
	i = max(i, 0)
	h := int64(c.Height)
	detail := int64(c.Detail)

	var top, bottom int64
	if c.Octaves > 0 {
		top, bottom = c.jagged(i)
	} else {
		top, bottom = c.snaking(i)
	}
	if detail > 0 {
		top += (detail + 1) * c.smooth(channelRoughTop, i, roughWavelength) / unit
		bottom += (detail + 1) * c.smooth(channelRoughBottom, i, roughWavelength) / unit
	}

	// Within the ramp, a weighted mean with the mouth: both terms are valid columns
	// and flooring each wall only widens the gap
	if ramp := int64(c.Ramp); i < ramp {
		widest := int64(c.widest())
		mouthTop := (h - widest) / 2
		mouthBottom := h - widest - mouthTop
		top = (mouthTop*(ramp-i) + top*i) / ramp
		bottom = (mouthBottom*(ramp-i) + bottom*i) / ramp
	}
	return Column{Top: int(top), Bottom: int(bottom)}
}

// snaking is column i's smooth walls: the gap keeps room for detail on both walls,
// and the channel snakes through the middle of the rows it leaves, its knots half a
// wavelength off the gap's
func (c Config) snaking(i int64) (top, bottom int64) {
	h, w := int64(c.Height), int64(c.Wavelength)
	gap := int64(c.MinGap+2*c.Detail) + int64(c.MaxGap-c.MinGap-2*c.Detail)*c.smooth(channelGap, i, w)/unit
	top = c.channel(i, gap)
	return top, h - gap - top
}

// jagged is column i's walls in a jagged cave: a band of the widest gap, placed as a
// snaking channel is, which each wall reaches into by its own fractal noise; walls
// reaching in together past the min gap and detail share what is left by their reach.
// Each wall's band and reach sum in fixed point and floor once, so a wall never steps
// a row out and back as the two floor apart; where the walls share, the floor takes
// the row two floors would leave, so the gap closes to the min gap and no wider
func (c Config) jagged(i int64) (top, bottom int64) {
	h, w := int64(c.Height), int64(c.Wavelength)
	widest := int64(c.widest())
	slack := h - 2*int64(c.MinWall) - widest
	travel := slack
	if c.Swing > 0 {
		travel = min(slack, 2*int64(c.Swing))
	}
	top = (int64(c.MinWall)+(slack-travel)/2)*unit + travel*c.snake(i+w/2, w)
	bottom = (h-widest)*unit - top
	reach := widest - int64(c.MinGap+2*c.Detail)
	in := reach * c.fractal(channelWallTop, i, w)
	out := reach * c.fractal(channelWallBottom, i, w)
	if in+out <= reach*unit {
		return (top + in) / unit, (bottom + out) / unit
	}
	top = (top + reach*(in*unit/(in+out))) / unit
	return top, h - int64(c.MinGap+2*c.Detail) - top
}

// channel is the top wall of a gap rows high snaking through the middle of the rows
// it leaves, at most Swing either side
func (c Config) channel(i, gap int64) int64 {
	w := int64(c.Wavelength)
	slack := int64(c.Height) - 2*int64(c.MinWall) - gap
	travel := slack
	if c.Swing > 0 {
		travel = min(slack, 2*int64(c.Swing))
	}
	return int64(c.MinWall) + (slack-travel)/2 + travel*c.snake(i+w/2, w)/unit
}

// fractal is channel's octaves at column i, the first at wavelength w, stretched and
// squared into [0, unit]
func (c Config) fractal(channel uint64, i, w int64) int64 {
	var sum, total int64
	amp := int64(unit)
	for o := range c.Octaves {
		sum += amp * c.smooth(channel+uint64(o)<<32, i, max(w>>o, 1))
		total += amp
		amp = amp * jaggedGain / 100
	}
	v := min(max(unit/2+(sum/total-unit/2)*jaggedStretch/100, 0), unit)
	return v * v / unit
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
