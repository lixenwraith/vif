package converge

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/snapshot"
	"github.com/lixenwraith/vif/internal/vlog"
	"github.com/lixenwraith/vif/pkg/linkpace"
)

// Corrections is one instance's half of the correction protocol. A run is a host or
// a guest, not both, so the two field sets are never both live; one object makes
// "this run is in a session" one lifetime rather than two that can disagree.
type Corrections struct {
	inst Instance
	tel  snapshot.Telemetry

	// authority is the term gate and the succession. It decides whether an
	// artifact may be acted on and whether this instance is the one publishing;
	// this decides what is published and what is installed.
	authority *Authority

	// publishMu serialises every world read a capture makes, so a join re-uses a
	// keyframe fresh enough and two joins arriving together share one read. It also
	// owns the per-peer schedule, so a peer's plan and the capture it is planned
	// against cannot be read a decision apart.
	publishMu   sync.Mutex
	baseline    snapshot.SharedCapture // last keyframe published; every delta names its tick
	keyBody     []byte                 // that keyframe as a bare capture, which is what a join sends
	haveKey     bool
	lastKeyTick uint64 // the tick that keyframe describes

	// sizes is the measured wire cost of each shape. At the storm high water a
	// keyframe is ~15.4 KiB against ~7.1 KiB for a delta, far enough apart that one
	// number would misprice the operating point.
	sizes     linkpace.Sizes
	haveSizes bool

	// bounds is the envelope every peer's controller moves inside, resolved once
	// so a session cannot hold two ideas of where its floor is.
	bounds linkpace.Bounds

	// peers is one publisher per directly linked participant: its controller, its
	// operating point, and the tick it is next due. A relayed participant has no
	// entry; it rides the schedule of whichever neighbour forwards to it.
	peers map[uint32]*peerPublisher

	// The operating point as last decided. base is the publication timeline's
	// granularity — the fastest peer's cadence — and keyPeriod the ticks between
	// whole worlds, session-wide because every guest holds the keyframe a delta names.
	base          uint64
	keyPeriod     uint64
	breached      bool
	saidFloor     bool
	saidUnrelayed bool
	nextBroadcast uint64

	// driven marks a run whose caller paces the cadence itself. Two publishers on one
	// cadence leave the receiver holding a world newer than the one the driver was
	// describing, so the first driven publish retires the pump.
	driven atomic.Bool

	// inbox holds reassembled correction bodies a guest has not applied yet: written
	// under the world lock by the tick that drained the last chunk, read between two
	// ticks by whatever drives this instance. It holds nothing but bytes.
	inboxMu sync.Mutex
	inbox   [][]byte
	dropped int64

	// installed is the guest's baseline: the last capture it installed whole; a delta
	// naming any other tick is counted rather than applied. keyTick is the receiving
	// end of the convergence floor — the last whole authoritative world that arrived
	// — so the distance from it to now is whether the floor is being met here.
	installedMu    sync.Mutex
	installed      snapshot.SharedCapture
	haveBase       bool
	lastInstalled  uint64
	keyTick        uint64
	guestBreached  bool
	saidGuestFloor bool

	// held is the playout buffer: a resolved correction whose authority tick this
	// instance has not reached yet, kept until it has. At most one waits.
	held     snapshot.SharedCapture
	haveHeld bool

	// selective is the manifest exchange's state on whichever side this run is.
	// selectiveMu covers the receiver's half, written by the tick that drained a
	// frame; publishMu covers the host's, which belongs to the publication schedule.
	selectiveMu sync.Mutex
	selective   selectiveState

	// authorityMu covers the succession queue: frames and departures arrive under
	// the world lock and are acted on between two ticks. Same lock-order rule as
	// selectiveMu: a queue an inbound frame appends to may not sit behind
	// publishMu.
	authorityMu sync.Mutex
	authorityIn []authorityFrame
	peersLost   []uint32

	applyMu sync.Mutex
	stop    chan struct{}
	wake    chan struct{}

	// The pump and the corrector are separate lifetimes because one run can be
	// both: a guest that becomes the authority keeps its apply loop and gains a
	// publication cadence. One shared Once would retire whichever started second.
	pumpDone    chan struct{}
	correctDone chan struct{}
	pumpOnce    sync.Once
	correctOnce sync.Once
	closeOnce   sync.Once
}

// peerPublisher is one direct link's schedule. The controller is per peer because
// the link is: two participants on one host can be a LAN cable and a phone, and a
// cadence chosen for the pair is wrong for both.
type peerPublisher struct {
	ctrl    *linkpace.Controller
	plan    linkpace.Plan
	demand  linkpace.Demand
	metrics linkpace.Metrics

	// nextTick is the tick this peer is next due a correction. Keyframes ignore
	// it: a guest that missed one refuses every delta that follows until the next,
	// so a keyframe goes to everyone and resets everyone's schedule.
	nextTick uint64

	// near is how many entities the last correction moved stood within the relevance
	// radius of this participant's cursor, share how far that stands above the
	// session's mean in percent. magEWMA is the far end's own recent magnitude, which
	// is what makes a *rise* detectable: a level says how busy the world is, only a
	// rise says the cadence is falling behind it.
	near    int
	share   int
	magEWMA float64
	haveMag bool

	sent    int64
	refused int64

	// The manifest exchange's per-peer standing: the last index sent, the last
	// answered, and how many have gone unanswered — which decides whether the peer
	// can still be repaired selectively or is owed a whole body. converged records an
	// answer that needed no state, the hash-only correction from the publishing side.
	manifestTick uint64
	answeredTick uint64
	silence      int
	converged    bool

	// relayed names the participants this peer forwards the index to and holds
	// retention for. The authority cannot see past its own links; the neighbour that
	// can is the one that says so.
	relayed []uint32

	// wide counts the publications this peer is owed a whole body for because its
	// last repair would have been wider than the world it was repairing toward.
	// It is a property of how far this participant's prediction has drifted rather
	// than of its link, so it decays on its own and the index is tried again.
	wide int
}

