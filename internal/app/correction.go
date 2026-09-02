// Package app: authority and correction.
//
// This is the file where the host stops being one participant among peers and
// becomes the authority, and where a guest stops re-deriving the session and starts
// predicting it.
//
// The two halves are deliberately asymmetric. The host publishes: it reads its own
// world on a cadence and broadcasts either the whole thing or the difference since
// the last whole one. A guest consumes: it keeps simulating, which is what makes
// its picture smooth and its own input immediate, and every correction that arrives
// replaces whatever its prediction had drifted to. Neither side negotiates and
// neither side re-derives the other's answer, which is the whole of D-11 as Phase 4
// weakened it: identical on the host, and on a guest equal to the host as of the
// last applied correction, and converging.
//
// Three properties are worth stating because they are what the design buys:
//
//   - Nothing acknowledges a correction. A keyframe supersedes everything before
//     it, so loss costs freshness for at most one keyframe interval and never
//     correctness. There is no repair path because there is nothing to repair.
//   - A correction never *fails*. It arrives late, or it is a delta against a
//     keyframe this instance does not hold, or it is superseded before it is
//     applied — and each of those is a counter rather than an error, because the
//     next one is self-sufficient.
//   - The magnitude is measured, not asserted. How far the guest's prediction had
//     drifted when the correction landed is the number that says whether the
//     cadence is right, and it is published where DESYNC used to be.
//
// Phase 5 changed what decides *when* the host publishes, and nothing else. The
// cadence was a constant; it is now a bounded controller per peer, driven by a
// real round trip, the delivery rate that round trip measures, and how much of
// what the next correction moves is near enough to that participant to matter.
// Three things are worth stating because they are what the change is bounded by:
//
//   - **One timeline, one baseline.** A correction is still computed once and is
//     still exact: every guest holds the same keyframe, and a delta names it.
//     What is per peer is which corrections a peer is *sent* — its cadence — and
//     that is the only thing relevance and priority move. Nothing is scoped, so
//     nothing about the authority weakens: a correction is the host's whole
//     world, or the exact difference from the last one, as it always was.
//
//   - **The floor is not adaptive.** Cadence and keyframe interval are
//     preferences the controller trades away under pressure; the guarantee that
//     a participant sees a whole authoritative world within
//     SnapshotFloorKeyframeTicks is not. A link that cannot carry that is
//     refused at admission and reported as an unrecoverable operating condition
//     mid-session — never quietly published to more slowly.
//
//   - **Timing paces the transport and enters nothing else.** No round trip,
//     delivery rate or jitter estimate reaches a component store, an RNG stream,
//     a replay or a game decision. They decide which frames leave a socket and
//     when, which is exactly the class of decision a session may make from the
//     wire.
package app

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// corrections is one instance's half of the authority protocol — whichever half it
// turns out to be. A run is a host or a guest, not both, so the two sets of fields
// are never both live; keeping them in one object is what makes "this run is in a
// session" a single lifetime rather than two that can disagree.
type corrections struct {
	a *App

	// publishMu serialises every world read a snapshot makes, which is what turns
	// the join's per-participant capture into a per-cadence one: a join that finds
	// a keyframe fresh enough already taken re-uses it, and two joins arriving
	// together share one read rather than stalling the world twice. It also owns
	// the whole per-peer schedule, so a peer's plan and the capture it is planned
	// against cannot be read a decision apart.
	publishMu   sync.Mutex
	baseline    SharedCapture // last keyframe published; every delta names its tick
	keyBody     []byte        // that keyframe as a bare capture, which is what a join sends
	haveKey     bool
	lastKeyTick uint64 // the tick that keyframe describes

	// sizes is what a correction currently costs on the wire, measured rather than
	// assumed. The controller prices a schedule from the compressed bodies it will
	// actually send. At the storm high water those are about 15.4 KiB for a
	// keyframe and 7.1 KiB for a delta, still different enough that a model using
	// one number would misprice the operating point.
	sizes     linkpace.Sizes
	haveSizes bool

	// bounds is the envelope every peer's controller moves inside, resolved once
	// so a session cannot hold two ideas of where its floor is.
	bounds linkpace.Bounds

	// peers is one publisher per directly linked participant: its controller, its
	// current operating point, and the tick it is next due. A participant reached
	// by relay has no entry, because this instance does not send to it — it rides
	// the schedule of whichever neighbour forwards to it, which is the honest
	// consequence of the flood and is stated in the docs rather than papered over.
	peers map[uint32]*peerPublisher

	// The session's operating point as last decided, kept for telemetry and for
	// the status bar. base is the publication timeline's granularity — the fastest
	// peer's cadence — and keyPeriod the ticks between whole worlds, which is
	// session-wide because every guest has to hold the keyframe a delta names.
	base          uint64
	keyPeriod     uint64
	breached      bool
	saidFloor     bool
	nextBroadcast uint64

	// driven marks a run whose caller paces the cadence itself. Two things pacing
	// one cadence is not a tuning problem, it is an ambiguity: a driver that
	// publishes at a tick it chose and a pump that publishes a moment later leave
	// the receiver holding a world *newer* than the one the driver was describing,
	// and nothing about that is recoverable from the outside. So the first driven
	// publish retires the pump, which is the documented division of labour
	// (PublishCorrection for a driven run, the pump for an interactive one) made
	// true by construction rather than by nobody using both.
	driven atomic.Bool

	// inbox holds reassembled correction bodies a guest has not applied yet. It is
	// written under the world lock by the tick that drained the last chunk and read
	// between two ticks by whatever drives this instance, so it is the one place
	// the two meet and it holds nothing but bytes.
	inboxMu sync.Mutex
	inbox   [][]byte
	dropped int64

	// installed is the guest's baseline: the last capture it installed whole. A
	// delta naming any other tick cannot be applied and is counted instead.
	//
	// keyTick beside it is the receiving end of the convergence floor. The host
	// promises a whole authoritative world every SnapshotFloorKeyframeTicks; this
	// is the tick of the last one that actually arrived, and the distance from
	// here to now is whether the promise is being kept on this link. A guest is
	// the participant the guarantee is *for*, so it is the participant that gets
	// to say when it is not being met.
	installedMu    sync.Mutex
	installed      SharedCapture
	haveBase       bool
	lastInstalled  uint64
	keyTick        uint64
	guestBreached  bool
	saidGuestFloor bool

	applyMu   sync.Mutex
	stop      chan struct{}
	wake      chan struct{}
	done      chan struct{}
	once      sync.Once
	closeOnce sync.Once
}

