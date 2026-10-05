package system

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// armWeapon grants one charge of one weapon to one cursor and settles the grant.
func armWeapon(w *engine.World, weapon *WeaponSystem, cursor core.Entity, wt component.WeaponType) {
	weapon.HandleEvent(event.GameEvent{
		Type:    event.EventWeaponAddRequest,
		Payload: &event.WeaponAddRequestPayload{Entity: cursor, Weapon: wt},
	})
}

func TestFiveOrbsSpaceEvenlyAtEveryEdgeAndResumeTogether(t *testing.T) {
	for _, edge := range []vmath.Point{{X: 79, Y: 20}, {X: 0, Y: 20}, {X: 40, Y: 0}, {X: 40, Y: 39}, {X: 79, Y: 0}} {
		w, cursor, _ := testCursorWorld(t)
		w.SetupLevel(80, 40, false, false, false)
		w.Resources.Time.DeltaTime = 50 * time.Millisecond
		s := NewWeaponSystem(w).(*WeaponSystem)
		var orbs orbSlots
		for weapon := range component.WeaponCount {
			orbs[weapon] = s.spawnOrbEntity(cursor, component.WeaponType(weapon))
		}
		for _, point := range []vmath.Point{edge, {X: 40, Y: 20}} {
			w.Positions.SetPosition(cursor, component.PositionComponent{X: point.X, Y: point.Y})
			for range 40 {
				s.updateOrbs(cursor, 0, orbs)
			}
			if point != edge {
				orb, _ := w.Components.Orb.GetPtr(orbs[component.WeaponEmitter])
				orb.OrbitAngle += 0.1 // A small recovery offset must not persist in open space.
				s.updateOrbs(cursor, 0, orbs)
			}
			var angles []float64
			for _, e := range orbs {
				orb, _ := w.Components.Orb.GetPtr(e)
				pos, _ := w.Positions.GetPosition(e)
				if !w.Positions.IsPointValidForOrbit(pos.X, pos.Y, component.WallBlockKinetic) {
					t.Fatalf("edge %v: orb at blocked cell %+v", edge, pos)
				}
				angles = append(angles, orb.OrbitAngle)
			}
			if point == edge {
				samples := vmath.SampleEllipseGridF(point.X, point.Y, parameter.OrbOrbitRadiusX, parameter.OrbOrbitRadiusY, vmath.EllipseSampleCount)
				blocked := make([]bool, len(samples))
				for i, p := range samples {
					blocked[i] = !w.Positions.IsPointValidForOrbit(p[0], p[1], component.WallBlockKinetic)
				}
				arc := vmath.FindUnblockedArcsF(blocked)[0]
				for i := range angles {
					angles[i] = vmath.NormalizeAngleF(angles[i] - arc.StartAngle)
				}
				slices.Sort(angles)
				for i := 1; i < len(angles); i++ {
					if math.Abs(angles[i]-angles[i-1]-arc.Length/float64(len(angles))) > 1e-8 {
						t.Fatalf("edge %v: uneven angles %v", edge, angles)
					}
				}
			} else {
				slices.Sort(angles)
				for i := range angles {
					gap := vmath.NormalizeAngleF(angles[(i+1)%len(angles)] - angles[i])
					if math.Abs(gap-vmath.TwoPi/float64(len(angles))) > 1e-8 {
						t.Fatalf("after edge %v: uneven free orbit %v", edge, angles)
					}
				}
			}
		}
	}
}

// orbsOwnedBy counts one cursor's orbs per weapon type straight from the store.
func orbsOwnedBy(w *engine.World, cursor core.Entity) [component.WeaponCount]int {
	var out [component.WeaponCount]int
	for _, e := range w.Components.Orb.Entities() {
		orb, ok := w.Components.Orb.GetComponent(e)
		if !ok || orb.OwnerEntity != cursor {
			continue
		}
		if orb.WeaponType >= 0 && orb.WeaponType < component.WeaponCount {
			out[orb.WeaponType]++
		}
	}
	return out
}

// settleDeaths runs the death system over whatever the weapon system emitted, so a
// reaped orb is gone from the store rather than merely requested.
func settleDeaths(w *engine.World, deaths *DeathSystem) {
	for _, ev := range w.Resources.Event.Queue.Consume() {
		if ev.Type == event.EventDeathBatch {
			deaths.HandleEvent(ev)
		}
	}
}

// Corrections must recover existing orbs and retire duplicates or invalid owners.
func TestOrbsAreRecoveredFromTheStoreRatherThanDuplicated(t *testing.T) {
	w, cursor, other := testCursorWorld(t)
	weapon := NewWeaponSystem(w).(*WeaponSystem)
	deaths := NewDeathSystem(w).(*DeathSystem)

	armWeapon(w, weapon, cursor, component.WeaponRod)
	armWeapon(w, weapon, cursor, component.WeaponLauncher)
	w.Resources.Event.Queue.Consume()

	weapon.Update()
	settleDeaths(w, deaths)
	if got := orbsOwnedBy(w, cursor); got != [component.WeaponCount]int{1, 1, 0} {
		t.Fatalf("orbs after arming rod and launcher = %v, want one each", got)
	}

	// A second orb for a pair that already has one, the shape a lost reference used
	// to leave behind. The survivor is the older entity, not whichever the dense
	// store happens to hold first.
	kept := orbSlotsOf(w, cursor)[component.WeaponRod]
	duplicate := weapon.spawnOrbEntity(cursor, component.WeaponRod)
	if duplicate == 0 || duplicate == kept {
		t.Fatalf("duplicate orb = %d, want a second entity beside %d", duplicate, kept)
	}
	// An orb owned by a cursor this instance does not simulate (D-2), and one for a
	// weapon that was never charged.
	remote := spawnRemoteCursor(t, w, 2, 30, 5, 9)
	orphan := weapon.spawnOrbEntity(remote, component.WeaponRod)
	stale := weapon.spawnOrbEntity(cursor, component.WeaponDisruptor)
	if orphan == 0 || stale == 0 {
		t.Fatal("the injected orbs were not created; the reap proves nothing")
	}
	w.Resources.Event.Queue.Consume()

	weapon.Update()
	settleDeaths(w, deaths)

	if got := orbsOwnedBy(w, cursor); got != [component.WeaponCount]int{1, 1, 0} {
		t.Fatalf("orbs after the reap = %v, want one rod and one launcher", got)
	}
	if !w.Components.Orb.HasEntity(kept) {
		t.Fatalf("the reap dropped the surviving orb %d rather than its duplicate", kept)
	}
	for name, e := range map[string]core.Entity{"duplicate": duplicate, "remote-owned": orphan, "uncharged": stale} {
		if w.Components.Orb.HasEntity(e) {
			t.Errorf("%s orb %d survived the reap", name, e)
		}
		if w.Positions.HasPosition(e) {
			t.Errorf("%s orb %d still holds a placement", name, e)
		}
	}
	if orbsOwnedBy(w, other) != [component.WeaponCount]int{} {
		t.Error("the reap gave the unarmed cursor orbs")
	}
}

// orbSlotsOf reads one cursor's orb index through the system's own accessor.
func orbSlotsOf(w *engine.World, cursor core.Entity) orbSlots {
	return NewWeaponSystem(w).(*WeaponSystem).orbsOf(cursor)
}
