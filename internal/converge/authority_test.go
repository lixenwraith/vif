package converge

import (
	"errors"
	"slices"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	link "github.com/lixenwraith/vif/pkg/network"
)

// chainOf renders identities as chain entries; an in-process session needs no
// addresses, because its links already exist.
func chainOf(ids ...link.PeerID) network.SuccessionChain {
	var out network.SuccessionChain
	for _, id := range ids {
		out = append(out, network.ChainEntry{ID: id, Addr: "mesh"})
	}
	return out
}

// record is the handoff a criterion hands to an instance, carrying the membership
// that instance closed on — a record whose players are not those the receiver holds
// is refused before anything is read from it.
func record(u *Authority, term network.AuthorityTerm, authority, predecessor link.PeerID) network.HandoffRecord {
	held := u.State()
	return network.HandoffRecord{
		Term: term, Authority: authority, Predecessor: predecessor,
		Roster: held.Roster, Anchor: held.Anchor,
	}
}

// rewind moves a run's walk and cut-off back by d, as d of wall time passing would.
func rewind(r *run, d time.Duration) {
	r.u.mu.Lock()
	defer r.u.mu.Unlock()
	for _, at := range []*time.Time{&r.u.opened, &r.u.walkFrom, &r.u.cutSince} {
		if !at.IsZero() {
			*at = at.Add(-d)
		}
	}
}

// TestASurvivorFollowsOneRecordPerLoss: a participant adopts one record per term,
// so a rival for a term adopted, or one carrying a roster this session never closed
// on, is refused; and having followed one host it ignores the other candidates'
// records for the same loss, whichever their term, as the group it did not join.
func TestASurvivorFollowsOneRecordPerLoss(t *testing.T) {
	t.Parallel()
	guest := session(t, 4, [][2]int{{1, 2}, {1, 3}, {1, 4}})[3]

	foreign := record(guest.u, network.FirstTerm+2, 3, 1)
	foreign.Roster = append(slices.Clone(foreign.Roster), network.RosterEntry{ID: 5, Slot: 4})
	if err := guest.u.adopt(foreign, 0); err == nil {
		t.Fatal("a record carrying a roster this session never closed on was adopted")
	}
	base := record(guest.u, network.FirstTerm+2, 3, 1)
	if err := guest.u.adopt(base, 0); err != nil {
		t.Fatalf("the second candidate's record for its own term must be adopted: %v", err)
	}
	rival := base
	rival.Authority = 2
	if err := guest.u.adopt(rival, 0); err == nil {
		t.Fatal("a second, different record for one term was adopted")
	}
	for _, other := range []network.HandoffRecord{
		record(guest.u, network.FirstTerm+1, 2, 1), record(guest.u, network.FirstTerm+3, 4, 1),
	} {
		if err := guest.u.adopt(other, 0); err != nil {
			t.Fatalf("another group's record was refused rather than ignored: %v", err)
		}
	}
	if got := guest.u.State(); got.Authority != 3 || got.Term != network.FirstTerm+2 {
		t.Fatalf("the survivor left the host it followed: %+v", got)
	}
}

func TestLossReportsCannotReplaceALinkedAuthority(t *testing.T) {
	t.Parallel()
	guest := session(t, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})[2]
	body, err := network.EncodeAuthorityReport(network.AuthorityReport{Term: network.FirstTerm, From: 2, Lost: 1})
	if err != nil {
		t.Fatal(err)
	}
	guest.u.Receive(uint8(network.MsgAuthorityReport), 2, body)
	if guest.u.Migrating() || guest.u.Term() != network.FirstTerm {
		t.Fatal("a neighbour's loss report displaced the connected authority")
	}
}

// TestTheTermGateRefusesWhatNobodyHandedThisInstance is the wire rule a fork runs
// under: older is ignored, equal acted on, newer refused rather than adopted, and
// no handoff is a way back in.
func TestTheTermGateRefusesWhatNobodyHandedThisInstance(t *testing.T) {
	t.Parallel()
	fork := session(t, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})[2]

	// A walk nothing answers: the last turn passes with no record.
	fork.u.beginSuccession(1)
	fork.u.giveUp("criterion")
	if got := fork.u.State(); !got.Fork {
		t.Fatalf("the instance did not become a local fork: %+v", got)
	}

	if fork.u.admit(network.FirstTerm+2, 1) {
		t.Fatal("a fork adopted an artifact from a term it was never handed")
	}
	if fork.stat("network.term_refused") == 0 {
		t.Fatal("the refusal was not counted")
	}
	if fork.u.admit(network.FirstTerm-1, 1) {
		t.Fatal("an artifact from a term the session has left was acted on")
	}
	if !fork.u.admit(network.FirstTerm, 1) {
		t.Fatal("an artifact from the term this instance holds was refused")
	}
	if got := fork.u.State(); got.Term != network.FirstTerm || !got.Fork {
		t.Fatalf("the refused artifacts moved the fork's authority: %+v", got)
	}
	if err := fork.u.adopt(record(fork.u, network.FirstTerm+1, 2, 1), 1); err != nil || fork.u.Term() != network.FirstTerm {
		t.Fatalf("a fork took a handoff (%v)", err)
	}
}

