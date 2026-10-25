package network

import (
	"slices"
)

// CrossingFence is one participant's ordinary-crossing boundary in a shared world:
// the source-local sequence through which that world holds the participant's
// crossings. A tick cannot tell: the authority commits a crossing that missed the
// playout lead at its own next tick, so a capture can lack one whose apply tick is
// past. The producer replays what lies past the fence, not what lies past a tick.
type CrossingFence struct {
	Source PeerID `json:"s"`
	Seq    uint64 `json:"q"`
}

// CrossingFences is the whole vector, one entry per participant that has produced
// anything the world contains. Sorted by source and free of zero entries, so two
// instances holding equal worlds encode equal bytes — a capture is compared by its
// bytes, and an unsorted map would make two identical worlds look different.
type CrossingFences []CrossingFence

// Seq is the boundary for one source. A source with no entry has produced nothing
// this world contains, which is the same statement as a zero sequence and is why
// Normalize drops those entries rather than carrying them.
func (f CrossingFences) Seq(source PeerID) uint64 {
	for _, e := range f {
		if e.Source == source {
			return e.Seq
		}
	}
	return 0
}

// Normalize sorts by source, drops zero sources and sequences, and collapses a
// repeated source to its highest. It is applied where a vector is built rather than
// where one is read, so everything downstream — comparison, encoding, the manifest's
// byte equality — sees one canonical form.
func (f CrossingFences) Normalize() CrossingFences {
	if len(f) == 0 {
		return nil
	}
	out := make(CrossingFences, 0, len(f))
	for _, e := range f {
		if e.Seq == 0 || e.Source == 0 {
			continue
		}
		if i := slices.IndexFunc(out, func(x CrossingFence) bool { return x.Source == e.Source }); i >= 0 {
			out[i].Seq = max(out[i].Seq, e.Seq)
			continue
		}
		out = append(out, e)
	}
	if len(out) == 0 {
		return nil
	}
	slices.SortFunc(out, func(a, b CrossingFence) int {
		switch {
		case a.Source < b.Source:
			return -1
		case a.Source > b.Source:
			return 1
		}
		return 0
	})
	return out
}

// Equal reports whether two normalized vectors describe the same boundary. A
// selective repair is validated against the manifest it answers, and a shard set
// assembled under a different fence would be a repair of a different world.
func (f CrossingFences) Equal(other CrossingFences) bool {
	return slices.Equal(f, other)
}
