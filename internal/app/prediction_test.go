package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/input"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
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

// TestPointerMovesOnlyToANewCell is the pointer's half of D-18: a report on the cell
// the cursor is already bound for is no move and no action, and one naming the cell
// it is leaving is a move. The store lags the prediction until the move settles, so
// both answers are read from the prediction.
func TestPointerMovesOnlyToANewCell(t *testing.T) {
	t.Parallel()
	a := mustHeadless(t, fixtureSeed, 100, 40)
	defer a.Close()
	tickUntilCursor(t, a)

	var from, to [2]int
	a.World().RunSafe(func() {
		pos, _ := a.World().LocalCursor()
		from, to = [2]int{pos.X, pos.Y}, [2]int{pos.X + 2, pos.Y}
	})
	// Reports arrive between settles, as a terminal delivers them inside one frame.
	report := func(cell [2]int) {
		var tx, ty int
		a.World().RunSafe(func() {
			cfg := a.World().Resources.Config
			ox, oy := cfg.MapOffset()
			tx = a.Context().GameXOffset + cell[0] - cfg.CameraX + ox
			ty = a.Context().GameYOffset + cell[1] - cfg.CameraY + oy
		})
		a.handleIntent(&input.Intent{Type: input.IntentMouseMove, Count: tx, Char: rune(ty)})
	}

	before := a.pushed()
	report(to)
	report(to)
	if n := a.pushed() - before; n != 1 {
		t.Fatalf("two reports on one cell pushed %d events, want one move", n)
	}
	report(from)
	if n := a.pushed() - before; n != 2 {
		t.Fatalf("a report on the cell being left pushed %d events in all, want a second move", n-1)
	}

	a.Settle()
	a.World().RunSafe(func() {
		if pos, _ := a.World().Positions.GetPosition(a.World().Resources.Player.Entity); pos.X != from[0] || pos.Y != from[1] {
			t.Fatalf("cursor settled on (%d,%d), want the last reported cell %v", pos.X, pos.Y, from)
		}
	})
}

// TestAPointerSweepOutlastsItsRingAndACorrection is D-18 across the session path: a
// sustained sweep keeps more cells in flight than the ring holds, as a pointer does
// at over a dozen cells a tick, and the view stays on the newest cell through every tick
// and a correction installed part-way; once the sweep has landed the queue is empty.
func TestAPointerSweepOutlastsItsRingAndACorrection(t *testing.T) {
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
				guest.World().PushPointerMove(cursor, lastX, lastY)
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