// TestASurvivorStandsFrozenThroughTheWalk: a survivor stops its run when its
// authority goes, and with no record it stands until the last candidate's turn has
// passed on the wall clock, since no tick passes to count it; the fork it ends in is
// told to the run.
func TestASurvivorStandsFrozenThroughTheWalk(t *testing.T) {
	t.Parallel()
	run := session(t, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})[2]
	turns := time.Duration(len(run.u.Chain())) * parameter.NetworkSuccessionWindow
	run.u.beginSuccession(1)
	rewind(run, turns-time.Millisecond)
	run.u.drive()
	if !run.world.frozen || run.u.State().Fork {
		t.Fatalf("inside the walk: frozen %v, fork %v", run.world.frozen, run.u.State().Fork)
	}
	rewind(run, 2*time.Millisecond)
	run.u.drive()
	if !run.u.State().Fork || run.world.forked != 1 || run.world.frozen {
		t.Fatalf("past the last turn: fork %v, told %d times, frozen %v",
			run.u.State().Fork, run.world.forked, run.world.frozen)
	}
}

// TestARecordAheadOfTheNoticeFreezesItsReader: a survivor the handoff reaches before
// it saw its authority go stands still with the rest, since its new authority does.
func TestARecordAheadOfTheNoticeFreezesItsReader(t *testing.T) {
	t.Parallel()
	guest := session(t, 3, [][2]int{{1, 2}, {2, 3}, {1, 3}})[2]
	if err := guest.u.adopt(record(guest.u, network.FirstTerm+1, 2, 1), 0); err != nil {
		t.Fatalf("the successor's record: %v", err)
	}
	if !guest.world.frozen {
		t.Fatal("the record moved this run's authority and left it playing on")
	}
}

// TestTheHostResumesOnceItsGroupIsIn: the new host holds its group still until
// every survivor's membership has reached it, the lost authority's own bots and the
// Wraiths aside, or until the window runs out on whoever is missing.
func TestTheHostResumesOnceItsGroupIsIn(t *testing.T) {
	t.Parallel()
	took := func(resumeBy time.Time) *run {
		r := newRun(t, 2, nil, append(roster(3),
			network.RosterEntry{ID: 4, Slot: 3, Holder: 1}, network.RosterEntry{ID: 5, Slot: 4, Away: true}))
		r.open(1, chainOf(2, 3), false)
		r.u.beginSuccession(1)
		if err := r.u.adopt(record(r.u, network.FirstTerm+1, 2, 1), 0); err != nil {
			t.Fatalf("the host's own record: %v", err)
		}
		r.u.mu.Lock()
		r.u.resumeBy = resumeBy
		r.u.mu.Unlock()
		r.u.driveResume()
		return r
	}
	waiting := took(time.Now().Add(time.Hour))
	if waiting.world.thawed != 0 {
		t.Fatal("the host resumed before participant 3's membership reached it")
	}
	body, err := network.EncodeAuthorityReport(network.AuthorityReport{Term: network.FirstTerm + 1, From: 3, Lost: 1})
	if err != nil {
		t.Fatal(err)
	}
	waiting.u.Receive(uint8(network.MsgAuthorityReport), 3, body)
	waiting.u.driveResume()
	if waiting.world.thawed != 1 {
		t.Fatalf("with its group in the host resumed %d times, want once", waiting.world.thawed)
	}
	if late := took(time.Now().Add(-time.Millisecond)); late.world.thawed != 1 {
		t.Fatalf("past the window the host resumed %d times, want once", late.world.thawed)
	}
}

// TestAPinnedAuthorityDoesNotMove is the option a deployment needs and a person
// hosting a game does not. The policy is the coordinator's and travels in the
// offer, because a disagreement about it is one instance electing and one refusing.
// What a pinned survivor keeps of the others is the run's rule, not the authority's:
// its fork is handed the roster it held.
func TestAPinnedAuthorityDoesNotMove(t *testing.T) {
	t.Parallel()
	guest := newRun(t, 2, nil, roster(2))
	guest.u.Open(network.SessionOffer{
		Host: 1, Assigned: 2, Term: network.FirstTerm,
		Roster: roster(2), Chain: chainOf(1, 2), FixedAuthority: true,
	}, 2)

	// The succession still opens — opening it is what floods the loss to survivors
	// a relay away — and ends one window earlier than a fruitless one, because no
	// record is coming.
	guest.u.peerLost(1)

	got := guest.u.State()
	if !got.Fork {
		t.Fatalf("a pinned session did not fork on losing its authority: %+v", got)
	}
	if got.Authority == 2 || got.Term != network.FirstTerm {
		t.Fatalf("a pinned session moved its authority: %+v", got)
	}
	if !guest.reg.Bools.Get("network.host_lost").Load() {
		t.Fatal("the survivor did not report the loss")
	}
	if guest.world.forked != 1 || len(guest.world.forkedAs) != 2 {
		t.Fatalf("the fork was told %d times with %d roster entries; want once with the 2 it held",
			guest.world.forked, len(guest.world.forkedAs))
	}

	// A pinned guest walks to nobody; the one wait it has is its redial of the host.
	redialing := newRun(t, 2, nil, roster(2))
	redialing.world.redial = true
	redialing.u.Open(network.SessionOffer{
		Host: 1, Assigned: 2, Term: network.FirstTerm, Roster: roster(2), FixedAuthority: true,
	}, 2)
	redialing.u.peerLost(1)
	if redialing.u.State().Fork || !redialing.world.frozen {
		t.Fatal("a pinned guest forked, or played on, before its redial ended")
	}
	redialing.u.RedialRefused(1)
	redialing.u.drive()
	if !redialing.u.State().Fork {
		t.Fatal("a pinned guest whose redial was refused did not fork")
	}
}

