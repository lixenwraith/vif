package app

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/component"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/snapshot"
	link "github.com/lixenwraith/vif/pkg/network"
)

// sessionOfferFor builds the closed roster a coordinator would hand out, for a test
// that drives HostSession and JoinSessionAt directly instead of over a socket.
func sessionOfferFor(an event.JoinAnchor, n int) network.SessionOffer {
	o := network.SessionOffer{
		Anchor:   an,
		Host:     engine.CoordinatorParticipant,
		Assigned: 2,
		Term:     network.FirstTerm,
	}
	for i := range n {
		o.Roster = append(o.Roster,
			network.RosterEntry{ID: link.PeerID(i + 1), Slot: uint8(i)})
	}
	return o
}

// TestSnapshotJoinCarriesTheGoldDeadline closes the gold-deadline defect a
// reproducing joiner had: it settled the FSM boot queue at a different point than its
// host, so the two entered MainSpawnGold a tick apart and carried origins 50 ms apart
// for every sequence. A joiner installs the host's world now and the gold carrier
// writes both instants relative to the capture's tick, so gold.timer is compared.
func TestSnapshotJoinCarriesTheGoldDeadline(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x14AD, 160, 48)
	defer host.Close()
	an := host.JoinAnchor()
	guest := mustJoiner(t, 0x14AD, 84, 26, an)
	defer guest.Close()

	offer := sessionOfferFor(an, 2)
	if err := host.HostSession(offer); err != nil {
		t.Fatalf("host session: %v", err)
	}

	// Far enough in that a gold sequence is live and has been counting down for a
	// while: a timer read at spawn would match by accident.
	host.Tick(60)
	if !goldActive(host) {
		t.Fatal("no gold sequence is live; the deadline comparison proves nothing")
	}
	before := goldTimer(host)

	if err := guest.JoinSessionAt(offer, mustRoundTrip(t, host), 0); err != nil {
		t.Fatalf("join session: %v", err)
	}
	if got := goldTimer(guest); got != before {
		t.Fatalf("gold deadline after the join = %d, host holds %d", got, before)
	}
	assertSharedParity(t, host, guest, 0)

	// The origins have to stay together, not merely coincide at the install: a
	// deadline rebased onto the wrong tick agrees once and then drifts by exactly
	// one tick's worth for the life of the sequence.
	for step := range 8 {
		host.Tick(5)
		guest.Tick(5)
		if !goldActive(host) {
			break
		}
		if got, want := goldTimer(guest), goldTimer(host); got != want {
			t.Fatalf("gold deadline %d ticks after the join = %d, host holds %d",
				(step+1)*5, got, want)
		}
	}
}

// TestSnapshotJoinTakesTheHostsWorldNotItsOwn is the join criterion at its
// simplest: a participant that arrives mid-run holds the host's world, not the one
// its own seed would have produced by then.
func TestSnapshotJoinTakesTheHostsWorldNotItsOwn(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x2B0B, 120, 40)
	defer host.Close()
	an := host.JoinAnchor()
	guest := mustJoiner(t, 0x2B0B, 120, 40, an)
	defer guest.Close()

	offer := sessionOfferFor(an, 2)
	if err := host.HostSession(offer); err != nil {
		t.Fatalf("host session: %v", err)
	}
	host.Tick(240)

	// The guest is driven to a different tick first, so an install that did nothing
	// could not pass.
	guest.Tick(20)
	if !sharedSurfacesDiffer(host, guest) {
		t.Fatal("the two runs already agree; the install would prove nothing")
	}

	cap := mustRoundTrip(t, host)
	if err := guest.JoinSessionAt(offer, cap, 0); err != nil {
		t.Fatalf("join session: %v", err)
	}
	if idx, lx, ly, differs := snapshot.FirstDiff(host.SnapshotShared(), guest.SnapshotShared()); differs {
		t.Fatalf("joined world differs at line %d\n  host:  %s\n  guest: %s\n%s",
			idx, lx, ly, strings.Join(snapshot.Diff(host.SnapshotShared(), guest.SnapshotShared(), 8), "\n"))
	}
	if got := guest.Position(); got.Tick != cap.Header.Tick || got.Run != cap.Header.Run {
		t.Fatalf("guest record position = run %d tick %d, capture named run %d tick %d",
			got.Run, got.Tick, cap.Header.Run, cap.Header.Tick)
	}
}

