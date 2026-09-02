// Package app: writing one repaired page into the sections a capture carries
// beside its component stores.
//
// The stores have a generated writer, because they are generated. These five do
// not: they are five fixed shapes, and each one's page is written by rebuilding
// the section with the page's rows replaced. The shape is the same every time —
// keep what the page does not own, take what the shard carries, restore the
// canonical order — and it is written out per section rather than abstracted,
// because the four registries of the status surface and the two identities of a
// stream are exactly the details an abstraction would have to leak.
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/fsm"
)

// applyMetaShard writes the shared allocator counter and the lifetime totals.
//
// The three travel together in one row because they are one fact: an installed
// world that took the next entity id without the creation total it belongs to
// would allocate correctly and report a history it never had.
func applyMetaShard(mine *SharedCapture, sh CorrectionShard) error {
	if sh.Pages != 1 {
		return fmt.Errorf("the meta section is one page, the shard names %d", sh.Pages)
	}
	if len(sh.Rows) != 1 || sh.Rows[0].Name != "scalars" {
		return errors.New("the meta section's page carries exactly one row")
	}
	var m metaScalars
	if err := json.Unmarshal(sh.Rows[0].Value, &m); err != nil {
		return fmt.Errorf("meta shard: %w", err)
	}
	mine.World.NextEntity, mine.World.Created, mine.World.Destroyed = m.NextEntity, m.Created, m.Destroyed
	return nil
}

// applyStreamShard replaces the RNG stream positions the page owns.
//
// A stream absent from the authority's page is dropped rather than kept: the
// stream inventory is a property of the build and of what the run has issued, and
// a receiver holding a stream the authority does not would draw from a position
// nobody else has.
func applyStreamShard(mine *SharedCapture, sh CorrectionShard) error {
	kept := mine.Streams[:0:0]
	for _, st := range mine.Streams {
		if rowPage(ManifestRow{Name: streamRowName(st)}, sh.Pages) == sh.Page {
			continue
		}
		kept = append(kept, st)
	}
	for _, row := range sh.Rows {
		var st engine.StreamState
		if err := json.Unmarshal(row.Value, &st); err != nil {
			return fmt.Errorf("stream shard: %w", err)
		}
		if streamRowName(st) != row.Name {
			return fmt.Errorf("stream shard row %q describes stream %q", row.Name, streamRowName(st))
		}
		kept = append(kept, st)
	}
	sort.Slice(kept, func(i, j int) bool { return streamRowName(kept[i]) < streamRowName(kept[j]) })
	mine.Streams = kept
	return nil
}

// applySystemShard replaces the declared private state (D-19) the page owns.
func applySystemShard(mine *SharedCapture, sh CorrectionShard) error {
	kept := mine.Systems[:0:0]
	for _, rec := range mine.Systems {
		if rowPage(ManifestRow{Name: rec.System}, sh.Pages) == sh.Page {
			continue
		}
		kept = append(kept, rec)
	}
	for _, row := range sh.Rows {
		var data []byte
		if err := json.Unmarshal(row.Value, &data); err != nil {
			return fmt.Errorf("system shard: %w", err)
		}
		kept = append(kept, SystemStateRecord{System: row.Name, Data: data})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].System < kept[j].System })
	mine.Systems = kept
	return nil
}

// applyStatusShard replaces the compared status cells the page owns.
//
// The four registries are separate namespaces, so a cell's identity is its type
// prefix and its key together, and a row whose prefix names no registry is refused
// rather than dropped: it would otherwise remove a cell from the receiver and put
// nothing back, which is a repair that makes the surface worse.
func applyStatusShard(mine *SharedCapture, sh CorrectionShard) error {
	next := StatusState{
		Ints:    filterStatusCells(mine.Status.Ints, "i:", sh, func(c IntCell) string { return c.Key }),
		Bools:   filterStatusCells(mine.Status.Bools, "b:", sh, func(c BoolCell) string { return c.Key }),
		Floats:  filterStatusCells(mine.Status.Floats, "f:", sh, func(c FloatCell) string { return c.Key }),
		Strings: filterStatusCells(mine.Status.Strings, "s:", sh, func(c StringCell) string { return c.Key }),
	}
	for _, row := range sh.Rows {
		prefix, key, ok := strings.Cut(row.Name, ":")
		if !ok {
			return fmt.Errorf("status shard row %q names no registry", row.Name)
		}
		switch prefix {
		case "i":
			var v int64
			if err := json.Unmarshal(row.Value, &v); err != nil {
				return fmt.Errorf("status shard %q: %w", row.Name, err)
			}
			next.Ints = append(next.Ints, IntCell{Key: key, Value: v})
		case "b":
			var v bool
			if err := json.Unmarshal(row.Value, &v); err != nil {
				return fmt.Errorf("status shard %q: %w", row.Name, err)
			}
			next.Bools = append(next.Bools, BoolCell{Key: key, Value: v})
		case "f":
			var v float64
			if err := json.Unmarshal(row.Value, &v); err != nil {
				return fmt.Errorf("status shard %q: %w", row.Name, err)
			}
			next.Floats = append(next.Floats, FloatCell{Key: key, Value: v})
		case "s":
			var v string
			if err := json.Unmarshal(row.Value, &v); err != nil {
				return fmt.Errorf("status shard %q: %w", row.Name, err)
			}
			next.Strings = append(next.Strings, StringCell{Key: key, Value: v})
		default:
			return fmt.Errorf("status shard row %q names registry %q", row.Name, prefix)
		}
	}
	sort.Slice(next.Ints, func(i, j int) bool { return next.Ints[i].Key < next.Ints[j].Key })
	sort.Slice(next.Bools, func(i, j int) bool { return next.Bools[i].Key < next.Bools[j].Key })
	sort.Slice(next.Floats, func(i, j int) bool { return next.Floats[i].Key < next.Floats[j].Key })
	sort.Slice(next.Strings, func(i, j int) bool { return next.Strings[i].Key < next.Strings[j].Key })
	mine.Status = next
	return nil
}

// filterStatusCells keeps the cells of one registry the repaired page does not own.
func filterStatusCells[T any](cells []T, prefix string, sh CorrectionShard, key func(T) string) []T {
	out := cells[:0:0]
	for _, c := range cells {
		if rowPage(ManifestRow{Name: prefix + key(c)}, sh.Pages) == sh.Page {
			continue
		}
		out = append(out, c)
	}
	return out
}

// applyFSMShard replaces the shared state machine's runtime position.
//
// One row and one page: the machine is a handful of regions, variables and delayed
// transitions, and splitting it would let a repair install a region's active state
// without the variable a guard on it reads.
func applyFSMShard(mine *SharedCapture, sh CorrectionShard) error {
	if sh.Pages != 1 {
		return fmt.Errorf("the fsm section is one page, the shard names %d", sh.Pages)
	}
	if len(sh.Rows) != 1 || sh.Rows[0].Name != "machine" {
		return errors.New("the fsm section's page carries exactly one row")
	}
	var m fsm.MachineState
	if err := json.Unmarshal(sh.Rows[0].Value, &m); err != nil {
		return fmt.Errorf("fsm shard: %w", err)
	}
	mine.FSM = m
	return nil
}