// TestALeavingInstanceAuthorsOnlyTheSessionItReopens: whatever the session it left
// still had going here — a fork, a lost host, a queued correction or succession
// message — ends with that session, and the instance authors the one it goes on in.
func TestALeavingInstanceAuthorsOnlyTheSessionItReopens(t *testing.T) {
	t.Parallel()
	guest := newRun(t, 2, nil, roster(2))
	guest.u.Open(network.SessionOffer{
		Host: 1, Assigned: 2, Term: network.FirstTerm, Roster: roster(2), FixedAuthority: true,
	}, 2)
	guest.u.peerLost(1)
	if !guest.u.State().Fork {
		t.Fatal("a pinned guest did not fork on losing its host; the case below proves nothing")
	}
	guest.c.Receive([]byte("a correction the left session sent"))
	guest.c.ReceiveAuthorityFrame(uint8(network.MsgAuthorityReport), 1, []byte("a report it sent"))

	err := guest.c.Leave(func() error {
		guest.u.Open(network.SessionOffer{
			Host: 2, Assigned: 2, Term: guest.u.Term() + 1, Roster: roster(2)[1:],
		}, 2)
		return nil
	})
	got := guest.u.State()
	if err != nil || got.Fork || got.Authority != 2 || got.Term != network.FirstTerm+1 || !guest.u.isAuthority() {
		t.Fatalf("after leaving (%v) the instance holds %+v; want it authoring the next term", err, got)
	}
	if guest.reg.Bools.Get("network.host_lost").Load() || guest.reg.Bools.Get("network.fork").Load() {
		t.Fatal("the session it reopened still reports the host it lost")
	}
	guest.c.inboxMu.Lock()
	inbox := len(guest.c.inbox)
	guest.c.inboxMu.Unlock()
	guest.c.authorityMu.Lock()
	frames := len(guest.c.authorityIn)
	guest.c.authorityMu.Unlock()
	if inbox != 0 || frames != 0 {
		t.Fatalf("the left session still has %d corrections and %d succession messages queued", inbox, frames)
	}
}

// TestTheWalkDialsEachCandidateInTurn: a survivor dials the first candidate while it
// redials its lost host, and one more each window after, passing over a participant
// away, which has no link to serve a term from; one cut off dials them all.
func TestTheWalkDialsEachCandidateInTurn(t *testing.T) {
	t.Parallel()
	members := roster(5)
	members[2].Away = true
	r := newRun(t, 5, nil, members)
	r.world.redial = true
	r.u.Open(network.SessionOffer{
		Host: 1, Assigned: 5, Term: network.FirstTerm, Roster: members, Chain: chainOf(3, 2, 4, 5),
	}, 5)
	now := time.Now()
	if got, want := r.u.walkTargets(now), []link.PeerID{2, 4}; !slices.Equal(got, want) {
		t.Fatalf("cut off, dials %v; want every candidate %v", got, want)
	}
	if got, ok := r.u.successor(); !ok || got != 2 {
		t.Fatalf("successor = %d (ok=%t), want 2, first on the chain after the away participant", got, ok)
	}
	r.u.beginSuccession(1)
	if got, want := r.u.walkTargets(now.Add(time.Hour)), []link.PeerID{2}; !slices.Equal(got, want) {
		t.Fatalf("redialing, dials %v; want the first candidate %v", got, want)
	}
	r.u.RedialRefused(1)
	now = time.Now()
	for _, c := range []struct {
		at   time.Duration
		want []link.PeerID
	}{
		{0, []link.PeerID{2}},
		{parameter.NetworkSuccessionWindow, []link.PeerID{2, 4}},
		{4 * parameter.NetworkSuccessionWindow, []link.PeerID{2, 4}},
	} {
		if got := r.u.walkTargets(now.Add(c.at)); !slices.Equal(got, c.want) {
			t.Fatalf("%v into the walk, dials %v; want %v", c.at, got, c.want)
		}
	}
}