// TestSnapshotJoinLeavesEachParticipantDrivingItsOwnCursor is the D-13 half of an
// install: the whole cursor component travels, so a capture carries the sender's
// answer to which cursor it drives. A receiver that adopted it would simulate the
// host's cursor and not its own — two participants writing one cell, and none writing
// the other.
func TestSnapshotJoinLeavesEachParticipantDrivingItsOwnCursor(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x2B0C, 120, 40)
	defer host.Close()
	an := host.JoinAnchor()
	guest := mustJoiner(t, 0x2B0C, 120, 40, an)
	defer guest.Close()

	offer := sessionOfferFor(an, 2)
	if err := host.HostSession(offer); err != nil {
		t.Fatalf("host session: %v", err)
	}
	host.Tick(30)
	if err := guest.JoinSessionAt(offer, mustRoundTrip(t, host), 0); err != nil {
		t.Fatalf("join session: %v", err)
	}

	// The roster is identical on both, entity for entity: it is re-derived from the
	// installed cursor store rather than adopted or rebuilt.
	for slot := range 2 {
		var hostE, guestE core.Entity
		host.World().RunSafe(func() { hostE = host.World().Resources.Player.Slot(uint8(slot)) })
		guest.World().RunSafe(func() { guestE = guest.World().Resources.Player.Slot(uint8(slot)) })
		if hostE == 0 || hostE != guestE {
			t.Fatalf("slot %d = entity %d on the host, %d on the guest", slot, hostE, guestE)
		}
	}

	// Each participant simulates its own and only its own.
	assertControl(t, host, 0, component.ControlLocal)
	assertControl(t, host, 1, component.ControlRemote)
	assertControl(t, guest, 0, component.ControlRemote)
	assertControl(t, guest, 1, component.ControlLocal)

	if got := guest.membership.localSlot(); got != 1 {
		t.Fatalf("guest follows slot %d, want its own slot 1", got)
	}
	if got := host.membership.localSlot(); got != 0 {
		t.Fatalf("host follows slot %d, want its own slot 0", got)
	}
}

// TestAJoinerDrivesOnlyItsOwnCursor: a mid-run joiner's place is empty until its
// arrival crosses, and its boot slot is another participant's, so it drives none. A
// correction the arrival already crossed into fences the arrival off and brings the
// cursor instead, which the joiner then drives.
func TestAJoinerDrivesOnlyItsOwnCursor(t *testing.T) {
	t.Parallel()
	host := mustHeadless(t, 0x2B0D, 120, 40)
	defer host.Close()
	an := host.JoinAnchor()
	guest := mustJoiner(t, 0x2B0D, 120, 40, an)
	defer guest.Close()

	host.Tick(30)
	if err := guest.JoinSessionAt(sessionOfferFor(an, 2), mustRoundTrip(t, host), 0); err != nil {
		t.Fatalf("join session: %v", err)
	}
	if got := guest.membership.localSlot(); got != parameter.NoPlayerSlot {
		t.Fatalf("a joiner awaiting its arrival follows slot %d", got)
	}

	host.World().RunSafe(func() {
		host.World().PushEventFull(event.EventParticipantJoined,
			&event.ParticipantJoinedPayload{Participant: 2, Slot: 1}, event.OriginSession, core.DomainPlayer)
	})
	host.Tick(10)
	guest.World().AdoptSession(engine.SessionState{Participant: 2, Authority: 1}) // its link's latch
	staged, err := guest.glue.StageShared(mustRoundTrip(t, host))
	if err == nil {
		err = staged.Commit()
	}
	if err != nil {
		t.Fatalf("correction: %v", err)
	}
	assertControl(t, guest, 1, component.ControlLocal)
	if got := guest.membership.localSlot(); got != 1 {
		t.Fatalf("a joiner follows slot %d once a correction brings its cursor, want 1", got)
	}
}

func assertControl(t *testing.T, a *App, slot uint8, want component.ControlKind) {
	t.Helper()
	var got component.ControlKind
	var found bool
	a.World().RunSafe(func() {
		e := a.World().Resources.Player.Slot(slot)
		if c, ok := a.World().Components.Cursor.GetComponent(e); ok {
			got, found = c.Control, true
		}
	})
	if !found {
		t.Fatalf("slot %d holds no cursor", slot)
	}
	if got != want {
		t.Fatalf("slot %d control = %v, want %v", slot, got, want)
	}
}

