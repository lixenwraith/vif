package journal

import (
	"cmp"
	"fmt"
	"slices"
	"sync"

	"github.com/lixenwraith/vi-fighter/internal/event"
)

// Capture is an in-memory JournalSink for deterministic harnesses. Retaining a
// record is safe because every field is a value: nothing references the pooled
// payload the producer still owns.
type Capture struct {
	mu      sync.Mutex
	records []event.JournalRecord
	anchors []event.JournalAnchor
}

// NewCapture creates an empty capture sink.
func NewCapture() *Capture { return &Capture{} }

// Record appends one record; the queue stamped it at push time, so the sink needs
// no correlation source of its own.
func (c *Capture) Record(r event.JournalRecord) {
	c.mu.Lock()
	c.records = append(c.records, r)
	c.mu.Unlock()
}

// Anchor appends one header record.
func (c *Capture) Anchor(a event.JournalAnchor) {
	c.mu.Lock()
	c.anchors = append(c.anchors, a)
	c.mu.Unlock()
}

// Records returns a copy of the captured records in emission order.
func (c *Capture) Records() []event.JournalRecord {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]event.JournalRecord, len(c.records))
	copy(out, c.records)
	return out
}

// Anchors returns a copy of the captured anchors; the first describes the run.
func (c *Capture) Anchors() []event.JournalAnchor {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]event.JournalAnchor, len(c.anchors))
	copy(out, c.anchors)
	return out
}

// CheckDense reports the first jseq gap; a gap is exactly one lost record.
// Sorting first means concurrent producer append order cannot read as a gap.
func (c *Capture) CheckDense() error {
	recs := c.Records()
	slices.SortFunc(recs, func(x, y event.JournalRecord) int { return cmp.Compare(x.JSeq, y.JSeq) })
	for i := range recs {
		if want := uint64(i + 1); recs[i].JSeq != want {
			return fmt.Errorf("journal gap at index %d: jseq %d, want %d", i, recs[i].JSeq, want)
		}
	}
	return nil
}
