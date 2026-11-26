//go:build probe

package app

import (
	"fmt"
	"testing"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// TestProbeFeatherTouches counts bodies that reach the drawn shield, out to its
// feather's end, and how many of those visits reach its radius, where a touch counts
func TestProbeFeatherTouches(t *testing.T) {
	for _, weapons := range []bool{false, true} {
		a := newMagicOn(t, 160, 50)
		w := a.World()
		layMagic(t, a)
		cursor := w.Resources.Player.Slot(0)
		w.RunSafe(func() {
			w.PushLocal(event.EventEnergySetRequest, &event.EnergySetPayload{Entity: cursor, Value: 100000, Weapons: weapons})
		})
		a.Tick(1)
		type visit struct{ in, counted bool }
		visits := map[core.Entity]*visit{}
		var drawn, counted int
		for range 3600 {
			a.Tick(1)
			w.RunSafe(func() {
				shield, _ := w.Components.Shield.GetPtr(cursor)
				at, _ := w.Positions.GetPosition(cursor)
				for _, e := range w.Components.Behavior.Entities() {
					h, ok := w.Components.Header.GetPtr(e)
					if !ok {
						continue
					}
					inDrawn, inReach := false, false
					for _, m := range h.MemberEntries {
						p, ok := w.Positions.GetPosition(m.Entity)
						if !ok {
							continue
						}
						dy := w.Resources.Config.CellAspect.Circular(float64(p.Y - at.Y))
						r := shield.Radius
						inDrawn = inDrawn || vmath.CircleContains(float64(p.X-at.X), dy, r*r*parameter.ShieldFeatherEndRatio*parameter.ShieldFeatherEndRatio)
						inReach = inReach || vmath.CircleContains(float64(p.X-at.X), dy, r*r)
					}
					v := visits[e]
					if v == nil {
						v = &visit{}
						visits[e] = v
					}
					if inDrawn && !v.in {
						v.in, v.counted = true, false
						drawn++
					}
					if inReach && v.in && !v.counted {
						v.counted = true
						counted++
					}
					if !inDrawn {
						v.in = false
					}
				}
			})
		}
		fmt.Printf("weapons %v: visits to the drawn shield %d, of which reached its radius %d\n", weapons, drawn, counted)
	}
}