// peerPublisher is one direct link's schedule.
//
// The controller is per peer because the link is: two participants on one host
// can be a LAN cable and a phone, and a cadence chosen for the pair is wrong for
// both. What is *not* per peer is the correction itself — see the file comment.
type peerPublisher struct {
	ctrl    *linkpace.Controller
	plan    linkpace.Plan
	demand  linkpace.Demand
	metrics linkpace.Metrics

	// nextTick is the tick this peer is next due a correction. Keyframes ignore
	// it: a guest that missed one refuses every delta that follows until the next,
	// so a keyframe goes to everyone and resets everyone's schedule.
	nextTick uint64

	// near is how many of the entities the last correction moved stood within the
	// relevance radius of this participant's cursor, and share how far that stands
	// above the session's mean, in percent. magEWMA is the far end's own recent
	// correction magnitude, which is what makes a *rise* in it detectable — a
	// level says how busy the world is, and only a rise says the cadence is
	// falling behind it.
	near    int
	share   int
	magEWMA float64
	haveMag bool

	sent    int64
	refused int64
}

// newCorrections builds the correction half of a session. It starts nothing: a
// pump belongs to a host and a corrector to a guest, and which this run is only
// becomes true when a transport is attached.
func newCorrections(a *App) *corrections {
	return &corrections{
		a:      a,
		bounds: CadenceBounds(),
		peers:  make(map[uint32]*peerPublisher),
		base:   parameter.SnapshotCorrectionTicks,
		keyPeriod: parameter.SnapshotCorrectionTicks *
			parameter.SnapshotKeyframeCorrections,
		stop: make(chan struct{}),
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
	}
}

// CadenceBounds is the envelope the correction controller may move inside. It is
// assembled here rather than in the parameter package because the envelope is a
// linkpace value and pkg may not see internal — so this function is the one place
// the game's numbers and the controller's contract meet, and the one place a test
// can check they still agree.
func CadenceBounds() linkpace.Bounds {
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

// startPump runs the host's cadence. Ticks are the unit rather than seconds because
// the cadence is a property of the simulation: a paused or slowed run should not
// keep publishing a world that is not moving.
func (c *corrections) startPump() {
	c.once.Do(func() { go c.pump() })
}

// pump publishes on the cadence and whenever a join asks for a keyframe.
//
// It runs on its own goroutine and not on the tick loop, deliberately. The read is
// one acquisition of the world lock — 1.2 ms at the storm high water, 2.4% of a
// tick — and the encode, the diff and the chunking are the expensive part and hold
// no lock at all. Putting the whole thing inside a tick would charge the simulation
// for the encode; putting it on the accept goroutine is what Phase 3 did and is
// what made a join a per-participant world read.
//
// The ticker runs at the *fastest* cadence the bounds allow rather than at the
// cadence in force, and the schedule is checked against the simulation's tick
// rather than against the ticker. A wake with nothing due reads no world and
// sends nothing, so the cost of asking often is a channel receive, and the
// benefit is that a peer whose link just improved does not wait out the old
// cadence before the new one takes effect.
func (c *corrections) pump() {
	defer close(c.done)
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
		if err := c.publishDue(); err != nil {
			vlog.Warn("app", "msg", "correction not published", "error", err.Error())
		}
	}
}

