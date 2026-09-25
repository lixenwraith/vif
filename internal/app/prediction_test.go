package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
)

// zeroHitPoints is the death every species system derives from, credited to one
// cursor and applied directly so the scenario is the correction path rather than
// the combat pipeline.
func zeroHitPoints(a *App, e, killer core.Entity) {
	a.World().RunSafe(func() {
		if c, ok := a.World().Components.Combat.GetPtr(e); ok {
			c.HitPoints = 0
			c.LastDamagedBy = killer
		}
	})
}

// boostGranted is the reward a kill pays its cursor. TotalDuration only grows while
// a boost is held, so it counts the rewards: one activation, or one and an extension.
func boostGranted(a *App, cursor core.Entity) (total time.Duration) {
	a.World().RunSafe(func() {
		if b, ok := a.World().Components.Boost.GetPtr(cursor); ok {
			total = b.TotalDuration
		}
	})
	return total
}

// alive reports whether one shared species instance is still in the world.
func alive(a *App, e core.Entity) (ok bool) {
	a.World().RunSafe(func() { ok = a.World().Components.Combat.HasEntity(e) })
	return ok
}

// TestAPredictedSharedDeathRewardsItsPlayerOnce is the rule the prediction ledger
// exists for. A guest kills a shared species, a correction published before the
// authority saw it restores the species, and the guest kills it again — so the
// shared derivation runs twice by design, and the player-domain reward behind it
// must still be paid exactly once, when the authority's own world proves the death.
func TestAPredictedSharedDeathRewardsItsPlayerOnce(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x9EAD1EDBE, 2, [][2]int{{1, 2}})
	host, guest := apps[0], apps[1]
	cursor := localCursors(t, apps)[1]
	advance := func() { tickAll(apps) }

	// One swarm, allocated on both instances at the agreed tick.
	host.Context().PushCrossing(event.EventSwarmSpawnRequest,
		&event.SwarmSpawnRequestPayload{X: 40, Y: 20})
	for range parameter.NetworkBarrierDelayTicks + 2 {
		advance()
	}
	var header core.Entity
	guest.World().RunSafe(func() {
		if e := guest.World().Components.Swarm.Entities(); len(e) > 0 {
			header = e[0]
		}
	})
	if header == 0 {
		t.Fatal("the crossed swarm never reached the guest")
	}
	if boostGranted(guest, cursor) != 0 {
		t.Fatal("the cursor was already carrying a boost before the kill")
	}

	var predicted, confirmed int
	guest.SetDispatchTap(func(ev event.GameEvent) {
		switch ev.Type {
		case event.EventSpeciesKilled:
			if ev.Phase == event.PhasePredicted {
				predicted++
			}
		case event.EventSpeciesKillConfirmed:
			confirmed++
		}
	})
	defer guest.SetDispatchTap(nil)

	// A species system raises the death in its own Update and the queue dispatches
	// it at the next tick's settle, so one derivation takes two ticks.
	derive := func() { advance(); advance() }

	// The guest predicts the death; the authority still holds the swarm.
	zeroHitPoints(guest, header, cursor)
	derive()
	if predicted == 0 {
		t.Fatal("the guest never derived the death it predicted")
	}
	if confirmed != 0 {
		t.Fatalf("a prediction was rewarded before any authority proved it (%d)", confirmed)
	}

	// The correction restores the species, and the guest kills it again.
	deliverCorrectionNow(t, host, []*App{guest}, advance)
	if !alive(guest, header) {
		t.Fatal("the correction did not restore the species the guest had killed")
	}
	zeroHitPoints(guest, header, cursor)
	derive()
	if predicted < 2 {
		t.Fatalf("the rollback produced %d derivations, want the two the bug is made of", predicted)
	}
	if confirmed != 0 {
		t.Fatalf("a re-derivation was rewarded (%d) before any authority proved it", confirmed)
	}

	// The authority kills it too, so its next world proves the death.
	zeroHitPoints(host, header, 0)
	derive()
	deliverCorrectionNow(t, host, []*App{guest}, advance)
	advance() // the install queues the confirmation; the next settle dispatches it
	if confirmed != 1 {
		t.Fatalf("%d confirmations for one death, want exactly 1 (%d derivations)", confirmed, predicted)
	}
	if got, want := boostGranted(guest, cursor), parameter.BoostBaseDuration; got != want {
		t.Fatalf("the kill granted %s of boost, want the %s one reward pays", got, want)
	}
}