func goldActive(a *App) bool {
	return a.World().Resources.Status.Bools.Get("gold.active").Load()
}

func goldTimer(a *App) int64 {
	return a.World().Resources.Status.Ints.Get("gold.timer").Load()
}

// TestAJoinerAdoptsTheHostsRngSession is what a restarted host needs from a join. The
// seed says which family of streams a run draws from and the session counter which
// game in that family, which a reset advances. A joiner that counted from one would
// build a different world and be refused on the identity check — which made a host
// that had ever reset unjoinable, and a dedicated host restarts routinely.
func TestAJoinerAdoptsTheHostsRngSession(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF
	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()

	// The run a fresh host reports, and the run it reports after a restart.
	first := host.JoinAnchor().Anchor.Session
	host.Context().PushEventOrigin(event.EventGameResetRequest,
		&event.GameResetPayload{}, event.OriginDebug)
	host.Settle()
	host.Tick(4)
	an := host.JoinAnchor()
	if an.Anchor.Session <= first {
		t.Fatalf("a restart left the session counter at %d, want it past %d",
			an.Anchor.Session, first)
	}

	cfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: 120, Height: 40}, network.SessionOffer{
		Anchor: an, Host: 1, Assigned: 2, Term: network.FirstTerm,
		Roster: []network.RosterEntry{
			{ID: 1, Slot: 0}, {ID: 2, Slot: 1},
		},
	})
	if err != nil {
		t.Fatalf("join config: %v", err)
	}
	if cfg.Session != an.Anchor.Session {
		t.Fatalf("the join config carries session %d, the host is on %d", cfg.Session, an.Anchor.Session)
	}
	guest, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("join app: %v", err)
	}
	defer guest.Close()
	if err := guest.JoinAt(an); err != nil {
		t.Fatalf("a joiner that adopted the host's session was refused: %v", err)
	}
}

// TestAJoinerAdoptsTheHostsTick: the tick is its host's choice, so a joiner takes it
// from the offer and passes the identity check, where one that kept its own is
// refused by that field.
func TestAJoinerAdoptsTheHostsTick(t *testing.T) {
	t.Parallel()
	const seed, tick = 0x71C4, 25 * time.Millisecond
	host, err := NewHeadless(Config{Seed: seed, Width: 120, Height: 40, TickInterval: tick,
		Resources: resource.Options{Embedded: true}})
	if err != nil {
		t.Fatalf("host: %v", err)
	}
	defer host.Close()
	an := host.JoinAnchor()
	cfg, err := ConfigForJoin(Config{Mode: ModeHeadless, Width: 120, Height: 40}, network.SessionOffer{
		Anchor: an, Host: 1, Assigned: 2, Term: network.FirstTerm,
		Roster: []network.RosterEntry{{ID: 1, Slot: 0}, {ID: 2, Slot: 1}},
	})
	if err != nil {
		t.Fatalf("join config: %v", err)
	}
	guest, err := NewHeadless(cfg)
	if err != nil {
		t.Fatalf("join app: %v", err)
	}
	defer guest.Close()
	if got := guest.World().TickInterval(); got != tick {
		t.Fatalf("the joiner ticks at %s, its host at %s", got, tick)
	}
	if err := identityFromAnchor(an).Verify(guest.membership.sessionIdentity()); err != nil {
		t.Fatalf("a joiner that adopted the host's tick was refused: %v", err)
	}
	kept := mustHeadless(t, seed, 120, 40)
	defer kept.Close()
	if err := identityFromAnchor(an).Verify(kept.membership.sessionIdentity()); err == nil ||
		!strings.Contains(err.Error(), "tick_ns") {
		t.Fatalf("a joiner on its own tick was told %v", err)
	}
}