// TestTheWalkWaitsOnARedial: a survivor redialing its lost host begins its walk when
// the redial is refused or a window after the loss, and not when another survivor's
// notice says the host went for it too.
func TestTheWalkWaitsOnARedial(t *testing.T) {
	t.Parallel()
	walking := func(r *run) bool {
		r.u.mu.Lock()
		defer r.u.mu.Unlock()
		return !r.u.walkFrom.IsZero()
	}
	lose := func() *run {
		r := session(t, 3, nil)[1]
		r.world.redial = true
		r.u.beginSuccession(1)
		if walking(r) || len(r.world.redials) != 1 {
			t.Fatalf("redials %v, walking %v; want one redial and the walk waiting on it", r.world.redials, walking(r))
		}
		return r
	}
	refused := lose()
	refused.u.RedialRefused(1)
	told := lose()
	body, err := network.EncodeAuthorityReport(network.AuthorityReport{Term: network.FirstTerm, From: 3, Lost: 1})
	if err != nil {
		t.Fatal(err)
	}
	told.u.Receive(uint8(network.MsgAuthorityReport), 3, body)
	timed := lose()
	rewind(timed, parameter.NetworkSuccessionWindow)
	timed.u.drive()
	if !walking(refused) || walking(told) || !walking(timed) {
		t.Fatalf("walking: refused %v, told by another survivor %v, timed out %v",
			walking(refused), walking(told), walking(timed))
	}
}

// TestAWalkingSurvivorIsTakenBackThroughACandidate: a survivor whose redial ended
// rejoins through the candidates whose turn has come, and is taken back while its
// walk is open, by its host or by a candidate relaying for it; one that followed a
// record takes no such admission.
func TestAWalkingSurvivorIsTakenBackThroughACandidate(t *testing.T) {
	t.Parallel()
	r := session(t, 3, nil)[2]
	r.world.redial = true
	r.u.beginSuccession(1)
	if targets, open := r.u.Rejoins(1); open || targets != nil {
		t.Fatalf("redialling, rejoins through %v (open %t); want nobody until the walk", targets, open)
	}
	r.u.RedialRefused(1)
	if targets, open := r.u.Rejoins(1); !open || !slices.Equal(targets, []link.PeerID{2}) {
		t.Fatalf("walking, rejoins through %v (open %t); want the candidate whose turn came, 2", targets, open)
	}
	if r.u.RedialAdmitted(2) {
		t.Fatal("taken back by a host it did not lose")
	}
	if !r.u.RedialAdmitted(1) || !r.u.State().Fork {
		t.Fatal("walking, the run taken back was not left to the one in its place")
	}
	if _, open := r.u.Rejoins(1); open {
		t.Fatal("left to the run in its place, it still rejoins")
	}
	followed := session(t, 3, nil)[2]
	followed.u.beginSuccession(1)
	if err := followed.u.adopt(record(followed.u, network.FirstTerm+2, 3, 1), 0); err != nil {
		t.Fatal(err)
	}
	if followed.u.RedialAdmitted(1) {
		t.Fatal("having taken the term, the run was taken back by the host it lost")
	}
}

// TestTheWalkGoesDownTheList: when the first candidate went with the host, the
// second takes the term two past the lost one's once a window has passed, the
// survivors follow it, and at the floor it keeps the first candidate's place as a
// Wraith, since nothing reported it into the group.
func TestTheWalkGoesDownTheList(t *testing.T) {
	t.Parallel()
	mesh := network.NewMesh()
	mesh.Link(1, 2)
	mesh.Link(1, 3)
	mesh.Link(1, 4)
	mesh.Peer(3, 4)
	runs := make([]*run, 4)
	for i := range runs {
		runs[i] = newRun(t, uint32(i+1), mesh.Node(link.PeerID(i+1)), roster(4))
		runs[i].open(1, chainOf(2, 3), false)
	}
	// Every guest holds the host's keyframe, which is what a candidate authors from.
	if err := runs[0].c.PublishDue(); err != nil {
		t.Fatalf("keyframe: %v", err)
	}
	deliver(runs, 2)
	_ = mesh.Node(1).Close()
	_ = mesh.Node(2).Close()
	host, guest := runs[2], runs[3]
	survivors := runs[2:]
	for _, r := range survivors {
		r.c.QueuePeerLost(1)
	}
	deliver(survivors, 2)
	if host.u.Term() != network.FirstTerm || guest.u.Term() != network.FirstTerm {
		t.Fatal("a term was taken before the first candidate's turn had passed")
	}
	for _, r := range survivors {
		rewind(r, parameter.NetworkSuccessionWindow)
	}
	deliver(survivors, 3)
	for _, r := range survivors {
		if got := r.u.State(); got.Authority != 3 || got.Term != network.FirstTerm+2 {
			t.Fatalf("participant %d holds %+v; want the second candidate's term", r.world.local, got)
		}
	}
	host.world.advance(parameter.NetworkSuccessionTicks)
	deliver(survivors, 1)
	if len(host.world.departed) != 1 || len(host.world.departed[0]) != 1 || host.world.departed[0][0].ID != 2 {
		t.Fatalf("at the floor the host kept %v as Wraiths; want the first candidate alone", host.world.departed)
	}
}