// TestAPointerSweepCrossesOncePerTick is the pointer's half of D-18: the view
// follows every cell the pointer names, the shared world gets the newest one a tick
// as a single placement, and a key move made after it still lands after it. A report
// on the cell the cursor is already bound for is no placement at all.
func TestAPointerSweepCrossesOncePerTick(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 100, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	var cursor core.Entity
	var fromX, fromY int
	a.World().RunSafe(func() {
		cursor = a.World().Resources.Player.Entity
		pos, _ := a.World().LocalCursor()
		fromX, fromY = pos.X, pos.Y
	})
	report := func(x, y int) {
		var tx, ty int
		a.World().RunSafe(func() {
			cfg := a.World().Resources.Config
			ox, oy := cfg.MapOffset()
			tx = a.Context().GameXOffset + x - cfg.CameraX + ox
			ty = a.Context().GameYOffset + y - cfg.CameraY + oy
		})
		a.handleIntent(&input.Intent{Type: input.IntentMouseMove, Count: tx, Char: rune(ty)})
	}
	view := func() (pos component.PositionComponent) {
		a.World().RunSafe(func() { pos, _ = a.World().CursorCell(cursor) })
		return pos
	}
	var moves int
	a.SetDispatchTap(func(ev event.GameEvent) {
		if ev.Type == event.EventCursorMoveRequest {
			moves++
		}
	})

	before := a.pushed()
	for dx := 1; dx <= 5; dx++ {
		report(fromX+dx, fromY)
		if pos := view(); pos.X != fromX+dx {
			t.Fatalf("view on column %d after the pointer named %d", pos.X, fromX+dx)
		}
	}
	if n := a.pushed() - before; n != 0 {
		t.Fatalf("a sweep between two ticks pushed %d events before the tick", n)
	}
	a.Tick(1)
	if moves != 1 || cursorPosition(a, cursor).X != fromX+5 {
		t.Fatalf("the tick applied %d placements, store column %d; want one, to %d",
			moves, cursorPosition(a, cursor).X, fromX+5)
	}

	report(fromX+5, fromY)
	a.Tick(1)
	if moves != 1 {
		t.Fatalf("a report on the cell the cursor holds placed it again: %d placements", moves)
	}

	report(fromX+6, fromY)
	a.World().RunSafe(func() { a.World().PushCursorMove(cursor, fromX+9, fromY) })
	a.Settle()
	a.Tick(1)
	if moves != 3 || cursorPosition(a, cursor).X != fromX+9 {
		t.Fatalf("pointer then key applied %d placements in all, store column %d; want 3, ending at %d",
			moves, cursorPosition(a, cursor).X, fromX+9)
	}
}

// TestASweepOutlastsItsRingAndACorrection is D-18 across the session path: a
// sustained sweep of key moves keeps more cells in flight than the ring holds, and
// the view stays on the newest cell through every tick and a correction installed
// part-way; once the sweep has landed the queue is empty.
func TestASweepOutlastsItsRingAndACorrection(t *testing.T) {
	t.Parallel()
	apps := meshSession(t, 0x5EEDBEEF, 2, [][2]int{{1, 2}})
	host, guest := apps[0], apps[1]
	cursor := localCursors(t, apps)[1]
	tickAll(apps)

	// Enough per tick that one lead of the sweep is more than the ring holds
	perTick := parameter.MaxPredictedCursorCells/parameter.NetworkBarrierDelayTicks + 8
	var lastX, lastY, n int
	sweep := func() {
		guest.World().RunSafe(func() {
			for range perTick {
				lastX, lastY = 10+n%40, 5+(n/40)%20
				guest.World().PushCursorMove(cursor, lastX, lastY)
				n++
			}
		})
	}
	view := func(stage string) {
		t.Helper()
		var pos component.PositionComponent
		guest.World().RunSafe(func() { pos, _ = guest.World().CursorCell(cursor) })
		if pos.X != lastX || pos.Y != lastY {
			t.Fatalf("%s: view on (%d,%d), want the newest cell (%d,%d)", stage, pos.X, pos.Y, lastX, lastY)
		}
	}
	advance := func() { sweep(); tickAll(apps); view(fmt.Sprintf("after %d cells", n)) }

	for range 2 * parameter.NetworkBarrierDelayTicks {
		advance()
	}
	deliverCorrectionNow(t, host, apps[1:], advance)
	view("the correction")
	for range parameter.NetworkBarrierDelayTicks {
		advance()
	}
	for range parameter.NetworkBarrierDelayTicks + 1 {
		tickAll(apps)
	}
	if pos := cursorPosition(guest, cursor); pos.X != lastX || pos.Y != lastY {
		t.Fatalf("store on (%d,%d) after the sweep landed, want (%d,%d)", pos.X, pos.Y, lastX, lastY)
	}
	var depth int
	guest.World().RunSafe(func() { depth = guest.World().Resources.Player.PredictedDepth() })
	if depth != 0 {
		t.Fatalf("the queue holds %d cells after the whole sweep landed", depth)
	}
}
