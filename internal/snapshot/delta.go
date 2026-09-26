package snapshot

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"slices"

	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/fsm"
)

// SharedCaptureDelta is one capture expressed against a baseline capture. Only the
// component stores are differenced, which is where the bytes are; streams, system
// records, the FSM and status travel whole, a constant few kilobytes that rule out
// a part a delta wrongly judged unchanged.
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

// ApplyCaptureDelta reconstructs the capture a delta describes. The tick says the
// caller holds the keyframe the delta names, and the integrity hash that the
// reconstruction is byte for byte what the sender held; without both, a delta on
// the wrong baseline would install cleanly as a world nobody has.
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
	want, err := Integrity(out)
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
	return EncodeJSON(correctionEnvelope{Full: &cap})
}

// EncodeCorrectionDelta renders a delta for transport.
func EncodeCorrectionDelta(d SharedCaptureDelta) ([]byte, error) {
	return EncodeJSON(correctionEnvelope{Delta: &d})
}

// DecodeCorrection parses a correction body and reports which shape it is.
func DecodeCorrection(b []byte) (CorrectionKind, SharedCapture, SharedCaptureDelta, error) {
	var env correctionEnvelope
	if err := DecodeJSON(b, &env); err != nil {
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

// WrittenDelta is a capture this instance wrote, against the capture it held just
// before the write: what a journal carries, since a replay holds that same world
// at that place. A part the write left as it was is not carried; Changed names
// which whole parts are, and Systems holds only the records that moved.
type WrittenDelta struct {
	Header  CaptureHeader           `json:"header"`
	World   engine.SharedWorldDelta `json:"world"`
	Changed WrittenParts            `json:"changed,omitempty"`
	Streams []engine.StreamState    `json:"streams,omitempty"`
	Systems []SystemStateRecord     `json:"systems,omitempty"`
	Status  StatusState             `json:"status,omitzero"`
	FSM     fsm.MachineState        `json:"fsm,omitzero"`
}

// WrittenParts flags the whole parts a WrittenDelta carries.
type WrittenParts uint8

const (
	writtenStreams WrittenParts = 1 << iota
	writtenStatus
	writtenFSM
	writtenSystemSet // the record list itself changed, so Systems is all of it
)

// DiffWritten expresses written against before, sealing written's integrity so a
// rebuild can be proved rather than trusted.
func DiffWritten(before, written SharedCapture) (WrittenDelta, error) {
	sum, err := Integrity(written)
	if err != nil {
		return WrittenDelta{}, err
	}
	d := diffParts(before, written)
	d.Header, d.World = written.Header, engine.DiffSharedWorld(before.World, written.World)
	d.Header.Integrity = sum
	return d, nil
}

// HoldsBesideWorld reports whether before already holds all written carries outside
// its component stores: the clock, the allocator, the streams, every carrier's
// record, the FSM and the status surface.
func HoldsBesideWorld(before, written SharedCapture) bool {
	d := diffParts(before, written)
	b, w := before.World, written.World
	return d.Changed == 0 && len(d.Systems) == 0 &&
		before.Header.Run == written.Header.Run && before.Header.Tick == written.Header.Tick &&
		b.NextEntity == w.NextEntity && b.Created == w.Created && b.Destroyed == w.Destroyed
}

// diffParts is a WrittenDelta's parts beside the world: the whole ones that changed,
// and the system records that moved.
func diffParts(before, written SharedCapture) WrittenDelta {
	var d WrittenDelta
	if !reflect.DeepEqual(before.Streams, written.Streams) {
		d.Changed |= writtenStreams
		d.Streams = written.Streams
	}
	if !reflect.DeepEqual(before.Status, written.Status) {
		d.Changed |= writtenStatus
		d.Status = written.Status
	}
	if !reflect.DeepEqual(before.FSM, written.FSM) {
		d.Changed |= writtenFSM
		d.FSM = written.FSM
	}
	if !sameSystemSet(before.Systems, written.Systems) {
		d.Changed |= writtenSystemSet
		d.Systems = written.Systems
		return d
	}
	for i, r := range written.Systems {
		if !bytes.Equal(r.Data, before.Systems[i].Data) {
			d.Systems = append(d.Systems, r)
		}
	}
	return d
}

// ApplyWritten rebuilds what a WrittenDelta describes from the capture held before
// the write. A rebuild whose integrity does not match means the world it started
// from was not the one the recorded run held: the replay diverged before here.
func ApplyWritten(before SharedCapture, d WrittenDelta) (SharedCapture, error) {
	out := SharedCapture{
		Header:  d.Header,
		World:   engine.ApplySharedWorldDelta(before.World, d.World),
		Streams: before.Streams,
		Systems: before.Systems,
		Status:  before.Status,
		FSM:     before.FSM,
	}
	if d.Changed&writtenStreams != 0 {
		out.Streams = d.Streams
	}
	if d.Changed&writtenStatus != 0 {
		out.Status = d.Status
	}
	if d.Changed&writtenFSM != 0 {
		out.FSM = d.FSM
	}
	if d.Changed&writtenSystemSet != 0 {
		out.Systems = d.Systems
	} else if len(d.Systems) > 0 {
		out.Systems = slices.Clone(before.Systems)
		for _, r := range d.Systems {
			i := slices.IndexFunc(out.Systems, func(s SystemStateRecord) bool { return s.System == r.System })
			if i < 0 {
				return SharedCapture{}, fmt.Errorf("written world names system %q the world before it lacks", r.System)
			}
			out.Systems[i] = r
		}
	}
	sum, err := Integrity(out)
	if err != nil {
		return SharedCapture{}, err
	}
	if sum != d.Header.Integrity {
		return SharedCapture{}, errors.New("the rebuilt world is not the one written: the world before it differed")
	}
	return out, nil
}

func sameSystemSet(a, b []SystemStateRecord) bool {
	return slices.EqualFunc(a, b, func(x, y SystemStateRecord) bool { return x.System == y.System })
}
