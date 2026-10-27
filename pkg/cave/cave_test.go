package cave

import (
	"cmp"
	"hash/fnv"
	"strconv"
	"testing"
)

// shapes are the configurations the property tests sweep: a terminal's cave, a
// short and rough one, a long gentle one and a tight one at its limits, then jagged
// ones: vanguard's on a terminal and on its smallest map, a tall rough one whose
// band snakes, and a tight one at its limits
var shapes = []Config{
	{Height: 40, MinGap: 8, MaxGap: 22, MinWall: 2, Wavelength: 48, Detail: 1, Ramp: 80},
	{Height: 24, MinGap: 6, MaxGap: 12, MinWall: 1, Wavelength: 16, Detail: 2, Ramp: 10},
	{Height: 60, MinGap: 12, MaxGap: 40, MinWall: 3, Wavelength: 200, Swing: 9, Detail: 0, Ramp: 0},
	{Height: 7, MinGap: 3, MaxGap: 5, MinWall: 1, Wavelength: 1, Swing: 40, Detail: 1, Ramp: 3},
	{Height: 40, MinGap: 9, MaxGap: 36, MinWall: 2, Wavelength: 64, Swing: 9, Ramp: 96, Octaves: 4},
	{Height: 18, MinGap: 9, MaxGap: 36, MinWall: 2, Wavelength: 64, Swing: 9, Ramp: 96, Octaves: 4},
	{Height: 120, MinGap: 9, MaxGap: 36, MinWall: 2, Wavelength: 64, Swing: 9, Detail: 2, Ramp: 0, Octaves: MaxOctaves},
	{Height: 13, MinGap: 5, MaxGap: 40, MinWall: 1, Wavelength: 8, Detail: 1, Ramp: 3, Octaves: 3},
}

func FuzzAColumnKeepsItsWallsAndGap(f *testing.F) {
	for seed, s := range shapes {
		f.Add(uint64(seed), s.Height, s.MinGap, s.MaxGap, s.MinWall, s.Wavelength, s.Swing, s.Detail, s.Ramp, s.Octaves, int64(0), 0, 0)
		f.Add(uint64(seed), s.Height, s.MinGap, s.MaxGap, s.MinWall, s.Wavelength, s.Swing, s.Detail, s.Ramp, s.Octaves, int64(0), s.MinGap*(seed%2), 8)
	}
	f.Add(^uint64(0), 40, 8, 22, 2, MaxWavelength, 1, 1, 0, 0, int64(1)<<40, 0, 0)
	f.Add(^uint64(0), 40, 8, 1<<20, 2, MaxWavelength, 1, 1, 0, MaxOctaves, int64(1)<<40, 0, 1)
	f.Fuzz(func(t *testing.T, seed uint64, height, minGap, maxGap, minWall, wavelength, swing, detail, ramp, octaves int, from int64, openGap, openRamp int) {
		c := Config{Seed: seed, Height: height, MinGap: minGap, MaxGap: maxGap, MinWall: minWall,
			Wavelength: wavelength, Swing: swing, Detail: detail, Ramp: ramp, Octaves: octaves}
		if openRamp != 0 {
			c.Open = Opening{At: from + int64(wavelength), Gap: openGap, Ramp: openRamp}
		}
		if c.Validate() != nil || height > 1<<12 || ramp > 1<<20 || openRamp > 1<<20 || from < 0 || from > 1<<50 {
			t.Skip()
		}
		for i := from; i < from+int64(4*wavelength+ramp+openRamp+8); i++ {
			col := c.At(i)
			if gap := height - col.Top - col.Bottom; col.Top < minWall || col.Bottom < minWall || gap < minGap {
				t.Fatalf("%+v column %d = %+v: walls under %d or gap %d under %d", c, i, col, minWall, gap, minGap)
			}
		}
	})
}

// An opening leaves every column up to its own as the cave had it, and past its ramp
// holds its gap centred: all the walls leave, or the gap it names
func TestAnOpeningKeepsTheCaveBeforeItAndHoldsItsGapPast(t *testing.T) {
	for _, s := range shapes {
		for _, gap := range []int{0, s.MinGap} {
			s.Seed = 7
			c := s
			c.Open = Opening{At: int64(s.Ramp) + 40, Gap: gap, Ramp: 16}
			if err := c.Validate(); err != nil {
				t.Fatalf("%+v: %v", c, err)
			}
			want := cmp.Or(gap, s.Height-2*s.MinWall)
			for i := range c.Open.At + 80 {
				col := c.At(i)
				if i <= c.Open.At && col != s.At(i) ||
					i >= c.Open.At+16 && (col.Top != (s.Height-want)/2 || s.Height-col.Top-col.Bottom != want) {
					t.Fatalf("%+v column %d = %+v, the cave's %+v, want a gap of %d past column %d", c, i, col, s.At(i), want, c.Open.At+16)
				}
			}
		}
	}
}