// newCorrections builds the correction half of a session. It starts nothing: a
// pump belongs to a host and a corrector to a guest, and which this run is only
// becomes true when a transport is attached.
func newCorrections(inst Instance, tel snapshot.Telemetry) *Corrections {
	return &Corrections{
		inst:   inst,
		tel:    tel,
		bounds: cadenceBounds(),
		peers:  make(map[uint32]*peerPublisher),
		base:   parameter.SnapshotCorrectionTicks,
		keyPeriod: parameter.SnapshotCorrectionTicks *
			parameter.SnapshotKeyframeCorrections,
		stop:        make(chan struct{}),
		wake:        make(chan struct{}, 1),
		pumpDone:    make(chan struct{}),
		correctDone: make(chan struct{}),
	}
}

// keyframePoll is how often KeyframeAt looks at a clock that has not yet reached
// the tick a join asked to be described a world at.
const keyframePoll = time.Millisecond

// cadenceBounds is the envelope the correction controller may move inside. It is
// assembled here because the envelope is a linkpace value and pkg may not see
// internal: this is the one place the game's numbers and the controller's contract
// meet, and the one place a test can check they still agree.
func cadenceBounds() linkpace.Bounds {
	return linkpace.Bounds{
		TickInterval:        parameter.GameUpdateInterval,
		MinCadenceTicks:     parameter.SnapshotCadenceMinTicks,
		NominalCadenceTicks: parameter.SnapshotCorrectionTicks,
		MaxCadenceTicks:     parameter.SnapshotCadenceMaxTicks,
		MinKeyframe:         parameter.SnapshotKeyframeMinCorrections,
		NominalKeyframe:     parameter.SnapshotKeyframeCorrections,
		MaxKeyframe:         parameter.SnapshotKeyframeMaxCorrections,
		FloorKeyframeTicks:  parameter.SnapshotFloorKeyframeTicks,
		Utilisation:         parameter.SnapshotLinkUtilisation,
		UrgentDrift:         parameter.SnapshotUrgentDriftPercent,
		UrgentRelevance:     parameter.SnapshotUrgentRelevancePercent,
		QuietCadence:        parameter.SnapshotCadenceQuietTicks,
		RecoverStepTicks:    parameter.SnapshotCadenceRecoverTicks,
		RecoverStepKeyframe: parameter.SnapshotCadenceRecoverKeyframe,
	}
}

// === host: publication ===

// StartPump runs the host's cadence. Ticks are the unit rather than seconds because
// the cadence is a property of the simulation: a paused or slowed run should not
// keep publishing a world that is not moving.
func (c *Corrections) StartPump() {
	c.pumpOnce.Do(func() { go c.pump() })
}

// pump publishes on the cadence and whenever a join asks for a keyframe. It runs
// on its own goroutine because the encode, diff and chunking hold no lock and
// would otherwise charge the simulation. The ticker runs at the fastest cadence
// the bounds allow rather than the one in force, and a wake with nothing due reads
// no world — so a peer whose link improved does not wait out the old cadence.
func (c *Corrections) pump() {
	defer close(c.pumpDone)
	interval := parameter.SnapshotCadenceMinTicks * parameter.GameUpdateInterval
	ticker := time.NewTicker(interval) // [wall] a publication cadence, not a game clock
	defer ticker.Stop()

	for {
		select {
		case <-c.stop:
			return
		case <-c.wake:
		case <-ticker.C:
		}
		if c.driven.Load() {
			continue // this run's caller paces its own corrections
		}
		// The authority's half of the selective exchange. Both halves belong between
		// two ticks — serving hashes and compresses, answering reads a world — and
		// on an interactive host this goroutine is the only thing that is.
		c.Apply()
		if err := c.PublishDue(); err != nil {
			vlog.Warn("app", "msg", "correction not published", "error", err.Error())
		}
	}
}

// Publish sends the next correction to every peer, whatever their schedule says.
// It is the driven form — a caller that paces its own cadence — and the immediacy
// is the point: a caller that asked for a correction now means now. It also
// retires the pump, so the run has one thing deciding when a correction leaves.
func (c *Corrections) Publish() error {
	c.driven.Store(true)
	return c.publishRound(true)
}

// PublishDue sends to the peers the adaptive schedule has made due. It is what the
// pump calls, and the counterpart of Publish for a driven run: Publish means now,
// this means when the schedule says.
func (c *Corrections) PublishDue() error { return c.publishRound(false) }

