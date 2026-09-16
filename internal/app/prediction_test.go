package app

import (
	"testing"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
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