// TestASurvivorRepeatsItsNoticeWhileItWalks: a survivor whose notice was lost on
// the wire, and that missed its host's record the same way, is answered on the
// notice it sends again each pass of its walk, rather than walking on to a fork
// beside a host it is linked to.
func TestASurvivorRepeatsItsNoticeWhileItWalks(t *testing.T) {
	t.Parallel()
	mesh := network.NewMesh()
	mesh.Link(1, 2)
	mesh.Link(1, 3)
	mesh.Peer(2, 3)
	runs := make([]*run, 3)
	for i := range runs {
		runs[i] = newRun(t, uint32(i+1), mesh.Node(link.PeerID(i+1)), roster(3))
		runs[i].open(1, chainOf(2, 3), false)
	}
	if err := runs[0].c.PublishDue(); err != nil {
		t.Fatalf("keyframe: %v", err)
	}
	deliver(runs, 2)
	_ = mesh.Node(1).Close()
	host, guest := runs[1], runs[2]
	survivors := runs[1:]
	// Every frame the survivors exchange as the loss lands is lost: the guest's
	// notice and the host's record alike.
	for _, r := range survivors {
		r.world.port.(*link.MeshPort).SetShape(link.LinkShape{LossEvery: 1})
		r.c.QueuePeerLost(1)
	}
	deliver(survivors, 3)
	if host.u.Holder() != 2 || guest.u.Holder() != 1 {
		t.Fatalf("after the loss the host follows %d and the guest %d; want the host authoring alone",
			host.u.Holder(), guest.u.Holder())
	}
	for _, r := range survivors {
		r.world.port.(*link.MeshPort).SetShape(link.LinkShape{})
	}
	guest.r.mu.Lock()
	guest.r.nextPass = time.Now().Add(time.Hour)
	guest.r.mu.Unlock()
	deliver(survivors, 2)
	if guest.u.Holder() != 1 {
		t.Fatal("the guest followed with no pass of its walk, so something besides the notice carried the record")
	}
	guest.r.mu.Lock()
	guest.r.nextPass = time.Time{}
	guest.r.mu.Unlock()
	deliver(survivors, 3)
	if got := guest.u.State(); got.Authority != 2 || got.Term != network.FirstTerm+1 {
		t.Fatalf("the guest holds %+v; want the host's term, answered on the notice sent again", got)
	}
}

// TestASurvivorCarriesOnlyWhomItRelaysFor: one that follows the new host answers a
// walking survivor's notice with the host's record only once it carries that
// survivor, which then follows the host through it and is counted in.
func TestASurvivorCarriesOnlyWhomItRelaysFor(t *testing.T) {
	t.Parallel()
	mesh := network.NewMesh()
	mesh.Link(1, 2)
	mesh.Link(1, 3)
	mesh.Link(1, 4)
	mesh.Peer(2, 3)
	mesh.Peer(3, 4)
	runs := make([]*run, 4)
	for i := range runs {
		runs[i] = newRun(t, uint32(i+1), mesh.Node(link.PeerID(i+1)), roster(4))
		runs[i].open(1, chainOf(2, 3), false)
	}
	if err := runs[0].c.PublishDue(); err != nil {
		t.Fatalf("keyframe: %v", err)
	}
	deliver(runs, 2)
	_ = mesh.Node(1).Close()
	host, relay, far := runs[1], runs[2], runs[3]
	survivors := runs[1:]
	for _, r := range survivors {
		r.c.QueuePeerLost(1)
	}
	deliver(survivors, 4)
	if got := relay.u.State(); got.Authority != 2 || got.Term != network.FirstTerm+1 {
		t.Fatalf("the relay holds %+v; want the first candidate's term", got)
	}
	if !far.u.Migrating() {
		t.Fatal("a survivor followed the host through a participant that does not relay")
	}

	relay.world.relaying = true
	far.u.sendReport() // as on a link it opens
	deliver(survivors, 4)
	got := far.u.State()
	if got.Authority != 2 || len(far.world.handoffs) != 1 || far.world.handoffs[0].via != 3 {
		t.Fatalf("the far survivor holds %+v after %v; want the host, followed through participant 3", got, far.world.handoffs)
	}
	if !slices.Equal(relay.world.carried, []uint32{4}) {
		t.Fatalf("the relay marked %v behind it; want participant 4", relay.world.carried)
	}
	host.u.mu.Lock()
	in := host.u.reportedLocked(4, network.FirstTerm+1)
	host.u.mu.Unlock()
	if !in {
		t.Fatal("the host did not count the carried survivor in its group")
	}
}

// dialingNode is a mesh node that opens links as a socket port does: a dial names
// a participant, which the node links to, or refuses while down, as a connect that
// does not go through.
type dialingNode struct {
	*link.MeshPort
	mesh *link.Mesh

	mu     sync.Mutex
	down   bool
	dialed []string
}

func (d *dialingNode) DialPeer(addr string) error {
	d.mu.Lock()
	d.dialed = append(d.dialed, addr)
	down := d.down
	d.mu.Unlock()
	if down {
		return errors.New("no route")
	}
	id, err := strconv.Atoi(addr)
	if err != nil {
		return err
	}
	d.mesh.Peer(link.PeerID(d.ParticipantID()), link.PeerID(id))
	return nil
}

func (d *dialingNode) dials() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.dialed)
}

func (d *dialingNode) setDown(down bool) {
	d.mu.Lock()
	d.down = down
	d.mu.Unlock()
}

