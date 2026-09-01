package genetic

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"slices"
)

// StreamingStateVersion is the serialized layout understood by Checkpoint and
// Restore. The normalized configuration is embedded and checked; operator
// identities are deliberately not serializable, so callers must restore into an
// engine constructed with semantically equivalent operators.
const StreamingStateVersion = 1

// ErrInvalidStreamingState identifies a checkpoint that cannot describe this
// engine. Restore validates the complete value before changing live state.
var ErrInvalidStreamingState = errors.New("genetic: invalid streaming state")

// PendingEvaluation is one genotype handed to a caller and still awaiting an
// outcome. ID is preserved so a restored host can complete work already in flight.
type PendingEvaluation[S Solution] struct {
	ID   EvalID `json:"id" toml:"id"`
	Data S      `json:"data" toml:"data"`
}

// StreamingState is a complete continuation point for StreamingEngine.
//
// Unlike Snapshot, which contains only the scored archive for persistence,
// StreamingState carries the normalized configuration and every engine-owned value
// that decides the next proposal: the PCG position, queued proposals, pending
// evaluations and their IDs, generation phase, and counters. With a non-nil
// Cloner, Checkpoint returns caller-owned solutions and Restore copies them back
// into engine-owned storage.
type StreamingState[S Solution, F Numeric] struct {
	Version       int                    `json:"version" toml:"version"`
	Config        StreamingConfig        `json:"config" toml:"config"`
	RNG           []byte                 `json:"rng" toml:"rng"`
	Archive       []Candidate[S, F]      `json:"archive" toml:"archive"`
	Proposals     []S                    `json:"proposals" toml:"proposals"`
	Pending       []PendingEvaluation[S] `json:"pending" toml:"pending"`
	Generation    int                    `json:"generation" toml:"generation"`
	Outcomes      int                    `json:"outcomes" toml:"outcomes"`
	LastDiversity float64                `json:"last_diversity" toml:"last_diversity"`
	NextID        uint64                 `json:"next_id" toml:"next_id"`
	Evicted       uint64                 `json:"evicted" toml:"evicted"`
	Running       bool                   `json:"running" toml:"running"`
}

// Checkpoint returns a complete, canonically ordered continuation point.
// Pending evaluations are ordered by ID and proposals retain FIFO order, making
// the value suitable for deterministic encoding and comparison.
func (e *StreamingEngine[S, F]) Checkpoint() (StreamingState[S, F], error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	rngState, err := e.rngSource.MarshalBinary()
	if err != nil {
		return StreamingState[S, F]{}, fmt.Errorf("genetic: checkpoint rng: %w", err)
	}
	state := StreamingState[S, F]{
		Version:       StreamingStateVersion,
		Config:        e.cfg,
		RNG:           rngState,
		Archive:       make([]Candidate[S, F], len(e.archive)),
		Proposals:     make([]S, e.proposals.n),
		Pending:       make([]PendingEvaluation[S], 0, e.pending.live),
		Generation:    e.gen,
		Outcomes:      e.outcomes,
		LastDiversity: e.lastDiv,
		NextID:        e.nextID,
		Evicted:       e.evicted,
		Running:       e.started.Load(),
	}
	for i, candidate := range e.archive {
		candidate.Data = e.cloneForCaller(candidate.Data)
		state.Archive[i] = candidate
	}
	for i := range e.proposals.n {
		state.Proposals[i] = e.cloneForCaller(
			e.proposals.buf[(e.proposals.head+i)&e.proposals.mask])
	}
	for i := 0; i < len(e.pending.ids); i++ {
		id := e.pending.ids[i]
		if id == 0 {
			continue
		}
		state.Pending = append(state.Pending, PendingEvaluation[S]{
			ID: id, Data: e.cloneForCaller(e.pending.data[i]),
		})
	}
	slices.SortFunc(state.Pending, func(a, b PendingEvaluation[S]) int {
		switch {
		case a.ID < b.ID:
			return -1
		case a.ID > b.ID:
			return 1
		default:
			return 0
		}
	})
	return state, nil
}

