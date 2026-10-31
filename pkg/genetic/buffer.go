package genetic

// ring is a fixed-capacity FIFO of queued proposals
type ring[S any] struct {
	buf  []S
	mask int
	head int
	n    int
}

func (r *ring[S]) init(capacity int) {
	c := nextPow2(capacity)
	r.buf = make([]S, c)
	r.mask = c - 1
}

func (r *ring[S]) free() int { return len(r.buf) - r.n }

func (r *ring[S]) push(v S) bool {
	if r.n == len(r.buf) {
		return false
	}
	r.buf[(r.head+r.n)&r.mask] = v
	r.n++
	return true
}

func (r *ring[S]) pop() (S, bool) {
	var zero S
	if r.n == 0 {
		return zero, false
	}
	v := r.buf[r.head]
	r.buf[r.head] = zero
	r.head = (r.head + 1) & r.mask
	r.n--
	return v, true
}

func (r *ring[S]) clear() {
	var zero S
	for i := range r.buf {
		r.buf[i] = zero
	}
	r.head, r.n = 0, 0
}

// pendingWindow is how many slots from its home slot an evaluation may occupy
const pendingWindow = 8

// pendingTable is a fixed-capacity open-addressing table keyed by EvalID. A put
// probes the id's window for a free slot and otherwise evicts the oldest id in
// it, so an evaluation is lost only once its window is full of newer ones.
// Slot id 0 marks a free entry
type pendingTable[S any] struct {
	mask uint64
	ids  []EvalID
	data []S
	live int
}

func (t *pendingTable[S]) init(capacity int) {
	c := nextPow2(capacity)
	t.ids = make([]EvalID, c)
	t.data = make([]S, c)
	t.mask = uint64(c - 1)
}

func (t *pendingTable[S]) window() uint64 { return uint64(min(pendingWindow, len(t.ids))) }

func (t *pendingTable[S]) put(id EvalID, v S) (evicted S, hit bool) {
	home := uint64(id) & t.mask
	oldest := home
	for k := range t.window() {
		i := (home + k) & t.mask
		if t.ids[i] == 0 {
			t.ids[i], t.data[i] = id, v
			t.live++
			return
		}
		if t.ids[i] < t.ids[oldest] {
			oldest = i
		}
	}
	evicted, hit = t.data[oldest], true
	t.ids[oldest], t.data[oldest] = id, v
	return
}

func (t *pendingTable[S]) take(id EvalID) (S, bool) {
	var zero S
	if id == 0 {
		return zero, false
	}
	home := uint64(id) & t.mask
	for k := range t.window() {
		i := (home + k) & t.mask
		if t.ids[i] == id {
			v := t.data[i]
			t.ids[i], t.data[i] = 0, zero
			t.live--
			return v, true
		}
	}
	return zero, false
}

// place installs a restored entry at the slot its checkpoint recorded, so the
// restored table evicts as the source would
func (t *pendingTable[S]) place(slot int, id EvalID, v S) {
	t.ids[slot], t.data[slot] = id, v
	t.live++
}

func (t *pendingTable[S]) inWindow(slot int, id EvalID) bool {
	return (uint64(slot)-(uint64(id)&t.mask))&t.mask < t.window()
}

func (t *pendingTable[S]) clear() {
	var zero S
	for i := 0; i < len(t.ids); i++ {
		t.ids[i], t.data[i] = 0, zero
	}
	t.live = 0
}

// freeList keeps solution buffers an engine no longer references for offspring
// to be written into, dropping what exceeds its capacity
type freeList[S any] struct {
	buf []S
}

func (f *freeList[S]) init(capacity int) { f.buf = make([]S, 0, capacity) }

func (f *freeList[S]) put(v S) {
	if len(f.buf) < cap(f.buf) {
		f.buf = append(f.buf, v)
	}
}

// take returns a recycled buffer, or the zero value when none is held
func (f *freeList[S]) take() S {
	var zero S
	n := len(f.buf)
	if n == 0 {
		return zero
	}
	v := f.buf[n-1]
	f.buf[n-1] = zero
	f.buf = f.buf[:n-1]
	return v
}

func (f *freeList[S]) clear() {
	var zero S
	for i := range f.buf {
		f.buf[i] = zero
	}
	f.buf = f.buf[:0]
}