// waitDials waits for the n-th dial, which runs off the loop that decided on it, and
// for every dial in flight to end, so the next decision is not held back by one.
func waitDials(t *testing.T, r *run, d *dialingNode, n int) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		r.r.mu.Lock()
		inFlight := len(r.r.dialing)
		r.r.mu.Unlock()
		if d.dials() >= n && inFlight == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d dials and %d in flight, want %d and none", d.dials(), inFlight, n)
		}
	}
}

// followThroughRelay builds the four-participant session of
// TestASurvivorCarriesOnlyWhomItRelaysFor with a far survivor that can dial but
// whose walk's dial of the first candidate does not go through, and runs the
// succession until it follows that candidate, now hosting, through participant 3.
func followThroughRelay(t *testing.T) (*run, *dialingNode) {
	t.Helper()
	mesh := network.NewMesh()
	mesh.Link(1, 2)
	mesh.Link(1, 3)
	mesh.Link(1, 4)
	mesh.Peer(2, 3)
	mesh.Peer(3, 4)
	far := &dialingNode{MeshPort: mesh.Node(4), mesh: mesh, down: true}
	chain := network.SuccessionChain{{ID: 2, Addr: "2"}, {ID: 3, Addr: "3"}}
	runs := make([]*run, 4)
	for i := range runs {
		var port engine.NetworkPort = mesh.Node(link.PeerID(i + 1))
		if i == 3 {
			port = far
		}
		runs[i] = newRun(t, uint32(i+1), port, roster(4))
		runs[i].open(1, chain, false)
	}
	if err := runs[0].c.PublishDue(); err != nil {
		t.Fatalf("keyframe: %v", err)
	}
	deliver(runs, 2)
	_ = mesh.Node(1).Close()
	survivors := runs[1:]
	for _, r := range survivors {
		r.c.QueuePeerLost(1)
	}
	deliver(survivors, 4)
	if !runs[3].u.Migrating() || slices.ContainsFunc(far.dialed, func(a string) bool { return a != "2" }) {
		t.Fatalf("the far survivor dialled %v and migrating is %t; want the first candidate alone, refused, and a walk", far.dialed, runs[3].u.Migrating())
	}
	runs[2].world.relaying = true
	runs[3].u.sendReport()
	deliver(survivors, 4)
	if got := runs[3].u.State(); got.Authority != 2 || len(runs[3].world.handoffs) != 1 || runs[3].world.handoffs[0].via != 3 {
		t.Fatalf("the far survivor holds %+v after %v; want the host, followed through participant 3", got, runs[3].world.handoffs)
	}
	// The dials so far went to the successor, kept linked before the loss, and to
	// the host as it was adopted; the criteria count from here.
	waitDials(t, runs[3], far, 0)
	runs[3].r.mu.Lock()
	runs[3].r.hostFor = 0
	runs[3].r.mu.Unlock()
	return runs[3], far
}

// TestAFollowerThroughARelayDialsItsHost: a survivor that follows the host through
// a participant relaying for it dials the host at its chain address and, linked,
// holds that link as its own; a host it cannot reach is dialled a bounded number
// of times, each after a longer wait, and followed as it was.
func TestAFollowerThroughARelayDialsItsHost(t *testing.T) {
	t.Parallel()
	t.Run("reaches it", func(t *testing.T) {
		t.Parallel()
		far, node := followThroughRelay(t)
		base := node.dials()
		node.setDown(false)
		far.u.drive()
		waitDials(t, far, node, base+1)
		if !node.Connected(2) || node.dialed[base] != "2" {
			t.Fatalf("dialled %v and linked to the host is %t; want the host's chain address, linked", node.dialed[base:], node.Connected(2))
		}
		far.u.drive()
		far.u.mu.Lock()
		direct := far.u.direct
		far.u.mu.Unlock()
		if _, following := far.u.followed(); !direct || following {
			t.Fatalf("linked to its host, direct is %t and following %t; want its own link", direct, following)
		}
		far.u.drive()
		if n := node.dials(); n != base+1 {
			t.Fatalf("%d dials once linked, want the one that linked it", n-base)
		}
	})
	t.Run("gives up on one it cannot reach", func(t *testing.T) {
		t.Parallel()
		far, node := followThroughRelay(t)
		base := node.dials()
		for i := 1; i <= parameter.NetworkHostDialAttempts; i++ {
			far.u.drive()
			far.u.drive() // paced: the second drive inside the wait dials nothing
			waitDials(t, far, node, base+i)
			far.r.mu.Lock()
			wait := far.r.nextHost.Sub(time.Now()).Round(parameter.NetworkRejoinPassInterval)
			far.r.nextHost = time.Time{} // as the wait passing
			far.r.mu.Unlock()
			if want := parameter.NetworkRejoinPassInterval << (i - 1); wait != want {
				t.Fatalf("dial %d waits %v before the next, want %v", i, wait, want)
			}
		}
		far.u.drive()
		if n := node.dials() - base; n != parameter.NetworkHostDialAttempts {
			t.Fatalf("%d dials of the host, want %d and then none", n, parameter.NetworkHostDialAttempts)
		}
		if _, following := far.u.followed(); !following {
			t.Fatal("past the bound the survivor stopped following the host it has")
		}
	})
}