// publish sends the next correction to every peer, whatever their schedule says.
// It is the driven form — a caller that paces its own cadence — and the immediacy
// is the point: a caller that asked for a correction now means now. It also
// retires the pump, so the run has one thing deciding when a correction leaves.
func (c *corrections) publish() error {
	c.driven.Store(true)
	return c.publishRound(true)
}

// publishDue sends to the peers the adaptive schedule has made due, and is what
// the pump calls.
func (c *corrections) publishDue() error { return c.publishRound(false) }

// publishRound is one decision and at most one world read.
//
// The shape is the whole of Phase 5's answer to "make the cadence a function of
// the link", and the ordering inside it is the part worth reading:
//
//  1. Every peer's controller takes a decision from its own measured link and its
//     own demand. That is where relevance and priority land — a participant whose
//     neighbourhood is churning asks for the minimum cadence, one with nothing
//     near it settles for the quiet one, and neither can ask for more than its
//     link carries.
//  2. The session composes those into one timeline: the base cadence is the
//     fastest peer's, and the keyframe period is the *longest* any peer planned,
//     capped by the floor. Longest rather than shortest because a keyframe is the
//     expensive frame and every peer has to hold the one a delta names, so the
//     session pays the cheapest whole-world period that still honours everyone's
//     floor.
//  3. One capture, one encode, one chunking. What is per peer is who receives it.
//  4. Due peers are served in priority order, so a bounded uplink spends itself on
//     the participants that need it before the ones that do not.
func (c *corrections) publishRound(force bool) error {
	port := c.a.sessionTransport()
	if port == nil || !port.IsRunning() || port.PeerCount() == 0 {
		return nil
	}

	c.publishMu.Lock()
	defer c.publishMu.Unlock()

	link, _ := port.(engine.LinkMeasuringPort)
	ids := c.peerIDs(link)
	if link == nil {
		// A transport that cannot name its links cannot be scheduled per peer, so
		// the session keeps the nominal operating point and broadcasts, which is
		// exactly what Phase 4 did. Adaptation is an improvement on a measured
		// link and never a requirement for a working one.
		return c.publishBroadcast(port, force)
	}
	if len(ids) == 0 {
		return nil
	}
	c.decideLocked(ids, link)

	tick := c.a.Position().Tick
	keyframe := !c.haveKey || tick >= c.lastKeyTick+c.keyPeriod
	due := c.dueLocked(ids, tick, force, keyframe)
	if len(due) == 0 {
		c.publishPlanTelemetryLocked(ids)
		return nil
	}

	cap, err := c.readWorld()
	if err != nil {
		return err
	}

	encodeStart := time.Now() // [wall] telemetry only; outside the world lock
	var body, joinBody []byte
	if keyframe {
		// Two encodings of one capture, and the second is not waste. A correction
		// travels in an envelope that says which of the two shapes it is; the join
		// handshake predates the envelope and sends a bare capture, because there
		// a capture is the only thing the message can be. Keeping the bare form is
		// what lets a join and a keyframe be the same object without the join
		// having to learn a shape it has no use for — and it is paid once per
		// keyframe rather than once per correction.
		body, err = EncodeCorrection(cap)
		if err == nil {
			joinBody, err = EncodeCapture(cap)
		}
	} else {
		body, err = EncodeCorrectionDelta(DiffCapture(c.baseline, cap))
	}
	if err != nil {
		return fmt.Errorf("correction encode: %w", err)
	}
	encodeDur := time.Since(encodeStart)

	chunks, err := network.EncodeSnapshotChunks(cap.Header.Tick, body)
	if err != nil {
		return fmt.Errorf("correction chunk: %w", err)
	}

	// Relevance is measured against what this correction actually moves, so it
	// steers the *next* decision rather than this one. A one-cycle loop is the
	// honest shape: the alternative is scoring a delta before computing it.
	near := c.relevanceLocked(cap, keyframe, link, ids)

	c.scoreRelevanceLocked(ids, near)

	sent := 0
	for _, id := range due {
		p := c.peers[id]
		if !c.sendTo(port, id, chunks) {
			p.refused++
			continue
		}
		p.sent++
		sent++
		p.nextTick = tick + p.plan.CadenceTicks
	}

	c.recordSizeLocked(keyframe, len(body))
	if keyframe {
		c.baseline, c.keyBody, c.haveKey, c.lastKeyTick = cap, joinBody, true, cap.Header.Tick
	}

	m := c.a.snapshotTelemetry
	m.encodeUS.Store(encodeDur.Microseconds())
	m.bytes.Store(int64(len(body)))
	m.sent.Add(1)
	m.sentBytes.Add(int64(len(body) * sent))
	if keyframe {
		m.keyframes.Add(1)
	}
	c.publishPlanTelemetryLocked(ids)
	vlog.Debug("app", "msg", "correction published",
		"tick", cap.Header.Tick, "keyframe", keyframe, "bytes", len(body),
		"chunks", len(chunks), "peers", sent, "of", len(ids),
		"cadence_ticks", c.base, "keyframe_period_ticks", c.keyPeriod,
		"encode_us", encodeDur.Microseconds())
	return nil
}