// A snaking wall's bound is the smoothstep's steepest slope over each channel's
// swing, plus a row for each floor taken
func TestAWallClimbsNoSteeperThanItsWavelengthAllows(t *testing.T) {
	for _, s := range shapes {
		if s.Octaves > 0 {
			continue
		}
		gapSwing := s.MaxGap - s.MinGap - 2*s.Detail
		centreSwing := s.Height - 2*s.MinWall - s.MinGap - 2*s.Detail
		if s.Swing > 0 {
			centreSwing = min(centreSwing, 2*s.Swing)
		}
		bound := (3*(gapSwing+centreSwing)+2*s.Wavelength-1)/(2*s.Wavelength) + 2
		if s.Detail > 0 {
			bound += (3*(s.Detail+1)+2*roughWavelength-1)/(2*roughWavelength) + 1
		}
		for seed := range uint64(64) {
			s.Seed = seed
			prev := s.At(int64(s.Ramp))
			for i := int64(s.Ramp) + 1; i < int64(s.Ramp+6*s.Wavelength); i++ {
				col := s.At(i)
				if d := max(abs(col.Top-prev.Top), abs(col.Bottom-prev.Bottom)); d > bound {
					t.Fatalf("%+v: column %d steps %d rows, bound %d", s, i, d, bound)
				}
				prev = col
			}
		}
	}
}

// A ship of a row, and a row to spare, passes from every column into the next
func TestEveryCaveCanBeFlown(t *testing.T) {
	const ship = 1
	for _, s := range shapes {
		for seed := range uint64(64) {
			s.Seed = seed
			prev := s.At(0)
			for i := int64(1); i < 4000; i++ {
				col := s.At(i)
				if overlap := min(s.Height-prev.Bottom, s.Height-col.Bottom) - max(prev.Top, col.Top); overlap < ship+1 {
					t.Fatalf("%+v: columns %d and %d share %d open rows", s, i-1, i, overlap)
				}
				prev = col
			}
		}
	}
}

// Over a long run a jagged cave's gap reaches both ends of its range, its rough
// rock aside: the min gap where both walls reach in, all the walls leave where
// neither does
func TestAJaggedCaveSpansItsGapRange(t *testing.T) {
	for _, s := range shapes {
		if s.Octaves == 0 {
			continue
		}
		for seed := range uint64(16) {
			s.Seed = seed
			narrowest, widest := s.Height, 0
			for i := int64(s.Ramp); i < int64(s.Ramp)+4000; i++ {
				col := s.At(i)
				narrowest, widest = min(narrowest, s.Height-col.Top-col.Bottom), max(widest, s.Height-col.Top-col.Bottom)
			}
			if narrowest > s.MinGap+2*s.Detail || widest < s.widest()-2*s.Detail {
				t.Fatalf("%+v: gaps from %d to %d, want %d to %d", s, narrowest, widest, s.MinGap, s.widest())
			}
		}
	}
}

// A jagged wall's median leaves next to no bump or notch of jaggedMedian columns or
// fewer where its noise holds none, its finest octave wider than the median and no
// rough rock laid: a wall's floor flickers so where its noise turns, about one run in
// forty, and the median leaves under one in five thousand
func TestAJaggedWallHoldsNoNarrowBump(t *testing.T) {
	for _, s := range shapes {
		if s.Octaves == 0 || s.Detail > 0 || s.Wavelength>>(s.Octaves-1) <= 2*jaggedMedian {
			continue
		}
		runs, narrow := 0, 0
		for seed := range uint64(64) {
			s.Seed = seed
			var tops, bottoms []int
			for i := range int64(4000) {
				col := s.At(i)
				tops, bottoms = append(tops, col.Top), append(bottoms, col.Bottom)
			}
			for _, wall := range [][]int{tops, bottoms} {
				// prev, run and length are the run before this one, this one and its length
				prev, run, length := -1, wall[0], 0
				for _, v := range wall {
					if v == run {
						length++
						continue
					}
					if prev >= 0 {
						runs++
						if (run > prev) == (run > v) && length <= jaggedMedian {
							narrow++
						}
					}
					prev, run, length = run, v, 1
				}
			}
		}
		if narrow*5000 > runs {
			t.Fatalf("%+v: %d of %d wall runs are a bump or notch of %d columns or fewer", s, narrow, runs, jaggedMedian)
		}
	}
}

// The digest pins the profile itself: a change to it moves every recorded cave
func TestAColumnIsFixedByItsIndexAndSeed(t *testing.T) {
	h := fnv.New64a()
	for seed, s := range shapes {
		s.Seed = uint64(seed) * 0x9e3779b97f4a7c15
		for i := range int64(500) {
			col := s.At(i)
			b := strconv.AppendInt(nil, int64(col.Top), 10)
			h.Write(append(strconv.AppendInt(append(b, ','), int64(col.Bottom), 10), ';'))
		}
	}
	if got, want := h.Sum64(), uint64(0xde3d9f58df3fc478); got != want {
		t.Fatalf("profile digest %#x, want %#x", got, want)
	}
}

func abs(v int) int { return max(v, -v) }