// Restore replaces the complete streaming state. The destination must have the
// same configuration and semantically equivalent initializer/operators as the
// source. A malformed or incompatible value leaves the engine unchanged.
func (e *StreamingEngine[S, F]) Restore(state StreamingState[S, F]) error {
	source, err := e.validateState(state)
	if err != nil {
		return err
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	e.releaseOwnedLocked()
	e.rngSource = source
	e.rng = rand.New(source)

	for _, candidate := range state.Archive {
		candidate.Data = e.cloneForEngineLocked(candidate.Data)
		e.archive = append(e.archive, candidate)
	}
	for _, proposal := range state.Proposals {
		// Capacity was validated above, so push cannot fail.
		e.proposals.push(e.cloneForEngineLocked(proposal))
	}
	for _, pending := range state.Pending {
		e.pending.put(pending.ID, e.cloneForEngineLocked(pending.Data))
	}

	e.gen = state.Generation
	e.outcomes = state.Outcomes
	e.lastDiv = state.LastDiversity
	e.nextID = state.NextID
	e.evicted = state.Evicted
	e.started.Store(state.Running)
	e.aggDirty = true
	e.publishLocked()
	return nil
}

// ValidateState checks whether Restore can accept state without changing the
// engine. It is useful to preflight a collection of engines before restoring any.
func (e *StreamingEngine[S, F]) ValidateState(state StreamingState[S, F]) error {
	_, err := e.validateState(state)
	return err
}

// validateState constructs the source Restore will install and checks every
// structural invariant before the live engine is touched.
func (e *StreamingEngine[S, F]) validateState(state StreamingState[S, F]) (*rand.PCG, error) {
	invalid := func(format string, args ...any) (*rand.PCG, error) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidStreamingState, fmt.Sprintf(format, args...))
	}

	if state.Version != StreamingStateVersion {
		return invalid("version %d, want %d", state.Version, StreamingStateVersion)
	}
	if state.Config != e.cfg {
		return invalid("configuration does not match destination engine")
	}
	source := rand.NewPCG(0, 0)
	if err := source.UnmarshalBinary(state.RNG); err != nil {
		return invalid("rng: %v", err)
	}
	if state.Generation < 0 {
		return invalid("negative generation %d", state.Generation)
	}
	if state.Outcomes < 0 || state.Outcomes >= e.cfg.MinOutcomesPerGen {
		return invalid("outcomes %d outside [0,%d)", state.Outcomes, e.cfg.MinOutcomesPerGen)
	}
	if len(state.Archive) > e.cfg.PoolSize {
		return invalid("archive size %d exceeds capacity %d", len(state.Archive), e.cfg.PoolSize)
	}
	for i := 1; i < len(state.Archive); i++ {
		if state.Archive[i-1].Score < state.Archive[i].Score {
			return invalid("archive is not score-descending at index %d", i)
		}
	}
	if len(state.Proposals) > len(e.proposals.buf) {
		return invalid("proposal count %d exceeds capacity %d", len(state.Proposals), len(e.proposals.buf))
	}
	if len(state.Pending) > len(e.pending.ids) {
		return invalid("pending count %d exceeds capacity %d", len(state.Pending), len(e.pending.ids))
	}

	ids := make(map[EvalID]struct{}, len(state.Pending))
	slots := make(map[uint64]EvalID, len(state.Pending))
	for _, pending := range state.Pending {
		if pending.ID == 0 {
			return invalid("pending evaluation has zero id")
		}
		if uint64(pending.ID) > state.NextID {
			return invalid("pending id %d exceeds next id %d", pending.ID, state.NextID)
		}
		if _, exists := ids[pending.ID]; exists {
			return invalid("duplicate pending id %d", pending.ID)
		}
		ids[pending.ID] = struct{}{}
		slot := uint64(pending.ID) & e.pending.mask
		if other, exists := slots[slot]; exists {
			return invalid("pending ids %d and %d collide in slot %d", other, pending.ID, slot)
		}
		slots[slot] = pending.ID
	}
	return source, nil
}

func (e *StreamingEngine[S, F]) cloneForCaller(src S) S {
	if e.cloner == nil {
		return src
	}
	var zero S
	return e.cloner.Clone(zero, src)
}

func (e *StreamingEngine[S, F]) cloneForEngineLocked(src S) S {
	if e.cloner == nil {
		return src
	}
	return e.cloner.Clone(e.takeFreeLocked(), src)
}

// releaseOwnedLocked makes the old semantic state unreachable while retaining a
// bounded set of reusable solution buffers. parents and offspring are scratch;
// clearing them prevents receiver-local scratch from influencing a custom operator.
func (e *StreamingEngine[S, F]) releaseOwnedLocked() {
	for i := range e.archive {
		e.recycleLocked(e.archive[i].Data)
	}
	e.archive = e.archive[:0]
	for {
		proposal, ok := e.proposals.pop()
		if !ok {
			break
		}
		e.recycleLocked(proposal)
	}
	for i := 0; i < len(e.pending.ids); i++ {
		id := e.pending.ids[i]
		if id != 0 {
			e.recycleLocked(e.pending.data[i])
		}
	}
	e.pending.clear()

	var zeroS S
	for i := range e.parents {
		e.parents[i] = Candidate[S, F]{}
	}
	for i := range e.offspring {
		e.offspring[i] = zeroS
	}
}
