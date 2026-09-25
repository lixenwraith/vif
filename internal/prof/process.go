package prof

import (
	"runtime/metrics"
	"sync/atomic"
	"time"

	"github.com/lixenwraith/vif/internal/status"
)

// usage is the operating system's account of this process; zero where the
// platform keeps none
type usage struct {
	user, sys             time.Duration
	peakRSS               int64 // bytes
	readOps, writeOps     int64 // block I/O operations
	switches, preemptions int64 // voluntary and involuntary context switches
}

// runtimeMetrics are the Go runtime readings a window samples, in this order
var runtimeMetrics = [...]string{
	"/gc/heap/allocs:bytes",
	"/gc/heap/allocs:objects",
	"/gc/cycles/total:gc-cycles",
	"/memory/classes/total:bytes",
	"/gc/heap/live:bytes",
	"/sched/goroutines:goroutines",
}

const (
	rtAllocBytes = iota
	rtAllocObjects
	rtGCCycles
	rtMapped
	rtLive
	rtGoroutines
)

// process turns successive readings into the proc group's per-window rates
type process struct {
	prev    usage
	prevRT  [len(runtimeMetrics)]uint64
	samples [len(runtimeMetrics)]metrics.Sample

	userPct, sysPct, peakMB, mappedMB, liveMB, allocMBs, gcPerSec *status.AtomicFloat
	allocsPerSec, goroutines, reads, writes, switches, preempts   *atomic.Int64
}

func (p *process) register(reg *status.Registry) {
	f, i := reg.Floats.Get, reg.Ints.Get
	p.userPct, p.sysPct = f("proc.cpu_user_pct"), f("proc.cpu_sys_pct")
	p.peakMB, p.mappedMB, p.liveMB = f("proc.rss_peak_mb"), f("proc.mem_mb"), f("proc.heap_live_mb")
	p.allocMBs, p.gcPerSec = f("proc.alloc_mb_s"), f("proc.gc_s")
	p.allocsPerSec, p.goroutines = i("proc.allocs_s"), i("proc.goroutines")
	p.reads, p.writes = i("proc.io_read_s"), i("proc.io_write_s")
	p.switches, p.preempts = i("proc.csw_s"), i("proc.preempt_s")
	for k, name := range runtimeMetrics {
		p.samples[k].Name = name
	}
}

// baseline takes the readings the next window's rates are measured against
func (p *process) baseline() {
	p.prev = readUsage()
	metrics.Read(p.samples[:])
	for k := range p.samples {
		p.prevRT[k] = p.value(k)
	}
}

// sample publishes the window's rates and gauges, then rebases
func (p *process) sample(span int64) {
	u := readUsage()
	metrics.Read(p.samples[:])
	var rt [len(runtimeMetrics)]uint64
	for k := range p.samples {
		rt[k] = p.value(k)
	}
	sec := float64(span) / float64(time.Second)
	pct := func(d time.Duration) float64 { return float64(d) * 100 / float64(span) }
	rate := func(d int64) int64 { return int64(float64(d) / sec) }
	const mb = 1 << 20

	p.userPct.Set(pct(u.user - p.prev.user))
	p.sysPct.Set(pct(u.sys - p.prev.sys))
	p.peakMB.Set(float64(u.peakRSS) / mb)
	p.reads.Store(rate(u.readOps - p.prev.readOps))
	p.writes.Store(rate(u.writeOps - p.prev.writeOps))
	p.switches.Store(rate(u.switches - p.prev.switches))
	p.preempts.Store(rate(u.preemptions - p.prev.preemptions))

	p.mappedMB.Set(float64(rt[rtMapped]) / mb)
	p.liveMB.Set(float64(rt[rtLive]) / mb)
	p.allocMBs.Set(float64(rt[rtAllocBytes]-p.prevRT[rtAllocBytes]) / mb / sec)
	p.allocsPerSec.Store(rate(int64(rt[rtAllocObjects] - p.prevRT[rtAllocObjects])))
	p.gcPerSec.Set(float64(rt[rtGCCycles]-p.prevRT[rtGCCycles]) / sec)
	p.goroutines.Store(int64(rt[rtGoroutines]))

	p.prev, p.prevRT = u, rt
}

// value reads sample k as an integer; a metric this runtime lacks reads zero
func (p *process) value(k int) uint64 {
	if p.samples[k].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return p.samples[k].Value.Uint64()
}

func (p *process) clear() {
	for _, c := range []*status.AtomicFloat{p.userPct, p.sysPct, p.peakMB, p.mappedMB, p.liveMB, p.allocMBs, p.gcPerSec} {
		c.Set(0)
	}
	for _, c := range []*atomic.Int64{p.allocsPerSec, p.goroutines, p.reads, p.writes, p.switches, p.preempts} {
		c.Store(0)
	}
}
