package network

import (
	"encoding/binary"
	"time"

	"github.com/lixenwraith/vif/pkg/linkpace"
)

// probePayload is [Seq:4][SentNano:8]: a sequence number to pair an echo with
// its probe, and the sender's own clock reading, which comes back untouched so
// the round trip is computed against the clock that started it. Neither end has
// to agree with the other about what time it is.
const probePayload = 12

// echoPayload is the probe's bytes returned verbatim, followed by
// [InBytes:8][LinkReport], then [Answered:8] from a peer that sends it. InBytes is
// what the far end has received on this link, which turns two echoes into a
// delivery rate and one echo into a backlog; Answered is its own clock when it
// read the probe, which times that rate where the bytes arrived.
const echoPayload = probePayload + 8 + linkReportSize

// probeEpoch is the origin an answer is timed from; only differences between
// two answers from one instance are ever read.
var probeEpoch = time.Now()

// linkReportSize is [Tick:8][LagTicks:4][Magnitude:4][CursorX:4][CursorY:4][Flags:1].
const linkReportSize = 25

// linkReportCursorValid marks the interest cell as one this instance actually
// holds. A participant between cursors reports none rather than reporting zero,
// which would place its interest in the map's corner.
const linkReportCursorValid uint8 = 1 << 0

// LinkReport is what one instance tells the peer probing it about its own
// picture: which tick it stands on, how far behind the session it believes it
// is, how much the last correction had to move, and where its cursor is.
//
// All four are scheduling inputs and nothing else. The tick and the lag say
// whether this participant is keeping up; the magnitude says whether the cadence
// is repairing its prediction as fast as the prediction drifts; the cursor cell
// says which part of the world is its business. A host may publish sooner
// because of any of them and may never decide anything about the simulation from
// them — a stale or wrong report costs a correction sent early.
type LinkReport struct {
	Tick      uint64
	LagTicks  uint32
	Magnitude uint32
	CursorX   int32
	CursorY   int32
	HasCursor bool
}

// encode writes the report into exactly linkReportSize bytes.
func (r LinkReport) encode() []byte {
	b := make([]byte, linkReportSize)
	binary.BigEndian.PutUint64(b[0:8], r.Tick)
	binary.BigEndian.PutUint32(b[8:12], r.LagTicks)
	binary.BigEndian.PutUint32(b[12:16], r.Magnitude)
	binary.BigEndian.PutUint32(b[16:20], uint32(r.CursorX))
	binary.BigEndian.PutUint32(b[20:24], uint32(r.CursorY))
	if r.HasCursor {
		b[24] = linkReportCursorValid
	}
	return b
}

// decodeLinkReport reads a report, reporting whether the frame carried one.
func decodeLinkReport(b []byte) (LinkReport, bool) {
	if len(b) < linkReportSize {
		return LinkReport{}, false
	}
	return LinkReport{
		Tick:      binary.BigEndian.Uint64(b[0:8]),
		LagTicks:  binary.BigEndian.Uint32(b[8:12]),
		Magnitude: binary.BigEndian.Uint32(b[12:16]),
		CursorX:   int32(binary.BigEndian.Uint32(b[16:20])),
		CursorY:   int32(binary.BigEndian.Uint32(b[20:24])),
		HasCursor: b[24]&linkReportCursorValid != 0,
	}, true
}

// interest renders the report's cursor as the estimator's interest cell.
func (r LinkReport) interest() linkpace.Cell {
	return linkpace.Cell{X: r.CursorX, Y: r.CursorY, Valid: r.HasCursor}
}

// AnswerProbe is the echo a handshake answers a probe with before a port owns its
// stream: the probe's bytes, what the stream has received, no report, and the time.
func AnswerProbe(probe []byte, inBytes uint64) []byte {
	return encodeEcho(probe, inBytes, LinkReport{}, time.Since(probeEpoch))
}

// encodeProbe renders one probe frame.
func encodeProbe(seq uint32, sent time.Time) []byte {
	b := make([]byte, probePayload)
	binary.BigEndian.PutUint32(b[0:4], seq)
	binary.BigEndian.PutUint64(b[4:12], uint64(sent.UnixNano()))
	return b
}

// encodeEcho answers a probe: its bytes untouched, then what this instance has
// received on the link, what it has to say about its own picture, and when.
func encodeEcho(probe []byte, inBytes uint64, report LinkReport, answered time.Duration) []byte {
	if len(probe) < probePayload {
		return nil
	}
	b := make([]byte, 0, echoPayload+8)
	b = append(b, probe[:probePayload]...)
	b = binary.BigEndian.AppendUint64(b, inBytes)
	b = append(b, report.encode()...)
	return binary.BigEndian.AppendUint64(b, uint64(answered))
}

// decodeEcho reads an answered probe; answered is zero from a peer that sends none.
func decodeEcho(b []byte) (seq uint32, sent time.Time, inBytes uint64, answered time.Duration, report LinkReport, ok bool) {
	if len(b) < echoPayload {
		return 0, time.Time{}, 0, 0, LinkReport{}, false
	}
	seq = binary.BigEndian.Uint32(b[0:4])
	sent = time.Unix(0, int64(binary.BigEndian.Uint64(b[4:12])))
	inBytes = binary.BigEndian.Uint64(b[12:20])
	if len(b) >= echoPayload+8 {
		answered = time.Duration(binary.BigEndian.Uint64(b[echoPayload:]))
	}
	report, ok = decodeLinkReport(b[20:])
	return seq, sent, inBytes, answered, report, ok
}

