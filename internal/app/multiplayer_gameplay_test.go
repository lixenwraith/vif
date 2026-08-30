package app

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/component"
	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/mode"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
)

// TestSessionRosterStartsAndRestartsEveryParticipant captures the two places the
// monitor script used its single player_entity variable as if it described the
// whole roster: lobby admission and the gameplay-wide defeat reset.
func TestSessionRosterStartsAndRestartsEveryParticipant(t *testing.T) {
	apps := meshSession(t, 0xA2A2, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	assertArmed := func(phase string) {
		t.Helper()
		for i, a := range apps {
			var heat int
			var energy int64
			var count int
			a.World().RunSafe(func() {
				w := a.World()
				count = w.Resources.Player.Count()
				if c, ok := w.Components.Heat.GetComponent(local[i]); ok {
					heat = c.Current
				}
				if c, ok := w.Components.Energy.GetComponent(local[i]); ok {
					energy = c.Current
				}
			})
			if count != len(apps) || heat != 10 || energy != 100 {
				t.Fatalf("%s participant %d: roster=%d heat=%d energy=%d, want %d/10/100",
					phase, i+1, count, heat, energy, len(apps))
			}
		}
	}

	assertArmed("start")

	// The shared monitor guard is normally published by MetaSystem after every
	// owner reports defeat. Set the already-folded value identically here so this
	// test exercises the real MonitorGlobalReset transition without constructing
	// two complete defeat sequences.
	for _, a := range apps {
		a.World().Resources.Status.Bools.Get("session.all_defeated").Store(true)
	}
	for range 6 {
		tickAll(apps)
	}

	assertArmed("global reset")
	assertMeshParity(t, apps, 6)
}

// TestLiveSessionRefusesAnInstanceLocalPause pins the operator policy: entering a
// local overlay or command mode may inspect a live session, but it must not stop
// that participant's production clock while its peers continue.
func TestLiveSessionRefusesAnInstanceLocalPause(t *testing.T) {
	apps := meshSession(t, 0xA1A1, 2, [][2]int{{1, 2}})
	localCursors(t, apps)

	apps[0].Context().SetPaused(true)
	apps[0].Settle()

	for i, a := range apps {
		if a.Context().TimeCtl.IsPaused() {
			t.Fatalf("participant %d paused inside a live session", i+1)
		}
	}
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}
	assertMeshParity(t, apps, 0)

	// A synchronous snapshot drains a second log sink while the world lock is
	// held. That may exceed the playout lead, so it is not a live inspection
	// operation even though the non-blocking debug overlay remains available.
	apps[0].Context().ClearStatusMessage()
	mode.ExecuteCommand(apps[0].Context(), "d save")
	if got := apps[0].Context().GetStatusMessage(); got != "Snapshot save unavailable in a live session" {
		t.Fatalf(":d save status=%q", got)
	}
}

// TestCoordinatorResetCrossesAndPreservesRoster reproduces :new as an operator
// injection on one instance. The session must restart at one agreed barrier tick,
// and the reset must rebuild the closed roster rather than the boot cursor alone.
func TestCoordinatorResetCrossesAndPreservesRoster(t *testing.T) {
	apps := meshSession(t, 0xA3A3, 2, [][2]int{{1, 2}})
	localCursors(t, apps)

	// The same command on a guest is operator-local refusal, not an artifact.
	mode.ExecuteCommand(apps[1].Context(), "n")
	apps[1].Settle()
	if got := apps[1].Position().Run; got != 0 {
		t.Fatalf("guest :new changed run to %d", got)
	}

	mode.ExecuteCommand(apps[0].Context(), "n")
	apps[0].Settle()
	for range parameter.NetworkBarrierDelayTicks + 8 {
		tickAll(apps)
	}

	for i, a := range apps {
		if got := a.Position().Run; got != 1 {
			t.Fatalf("participant %d reset run=%d, want 1", i+1, got)
		}
		var count int
		a.World().RunSafe(func() { count = a.World().Resources.Player.Count() })
		if count != len(apps) {
			t.Fatalf("participant %d roster=%d after reset, want %d", i+1, count, len(apps))
		}
	}
	assertMeshParity(t, apps, 0)
}