// publishRound is one decision and at most one world read. Each peer's controller
// decides from its own link and demand; the session composes those into one
// timeline, base cadence the fastest peer's and keyframe period the longest any
// peer planned, because every peer must hold the keyframe a delta names. One
// capture, one encode, one chunking; what is per peer is who receives it and when.
func (c *Corrections) publishRound(force bool) error {
	port := c.inst.Transport()
	if port == nil || !port.IsRunning() || port.PeerCount() == 0 {
		return nil
	}

	c.publishMu.Lock()
	defer c.publishMu.Unlock()

	link, _ := port.(engine.LinkMeasuringPort)
	ids := c.peerIDs(link)
	if link == nil {
		// A transport that cannot name its links cannot be scheduled per peer, so
		// the session keeps the nominal operating point and broadcasts. Adaptation
		// improves a measured link; it is not a requirement for a working one.
		return c.publishBroadcast(port, force)
	}
	if len(ids) == 0 {
		return nil
	}
	c.decideLocked(ids, link)

	c.forgetRestartedRunLocked()
	tick := c.inst.Position().Tick
	keyframe := (!c.haveKey || tick >= c.lastKeyTick+c.keyPeriod) && !c.allProvedLocked(ids, tick)
	due := c.dueLocked(ids, tick, force, keyframe)
	if len(due) == 0 {
		c.publishPlanTelemetryLocked(ids)
		return nil
	}

	cap, err := c.readWorld()
	if err != nil {
		return err
	}

	// One index per publication, whether or not it leads the correction. A keyframe
	// does not carry it, but retention answers a request naming that tick and is this
	// instance's evidence that its world is current if it is ever elected.
	index, err := snapshot.BuildManifest(cap, c.inst.LocalParticipant())
	if err != nil {
		return fmt.Errorf("correction manifest: %w", err)
	}
	c.retainLocked(cap, index, true)

	// A non-keyframe cadence leads with the index rather than the body. A peer
	// still answering manifests is repaired selectively, which on a converged link
	// moves no state at all; only the peers the index did not reach are owed the
	// whole difference. The keyframe cadence is untouched: it is the convergence
	// floor.
	bodyPeers := due
	if !keyframe {
		missed, mErr := c.publishManifest(port, index, due)
		switch {
		case mErr != nil:
			vlog.Warn("app", "msg", "correction index not published", "error", mErr.Error())
		case c.canAnswerEveryParticipantLocked(ids):
			bodyPeers = missed
		default:
			// A participant nobody can answer is still owed an authority, and the
			// only thing that reaches it is the flood. The index still goes to the
			// direct peers — it is what a relaying neighbour answers, and its answer
			// is how this instance learns the session has become answerable — but
			// the whole body goes out beside it until that is true.
		}
	}

	body, joinBody, encodeDur, err := c.encodeBodyLocked(cap, keyframe, keyframe || len(bodyPeers) > 0)
	if err != nil {
		return err
	}
	var chunks [][]byte
	if len(body) > 0 {
		if chunks, err = network.EncodeSnapshotChunks(cap.Header.Tick, body); err != nil {
			return fmt.Errorf("correction chunk: %w", err)
		}
	}

	// Relevance is measured against what this correction actually moves, so it
	// steers the *next* decision rather than this one. A one-cycle loop is the
	// honest shape: the alternative is scoring a delta before computing it.
	near := c.relevanceLocked(cap, keyframe, link, ids)

	c.scoreRelevanceLocked(ids, near)

	sent := 0
	for _, id := range due {
		p := c.peers[id]
		if slices.Contains(bodyPeers, id) && len(chunks) > 0 {
			if !c.sendTo(port, id, chunks) {
				p.refused++
				continue
			}
			sent++
		}
		p.sent++
		p.nextTick = tick + p.plan.CadenceTicks
	}

	c.recordPublicationLocked(cap, keyframe, body, joinBody, encodeDur, sent)
	if keyframe {
		// A whole world resets every peer's standing in the selective exchange: it
		// is the state each one now holds, so the next manifest is answered against
		// it rather than against whatever the peer had drifted to.
		for _, p := range c.peers {
			p.silence, p.wide = 0, 0
		}
	}
	c.publishPlanTelemetryLocked(ids)
	vlog.Debug("app", "msg", "correction published",
		"tick", cap.Header.Tick, "keyframe", keyframe, "bytes", len(body),
		"chunks", len(chunks), "whole_bodies", sent, "peers", len(due), "of", len(ids),
		"cadence_ticks", c.base, "keyframe_period_ticks", c.keyPeriod,
		"encode_us", encodeDur.Microseconds())
	return nil
}

// publishBroadcast is the unmeasured path: one correction to everyone on the
// nominal schedule, so a transport without link measurement keeps a working
// authority rather than a silent one. Caller MUST hold publishMu.
func (c *Corrections) publishBroadcast(port engine.NetworkPort, force bool) error {
	c.forgetRestartedRunLocked()
	tick := c.inst.Position().Tick
	keyframe := !c.haveKey || tick >= c.lastKeyTick+c.keyPeriod
	if !force && !keyframe && tick < c.nextBroadcast {
		return nil
	}
	cap, err := c.readWorld()
	if err != nil {
		return err
	}
	body, joinBody, encodeDur, err := c.encodeBodyLocked(cap, keyframe, true)
	if err != nil {
		return err
	}
	chunks, err := network.EncodeSnapshotChunks(cap.Header.Tick, body)
	if err != nil {
		return fmt.Errorf("correction chunk: %w", err)
	}
	for _, chunk := range chunks {
		port.Broadcast(uint8(network.MsgStateCorrection), chunk)
	}
	c.nextBroadcast = tick + c.base
	c.recordPublicationLocked(cap, keyframe, body, joinBody, encodeDur, 1)
	c.publishPlanTelemetryLocked(nil)
	return nil
}

// encodeBodyLocked encodes one capture into the shape the schedule asked for and
// returns what it cost. A keyframe is encoded twice: the bare capture is what the
// join handshake sends, and keeping it is what lets a join and a keyframe be one
// object. want is false when nobody is owed a body, which the selective exchange
// makes the ordinary case. Caller MUST hold publishMu.
func (c *Corrections) encodeBodyLocked(
	cap snapshot.SharedCapture, keyframe, want bool,
) (body, joinBody []byte, took time.Duration, err error) {
	if !want {
		return nil, nil, 0, nil
	}
	start := time.Now() // [wall] telemetry only; outside the world lock
	if keyframe {
		if body, err = snapshot.EncodeCorrection(cap); err == nil {
			joinBody, err = snapshot.EncodeCapture(cap)
		}
	} else {
		body, err = snapshot.EncodeCorrectionDelta(snapshot.DiffCapture(c.baseline, cap))
	}
	if err != nil {
		return nil, nil, 0, fmt.Errorf("correction encode: %w", err)
	}
	return body, joinBody, time.Since(start), nil
}