// linkMeter is one peer's measurement state: the estimate itself, the probe
// sequence, and the two cumulative byte counters echoes are turned into a delivery
// rate and a backlog by.
type linkMeter struct {
	link *linkpace.Link

	// window is the shortest span a delivery rate is measured over, half the probe
	// interval: reads the far end's scheduler bunched together would otherwise
	// divide by microseconds. lostAfter is how many probes may go out after the last
	// one answered before each further one is charged as lost; zero charges none.
	window    time.Duration
	lostAfter uint32

	// heldSince is when the sender's frames for this peer began waiting on the
	// link, zero while it takes them; heldAfter is how long they wait before the
	// link is read as the limit, two probe intervals: a backlog reading is stale
	// by one, and a lost probe doubles it, so a shorter wait is the reading's.
	heldSince time.Time
	heldAfter time.Duration

	seq    uint32 // last probe sent
	echoed uint32 // last probe answered

	// lastDelivered is the newest count, which the backlog reads; the rate is
	// measured from rateDelivered at rateAt, at least window earlier.
	lastDelivered uint64
	rateDelivered uint64
	rateAt        time.Duration
	haveDelivered bool

	// The two cumulative counters have no shared origin: this end starts counting
	// when it accepted the stream, the far end when its port took the stream over,
	// and a join puts a whole capture between those moments. Only differences mean
	// anything, so the first sample after a restart sets the origin; the joiner's
	// ready is one (rebase), since its counter may pass the gate's before an echo.
	baseSent      uint64
	baseDelivered uint64
	haveBase      bool
}

// newLinkMeter meters a link probed every probe, a probe unanswered for lost
// counting as lost; a zero interval probes nothing and measures transfers alone.
func newLinkMeter(probe, lost time.Duration) *linkMeter {
	m := &linkMeter{link: linkpace.NewLink(linkpace.LinkConfig{}), window: probe / 2, heldAfter: 2 * probe}
	if probe > 0 {
		m.lostAfter = uint32(lost / probe)
	}
	return m
}

// rebase discards the origin, so the next echo sets it again.
func (m *linkMeter) rebase() { m.haveBase, m.haveDelivered = false, false }

// hold records whether the sender's frames for this peer are waiting on the link now.
func (m *linkMeter) hold(now time.Time, held bool) {
	switch {
	case !held:
		m.heldSince = time.Time{}
	case m.heldSince.IsZero():
		m.heldSince = now
	}
}

// nextProbe advances the sequence, charging a miss while no echo has come back for
// lostAfter probes. A probe is the only loss signal this protocol has, and a stream
// delivers every one: an echo slower than the probe interval is a slow link.
func (m *linkMeter) nextProbe() uint32 {
	if m.lostAfter > 0 && m.seq-m.echoed >= m.lostAfter {
		m.link.Miss()
	}
	m.seq++
	return m.seq
}

// observe folds one answered probe into the estimate.
//
// sentBytes is what this instance has queued for that peer, and delivered is
// what the peer says it has received; the difference is the backlog, which is
// what separates "the link is fast" from "the sender was idle". The rate is timed
// on the peer's answers, since echoes a stalled return path releases together
// arrive microseconds apart; a peer that sends no answer time is timed on arrival.
func (m *linkMeter) observe(now, sent time.Time, seq uint32, delivered, sentBytes uint64, answered time.Duration, report LinkReport) {
	sample := linkpace.Sample{
		RTT:       now.Sub(sent),
		Held:      m.heldAfter > 0 && !m.heldSince.IsZero() && now.Sub(m.heldSince) >= m.heldAfter,
		LagTicks:  uint64(report.LagTicks),
		Magnitude: int(report.Magnitude),
		Interest:  report.interest(),
	}
	at := answered
	if at <= 0 {
		at = time.Duration(now.UnixNano())
	}
	rebase := !m.haveBase || delivered < m.baseDelivered || sentBytes < m.baseSent ||
		delivered < m.lastDelivered
	if rebase {
		m.baseSent, m.baseDelivered, m.haveBase = sentBytes, delivered, true
	} else {
		if offered, arrived := sentBytes-m.baseSent, delivered-m.baseDelivered; offered > arrived {
			sample.Backlog = int64(offered - arrived)
		}
		if elapsed := at - m.rateAt; m.haveDelivered && elapsed >= m.window {
			sample.Delivered, sample.Elapsed = int64(delivered-m.rateDelivered), elapsed
			m.rateDelivered, m.rateAt = delivered, at
		}
	}
	if rebase || !m.haveDelivered {
		m.rateDelivered, m.rateAt = delivered, at
	}
	m.lastDelivered, m.haveDelivered = delivered, true
	m.echoed = max(m.echoed, seq)
	m.link.Observe(sample)
}

// backlog is what sent, this end's count for the peer now, holds beyond what the
// peer had received at its latest echo. Zero until an echo has set the origin.
func (m *linkMeter) backlog(sent uint64) int64 {
	if !m.haveBase || sent < m.baseSent {
		return 0
	}
	return max(int64(sent-m.baseSent)-int64(m.lastDelivered-m.baseDelivered), 0)
}