// publishBroadcast is the unmeasured path: one correction to everyone on the
// nominal schedule. It exists so a transport without link measurement — a
// harness port, an embedder's own — keeps a working authority rather than a
// silent one.
//
// Caller MUST hold publishMu.
func (c *corrections) publishBroadcast(port engine.NetworkPort, force bool) error {
	tick := c.a.Position().Tick
	keyframe := !c.haveKey || tick >= c.lastKeyTick+c.keyPeriod
	if !force && !keyframe && tick < c.nextBroadcast {
		return nil
	}
	cap, err := c.readWorld()
	if err != nil {
		return err
	}
	encodeStart := time.Now() // [wall] telemetry only; outside the world lock
	var body, joinBody []byte
	if keyframe {
		if body, err = EncodeCorrection(cap); err == nil {
			joinBody, err = EncodeCapture(cap)
		}
	} else {
		body, err = EncodeCorrectionDelta(DiffCapture(c.baseline, cap))
	}
	if err != nil {
		return fmt.Errorf("correction encode: %w", err)
	}
	encodeDur := time.Since(encodeStart)

	chunks, err := network.EncodeSnapshotChunks(cap.Header.Tick, body)
	if err != nil {
		return fmt.Errorf("correction chunk: %w", err)
	}
	for _, chunk := range chunks {
		port.Broadcast(uint8(network.MsgStateCorrection), chunk)
	}
	c.nextBroadcast = tick + c.base
	c.recordSizeLocked(keyframe, len(body))
	if keyframe {
		c.baseline, c.keyBody, c.haveKey, c.lastKeyTick = cap, joinBody, true, cap.Header.Tick
	}

	m := c.a.snapshotTelemetry
	m.encodeUS.Store(encodeDur.Microseconds())
	m.bytes.Store(int64(len(body)))
	m.sent.Add(1)
	m.sentBytes.Add(int64(len(body)))
	if keyframe {
		m.keyframes.Add(1)
	}
	c.publishPlanTelemetryLocked(nil)
	return nil
}

// sendTo delivers one correction's chunks to one participant, reporting whether
// the whole of it was taken.
//
// A chunk the peer's send queue refuses ends the transfer for that peer rather
// than continuing into a body that can only reassemble as a truncated one. There
// is nothing to repair — the next correction is self-sufficient and a keyframe
// supersedes everything before it — so the refusal is counted and the peer moves
// on, which is the same answer the rest of this protocol gives to loss.
func (c *corrections) sendTo(port engine.NetworkPort, id uint32, chunks [][]byte) bool {
	for _, chunk := range chunks {
		if !port.Send(id, uint8(network.MsgStateCorrection), chunk) {
			return false
		}
	}
	return true
}

// peerIDs is the participants this instance sends corrections to directly, and
// retires the publishers of the ones that have gone.
func (c *corrections) peerIDs(link engine.LinkMeasuringPort) []uint32 {
	if link == nil {
		return nil
	}
	ids := link.Peers()
	if len(ids) == 0 {
		return nil
	}
	live := make(map[uint32]struct{}, len(ids))
	for _, id := range ids {
		live[id] = struct{}{}
	}
	for id := range c.peers {
		if _, ok := live[id]; !ok {
			delete(c.peers, id)
		}
	}
	return ids
}