func TestReproducingJoinAdmissionAndRefusals(t *testing.T) {
	t.Parallel()
	const seed = 0x5EEDBEEF

	host := mustHeadless(t, seed, 120, 40)
	defer host.Close()
	host.SetupLevel(100, 30, true, false)

	an := host.JoinAnchor()
	if an.Anchor.MapWidth != 100 || an.Anchor.MapHeight != 30 || an.Anchor.CropOnResize {
		t.Fatalf("host latch = %dx%d crop %t, want 100x30 crop false",
			an.Anchor.MapWidth, an.Anchor.MapHeight, an.Anchor.CropOnResize)
	}

	guest := mustHeadless(t, seed, 180, 56)
	defer guest.Close()
	if err := guest.Join(an); err != nil {
		t.Fatalf("join: %v", err)
	}
	var gotW, gotH int
	var gotCrop bool
	guest.World().RunSafe(func() {
		cfg := guest.World().Resources.Config
		gotW, gotH, gotCrop = cfg.MapWidth, cfg.MapHeight, cfg.CropOnResize
	})
	if gotW != 100 || gotH != 30 || gotCrop {
		t.Fatalf("guest map = %dx%d crop %t, want the host's 100x30 crop false", gotW, gotH, gotCrop)
	}
	// Only the shared bounds are adopted; the guest keeps its own terminal.
	if guest.Context().Width != 180 || guest.Context().Height != 56 {
		t.Fatalf("guest terminal = %dx%d, want its own 180x56",
			guest.Context().Width, guest.Context().Height)
	}

	foreign := mustHeadless(t, 0xC0FFEE, 120, 40)
	defer foreign.Close()
	if err := foreign.Join(an); err == nil || !strings.Contains(err.Error(), "seed") {
		t.Fatalf("join with a foreign seed = %v, want a seed mismatch", err)
	}

	host.Tick(5)
	late := mustHeadless(t, seed, 120, 40)
	defer late.Close()
	if err := late.Join(host.JoinAnchor()); !errors.Is(err, ErrJoinMidRun) {
		t.Fatalf("join with a mid-run host = %v, want ErrJoinMidRun", err)
	}
}

// writeScenario writes a scenario named name under root's scenario/: an entry, a
// region file and the macro library it expands against, so a transfer carries all
// three. The description is the only line that differs between two of them.
func writeScenario(t *testing.T, root, name, description string) string {
	t.Helper()
	dir := filepath.Join(root, "scenario", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"scenario.toml": fmt.Sprintf("description = %q\nmacro_files = [\"macros.toml\"]\n"+
			"[regions.main]\ninitial = \"MainStart\"\nfile = \"main.toml\"\n"+
			"[regions.player]\ninitial = \"PlayerBoot\"\nbackground = true\n"+
			"[states.PlayerBoot]\nparent = \"Root\"\non_enter = [{ action = \"EmitEvent\", "+
			"event = \"EventCursorSpawnRequest\", payload = { center = true } }]\n", description),
		"macros.toml": "[macros.Hold]\nparams = { next = \"string\" }\nbody = { parent = \"Root\", transitions = " +
			"[{ trigger = \"Tick\", target = \"${next}\", guard = \"StateTimeExceeds\", guard_args = { ms = 500 } }] }\n",
		"main.toml": "[states]\nMainStart = { macro = \"Hold\", next = \"MainRepeat\" }\n" +
			"MainRepeat = { macro = \"Hold\", next = \"MainStart\" }\n",
	}
	for file, body := range files {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestAJoinerPlaysTheHostsScenarioWhateverItsRootsHold: a joiner whose roots lack
// the session's scenario, hold other bytes under its name, or are skipped by -d is
// served the coordinator's bytes. The coordinator admits a joiner only on an equal
// scenario digest, so the session running at all is the proof.
func TestAJoinerPlaysTheHostsScenarioWhateverItsRootsHold(t *testing.T) {
	// Not parallel: this drives a real socket against wall-clock deadlines.
	hostRoot, otherRoot := t.TempDir(), t.TempDir()
	writeScenario(t, hostRoot, "duel", "The session's duel.")
	writeScenario(t, otherRoot, "duel", "Another duel.")
	script := filepath.Join(t.TempDir(), "idle.toml")
	if err := os.WriteFile(script, []byte("schema = 1\nticks = 3\nwidth = 80\nheight = 24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := Config{Seed: 0x5C72, Resources: resource.Options{Dir: hostRoot, Scenario: "duel"}}
	for _, tc := range []struct {
		name  string
		guest resource.Options
	}{
		{"no root holds it", resource.Options{}},
		{"a root holds other bytes under its name", resource.Options{Dir: otherRoot}},
		{"-d skips the roots", resource.Options{Embedded: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, guest := runScriptPair(t, host, script, Config{Resources: tc.guest}, script)
			if guest.Ticks != 3 {
				t.Fatalf("guest stats = %+v", guest)
			}
		})
	}
}
