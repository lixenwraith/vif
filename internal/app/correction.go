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
package app

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
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
	// together share one read rather than stalling the world twice.
	publishMu sync.Mutex
	baseline  SharedCapture // last keyframe broadcast; every delta names its tick
	keyBody   []byte        // that keyframe as a bare capture, which is what a join sends
	haveKey   bool
	sinceKey  int

	// inbox holds reassembled correction bodies a guest has not applied yet. It is
	// written under the world lock by the tick that drained the last chunk and read
	// between two ticks by whatever drives this instance, so it is the one place
	// the two meet and it holds nothing but bytes.
	inboxMu sync.Mutex
	inbox   [][]byte
	dropped int64

	// installed is the guest's baseline: the last capture it installed whole. A
	// delta naming any other tick cannot be applied and is counted instead.
	installedMu   sync.Mutex
	installed     SharedCapture
	haveBase      bool
	lastInstalled uint64

	applyMu   sync.Mutex
	stop      chan struct{}
	wake      chan struct{}
	done      chan struct{}
	once      sync.Once
	closeOnce sync.Once
}

// newCorrections builds the correction half of a session. It starts nothing: a
// pump belongs to a host and a corrector to a guest, and which this run is only
// becomes true when a transport is attached.
func newCorrections(a *App) *corrections {
	return &corrections{
		a:    a,
		stop: make(chan struct{}),
		wake: make(chan struct{}, 1),
		done: make(chan struct{}),
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
func (c *corrections) pump() {
	defer close(c.done)
	interval := parameter.SnapshotCorrectionTicks * parameter.GameUpdateInterval
	ticker := time.NewTicker(interval) // [wall] a publication cadence, not a game clock
	defer ticker.Stop()

	for {
		select {
		case <-c.stop:
			return
		case <-c.wake:
		case <-ticker.C:
		}
		if err := c.publish(); err != nil {
			vlog.Warn("app", "msg", "correction not published", "error", err.Error())
		}
	}
}

// publish takes one capture and broadcasts it as a keyframe or as a delta.
//
// Which of the two is decided here and nowhere else, so the baseline every delta
// names is always a keyframe this host actually sent. A receiver that missed the
// keyframe drops the deltas that follow it, which costs it freshness until the next
// one and costs the host nothing — there is no retransmission and no acknowledgement
// to keep.
func (c *corrections) publish() error {
	port := c.a.sessionTransport()
	if port == nil || !port.IsRunning() || port.PeerCount() == 0 {
		return nil
	}

	c.publishMu.Lock()
	defer c.publishMu.Unlock()

	cap, err := c.captureLocked()
	if err != nil {
		return err
	}

	keyframe := !c.haveKey || c.sinceKey >= parameter.SnapshotKeyframeCorrections
	encodeStart := time.Now() // [wall] telemetry only; outside the world lock
	var body, plain []byte
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
			plain, err = EncodeCapture(cap)
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

	if keyframe {
		c.baseline, c.keyBody, c.haveKey, c.sinceKey = cap, plain, true, 0
	} else {
		c.sinceKey++
	}

	m := c.a.snapshotTelemetry
	m.encodeUS.Store(encodeDur.Microseconds())
	m.bytes.Store(int64(len(body)))
	m.sent.Add(1)
	m.sentBytes.Add(int64(len(body)))
	if keyframe {
		m.keyframes.Add(1)
	}
	vlog.Debug("app", "msg", "correction published",
		"tick", cap.Header.Tick, "keyframe", keyframe, "bytes", len(body),
		"chunks", len(chunks), "encode_us", encodeDur.Microseconds())
	return nil
}

// captureLocked reads the world and publishes what the read cost.
// Caller MUST hold publishMu; it MUST NOT hold the world lock.
func (c *corrections) captureLocked() (SharedCapture, error) {
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
			body, tick, err := c.keyframeNowLocked()
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

// keyframeNowLocked reads the world and makes the result this host's baseline.
// Caller MUST hold publishMu.
func (c *corrections) keyframeNowLocked() ([]byte, uint64, error) {
	cap, err := c.captureLocked()
	if err != nil {
		return nil, 0, err
	}
	body, err := EncodeCapture(cap)
	if err != nil {
		return nil, 0, fmt.Errorf("capture encode: %w", err)
	}
	c.baseline, c.keyBody, c.haveKey, c.sinceKey = cap, body, true, 0
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
	ticker := time.NewTicker(parameter.SnapshotCorrectionTicks * parameter.GameUpdateInterval) // [wall]
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

// setBaseline records the keyframe later deltas are computed against.
func (c *corrections) setBaseline(cap SharedCapture) {
	c.installedMu.Lock()
	c.installed, c.haveBase = cap, true
	c.installedMu.Unlock()
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
// run that paces its own cadence. An interactive host has the pump instead.
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
