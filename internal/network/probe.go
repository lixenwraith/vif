// Package network: the round trip.
//
// Nothing in this protocol used to make one. Every measurement a session had was
// one-directional — what this instance sent, and how far behind the newest tick
// it had *heard about* it stood — so there was no way to say how long a link
// actually took, how much its timing varied, or how much of what was sent had
// arrived. A cadence chosen from a constant was the only cadence available.
//
// The probe is that missing measurement, and where it lives is the design.
// It lives entirely in the transport: a probe is emitted by the port, answered
// by the receiving port before the frame ever reaches a tick, and folded into a
// per-peer estimate the port owns. No game state is read to answer one and no
// tick has to run for one to complete, which is both why the number is a wire
// round trip rather than a scheduling artifact and why network timing cannot
// leak into the simulation through it: the world's only contribution is an
// opaque LinkReport it publishes when it feels like it, and the only thing it
// gets back is an estimate it may schedule transport from.
package network

import (
	"encoding/binary"
	"time"

	"github.com/lixenwraith/vi-fighter/pkg/linkpace"
)

// probePayload is [Seq:4][SentNano:8]: a sequence number to pair an echo with
// its probe, and the sender's own clock reading, which comes back untouched so
// the round trip is computed against the clock that started it. Neither end has
// to agree with the other about what time it is.
const probePayload = 12

// echoPayload is the probe's bytes returned verbatim, followed by
// [InBytes:8][LinkReport]. InBytes is what the far end has received on this link,
// which is what turns two echoes into a delivery rate and one echo into a
// backlog.
const echoPayload = probePayload + 8 + linkReportSize

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

// encodeProbe renders one probe frame.
func encodeProbe(seq uint32, sent time.Time) []byte {
	b := make([]byte, probePayload)
	binary.BigEndian.PutUint32(b[0:4], seq)
	binary.BigEndian.PutUint64(b[4:12], uint64(sent.UnixNano()))
	return b
}

// encodeEcho answers a probe: its bytes untouched, then what this instance has
// received on the link and what it has to say about its own picture.
func encodeEcho(probe []byte, inBytes uint64, report LinkReport) []byte {
	if len(probe) < probePayload {
		return nil
	}
	b := make([]byte, 0, echoPayload)
	b = append(b, probe[:probePayload]...)
	b = binary.BigEndian.AppendUint64(b, inBytes)
	return append(b, report.encode()...)
}

// decodeEcho reads an answered probe.
func decodeEcho(b []byte) (seq uint32, sent time.Time, inBytes uint64, report LinkReport, ok bool) {
	if len(b) < echoPayload {
		return 0, time.Time{}, 0, LinkReport{}, false
	}
	seq = binary.BigEndian.Uint32(b[0:4])
	sent = time.Unix(0, int64(binary.BigEndian.Uint64(b[4:12])))
	inBytes = binary.BigEndian.Uint64(b[12:20])
	report, ok = decodeLinkReport(b[20:])
	return seq, sent, inBytes, report, ok
}

// linkMeter is one peer's measurement state: the estimate itself, the probe
// sequence, and the two cumulative byte counters two consecutive echoes are
// turned into a delivery rate by.
type linkMeter struct {
	link *linkpace.Link

	seq     uint32 // last probe sent
	echoed  uint32 // last probe answered
	pending bool   // a probe is outstanding

	lastDelivered uint64
	lastEchoAt    time.Time
	haveDelivered bool

	// The two cumulative counters have no shared origin: this end starts counting
	// when it accepted the stream, the far end when its port took the stream over,
	// and a mid-run join means those are different moments separated by a whole
	// capture. Only their *differences* mean anything, so the first sample after
	// either counter restarts establishes an origin and the ones after it measure
	// growth from there. Without this a join would leave a standing backlog the
	// size of the world it installed, and the link would read as permanently
	// saturated for the rest of the session.
	baseSent      uint64
	baseDelivered uint64
	haveBase      bool
}

func newLinkMeter() *linkMeter {
	return &linkMeter{link: linkpace.NewLink(linkpace.LinkConfig{})}
}

// nextProbe advances the sequence, charging the previous probe as lost when it
// was never answered. A probe that went unanswered is the only loss signal this
// protocol has: corrections are not acknowledged and epochs are not repaired, so
// nothing else on the link ever notices a frame that did not arrive.
func (m *linkMeter) nextProbe() uint32 {
	if m.pending {
		m.link.Miss()
	}
	m.seq++
	m.pending = true
	return m.seq
}

// observe folds one answered probe into the estimate.
//
// sentBytes is what this instance has queued for that peer, and delivered is
// what the peer says it has received; the difference is the backlog, which is
// what separates "the link is fast" from "the sender was idle". An echo for a
// probe older than the newest is folded in anyway — its round trip is real — but
// it does not clear the outstanding flag, so the newer probe is still charged as
// lost if it never returns.
func (m *linkMeter) observe(now, sent time.Time, seq uint32, delivered uint64, sentBytes uint64, report LinkReport) {
	sample := linkpace.Sample{
		RTT:       now.Sub(sent),
		LagTicks:  uint64(report.LagTicks),
		Magnitude: int(report.Magnitude),
		Interest:  report.interest(),
	}
	rebase := !m.haveBase || delivered < m.baseDelivered || sentBytes < m.baseSent ||
		delivered < m.lastDelivered
	if rebase {
		m.baseSent, m.baseDelivered, m.haveBase = sentBytes, delivered, true
	} else {
		if offered, arrived := sentBytes-m.baseSent, delivered-m.baseDelivered; offered > arrived {
			sample.Backlog = int64(offered - arrived)
		}
		if m.haveDelivered && now.After(m.lastEchoAt) {
			sample.Delivered = int64(delivered - m.lastDelivered)
			sample.Elapsed = now.Sub(m.lastEchoAt)
		}
	}
	m.lastDelivered, m.lastEchoAt, m.haveDelivered = delivered, now, true
	if seq >= m.echoed {
		m.echoed = seq
	}
	if seq == m.seq {
		m.pending = false
	}
	m.link.Observe(sample)
}
