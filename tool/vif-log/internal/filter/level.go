package filter

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/logfile"
)

func init() {
	Register(Desc{Kind: "level", Help: "level set (IWE) or threshold (>=WARN)", New: newLevel})
}

// Level filters by a bitmask over logfile.Level. Threshold mode is a mutation
// of the mask, not a second representation.
type Level struct{ Mask uint16 }

// NewLevel returns a level filter with every level enabled.
func NewLevel() *Level { return &Level{Mask: (1 << uint(logfile.LevelCount)) - 1} }

func newLevel(arg string) (Filter, error) {
	f := NewLevel()
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return f, nil
	}
	if t, ok := strings.CutPrefix(arg, ">="); ok {
		l, found := levelByName(strings.TrimSpace(t))
		if !found {
			return nil, fmt.Errorf("filter/level: unknown level %q", t)
		}
		f.Threshold(l)
		return f, nil
	}
	f.Mask = 0
	for i := 0; i < len(arg); i++ {
		l, ok := logfile.LevelByInitial(arg[i] &^ 0x20)
		if !ok {
			return nil, fmt.Errorf("filter/level: unknown level initial %q", arg[i])
		}
		f.Mask |= 1 << uint(l)
	}
	return f, nil
}

func levelByName(s string) (logfile.Level, bool) {
	s = strings.ToUpper(s)
	for i := logfile.Level(0); i < logfile.LevelCount; i++ {
		if i.String() == s {
			return i, true
		}
	}
	return logfile.LevelBad, false
}

func (f *Level) Kind() string      { return "level" }
func (f *Level) Needs() Need       { return 0 }
func (f *Level) Match(c *Ctx) bool { return f.Mask&(1<<uint(c.Meta.Lvl)) != 0 }

// Has reports whether level l passes.
func (f *Level) Has(l logfile.Level) bool { return f.Mask&(1<<uint(l)) != 0 }

// Toggle flips one level.
func (f *Level) Toggle(l logfile.Level) { f.Mask ^= 1 << uint(l) }

// SetAll enables or disables every level.
func (f *Level) SetAll(on bool) {
	if on {
		f.Mask = (1 << uint(logfile.LevelCount)) - 1
	} else {
		f.Mask = 0
	}
}

// Threshold enables the ordered levels at or above l, leaving PROC and BAD alone.
func (f *Level) Threshold(l logfile.Level) {
	for i := range logfile.Level(logfile.OrderedCount) {
		if i >= l {
			f.Mask |= 1 << uint(i)
		} else {
			f.Mask &^= 1 << uint(i)
		}
	}
}

// ThresholdToggle applies the threshold at l, or restores every ordered level
// when the mask already has exactly that shape. Digit keys use this: 2 hides
// TRACE, 2 again brings it back.
func (f *Level) ThresholdToggle(l logfile.Level) {
	if f.isThreshold(l) {
		for i := range logfile.Level(logfile.OrderedCount) {
			f.Mask |= 1 << uint(i)
		}
		return
	}
	f.Threshold(l)
}

// isThreshold reports whether the ordered levels are exactly those at or above l.
func (f *Level) isThreshold(l logfile.Level) bool {
	for i := range logfile.Level(logfile.OrderedCount) {
		if f.Has(i) != (i >= l) {
			return false
		}
	}
	return true
}

// AllOn reports whether no level is filtered out.
func (f *Level) AllOn() bool {
	return f.Mask&((1<<uint(logfile.LevelCount))-1) == (1<<uint(logfile.LevelCount))-1
}

// Shift moves the threshold by d, clamped to the ordered range.
func (f *Level) Shift(d int) {
	t := int(f.LowestOrdered())
	t += d
	if t < 0 {
		t = 0
	}
	if t >= logfile.OrderedCount {
		t = logfile.OrderedCount - 1
	}
	f.Threshold(logfile.Level(t))
}

// LowestOrdered returns the lowest enabled ordered level, ERROR if none.
func (f *Level) LowestOrdered() logfile.Level {
	for i := logfile.Level(0); i < logfile.Level(logfile.OrderedCount); i++ {
		if f.Has(i) {
			return i
		}
	}
	return logfile.LevelError
}

// Label reports the level filter only when it hides something; the header
// strip shows the per-level state.
func (f *Level) Label() string {
	if f.AllOn() {
		return ""
	}
	if l, ok := f.thresholdShape(); ok {
		return "lvl>=" + l.String()
	}
	var b strings.Builder
	b.WriteString("lvl:")
	for i := range logfile.LevelCount {
		if f.Has(i) {
			b.WriteByte(i.Initial())
		}
	}
	return b.String()
}

// thresholdShape reports whether the mask is "ordered >= t, plus PROC and BAD".
func (f *Level) thresholdShape() (logfile.Level, bool) {
	if !f.Has(logfile.LevelProc) || !f.Has(logfile.LevelBad) {
		return 0, false
	}
	t := f.LowestOrdered()
	if t == 0 || !f.isThreshold(t) {
		return 0, false
	}
	return t, true
}
