package vlog

import "sync/atomic"

// Correlation owns the run, tick and frame stamp for one runtime.
type Correlation struct {
	run   atomic.Uint64
	tick  atomic.Uint64
	frame atomic.Uint64
}

// NewCorrelation creates an independent zero-valued stamp.
func NewCorrelation() *Correlation { return &Correlation{} }

// SetRun publishes the reset generation stamped on subsequent records.
func (c *Correlation) SetRun(n uint64) { c.run.Store(n) }

// SetTick publishes the game tick stamped on subsequent records.
func (c *Correlation) SetTick(n uint64) { c.tick.Store(n) }

// SetFrame publishes the render frame stamped on subsequent records.
func (c *Correlation) SetFrame(n uint64) { c.frame.Store(n) }

// NextRun advances the reset generation.
func (c *Correlation) NextRun() uint64 { return c.run.Add(1) }

// Stamp returns one runtime's live correlation values.
func (c *Correlation) Stamp() (uint64, uint64, uint64) {
	return c.run.Load(), c.tick.Load(), c.frame.Load()
}

var defaultCorrelation = NewCorrelation()

// DefaultCorrelation is the process logger's stamp owner.
func DefaultCorrelation() *Correlation { return defaultCorrelation }