// decideLocked takes one decision per peer and composes them into the session's
// timeline. Caller MUST hold publishMu.
func (c *corrections) decideLocked(ids []uint32, link engine.LinkMeasuringPort) {
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

// dueLocked returns the peers to serve, in priority order. Caller MUST hold
// publishMu.
//
// A keyframe goes to every peer whatever their cadence: a guest that missed one
// refuses every delta that follows it, so withholding a keyframe from a slow peer
// would not save it bytes, it would cost it the rest of the interval.
//
// The order matters when the uplink cannot serve everyone in one round. Highest
// demand first — the participant whose neighbourhood is churning or whose
// prediction has drifted furthest — then whoever has waited longest, so priority
// is a preference and never a starvation.
func (c *corrections) dueLocked(ids []uint32, tick uint64, force, keyframe bool) []uint32 {
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
// level and returns how far this one stands above it.
//
// The level is what a busy world produces and is not by itself a reason to
// publish sooner: measured on the shipped storm scenario, a correction moves the
// whole shared population every cadence, so a threshold on the level fires
// permanently and spends the entire uplink on a condition that is simply what a
// storm looks like. A *rise* above the peer's own level is the thing worth
// reacting to, because it says the prediction is now drifting faster than the
// cadence repairs it.
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
func (c *corrections) sizesLocked() linkpace.Sizes {
	if c.haveSizes {
		return c.sizes
	}
	return linkpace.Sizes{}
}

// recordSizeLocked folds the correction just encoded into the cost model.
// Caller MUST hold publishMu.
//
// It is an exponential average rather than the last value because the two shapes
// alternate and both are needed: a controller repriced from whichever frame went
// out last would swing sixfold every keyframe on this world.
func (c *corrections) recordSizeLocked(keyframe bool, bytes int) {
	const smoothing = 0.25
	blend := func(cur int64) int64 {
		if cur == 0 {
			return int64(bytes)
		}
		return cur + int64(smoothing*float64(int64(bytes)-cur))
	}
	if keyframe {
		c.sizes.Keyframe = blend(c.sizes.Keyframe)
	} else {
		c.sizes.Delta = blend(c.sizes.Delta)
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
// Deliberately not named *Locked: in this package that suffix means the caller holds
// the *world* lock, and here the caller must hold publishMu and must NOT hold the
// world lock, because this acquires it.
func (c *corrections) readWorld() (SharedCapture, error) {
	started := time.Now() // [wall] measures the stall this instance takes
	cap, err := c.a.CaptureShared()
	dur := time.Since(started)
	if err != nil {
		return SharedCapture{}, fmt.Errorf("session capture: %w", err)
	}
	c.a.snapshotTelemetry.captureUS.Store(dur.Microseconds())
	return cap, nil
}

// keyframeAt returns a keyframe describing the world at or after minTick, taking
// one if the newest is older than that.
//
// minTick is not a preference. D-22 admits a joiner before the world is read for
// it, so that the epochs produced in between reach it rather than falling into the
// gap — but an epoch produced *before* the admission and flushed before it was
// registered reaches nobody, and it is not in a capture taken at the admission tick
// either, because its apply tick is still ahead. The caller therefore asks for a
// capture far enough ahead that every such artifact has already been applied into
// it, and the barrier's floor then drops the copies that do arrive.
//
// The reuse is the second half of the point. Phase 3 read the world once per join,
// on the accept goroutine; a join now takes whichever keyframe the cadence has
// already produced, and only reads the world itself when none is fresh enough.
func (c *corrections) keyframeAt(minTick uint64, deadline time.Time) ([]byte, uint64, error) {
	for {
		c.publishMu.Lock()
		if c.haveKey && c.baseline.Header.Tick >= minTick {
			body, tick := c.keyBody, c.baseline.Header.Tick
			c.publishMu.Unlock()
			return body, tick, nil
		}
		if c.a.Position().Tick >= minTick {
			body, tick, err := c.takeKeyframe()
			c.publishMu.Unlock()
			return body, tick, err
		}
		c.publishMu.Unlock()

		if !time.Now().Before(deadline) { // [wall] a link bound, not a game one
			return nil, 0, fmt.Errorf(
				"session did not reach tick %d before the join deadline", minTick)
		}
		time.Sleep(joinEpochPoll)
	}
}

// takeKeyframe reads the world and makes the result this host's baseline.
// Caller MUST hold publishMu, and MUST NOT hold the world lock.
func (c *corrections) takeKeyframe() ([]byte, uint64, error) {
	cap, err := c.readWorld()
	if err != nil {
		return nil, 0, err
	}
	body, err := EncodeCapture(cap)
	if err != nil {
		return nil, 0, fmt.Errorf("capture encode: %w", err)
	}
	c.baseline, c.keyBody, c.haveKey, c.lastKeyTick = cap, body, true, cap.Header.Tick
	c.recordSizeLocked(true, len(body))
	c.a.snapshotTelemetry.bytes.Store(int64(len(body)))
	c.a.snapshotTelemetry.keyframes.Add(1)
	vlog.Info("app", "msg", "session capture",
		"tick", cap.Header.Tick, "bytes", len(body),
		"streams", len(cap.Streams), "systems", len(cap.Systems))
	return body, cap.Header.Tick, nil
}

// === guest: reception ===

// receive takes one reassembled correction body. It runs under the world lock, from
// the tick that drained the last chunk, so it does exactly one thing.
//
// The queue drops the *oldest* when it is full. A correction supersedes every
// earlier one, so an instance that cannot keep up should lose the stale corrections
// rather than the fresh ones — the opposite choice would make a slow guest apply an
// ever-older authority.
func (c *corrections) receive(body []byte) {
	c.inboxMu.Lock()
	if len(c.inbox) >= parameter.SnapshotCorrectionQueue {
		c.inbox = append(c.inbox[:0], c.inbox[1:]...)
		c.dropped++
	}
	c.inbox = append(c.inbox, body)
	c.inboxMu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// startCorrector runs the guest's apply loop.
//
// A correction is applied between two ticks, and World.RunSafe is what makes that
// true by construction rather than by a handshake: a tick runs entirely inside one
// acquisition of the update mutex, so a commit that takes the mutex is necessarily
// between two of them. That is why this can be its own goroutine at all.
func (c *corrections) startCorrector() {
	c.once.Do(func() { go c.correct() })
}

func (c *corrections) correct() {
	defer close(c.done)
	// A wake accompanies every arrival; the ticker is the backstop for a wake lost
	// to a full channel, and costs one queue check per cadence when idle.
	ticker := time.NewTicker(parameter.SnapshotCadenceMinTicks * parameter.GameUpdateInterval) // [wall]
	defer ticker.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-c.wake:
		case <-ticker.C:
		}
		c.apply()
	}
}

// apply drains every correction waiting and installs the newest one that resolves.
//
// It is serialised against itself. A driven run reaches it from Tick and an
// interactive one from its own goroutine, and a run that is somehow both would
// otherwise have two installs sharing one staging world.
//
// Only the newest is installed, and the rest are counted as superseded rather than
// applied in turn: installing an older authority after a newer one would move this
// instance backwards, and a keyframe carries no information the one after it lacks.
// The exception is a delta, which is only meaningful against the baseline it names
// — so the queue is walked in order, each entry resolved against whatever baseline
// the previous ones left, and the last one that resolves is the one installed.
func (c *corrections) apply() {
	c.applyMu.Lock()
	defer c.applyMu.Unlock()

	c.observeFloor()

	c.inboxMu.Lock()
	pending := c.inbox
	c.inbox = nil
	dropped := c.dropped
	c.dropped = 0
	c.inboxMu.Unlock()

	if len(pending) == 0 {
		if dropped > 0 {
			c.a.snapshotTelemetry.superseded.Add(dropped)
		}
		return
	}
	c.a.snapshotTelemetry.superseded.Add(dropped)

	var (
		newest SharedCapture
		found  bool
	)
	for _, body := range pending {
		cap, err := c.resolve(body)
		if err != nil {
			c.a.snapshotTelemetry.refused.Add(1)
			vlog.Debug("app", "msg", "correction refused", "error", err.Error())
			continue
		}
		if found && cap.Header.Tick <= newest.Header.Tick {
			c.a.snapshotTelemetry.superseded.Add(1)
			continue
		}
		if found {
			c.a.snapshotTelemetry.superseded.Add(1)
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

// resolve turns one correction body into a whole capture, reconstructing a delta
// against the baseline this instance holds. Reconstruction re-checks the capture's
// own integrity hash, so a delta applied to the wrong baseline is refused rather
// than installed as a world nobody has.
func (c *corrections) resolve(body []byte) (SharedCapture, error) {
	kind, full, delta, err := DecodeCorrection(body)
	if err != nil {
		return SharedCapture{}, err
	}
	if kind == CorrectionKeyframe {
		c.setBaseline(full)
		return full, nil
	}
	base, ok := c.baselineCapture()
	if !ok {
		return SharedCapture{}, errors.New("correction delta arrived before any keyframe")
	}
	cap, err := ApplyCaptureDelta(base, delta)
	if err != nil {
		return SharedCapture{}, err
	}
	return cap, nil
}

// install stages a correction into the persistent staging world and commits it,
// publishing how far this instance had drifted on the way.
//
// The only correction refused here is one the authority already superseded. What is
// compared is the last correction *installed*, never this instance's own tick: a
// guest's tick is a prediction like everything else about it, and a correction that
// rebases it backwards is the ordinary case rather than an error — the capture
// describes tick T and the guest had already extrapolated past it.
func (c *corrections) install(cap SharedCapture) error {
	c.installedMu.Lock()
	stale := c.lastInstalled > 0 && cap.Header.Tick <= c.lastInstalled
	c.installedMu.Unlock()
	if stale {
		c.a.snapshotTelemetry.superseded.Add(1)
		return nil
	}
	staged, err := c.a.StageShared(cap)
	if err != nil {
		return err
	}
	if err := staged.Commit(); err != nil {
		return err
	}
	diff := staged.Difference()
	c.installedMu.Lock()
	c.lastInstalled = cap.Header.Tick
	c.installedMu.Unlock()

	m := c.a.snapshotTelemetry
	m.applied.Add(1)
	m.correctionEntries.Store(int64(diff.Entries))
	m.correctionEntities.Store(int64(diff.Entities))
	m.correctionCells.Store(int64(diff.CellShift))
	m.correctionTick.Store(int64(cap.Header.Tick))
	return nil
}

// setBaseline records the keyframe later deltas are computed against, and the
// tick the convergence floor is measured from.
func (c *corrections) setBaseline(cap SharedCapture) {
	c.installedMu.Lock()
	c.installed, c.haveBase, c.keyTick = cap, true, cap.Header.Tick
	c.installedMu.Unlock()
}

// observeFloor is the guest's half of the convergence guarantee, and it is a
// different claim from the host's.
//
// The host's controller says whether the *link* can carry a whole world per floor
// window; this says whether one actually arrived. They can disagree in both
// directions, and both disagreements are worth having: a host whose uplink looks
// adequate can still be failing a guest whose downlink is not, and a guest can
// keep converging across a window the host had already given up on.
//
// The grace is not slack in the promise. The floor is a *publication* guarantee
// and a receiver additionally pays the transfer, the reassembly and the install,
// so reporting the instant the window elapses would fire on every ordinary slow
// keyframe. One nominal keyframe period is the smallest margin that cannot.
func (c *corrections) observeFloor() {
	c.installedMu.Lock()
	have, since := c.haveBase, c.keyTick
	c.installedMu.Unlock()
	if !have {
		return // nothing has been installed here; this run is not a receiver
	}

	tick := c.a.Position().Tick
	age := uint64(0)
	if tick > since {
		age = tick - since
	}
	m := c.a.snapshotTelemetry
	m.keyframeAge.Store(int64(age))

	breached := age > parameter.SnapshotFloorKeyframeTicks+parameter.SnapshotFloorGraceTicks
	m.floorBreached.Store(breached)
	if breached {
		m.constrained.Store(true)
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
		c.a.ctx.SetStatusMessage("Link recovered; the authority is arriving again",
			parameter.StatusMessageDefaultTimeout, false)
		return
	}
	vlog.Warn("app", "msg", "no authoritative world inside the convergence floor",
		"age_ticks", age, "floor_ticks", parameter.SnapshotFloorKeyframeTicks,
		"grace_ticks", parameter.SnapshotFloorGraceTicks)
	c.a.ctx.SetStatusMessage(
		"No authoritative world within the convergence floor; this instance is predicting alone",
		4*parameter.StatusMessageDefaultTimeout, true)
}

func (c *corrections) baselineCapture() (SharedCapture, bool) {
	c.installedMu.Lock()
	defer c.installedMu.Unlock()
	return c.installed, c.haveBase
}

// close stops whichever half was started and waits for it. Closing a session that
// started neither — a harness that attached a transport and published nothing — is
// the ordinary case rather than an error, so the wait is skipped when the Once
// fires here.
func (c *corrections) close() {
	c.closeOnce.Do(func() {
		neverStarted := false
		c.once.Do(func() { neverStarted = true })
		close(c.stop)
		if !neverStarted {
			<-c.done
		}
	})
}

// === App surface ===

// receiveCorrection queues one reassembled correction. It is the seam every
// transport binds to — the one a service contributes at construction and the one a
// mid-run `:host` or a join attaches later — so a correction reaches the same queue
// however this run came to be in a session.
//
// Caller holds the world lock: this must do nothing but take the bytes.
func (a *App) receiveCorrection(_ uint64, body []byte) {
	if a.corrections != nil {
		a.corrections.receive(body)
	}
}

// ApplyPendingCorrections installs whatever authority has arrived, between two
// ticks. A caller-driven run reaches it from Tick; an interactive one has a
// goroutine of its own, because nothing on its side calls Tick.
func (a *App) ApplyPendingCorrections() {
	if a.corrections == nil {
		return
	}
	a.corrections.apply()
}

// PublishCorrection takes one authoritative capture and broadcasts it, for a driven
// run that paces its own cadence. An interactive host has the pump instead, and
// calling this retires it: a run with two things deciding when a correction leaves
// hands its guests a world newer than the one its driver was describing.
func (a *App) PublishCorrection() error {
	if a.corrections == nil {
		return errors.New("this run is not in a session")
	}
	return a.corrections.publish()
}

// adoptCorrectionBaseline records the capture a join installed, so the deltas that
// follow it have the keyframe they name. It is the same object from both ends: the
// host sent a keyframe and this is the instance that installed it.
func (a *App) adoptCorrectionBaseline(cap SharedCapture) {
	if a.corrections != nil {
		a.corrections.setBaseline(cap)
	}
}

// PeerCadence is one link's operating point and the measurements it came from.
type PeerCadence struct {
	Participant      uint32
	CadenceTicks     uint64
	KeyframeInterval int
	Constrained      bool
	FloorBreached    bool
	RTT              time.Duration
	Jitter           time.Duration
	ThroughputBps    float64
	Saturated        bool

	// Drift is the rise in this participant's own correction magnitude, in
	// percent; Relevance how far its share of the last correction stood above the
	// session's mean; Near the raw count behind that share.
	Drift     int
	Relevance int
	Near      int
}

// CadenceReport is the session's operating point: the timeline the host is
// publishing on, and the per-link decisions it was composed from.
//
// It exists because the aggregate telemetry cannot answer the question an
// operator actually has when one participant's picture is coarse — *which* link
// is the constrained one. The status bar shows the worst; this names it.
type CadenceReport struct {
	CadenceTicks        uint64
	KeyframePeriodTicks uint64
	KeyframeInterval    int
	Constrained         bool
	FloorBreached       bool
	KeyframeBytes       int64
	DeltaBytes          int64
	Peers               []PeerCadence
}

// CadenceReport describes what the correction cadence is doing. A run that is not
// publishing returns the zero value, which reads as "nominal, no links".
func (a *App) CadenceReport() CadenceReport {
	if a.corrections == nil {
		return CadenceReport{}
	}
	c := a.corrections
	c.publishMu.Lock()
	defer c.publishMu.Unlock()

	out := CadenceReport{
		CadenceTicks:        c.base,
		KeyframePeriodTicks: c.keyPeriod,
		FloorBreached:       c.breached,
		KeyframeBytes:       c.sizes.Keyframe,
		DeltaBytes:          c.sizes.Delta,
	}
	if c.base > 0 {
		out.KeyframeInterval = int(c.keyPeriod / c.base)
	}
	for id, p := range c.peers {
		out.Constrained = out.Constrained || p.plan.Constrained
		out.Peers = append(out.Peers, PeerCadence{
			Participant:      id,
			CadenceTicks:     p.plan.CadenceTicks,
			KeyframeInterval: p.plan.KeyframeInterval,
			Constrained:      p.plan.Constrained,
			FloorBreached:    p.plan.FloorBreached,
			RTT:              p.metrics.RTT,
			Jitter:           p.metrics.Jitter,
			ThroughputBps:    p.metrics.Throughput,
			Saturated:        p.metrics.Saturated,
			Drift:            p.demand.Drift,
			Relevance:        p.demand.Relevance,
			Near:             p.near,
		})
	}
	slices.SortFunc(out.Peers, func(x, y PeerCadence) int {
		return cmp.Compare(x.Participant, y.Participant)
	})
	return out
}

// cadenceSizes is what a correction currently costs on this world, for a caller
// deciding whether a link can carry the convergence floor. Until one has been
// published the answer is the keyframe a join was cut for, which is the same
// object measured the same way.
func (a *App) cadenceSizes() linkpace.Sizes {
	if a.corrections == nil {
		return linkpace.Sizes{}
	}
	a.corrections.publishMu.Lock()
	defer a.corrections.publishMu.Unlock()
	return a.corrections.sizes
}

// admitLink refuses a participant whose link cannot carry the convergence floor.
//
// This is the requirement's refusal path, and it is a deliberate refusal rather
// than a degraded session. A participant admitted onto a link that cannot deliver
// one whole authoritative world per floor window would play, would drift, and
// would have nothing scheduled that repairs it — the failure the whole
// authoritative model exists to prevent. Refusing is the same answer the join
// already gives when its catch-up gap exceeds the playout lead.
//
// The measurement is the join's own transfer, which is the one rate available
// before a probe has completed a round trip: the bytes went out, the joiner
// answered when it had them all, and the host was pushing the whole time. It
// includes the joiner's install, so it *understates* the link — which errs toward
// refusing a marginal one, and the margin between a working link and the floor is
// two orders of magnitude on this world.
func (a *App) admitLink(port *network.SocketPort, id network.PeerID, bytes int, elapsed time.Duration) error {
	if bytes <= 0 || elapsed <= 0 {
		return nil
	}
	port.ObserveTransfer(uint32(id), int64(bytes), elapsed)
	sizes := a.cadenceSizes()
	if sizes.Keyframe == 0 {
		sizes.Keyframe = int64(bytes)
	}
	rate := linkpace.TransferRate(int64(bytes), elapsed)
	if err := linkpace.Admit(CadenceBounds(), rate, sizes); err != nil {
		return fmt.Errorf("participant %d: %w", id, err)
	}
	vlog.Debug("app", "msg", "join link measured",
		"participant", id, "bytes", bytes, "ms", elapsed.Milliseconds(),
		"bytes_per_second", int64(rate))
	return nil
}

// admitMeasuredLink is the same refusal against a link the probes have already
// measured, which is what a tick-zero lobby has: its port has been up for the
// whole wait, so several round trips have completed before the gate closes.
func (a *App) admitMeasuredLink(port *network.SocketPort, id network.PeerID) error {
	sizes := a.cadenceSizes()
	if sizes.Keyframe == 0 {
		return nil
	}
	if err := linkpace.AdmitMetrics(CadenceBounds(), port.LinkMetric(uint32(id)), sizes); err != nil {
		return fmt.Errorf("participant %d: %w", id, err)
	}
	return nil
}

// correctionMagnitude reports the last correction's size, for a caller that wants
// the number without reading the registry.
func (a *App) correctionMagnitude() engine.WorldDifference {
	m := a.snapshotTelemetry
	return engine.WorldDifference{
		Entries:   int(m.correctionEntries.Load()),
		Entities:  int(m.correctionEntities.Load()),
		CellShift: int(m.correctionCells.Load()),
	}
}