// recordPublicationLocked folds one publication into the cost model, the baseline
// and the telemetry. bodies is how many peers were sent the whole body, which is
// what the wire total is priced from.
//
// Caller MUST hold publishMu.
func (c *Corrections) recordPublicationLocked(
	cap snapshot.SharedCapture, keyframe bool, body, joinBody []byte, took time.Duration, bodies int,
) {
	if len(body) > 0 {
		c.recordSizeLocked(keyframe, len(body))
	}
	if keyframe {
		c.baseline, c.keyBody, c.haveKey, c.lastKeyTick = cap, joinBody, true, cap.Header.Tick
	}
	m := c.tel
	m.EncodeUS.Store(took.Microseconds())
	m.Bytes.Store(int64(len(body)))
	m.Sent.Add(1)
	m.SentBytes.Add(int64(len(body) * bodies))
	if keyframe {
		m.Keyframes.Add(1)
	}
}

// sendTo delivers one correction's chunks to one participant, reporting whether
// the whole of it was taken. A refused chunk ends that peer's transfer rather than
// continuing into a body that can only reassemble truncated; the next correction
// is self-sufficient, so the refusal is counted and the peer moves on.
func (c *Corrections) sendTo(port engine.NetworkPort, id uint32, chunks [][]byte) bool {
	for _, chunk := range chunks {
		if !port.Send(id, uint8(network.MsgStateCorrection), chunk) {
			return false
		}
	}
	return true
}

// peerIDs is the participants this instance sends corrections to directly, and
// retires the publishers of the ones that have gone.
func (c *Corrections) peerIDs(link engine.LinkMeasuringPort) []uint32 {
	if link == nil {
		return nil
	}
	ids := link.Peers()
	if len(ids) == 0 {
		return nil
	}
	for id := range c.peers {
		if !slices.Contains(ids, id) {
			delete(c.peers, id)
		}
	}
	return ids
}

// decideLocked takes one decision per peer and composes them into the session's
// timeline. Caller MUST hold publishMu.
func (c *Corrections) decideLocked(ids []uint32, link engine.LinkMeasuringPort) {
	sizes := c.sizesLocked()
	base, keyPeriod, breached := uint64(0), uint64(0), false

	for _, id := range ids {
		p, ok := c.peers[id]
		if !ok {
			ctrl, err := linkpace.NewController(c.bounds)
			if err != nil {
				// The envelope is a build constant; a controller that will not
				// build is a programming error rather than a link condition, and
				// the session keeps its nominal cadence rather than stopping.
				vlog.Error("app", "msg", "cadence bounds refused", "error", err.Error())
				return
			}
			p = &peerPublisher{ctrl: ctrl, plan: ctrl.Plan()}
			c.peers[id] = p
		}
		var m linkpace.Metrics
		if link != nil {
			m = link.LinkMetric(id)
		}
		p.metrics = m
		// The magnitude comes back on the peer's own echo: it is how far *that*
		// participant's prediction had drifted when the last correction reached
		// it. What is fed to the controller is the rise rather than the level —
		// see driftPercent — and the relevance beside it is this participant's
		// share of the last correction measured against the session's mean.
		if m.Samples > 0 {
			p.demand = linkpace.Demand{
				Known:     true,
				Drift:     p.driftPercent(m.Magnitude),
				Relevance: p.share,
				Idle:      p.near == 0 && m.Magnitude == 0,
			}
		}
		p.plan = p.ctrl.Update(m, sizes, p.demand)

		if base == 0 || p.plan.CadenceTicks < base {
			base = p.plan.CadenceTicks
		}
		if period := p.plan.KeyframePeriodTicks(); period > keyPeriod {
			keyPeriod = period
		}
		breached = breached || p.plan.FloorBreached
	}
	if base == 0 {
		base = c.bounds.NominalCadenceTicks
	}
	if keyPeriod == 0 || keyPeriod > c.bounds.FloorKeyframeTicks {
		keyPeriod = c.bounds.FloorKeyframeTicks
	}
	c.base, c.keyPeriod, c.breached = base, keyPeriod, breached
}

// dueLocked returns the peers to serve, in priority order: highest demand first,
// then whoever has waited longest, so priority is a preference and never a
// starvation. A keyframe goes to every peer whatever their cadence — a guest that
// missed one refuses every delta after it. Caller MUST hold publishMu.
func (c *Corrections) dueLocked(ids []uint32, tick uint64, force, keyframe bool) []uint32 {
	due := make([]uint32, 0, len(ids))
	for _, id := range ids {
		p := c.peers[id]
		if p == nil {
			continue
		}
		if force || keyframe || tick >= p.nextTick {
			due = append(due, id)
		}
	}
	slices.SortFunc(due, func(a, b uint32) int {
		pa, pb := c.peers[a], c.peers[b]
		if n := cmp.Compare(urgency(pb.demand), urgency(pa.demand)); n != 0 {
			return n
		}
		if n := cmp.Compare(pa.nextTick, pb.nextTick); n != 0 {
			return n
		}
		return cmp.Compare(a, b)
	})
	return due
}

// urgency scores a participant's claim on the next correction. It orders peers
// against each other and decides nothing on its own — what a peer actually
// receives is still its controller's plan, which the link bounds.
func urgency(d linkpace.Demand) int {
	if !d.Known {
		return 0
	}
	return d.Drift + d.Relevance
}

