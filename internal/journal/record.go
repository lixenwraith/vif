package journal

import (
	"errors"
	"fmt"
	"sync"

	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/vlog"
)

// RecordStats is the final accounting for one recording session.
type RecordStats struct {
	Emitted      uint64
	EncodeFailed uint64
	Path         string
}

// Recorder owns the attachment between an event queue and its journal sink. If
// Start opened a vlog journal file, Close also drains that file; injected sinks
// remain owned by their caller.
type Recorder struct {
	mu      sync.Mutex
	queue   *event.EventQueue
	journal *event.Journal
	file    *vlog.JournalFile // nil for an injected sink
	path    string
	closed  bool
	stats   RecordStats
}

// Start attaches a replay journal and emits its initial anchor. A nil sink opens a
// vif-jrn file of the recorder's own, its envelopes stamped by log, the run's own
// handle (nil for the process default); harnesses pass an in-memory Capture instead.
func Start(queue *event.EventQueue, anchor event.JournalAnchor, sink event.JournalSink, log *vlog.Log) (*Recorder, error) {
	if queue == nil {
		return nil, errors.New("journal: event queue is nil")
	}
	if queue.Journal() != nil {
		return nil, errors.New("journal: event queue already has a recorder")
	}

	r := &Recorder{queue: queue}
	if sink == nil {
		f, err := vlog.OpenJournal()
		if err != nil {
			return nil, fmt.Errorf("journal: %w", err)
		}
		r.file, r.path = f, f.Path()
		if log == nil {
			log = vlog.Default()
		}
		sink = event.VlogSink(f, log)
	}

	r.journal = event.NewJournal(sink)
	queue.SetJournal(r.journal)
	r.journal.SetAnchor(anchor, queue.Stamp())
	return r, nil
}

// Capture records a world the run wrote, placed after the records emitted so far.
func (r *Recorder) Capture(c event.JournalCapture) {
	c.JSeq = r.journal.Mark()
	r.journal.Capture(c)
}

// Path returns the dedicated journal path, or an empty string for an injected sink.
func (r *Recorder) Path() string {
	if r == nil {
		return ""
	}
	return r.path
}

// Stats returns the live or final accounting.
func (r *Recorder) Stats() RecordStats {
	if r == nil {
		return RecordStats{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.stats
	}
	emitted, failed := r.journal.Stats()
	return RecordStats{Emitted: emitted, EncodeFailed: failed, Path: r.path}
}

// Close detaches capture before draining a file. Call it after queue producers
// are quiescent, as App.Close does. It is idempotent.
func (r *Recorder) Close() (RecordStats, error) {
	if r == nil {
		return RecordStats{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return r.stats, nil
	}

	r.journal.Finish(r.queue.Stamp())
	if r.queue.Journal() == r.journal {
		r.queue.SetJournal(nil)
	}
	r.stats.Emitted, r.stats.EncodeFailed = r.journal.Stats()
	r.stats.Path = r.path
	r.closed = true
	if r.file != nil {
		return r.stats, r.file.Close()
	}
	return r.stats, nil
}
