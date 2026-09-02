// Package app: a capture expressed against another capture.
//
// Phase 3 put a whole world on the wire because a join needs a whole world. A
// correction does not: a guest already holds one, and what it is missing is the
// difference between the world it predicted and the world the host actually has.
//
// The measurement is what forced this rather than a preference for it. The storm
// high water is 176 KiB of schema per capture: before the wire codec, full captures
// were 859 KiB/s at 5 Hz. Exact deltas remove unchanged schema and the bounded
// deflate envelope then reduces both shapes; they solve different parts of the cost.
//
// A correction is therefore one of two things and says which: a **keyframe**, which
// is a whole capture and is self-sufficient, or a **delta**, which names the
// keyframe it was computed against and is worthless without it. A receiver holding a
// different baseline drops a delta rather than guessing, and waits for the next
// keyframe; that is a bounded wait by construction, because the host takes one every
// SnapshotKeyframeCorrections corrections.
//
// The proof that a delta is lossless is not a comparison — it is the capture's own
// integrity hash. Reconstruct, re-hash, and the header's field either matches or the
// correction is refused. A delta that produced a world merely *equivalent* to the
// sender's — the same entities in a different store order, say — passes every value
// check and fails that hash, which is why the delta carries entity order at all.
package app

import (
	"errors"
	"fmt"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
)

// SharedCaptureDelta is one capture expressed against a baseline capture.
//
// Only the world half is differenced, and that is where the measurement said the
// bytes are: 52 component stores against 24 stream positions, 5 system records, two
// FSM regions and a status surface. The rest is carried whole, which costs a
// constant few kilobytes and removes a whole class of partial-state bug — a stream
// position or an FSM region that a delta decided had not changed and had.
type SharedCaptureDelta struct {
	// Header is the *next* capture's header, carried whole. Its Integrity field is
	// what the receiver checks the reconstruction against, so it is the one field
	// here that is load-bearing rather than descriptive.
	Header CaptureHeader `json:"header"`

	// BaselineTick names the keyframe this delta was computed against. A receiver
	// holding a different one cannot apply it and must not try.
	BaselineTick uint64 `json:"baseline_tick"`

	World   engine.SharedWorldDelta `json:"world"`
	Streams []engine.StreamState    `json:"streams"`
	Systems []SystemStateRecord     `json:"systems"`
	Status  StatusState             `json:"status"`
	FSM     fsm.MachineState        `json:"fsm"`
}

// DiffCapture expresses next as a difference against base.
//
// Nothing here reads the world: both captures are already taken, so this runs on
// whichever goroutine is publishing corrections rather than under the world lock.
func DiffCapture(base, next SharedCapture) SharedCaptureDelta {
	return SharedCaptureDelta{
		Header:       next.Header,
		BaselineTick: base.Header.Tick,
		World:        engine.DiffSharedWorld(base.World, next.World),
		Streams:      next.Streams,
		Systems:      next.Systems,
		Status:       next.Status,
		FSM:          next.FSM,
	}
}

// ApplyCaptureDelta reconstructs the capture a delta describes.
//
// The baseline must be the keyframe the delta names — a receiver that lost one and
// applied a delta to the wrong world would produce a capture that installs cleanly
// and describes a world nobody has. Both checks are here rather than at the call
// site: the tick says the caller has the right baseline, and the integrity hash
// says the reconstruction is byte-for-byte what the sender held.
func ApplyCaptureDelta(base SharedCapture, d SharedCaptureDelta) (SharedCapture, error) {
	if base.Header.Tick != d.BaselineTick {
		return SharedCapture{}, fmt.Errorf(
			"correction delta names baseline tick %d, this instance holds %d",
			d.BaselineTick, base.Header.Tick)
	}
	out := SharedCapture{
		Header:  d.Header,
		World:   engine.ApplySharedWorldDelta(base.World, d.World),
		Streams: d.Streams,
		Systems: d.Systems,
		Status:  d.Status,
		FSM:     d.FSM,
	}
	want, err := captureIntegrity(out)
	if err != nil {
		return SharedCapture{}, err
	}
	if want != out.Header.Integrity {
		return SharedCapture{}, errors.New(
			"correction delta reconstructed a body its header does not describe")
	}
	return out, nil
}

// CorrectionKind names which of the two shapes a correction body carries.
type CorrectionKind uint8

// The two shapes. Keyframe is self-sufficient; Delta needs the keyframe it names.
const (
	CorrectionKeyframe CorrectionKind = iota
	CorrectionDelta
)

// correctionEnvelope is what one correction looks like on the wire, before it is
// chunked. Exactly one of the two bodies is present, and the receiver reads which
// from the field that is there rather than from a flag it has to trust.
type correctionEnvelope struct {
	Full  *SharedCapture      `json:"full,omitempty"`
	Delta *SharedCaptureDelta `json:"delta,omitempty"`
}

// EncodeCorrection renders a keyframe for transport.
func EncodeCorrection(cap SharedCapture) ([]byte, error) {
	return encodeSnapshotJSON(correctionEnvelope{Full: &cap})
}

// EncodeCorrectionDelta renders a delta for transport.
func EncodeCorrectionDelta(d SharedCaptureDelta) ([]byte, error) {
	return encodeSnapshotJSON(correctionEnvelope{Delta: &d})
}

// DecodeCorrection parses a correction body and reports which shape it is.
func DecodeCorrection(b []byte) (CorrectionKind, SharedCapture, SharedCaptureDelta, error) {
	var env correctionEnvelope
	if err := decodeSnapshotJSON(b, &env); err != nil {
		return 0, SharedCapture{}, SharedCaptureDelta{}, fmt.Errorf("correction decode: %w", err)
	}
	switch {
	case env.Full != nil && env.Delta != nil:
		return 0, SharedCapture{}, SharedCaptureDelta{},
			errors.New("correction carries both a keyframe and a delta")
	case env.Full != nil:
		return CorrectionKeyframe, *env.Full, SharedCaptureDelta{}, nil
	case env.Delta != nil:
		return CorrectionDelta, SharedCapture{}, *env.Delta, nil
	default:
		return 0, SharedCapture{}, SharedCaptureDelta{}, errors.New("correction carries neither shape")
	}
}