// TestTheSuccessorDialIsPaced: a participant keeps a link to the first candidate
// warm; one its connect does not reach is dialled after a growing wait and then left,
// as a followed host is, and another successor, or a link to it that stood and
// dropped, starts the attempts over.
func TestTheSuccessorDialIsPaced(t *testing.T) {
	t.Parallel()
	mesh := network.NewMesh()
	mesh.Link(1, 2)
	mesh.Link(1, 3)
	mesh.Link(1, 4)
	node := &dialingNode{MeshPort: mesh.Node(4), mesh: mesh, down: true}
	chain := network.SuccessionChain{{ID: 2, Addr: "2"}, {ID: 3, Addr: "3"}, {ID: 4, Addr: "4"}}
	runs := make([]*run, 4)
	for i := range runs {
		var port engine.NetworkPort = mesh.Node(link.PeerID(i + 1))
		if i == 3 {
			port = node
		}
		runs[i] = newRun(t, uint32(i+1), port, roster(4))
		runs[i].open(1, chain, false)
	}
	far := runs[3]
	passWait := func() {
		far.r.mu.Lock()
		far.r.nextSucc = time.Time{}
		far.r.mu.Unlock()
	}
	for i := 1; i <= parameter.NetworkHostDialAttempts; i++ {
		far.u.drive()
		far.u.drive() // paced: the second drive inside the wait dials nothing
		waitDials(t, far, node, i)
		far.r.mu.Lock()
		wait := far.r.nextSucc.Sub(time.Now()).Round(parameter.NetworkRejoinPassInterval)
		far.r.mu.Unlock()
		if want := parameter.NetworkRejoinPassInterval << (i - 1); wait != want || node.dialed[i-1] != "2" {
			t.Fatalf("dial %d went to %q and waits %v before the next; want the first candidate and %v",
				i, node.dialed[i-1], wait, want)
		}
		passWait()
	}
	far.u.drive()
	if n := node.dials(); n != parameter.NetworkHostDialAttempts {
		t.Fatalf("%d dials of the successor, want %d and then none", n, parameter.NetworkHostDialAttempts)
	}

	far.u.mu.Lock()
	far.u.chain = network.SuccessionChain{{ID: 3, Addr: "3"}, {ID: 2, Addr: "2"}, {ID: 4, Addr: "4"}}
	far.u.mu.Unlock()
	far.u.drive()
	waitDials(t, far, node, parameter.NetworkHostDialAttempts+1)
	if last := node.dialed[len(node.dialed)-1]; last != "3" {
		t.Fatalf("another successor was dialled at %q, want its own address", last)
	}

	node.setDown(false)
	passWait()
	far.u.drive()
	waitDials(t, far, node, parameter.NetworkHostDialAttempts+2)
	if !node.Connected(3) {
		t.Fatal("the successor's connect went through and left no link")
	}
	far.u.drive() // linked: the attempts start over on a drop
	node.setDown(true)
	_ = mesh.Node(3).Close()
	for deadline := time.Now().Add(time.Second); node.Connected(3); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("the successor's link did not drop with its node")
		}
	}
	far.u.drive()
	waitDials(t, far, node, parameter.NetworkHostDialAttempts+3)
	far.r.mu.Lock()
	tries := far.r.succTries
	far.r.mu.Unlock()
	if tries != 1 {
		t.Fatalf("after the link dropped the successor is on attempt %d, want the first again", tries)
	}
}

// TestACutOffParticipantGoesOnAlone: one with no path to its authority, through a
// relay that left or carries it no more, plays on alone once a walk down the whole
// list has passed with nobody carrying it.
func TestACutOffParticipantGoesOnAlone(t *testing.T) {
	t.Parallel()
	r := session(t, 3, nil)[2]
	r.world.cut = true
	r.u.drive()
	if r.u.State().Fork {
		t.Fatal("a participant went on alone the moment it lost its path")
	}
	rewind(r, time.Duration(len(r.u.Chain())+1)*parameter.NetworkSuccessionWindow)
	r.u.drive()
	if !r.u.State().Fork || r.world.forked != 1 {
		t.Fatalf("past a whole walk: fork %v, told %d times", r.u.State().Fork, r.world.forked)
	}
}

