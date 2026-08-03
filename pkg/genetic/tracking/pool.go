package tracking

import "sync"

// CollectorPool manages reusable collectors
type CollectorPool struct {
	standard  []*StandardCollector
	composite []*CompositeCollector
	mu        sync.Mutex
}

// NewCollectorPool creates a pool with optional pre-allocation
func NewCollectorPool(prealloc int) *CollectorPool {
	p := &CollectorPool{
		standard:  make([]*StandardCollector, 0, prealloc),
		composite: make([]*CompositeCollector, 0, prealloc/4),
	}
	return p
}

// AcquireStandard gets or creates a StandardCollector
func (p *CollectorPool) AcquireStandard() *StandardCollector {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.standard) > 0 {
		c := p.standard[len(p.standard)-1]
		p.standard = p.standard[:len(p.standard)-1]
		c.Reset()
		return c
	}
	return NewStandardCollector()
}

// ReleaseStandard returns a collector to the pool
func (p *CollectorPool) ReleaseStandard(c *StandardCollector) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.standard = append(p.standard, c)
}

// AcquireComposite gets or creates a CompositeCollector
func (p *CollectorPool) AcquireComposite() *CompositeCollector {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.composite) > 0 {
		c := p.composite[len(p.composite)-1]
		p.composite = p.composite[:len(p.composite)-1]
		c.Reset()
		return c
	}
	return NewCompositeCollector()
}

// ReleaseComposite returns a collector to the pool
func (p *CollectorPool) ReleaseComposite(c *CompositeCollector) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.composite = append(p.composite, c)
}