// TestOneSharedQuasarTriggerProducesOneSpawn models the old MainEscalate fan-out:
// the same shared decision asks every player-domain FuseSystem to act. It is one
// logical fusion and therefore must yield one shared spawn request, not N.
func TestOneSharedQuasarTriggerProducesOneSpawn(t *testing.T) {
	apps := meshSession(t, 0xA4A4, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	spawns := make([]int, len(apps))
	for i, a := range apps {
		i := i
		a.SetDispatchTap(func(ev event.GameEvent) {
			if ev.Type == event.EventQuasarSpawnRequest {
				spawns[i]++
			}
		})
		a.World().Resources.Status.Ints.Get("kills.drain").Store(9)
	}
	// Participant 2 produces the tenth shared defeat. The crossing is delivered
	// to both FSMs, but its causal cursor elects only participant 2's fuse system.
	apps[1].Context().PushCrossing(event.EventDrainDefeated,
		&event.DrainDefeatedPayload{Entity: local[1]})
	apps[1].Settle()

	// Fusion waits 600ms; the driven clock advances 50ms per tick.
	for range 20 + parameter.NetworkBarrierDelayTicks {
		tickAll(apps)
	}
	for i, got := range spawns {
		if got != 1 {
			t.Fatalf("participant %d observed %d quasar spawn requests, want 1", i+1, got)
		}
	}
}

// TestExplosionPresentationStaysWithItsProducer is the presentation half of the
// explosion split. The combat artifact reaches the peer; the smoke center does not.
func TestExplosionPresentationStaysWithItsProducer(t *testing.T) {
	apps := meshSession(t, 0xA5A5, 2, [][2]int{{1, 2}})
	local := localCursors(t, apps)

	apps[0].Context().PushLocal(event.EventExplosionVisualRequest,
		&event.ExplosionVisualRequestPayload{X: 10, Y: 10, Radius: 4, Type: event.ExplosionTypeMissile})
	apps[0].Context().PushCrossing(event.EventExplosionRequest,
		&event.ExplosionRequestPayload{Entity: local[0], X: 10, Y: 10, Radius: 4})
	apps[0].Settle()
	for range parameter.NetworkBarrierDelayTicks + 2 {
		tickAll(apps)
	}

	for i, a := range apps {
		var centers int
		a.World().RunSafe(func() { centers = a.World().Resources.Transient.ExplosionCount })
		want := 0
		if i == 0 {
			want = 1
		}
		if centers != want {
			t.Fatalf("participant %d has %d missile visual centers, want %d", i+1, centers, want)
		}
	}
}

// TestRuntimeDigestReportsAndClearsSharedDivergence proves the live instrument
// against a deliberately corrupted shared position. It also pins the transient
// SYNCED acknowledgement after the exact state is restored.
func TestRuntimeDigestReportsAndClearsSharedDivergence(t *testing.T) {
	apps := meshSession(t, 0xD165E57, 2, [][2]int{{1, 2}})
	localCursors(t, apps)

	var target core.Entity
	var original component.PositionComponent
	for range 8 {
		apps[0].World().RunSafe(func() {
			for _, e := range apps[0].World().Components.Header.Entities() {
				if e.Domain() == core.DomainShared {
					target = e
					original, _ = apps[0].World().Positions.GetPosition(e)
					break
				}
			}
		})
		if target != 0 {
			break
		}
		tickAll(apps)
	}
	if target == 0 {
		t.Fatal("no shared composite available for divergence probe")
	}

	apps[0].World().RunSafe(func() {
		p := original
		p.X++
		apps[0].World().Positions.SetPosition(target, p)
	})
	for range 2*parameter.NetworkDigestTicks + 2 {
		tickAll(apps)
	}
	for i, a := range apps {
		if got := a.World().Resources.Status.Strings.Get("network.sync_state").Load(); got != "desync" {
			t.Fatalf("participant %d sync state=%q, want desync", i+1, got)
		}
	}

	apps[0].World().RunSafe(func() { apps[0].World().Positions.SetPosition(target, original) })
	for range 2*parameter.NetworkDigestTicks + 2 {
		tickAll(apps)
	}
	for i, a := range apps {
		if got := a.World().Resources.Status.Strings.Get("network.sync_state").Load(); got != "synced" {
			t.Fatalf("participant %d sync state=%q after repair, want synced", i+1, got)
		}
	}

	for range parameter.NetworkResyncNoticeTicks + 1 {
		tickAll(apps)
	}
	for i, a := range apps {
		if got := a.World().Resources.Status.Strings.Get("network.sync_state").Load(); got != "" {
			t.Fatalf("participant %d retained sync notice %q", i+1, got)
		}
	}
}

// TestSharedSnapshotExcludesLocalSchedulerTiming pins the distinction the live
// digest needs but the manual-clock harness cannot produce naturally: two real
// schedulers have different wall origins and can miss different deadlines even
// while they complete the same absolute simulation tick.
func TestSharedSnapshotExcludesLocalSchedulerTiming(t *testing.T) {
	a := mustHeadless(t, 0xD165E58, 120, 40)
	b := mustHeadless(t, 0xD165E58, 120, 40)
	defer a.Close()
	defer b.Close()
	tickUntilCursor(t, a)
	tickUntilCursor(t, b)

	a.World().Resources.Status.Ints.Get("engine.tick_slips").Store(3)
	a.World().Resources.Status.Ints.Get("time.game_elapsed_ms").Store(17_000)
	a.World().Resources.Status.Ints.Get("gold.timer").Store(4_200)
	assertSharedParity(t, a, b, 0)
}
