package bank

import (
	"math"
	"slices"
	"testing"

	"github.com/lixenwraith/audio"
)

func loadSounds(t *testing.T) []*audio.SoundDef {
	t.Helper()
	bank, err := Sounds()
	if err != nil {
		t.Fatalf("sounds: %v", err)
	}
	if len(bank.Sound) == 0 || len(bank.Voice) == 0 {
		t.Fatal("the bank is missing its sounds or voices")
	}
	return bank.Sound
}

// A group draws both slots at every tier, with a second member each to vary to, so a
// tier change or a group change never leaves a slot silent or stuck on one pattern.
func TestGroupsCoverEveryTier(t *testing.T) {
	pats, err := Patterns()
	if err != nil {
		t.Fatalf("patterns: %v", err)
	}
	groups := map[string]bool{}
	for _, p := range pats {
		for _, g := range p.Groups {
			groups[g] = true
		}
	}
	if len(groups) < 2 {
		t.Fatalf("the bank names %d groups, want several to move between", len(groups))
	}
	for g := range groups {
		for tier := range audio.IntensityCount {
			var n [2]int
			for _, p := range pats {
				in := len(p.Groups) == 0 || slices.Contains(p.Groups, g)
				if in && p.Tiers&(1<<tier) != 0 && (p.Role == audio.RoleRhythm || p.Role == audio.RoleMelody) {
					n[p.Role-audio.RoleRhythm]++
				}
			}
			if n[0] < 2 || n[1] < 2 {
				t.Errorf("group %s at %s draws from %d rhythms and %d melodies, want two of each",
					g, tier, n[0], n[1])
			}
		}
	}
}

func checkBuffer(t *testing.T, name string, buf []float64) float64 {
	t.Helper()
	var peak float64
	for _, v := range buf {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			t.Fatalf("%s: non-finite sample", name)
		}
		if a := math.Abs(v); a > peak {
			peak = a
		}
	}
	return peak
}

func TestSoundsRender(t *testing.T) {
	for _, d := range loadSounds(t) {
		buf, err := audio.RenderPreview(d, audio.SoundShape{})
		if err != nil {
			t.Fatalf("%s: %v", d.Name, err)
		}
		peak := checkBuffer(t, d.Name, buf)
		if peak < 0.01 {
			t.Errorf("%s: renders silent (peak %g)", d.Name, peak)
		}
		if peak > 1.0 {
			t.Errorf("%s: peak %g exceeds unity", d.Name, peak)
		}
	}
}

// A failure here usually means the encoder lost float precision, not that the
// spec model is wrong.
func TestSoundsRoundTrip(t *testing.T) {
	defs := loadSounds(t)
	data, err := audio.MarshalSounds(defs)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	bank, err := audio.LoadSoundsTOML(data)
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, data)
	}
	back := bank.Sound
	if len(back) != len(defs) {
		t.Fatalf("round trip produced %d sounds, want %d", len(back), len(defs))
	}
	for i := range defs {
		a, err := audio.RenderPreview(defs[i], audio.SoundShape{})
		if err != nil {
			t.Fatal(err)
		}
		b, err := audio.RenderPreview(back[i], audio.SoundShape{})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(a, b) {
			t.Errorf("%s: audio differs after round trip", defs[i].Name)
		}
	}
}