// driftPercent folds the far end's reported correction magnitude into a running
// level and returns how far this one stands above it. The rise rather than the
// level: on the storm scenario a correction moves the whole shared population every
// cadence, so any threshold on the level fires permanently.
func (p *peerPublisher) driftPercent(magnitude int) int {
	const smoothing = 0.25
	cur := float64(magnitude)
	out := 0
	if p.haveMag && p.magEWMA >= 1 {
		if rise := cur - p.magEWMA; rise > 0 {
			out = int(100 * rise / p.magEWMA)
		}
	}
	if !p.haveMag {
		p.magEWMA, p.haveMag = cur, true
	} else {
		p.magEWMA += smoothing * (cur - p.magEWMA)
	}
	return out
}

// sizesLocked is what a correction currently costs, falling back to the nominal
// world until one has been sent. Caller MUST hold publishMu.
func (c *Corrections) sizesLocked() linkpace.Sizes {
	if c.haveSizes {
		return c.sizes
	}
	return linkpace.Sizes{}
}

// blendSize folds one measurement into the cost model. An exponential average
// rather than the last value: the two shapes alternate, and a controller repriced
// from whichever went out last would swing sixfold every keyframe on this world.
func blendSize(cur int64, bytes int) int64 {
	const smoothing = 0.25
	if cur == 0 {
		return int64(bytes)
	}
	return cur + int64(smoothing*float64(int64(bytes)-cur))
}

// recordSizeLocked folds the correction just encoded into the cost model.
// Caller MUST hold publishMu.
func (c *Corrections) recordSizeLocked(keyframe bool, bytes int) {
	if keyframe {
		c.sizes.Keyframe = blendSize(c.sizes.Keyframe, bytes)
	} else {
		c.sizes.Delta = blendSize(c.sizes.Delta, bytes)
	}
	// A delta is only priceable once one has been produced. Until then the
	// keyframe stands in for both, which overprices a schedule and therefore
	// errs toward a slower cadence — the safe direction for a link nobody has
	// measured a delta over yet.
	if c.sizes.Delta == 0 {
		c.sizes.Delta = c.sizes.Keyframe
	}
	c.haveSizes = c.sizes.Keyframe > 0
}

// readWorld takes one capture and publishes what the read cost.
//
// Deliberately not named *Locked: that suffix means the caller holds the *world*
// lock, and here the caller must hold publishMu and must NOT hold the world lock,
// because this acquires it.
func (c *Corrections) readWorld() (snapshot.SharedCapture, error) {
	started := time.Now() // [wall] measures the stall this instance takes
	cap, err := c.inst.CaptureShared()
	dur := time.Since(started)
	if err != nil {
		return snapshot.SharedCapture{}, fmt.Errorf("session capture: %w", err)
	}
	c.tel.CaptureUS.Store(dur.Microseconds())
	return cap, nil
}

// forgetRestartedRunLocked drops the keyframe this host holds when it describes a
// game the run has since restarted. A reset re-bases the tick counter and every
// decision here reads ticks, so the stale keyframe would read as arbitrarily fresh.
// The run number is what distinguishes them. Caller MUST hold publishMu.
func (c *Corrections) forgetRestartedRunLocked() {
	if !c.haveKey || c.baseline.Header.Run == c.inst.Position().Run {
		return
	}
	vlog.Info("app", "msg", "keyframe dropped across a restart",
		"baseline_run", c.baseline.Header.Run, "run", c.inst.Position().Run)
	c.baseline, c.keyBody, c.haveKey, c.lastKeyTick = snapshot.SharedCapture{}, nil, false, 0
}

// KeyframeAt returns a keyframe describing the world at or after minTick, taking
// one if the newest is older than that. minTick is not a preference: an epoch
// flushed just before the admission reaches nobody and is not in a capture taken at
// the admission tick either, so the caller asks for one far enough ahead that every
// such artifact has been applied into it. A join reuses whatever the cadence made.
func (c *Corrections) KeyframeAt(minTick uint64, deadline time.Time) ([]byte, uint64, error) {
	for {
		c.publishMu.Lock()
		c.forgetRestartedRunLocked()
		if c.haveKey && c.baseline.Header.Tick >= minTick {
			body, tick := c.keyBody, c.baseline.Header.Tick
			c.publishMu.Unlock()
			return body, tick, nil
		}
		if c.inst.Position().Tick >= minTick {
			body, tick, err := c.takeKeyframe()
			c.publishMu.Unlock()
			return body, tick, err
		}
		c.publishMu.Unlock()

		if !time.Now().Before(deadline) { // [wall] a link bound, not a game one
			return nil, 0, fmt.Errorf(
				"session did not reach tick %d before the join deadline", minTick)
		}
		// Waiting for a tick the clock will never reach, if the run is closing. The
		// deadline alone would answer eventually; selecting on the stop is what
		// keeps a join arriving as the process shuts down from adding its whole
		// length to the time between SIGTERM and exit.
		select {
		case <-c.stop:
			return nil, 0, errors.New("session is shutting down")
		case <-time.After(keyframePoll):
		}
	}
}

// takeKeyframe reads the world and makes the result this host's baseline.
// Caller MUST hold publishMu, and MUST NOT hold the world lock.
func (c *Corrections) takeKeyframe() ([]byte, uint64, error) {
	cap, err := c.readWorld()
	if err != nil {
		return nil, 0, err
	}
	body, err := snapshot.EncodeCapture(cap)
	if err != nil {
		return nil, 0, fmt.Errorf("capture encode: %w", err)
	}
	c.baseline, c.keyBody, c.haveKey, c.lastKeyTick = cap, body, true, cap.Header.Tick
	c.recordSizeLocked(true, len(body))
	c.tel.Bytes.Store(int64(len(body)))
	c.tel.Keyframes.Add(1)
	vlog.Info("app", "msg", "session capture",
		"tick", cap.Header.Tick, "bytes", len(body),
		"streams", len(cap.Streams), "systems", len(cap.Systems))
	return body, cap.Header.Tick, nil
}

