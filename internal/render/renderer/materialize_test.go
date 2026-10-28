package renderer

import (
	"math"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/parameter"
)

// A beam's lit segment never falls to black and never snaps: every cell holds at
// least the tail floor (the pulsing head cells their trough of it), and across the
// fill-to-hold and hold-to-recede boundaries no cell outside the head jumps, at
// each style's own duration and at a request's shorter or longer one.
func TestMaterializeBeamKeepsItsGradientAcrossPhases(t *testing.T) {
	const distance = 24
	styles := component.MaterializeStyles[:]
	styles = append(styles,
		timedStyle(component.MaterializeStyles[component.SpawnTypeSwarm], 250*time.Millisecond),
		timedStyle(component.MaterializeStyles[component.SpawnTypeNugget], 2*time.Second))
	for spawn, style := range styles {
		profile := func(elapsed time.Duration) map[int]float64 {
			tail, head := beamSpan(style, elapsed, distance)
			cells := map[int]float64{}
			for offset := tail; offset <= head; offset++ {
				cells[offset] = beamIntensity(style, elapsed, offset, tail, head)
			}
			return cells
		}
		for elapsed := time.Duration(0); elapsed < style.Duration(); elapsed += parameter.GameUpdateInterval {
			_, head := beamSpan(style, elapsed, distance)
			for offset, v := range profile(elapsed) {
				floor := parameter.MaterializeTailIntensity
				if elapsed < style.Fill && offset >= head-2 {
					floor *= 0.6
				}
				if v < floor-1e-9 || v > 1+1e-9 {
					t.Fatalf("%v at %v: cell %d intensity %.3f outside [%.2f, 1]", spawn, elapsed, offset, v, floor)
				}
			}
		}
		lastFill, hold := profile(style.Fill-time.Nanosecond), profile(style.Fill)
		for offset, before := range lastFill {
			if offset >= distance-3 {
				continue
			}
			if after, ok := hold[offset]; !ok || math.Abs(after-before) > 0.2 {
				t.Fatalf("%v: the hold snapped cell %d from %.2f to %.2f", spawn, offset, before, after)
			}
		}
		lastHold, recede := profile(style.Fill+style.Hold-time.Nanosecond), profile(style.Fill+style.Hold)
		for offset, before := range lastHold {
			if after, ok := recede[offset]; !ok || after != before {
				t.Fatalf("%v: the recede snapped cell %d from %.2f to %.2f", spawn, offset, before, after)
			}
		}
	}
}

// A materialize timed off its style's duration spreads the style's fill, hold and
// recede over its own in the style's proportions, the three summing to it; one
// timed 0 keeps its style
func TestATimedMaterializeKeepsItsStylesProportions(t *testing.T) {
	for spawn, style := range component.MaterializeStyles {
		for _, d := range []time.Duration{0, style.Duration(), 250 * time.Millisecond, 3 * time.Second} {
			got := timedStyle(style, d)
			if d == 0 {
				if got != style {
					t.Fatalf("%d timed 0: %+v, want its style %+v", spawn, got, style)
				}
				continue
			}
			share := float64(d) / float64(style.Duration())
			if got.Duration() != d ||
				math.Abs(float64(got.Fill)-float64(style.Fill)*share) > 1 ||
				math.Abs(float64(got.Hold)-float64(style.Hold)*share) > 1 {
				t.Fatalf("%d timed %v: %+v, want its %+v scaled by %.3f", spawn, d, got, style, share)
			}
		}
	}
}
