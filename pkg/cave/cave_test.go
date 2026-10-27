package cave

import (
	"hash/fnv"
	"strconv"
	"testing"
)

// shapes are the configurations the property tests sweep: a terminal's cave, a
// short and rough one, a long gentle one and a tight one at its limits
var shapes = []Config{
	{Height: 40, MinGap: 8, MaxGap: 22, MinWall: 2, Wavelength: 48, Detail: 1, Ramp: 80},
	{Height: 24, MinGap: 6, MaxGap: 12, MinWall: 1, Wavelength: 16, Detail: 2, Ramp: 10},
	{Height: 60, MinGap: 12, MaxGap: 40, MinWall: 3, Wavelength: 200, Swing: 9, Detail: 0, Ramp: 0},
	{Height: 7, MinGap: 3, MaxGap: 5, MinWall: 1, Wavelength: 1, Swing: 40, Detail: 1, Ramp: 3},
}

func FuzzAColumnKeepsItsWallsAndGap(f *testing.F) {
	for seed, s := range shapes {
		f.Add(uint64(seed), s.Height, s.MinGap, s.MaxGap, s.MinWall, s.Wavelength, s.Swing, s.Detail, s.Ramp, int64(0))
	}
	f.Add(^uint64(0), 40, 8, 22, 2, MaxWavelength, 1, 1, 0, int64(1)<<40)
	f.Fuzz(func(t *testing.T, seed uint64, height, minGap, maxGap, minWall, wavelength, swing, detail, ramp int, from int64) {
		c := Config{Seed: seed, Height: height, MinGap: minGap, MaxGap: maxGap, MinWall: minWall,
			Wavelength: wavelength, Swing: swing, Detail: detail, Ramp: ramp}
		if c.Validate() != nil || height > 1<<12 || ramp > 1<<20 || from < 0 || from > 1<<50 {
			t.Skip()
		}
		for i := from; i < from+int64(4*wavelength+ramp+8); i++ {
			col := c.At(i)
			if gap := height - col.Top - col.Bottom; col.Top < minWall || col.Bottom < minWall || gap < minGap {
				t.Fatalf("%+v column %d = %+v: walls under %d or gap %d under %d", c, i, col, minWall, gap, minGap)
			}
		}
	})
}

// The bound is the smoothstep's steepest slope over each channel's swing, plus a
// row for each floor taken
func TestAWallClimbsNoSteeperThanItsWavelengthAllows(t *testing.T) {
	for _, s := range shapes {
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
	if got, want := h.Sum64(), uint64(0x93cfdbca2fe01ab8); got != want {
		t.Fatalf("profile digest %#x, want %#x", got, want)
	}
}

func abs(v int) int { return max(v, -v) }