// === guest: reception ===

// Receive takes one reassembled correction body under the world lock, from the tick
// that drained the last chunk, so it does exactly one thing. A full queue drops the
// oldest: a correction supersedes every earlier one, so a guest that cannot keep up
// should lose the stale ones rather than the fresh.
func (c *Corrections) Receive(body []byte) {
	if c.authority.isAuthority() {
		return // this instance's own publication, back round a mesh flood with cycles
	}
	c.inboxMu.Lock()
	c.inbox = append(c.inbox, body)
	if n := len(c.inbox); n > parameter.SnapshotCorrectionQueue {
		c.inbox = keepNewest(c.inbox, parameter.SnapshotCorrectionQueue)
		c.dropped += int64(n - len(c.inbox))
	}
	c.inboxMu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// keepNewest bounds one of the protocol's queues in place, dropping the oldest. A
// correction supersedes every earlier one and so does a manifest, so an instance
// that cannot keep up should lose the stale end rather than the fresh.
func keepNewest[T any](q []T, n int) []T {
	if len(q) <= n {
		return q
	}
	return append(q[:0], q[len(q)-n:]...)
}

// StartCorrector runs the guest's apply loop. A tick runs entirely inside one
// acquisition of the update mutex, so a commit that takes it is between two ticks
// by construction — which is why this can be its own goroutine.
func (c *Corrections) StartCorrector() {
	c.correctOnce.Do(func() { go c.correct() })
}

func (c *Corrections) correct() {
	defer close(c.correctDone)
	for {
		// A wake accompanies every arrival; the timer is the backstop for a wake
		// lost to a full channel, and costs one queue check per cadence when idle.
		// A held correction is released by the clock and nothing wakes on that, so
		// while one is waiting the backstop runs at the tick instead.
		wait := parameter.SnapshotCadenceMinTicks * parameter.GameUpdateInterval // [wall]
		if c.holding() {
			wait = parameter.GameUpdateInterval
		}
		select {
		case <-c.stop:
			return
		case <-c.wake:
		case <-time.After(wait):
		}
		c.Apply()
	}
}

// Apply drains every correction waiting and installs the newest one that resolves;
// the rest are counted as superseded. It is serialised against itself, because two
// installs would share one staging world. The queue is walked in order because a
// delta is only meaningful against the baseline the entries before it left.
func (c *Corrections) Apply() {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()

	// Whatever the transport holds is translated first, without advancing a tick, so
	// the exchange completes inside one cadence rather than paying a tick per leg.
	// It is also what keeps the comparison tick-aligned: a receiver that saw a
	// manifest only as part of a tick would index a world one tick past the one it
	// describes, and never agree with the authority about anything.
	c.drainTransport()

	// The succession runs here for the same reason both halves of the selective
	// exchange do: it belongs between two ticks, and this is the one loop every
	// instance runs whichever half of the protocol it is.
	c.driveAuthority()

	// The playout buffer is released before anything new is resolved, so a
	// correction whose tick this instance has now reached is installed before a
	// fresher one can supersede it.
	c.releaseHeld()

	c.observeFloor()

	// Both halves of the manifest exchange belong between two ticks: serving a
	// request means hashing and compressing, answering one means reading this
	// instance's own world. A host reaches this from its pump or driver and serves
	// requests; a guest reaches it the same way and answers manifests.
	c.serveRequests()
	c.applySelective()

	c.inboxMu.Lock()
	pending := c.inbox
	c.inbox = nil
	dropped := c.dropped
	c.dropped = 0
	c.inboxMu.Unlock()

	if len(pending) == 0 {
		if dropped > 0 {
			c.tel.Superseded.Add(dropped)
		}
		return
	}
	c.tel.Superseded.Add(dropped)

	var (
		newest snapshot.SharedCapture
		found  bool
	)
	for _, body := range pending {
		cap, err := c.resolve(body)
		if err != nil {
			c.tel.Refused.Add(1)
			vlog.Debug("app", "msg", "correction refused", "error", err.Error())
			continue
		}
		if found && cap.Header.Tick <= newest.Header.Tick {
			c.tel.Superseded.Add(1)
			continue
		}
		if found {
			c.tel.Superseded.Add(1)
		}
		newest, found = cap, true
	}
	if !found {
		return
	}
	if err := c.install(newest); err != nil {
		vlog.Warn("app", "msg", "correction not applied",
			"tick", newest.Header.Tick, "error", err.Error())
	}
}

// drainTransport translates whatever the endpoint is holding without advancing a
// tick, so a manifest, a request or a repair is acted on the moment it lands
// rather than one tick later.
func (c *Corrections) drainTransport() { c.inst.DrainOffTick() }

// resolve turns one correction body into a whole capture proved by its own integrity
// hash, reconstructing a delta against the baseline this instance holds, so a corrupt
// keyframe or a delta applied to the wrong baseline is refused rather than installed
// as a world nobody has.
func (c *Corrections) resolve(body []byte) (snapshot.SharedCapture, error) {
	kind, full, delta, err := snapshot.DecodeCorrection(body)
	if err != nil {
		return snapshot.SharedCapture{}, err
	}
	// The term gate, applied where the artifact is decoded rather than where it is
	// queued: an inbound frame arrives under the world lock and the queue may only
	// take bytes. A correction from a term this instance has left is in flight
	// across a handoff and is dropped; one from a term it was never handed is the
	// split-brain case and is refused loudly.
	header := full.Header
	if kind == snapshot.CorrectionDelta {
		header = delta.Header
	}
	if !c.authority.admit(header.Term, 0) {
		return snapshot.SharedCapture{}, errors.New("correction carries a term this instance does not hold")
	}
	if kind == snapshot.CorrectionKeyframe {
		if sum, err := snapshot.Integrity(full); err != nil || sum != full.Header.Integrity {
			return snapshot.SharedCapture{}, errors.New("correction keyframe does not match its integrity hash")
		}
		c.SetBaseline(full)
		return full, nil
	}
	base, ok := c.baselineCapture()
	if !ok {
		return snapshot.SharedCapture{}, errors.New("correction delta arrived before any keyframe")
	}
	cap, err := snapshot.ApplyCaptureDelta(base, delta)
	if err != nil {
		return snapshot.SharedCapture{}, err
	}
	return cap, nil
}

// install stages a correction into the persistent staging world and commits it,
// publishing how far this instance had drifted. Two refusals: a correction the
// authority superseded, measured against the last one installed and never against
// this instance's own tick, which is itself a prediction; and one this instance has
// not yet reached, which is held rather than dropped.
func (c *Corrections) install(cap snapshot.SharedCapture) error {
	c.installedMu.Lock()
	stale := c.lastInstalled > 0 && cap.Header.Tick <= c.lastInstalled
	c.installedMu.Unlock()
	if stale {
		c.tel.Superseded.Add(1)
		return nil
	}
	if c.hold(cap) {
		return nil
	}
	return c.commit(cap)
}

// commit installs one correction whose playout time has come.
func (c *Corrections) commit(cap snapshot.SharedCapture) error {
	diff, err := c.inst.InstallCapture(cap)
	if err != nil {
		return err
	}
	c.installedMu.Lock()
	c.lastInstalled = cap.Header.Tick
	c.installedMu.Unlock()

	// What was just installed is provably the authority's world at that tick — a
	// whole correction re-checks its own integrity hash and a repair reproduces the
	// authority's root — so an index over it carries the authority's root and can
	// be served. That retention is a successor's evidence that its world is current
	// and a relay's ability to answer for a participant behind it.
	c.retainInstalled(cap)

	m := c.tel
	m.Applied.Add(1)
	m.CorrectionEntries.Store(int64(diff.Entries))
	m.CorrectionEntities.Store(int64(diff.Entities))
	m.CorrectionCells.Store(int64(diff.CellShift))
	m.CorrectionTick.Store(int64(cap.Header.Tick))
	return nil
}

// hold is the correction playout buffer: one ahead of the clock inside the lead
// waits for its tick, a newer arrival replacing an older one still waiting. One
// further ahead is a session this instance has fallen behind, adopted at once as
// the one step the clock takes forward; one behind the clock is projected by the
// commit instead. Reports whether the capture was deferred.
func (c *Corrections) hold(cap snapshot.SharedCapture) bool {
	at := c.inst.Position()
	if cap.Header.Run != at.Run || reached(cap, at.Tick) {
		return false
	}
	if cap.Header.Tick-at.Tick > c.holdWindow() {
		c.tel.Jumped.Add(1)
		return false
	}
	c.installedMu.Lock()
	c.held, c.haveHeld = cap, true
	c.installedMu.Unlock()
	c.tel.Held.Add(1)
	return true
}

// holdWindow is how far ahead of the clock a correction is worth waiting for.
func (c *Corrections) holdWindow() uint64 {
	return parameter.NetworkHoldTicks
}

// reached reports whether this instance's clock has arrived at the tick a
// correction describes. One predicate for both halves of the buffer, so deferring
// and releasing cannot disagree about what the buffer is for.
func reached(cap snapshot.SharedCapture, tick uint64) bool {
	return tick >= cap.Header.Tick
}

// releaseHeld installs the held correction once this instance has reached the tick
// it describes.
func (c *Corrections) releaseHeld() {
	c.installedMu.Lock()
	cap, have := c.held, c.haveHeld
	c.installedMu.Unlock()
	if !have || !reached(cap, c.inst.Position().Tick) {
		return
	}
	c.installedMu.Lock()
	c.held, c.haveHeld = snapshot.SharedCapture{}, false
	c.installedMu.Unlock()
	if err := c.commit(cap); err != nil {
		vlog.Warn("app", "msg", "held correction not applied",
			"tick", cap.Header.Tick, "error", err.Error())
	}
}

// dropHeld discards the playout buffer, for an instance that has stopped being a
// receiver.
func (c *Corrections) dropHeld() {
	c.installedMu.Lock()
	c.held, c.haveHeld = snapshot.SharedCapture{}, false
	c.installedMu.Unlock()
}

// holding reports whether a correction is waiting on the clock, which is what makes
// the apply loop poll at the tick rather than at the cadence.
func (c *Corrections) holding() bool {
	c.installedMu.Lock()
	defer c.installedMu.Unlock()
	return c.haveHeld
}

// allProvedLocked reports whether every peer has proved it holds the authority's
// world recently — a hash-only answer is that proof — so a keyframe would carry
// nothing any of them lacks. Caller MUST hold publishMu.
func (c *Corrections) allProvedLocked(ids []uint32, tick uint64) bool {
	for _, id := range ids {
		p := c.peers[id]
		if p == nil || !p.converged || tick >= p.answeredTick+parameter.SnapshotFloorKeyframeTicks/2 {
			return false
		}
	}
	return len(ids) > 0
}

// proved records that this instance holds the authority's world as of tick, which
// meets the convergence floor as a whole keyframe would.
func (c *Corrections) proved(tick uint64) {
	c.installedMu.Lock()
	c.keyTick = max(c.keyTick, tick)
	c.installedMu.Unlock()
}

// holdingThrough reports whether a held correction has become due by tick.
func (c *Corrections) holdingThrough(tick uint64) bool {
	c.installedMu.Lock()
	defer c.installedMu.Unlock()
	return c.haveHeld && c.held.Header.Tick <= tick
}

// SetBaseline records the keyframe later deltas are computed against, and the
// tick the convergence floor is measured from.
func (c *Corrections) SetBaseline(cap snapshot.SharedCapture) {
	c.installedMu.Lock()
	c.installed, c.haveBase, c.keyTick = cap, true, cap.Header.Tick
	c.installedMu.Unlock()
	// A whole world is the answer to every request the selective exchange can
	// make, so whatever this instance was waiting for it is no longer waiting.
	c.clearKeyframeWait()
	// Retained for the same reason an installed correction is: this capture came
	// off the gate as the authority's own world and is what a delta will be named
	// against. It is also what makes a guest electable from the moment it joins —
	// a successor needs a baseline, and until this a participant admitted seconds
	// before the host went had none.
	c.retainInstalled(cap)
}

// observeFloor is the guest's half of the convergence guarantee: the controller
// says whether the link can carry a whole world per floor window, this says whether
// one arrived. The grace is not slack — the floor is a publication guarantee and a
// receiver also pays transfer, reassembly and install, so reporting the instant the
// window elapses would fire on every ordinary slow keyframe.
func (c *Corrections) observeFloor() {
	c.installedMu.Lock()
	have, since := c.haveBase, c.keyTick
	c.installedMu.Unlock()
	if !have {
		return // nothing has been installed here; this run is not a receiver
	}

	tick := c.inst.Position().Tick
	age := uint64(0)
	if tick > since {
		age = tick - since
	}
	m := c.tel
	m.KeyframeAge.Store(int64(age))

	// The authoring instance is not a receiver: it produces the world every floor
	// window is measured against, so a successor that installed until it took the
	// term would report its own publication as an absence.
	breached := !c.authority.isAuthority() &&
		age > parameter.SnapshotFloorKeyframeTicks+parameter.SnapshotFloorGraceTicks
	m.FloorBreached.Store(breached)
	if breached {
		m.Constrained.Store(true)
	}

	c.installedMu.Lock()
	changed := breached != c.saidGuestFloor
	c.guestBreached, c.saidGuestFloor = breached, breached
	c.installedMu.Unlock()
	if !changed {
		return
	}
	if !breached {
		vlog.Info("app", "msg", "authoritative world arrived inside the convergence floor",
			"age_ticks", age)
		c.inst.SetStatusMessage("Link recovered; the authority is arriving again",
			parameter.StatusMessageDefaultTimeout, false)
		return
	}
	vlog.Warn("app", "msg", "no authoritative world inside the convergence floor",
		"age_ticks", age, "floor_ticks", parameter.SnapshotFloorKeyframeTicks,
		"grace_ticks", parameter.SnapshotFloorGraceTicks)
	c.inst.SetStatusMessage(
		"No authoritative world within the convergence floor; this instance is predicting alone",
		4*parameter.StatusMessageDefaultTimeout, true)
}

func (c *Corrections) baselineCapture() (snapshot.SharedCapture, bool) {
	c.installedMu.Lock()
	defer c.installedMu.Unlock()
	return c.installed, c.haveBase
}

// Close stops whichever half was started and waits for it. Closing a session that
// started neither — a harness that attached a transport and published nothing — is
// the ordinary case rather than an error, so the wait is skipped when the Once
// fires here.
func (c *Corrections) Close() {
	c.closeOnce.Do(func() {
		pumped, corrected := true, true
		c.pumpOnce.Do(func() { pumped = false })
		c.correctOnce.Do(func() { corrected = false })
		close(c.stop)
		if pumped {
			<-c.pumpDone
		}
		if corrected {
			<-c.correctDone
		}
	})
}

// currentSizes is what a correction currently costs on this world, for a caller deciding
// whether a link can carry the convergence floor. Until one has been published the
// answer is the keyframe a join was cut for, which is the same object measured the
// same way.
func (c *Corrections) currentSizes() linkpace.Sizes {
	c.publishMu.Lock()
	defer c.publishMu.Unlock()
	return c.sizes
}

// AdmitLink refuses a participant whose link cannot carry the convergence floor: it
// would play, drift, and have nothing scheduled that repairs it. The measurement is
// the join's own transfer, the one rate available before a probe has completed a
// round trip. It includes the joiner's install, so it understates the link — which
// errs toward refusing a marginal one.
func (c *Corrections) AdmitLink(port *network.SocketPort, id network.PeerID, bytes int, elapsed time.Duration) error {
	if bytes <= 0 || elapsed <= 0 {
		return nil
	}
	port.ObserveTransfer(uint32(id), int64(bytes), elapsed)
	sizes := c.currentSizes()
	if sizes.Keyframe == 0 {
		sizes.Keyframe = int64(bytes)
	}
	rate := linkpace.TransferRate(int64(bytes), elapsed)
	if err := linkpace.Admit(cadenceBounds(), rate, sizes); err != nil {
		return fmt.Errorf("participant %d: %w", id, err)
	}
	vlog.Debug("app", "msg", "join link measured",
		"peer", id, "bytes", bytes, "ms", elapsed.Milliseconds(),
		"bytes_per_second", int64(rate))
	return nil
}

// AdmitMeasuredLink is the same refusal against a link the probes have already
// measured, which is what a tick-zero lobby has: its port has been up for the
// whole wait, so several round trips have completed before the gate closes.
func (c *Corrections) AdmitMeasuredLink(port *network.SocketPort, id network.PeerID) error {
	sizes := c.currentSizes()
	if sizes.Keyframe == 0 {
		return nil
	}
	if err := linkpace.AdmitMetrics(cadenceBounds(), port.LinkMetric(uint32(id)), sizes); err != nil {
		return fmt.Errorf("participant %d: %w", id, err)
	}
	return nil
}