// TestACutOffPersonRedialsItsRelayFirst: a participant that lost its path at the
// relay it joined through redials that relay for one window before it walks, as a
// survivor redials its lost host; refused, or past the window, it walks every
// candidate, and admitted back it takes no turn, the run admitted being in its place.
func TestACutOffPersonRedialsItsRelayFirst(t *testing.T) {
	t.Parallel()
	cut := func() *run {
		r := session(t, 3, nil)[2]
		r.u.Open(network.SessionOffer{
			Host: 1, Assigned: 3, Term: network.FirstTerm, Roster: r.world.WorldRoster(), Chain: chainOf(2, 3), Relay: 2,
		}, 3)
		r.world.cut, r.world.redial = true, true
		r.u.drive()
		if targets := r.u.walkTargets(time.Now()); !slices.Equal(r.world.redials, []link.PeerID{2}) || targets != nil {
			t.Fatalf("cut off, redials %v and dials %v; want the relay redialled and the walk waiting", r.world.redials, targets)
		}
		return r
	}
	refused := cut()
	refused.u.RedialRefused(2)
	timed := cut()
	rewind(timed, parameter.NetworkSuccessionWindow)
	timed.u.drive()
	admitted := cut()
	if !admitted.u.RedialAdmitted(2) || !admitted.u.State().Fork {
		t.Fatal("admitted back, the run was not left to the one in its place")
	}
	now := time.Now()
	for name, r := range map[string]*run{"refused": refused, "timed out": timed} {
		if got, want := r.u.walkTargets(now), []link.PeerID{2}; !slices.Equal(got, want) {
			t.Fatalf("%s, dials %v; want every candidate %v", name, got, want)
		}
		if r.u.State().Fork || len(r.world.redials) != 1 {
			t.Fatalf("%s: fork %v, redials %v; want the walk, after the one redial", name, r.u.State().Fork, r.world.redials)
		}
	}
	rewind(timed, time.Duration(len(timed.u.Chain()))*parameter.NetworkSuccessionWindow)
	timed.u.drive()
	if !timed.u.State().Fork || timed.world.forked != 1 {
		t.Fatalf("past the redial and a whole walk: fork %v, told %d times", timed.u.State().Fork, timed.world.forked)
	}
	kept := session(t, 3, nil)[2]
	kept.world.cut, kept.world.redial = true, true
	kept.u.drive()
	if len(kept.world.redials) != 0 || kept.u.walkTargets(now) == nil {
		t.Fatalf("one that joined at the host redials %v and dials %v when cut off; want no redial and the walk", kept.world.redials, kept.u.walkTargets(now))
	}
}

// TestAGuestAdmitsAPeerLink is the failure a live session hit: admission read the
// coordinator's roster, which a guest never fills, so a survivor's link to the
// successor was refused and both forked. Admission is roster membership, and
// deliberately not the term — a survivor still electing is exactly who needs to
// open one.
func TestAGuestAdmitsAPeerLink(t *testing.T) {
	t.Parallel()
	guest := session(t, 3, [][2]int{{1, 2}, {1, 3}})[2]

	if err := guest.r.admitPeerLink(2); err != nil {
		t.Fatalf("a guest refused a participant of its own session: %v", err)
	}
	if err := guest.r.admitPeerLink(9); err == nil {
		t.Fatal("a guest admitted a participant that is not in the session")
	}
	if err := guest.r.admitPeerLink(3); err == nil {
		t.Fatal("a guest admitted a link from itself")
	}
}

// TestALinkIsBudgetedPerParticipantItCarries: over a window a link may send the
// ingress budget for itself, each bot it holds and each person the authority has no
// link to; past that it is evicted.
func TestALinkIsBudgetedPerParticipantItCarries(t *testing.T) {
	t.Parallel()
	mesh := network.NewMesh()
	mesh.Link(1, 2)
	mesh.Link(1, 4)
	mesh.Link(2, 5)
	host := newRun(t, 1, mesh.Node(1), []network.RosterEntry{
		{ID: 1}, {ID: 2, Slot: 1}, {ID: 3, Slot: 2, Holder: 2}, {ID: 4, Slot: 3}, {ID: 5, Slot: 4},
	})
	host.open(1, nil, true)
	host.u.SetSlowPolicy(SlowPolicy{Window: time.Second})
	host.u.ingress = 100
	host.world.ingress = map[uint32]uint64{}
	start := time.Unix(1_000_000, 0)

	host.u.driveIngress(start)
	host.world.ingress[2], host.world.ingress[4] = 250, 250
	host.u.driveIngress(start.Add(time.Second))
	if len(host.world.dropped) != 1 || host.world.dropped[0] != 4 {
		t.Fatalf("evicted %v, want only participant 4, which holds no bot", host.world.dropped)
	}
}

// TestOnlyTheParticipantThatStaysLateIsEvicted: the first window after arrival is
// grace, a participant whose own crossings keep landing late is dropped at the end
// of a window, and one that is never late is kept whatever the session costs it.
func TestOnlyTheParticipantThatStaysLateIsEvicted(t *testing.T) {
	t.Parallel()
	host := newRun(t, 1, nil, roster(3))
	host.open(1, nil, true)
	host.u.SetSlowPolicy(SlowPolicy{Window: time.Second, LatePerSecond: 1})
	host.world.late = map[uint32]uint64{}
	start := time.Unix(1_000_000, 0)

	host.u.driveEviction(start)
	host.world.late[2] = 50
	host.u.driveEviction(start.Add(2 * time.Second))
	if len(host.world.dropped) != 0 {
		t.Fatalf("evicted %v inside the grace window", host.world.dropped)
	}
	host.world.late[2] = 100
	host.u.driveEviction(start.Add(4 * time.Second))
	if len(host.world.dropped) != 1 || host.world.dropped[0] != 2 {
		t.Fatalf("evicted %v, want only the late participant 2", host.world.dropped)
	}
}
