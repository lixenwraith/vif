//go:build probe

package app

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestProbeTempestCarry reads how far round the square the tempest carries bodies:
// the clockwise degrees from where each body first stands to where it first
// touches the shield, with the tempest alone held on the middle column
func TestProbeTempestCarry(t *testing.T) {
	for _, screen := range [][2]int{{80, 24}, {130, 30}, {160, 50}} {
		a := newMagicOn(t, screen[0], screen[1])
		w := a.World()
		layMagic(t, a)
		cursor := w.Resources.Player.Slot(0)
		w.RunSafe(func() {
			w.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: cursor, Value: 100000})
		})
		start, last := map[core.Entity]float64{}, map[core.Entity]float64{}
		var carried []float64
		done := map[core.Entity]bool{}
		for range 2400 {
			w.RunSafe(func() {
				for _, e := range w.Components.Square.Entities() {
					sq, _ := w.Components.Square.GetPtr(e)
					sq.Lines = [component.SquareLines]uint8{}
					sq.Lines[component.SquareLines-1] = uint8(component.SquareTempest) + 1
				}
			})
			a.Tick(1)
			w.RunSafe(func() {
				at, _ := w.Positions.GetPosition(cursor)
				shield, _ := w.Components.Shield.GetPtr(cursor)
				angle := func(x, y int) float64 {
					return math.Atan2(w.Resources.Config.CellAspect.Circular(float64(y-at.Y)), float64(x-at.X))
				}
				for _, e := range w.Components.Behavior.Entities() {
					p, ok := w.Positions.GetPosition(e)
					if !ok || done[e] {
						continue
					}
					now := angle(p.X, p.Y)
					if _, seen := start[e]; !seen {
						start[e], last[e] = 0, now
					}
					step := now - last[e]
					for step > math.Pi {
						step -= 2 * math.Pi
					}
					for step <= -math.Pi {
						step += 2 * math.Pi
					}
					start[e] += step
					last[e] = now
					h, _ := w.Components.Header.GetPtr(e)
					for _, m := range h.MemberEntries {
						mp, ok := w.Positions.GetPosition(m.Entity)
						dy := w.Resources.Config.CellAspect.Circular(float64(mp.Y - at.Y))
						if ok && vmath.CircleContains(float64(mp.X-at.X), dy, shield.Radius*shield.Radius) {
							carried = append(carried, start[e]*180/math.Pi)
							done[e] = true
							break
						}
					}
				}
			})
		}
		slices.Sort(carried)
		in := 0
		for _, d := range carried {
			if d >= 45 && d <= 135 {
				in++
			}
		}
		med := 0.0
		if len(carried) > 0 {
			med = carried[len(carried)/2]
		}
		fmt.Printf("screen %dx%d: %d bodies touched of %d, median carried %.0f degrees, %d%% carried 45 to 135\n",
			screen[0], screen[1], len(carried), len(start), med, 100*in/max(len(carried), 1))
	}
}
