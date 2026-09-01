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

// pendingTable is a fixed-capacity slot table keyed by EvalID. Because ids are
// monotonic, a colliding insert always evicts the oldest entry, bounding memory
// without a sweep. Slot id 0 marks a free entry
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

func (t *pendingTable[S]) put(id EvalID, v S) (evicted S, hit bool) {
	i := uint64(id) & t.mask
	if t.ids[i] != 0 {
		evicted, hit = t.data[i], true
	} else {
		t.live++
	}
	t.ids[i], t.data[i] = id, v
	return
}

func (t *pendingTable[S]) take(id EvalID) (S, bool) {
	var zero S
	i := uint64(id) & t.mask
	if id == 0 || t.ids[i] != id {
		return zero, false
	}
	v := t.data[i]
	t.ids[i], t.data[i] = 0, zero
	t.live--
	return v, true
}

func (t *pendingTable[S]) clear() {
	var zero S
	for i := 0; i < len(t.ids); i++ {
		t.ids[i], t.data[i] = 0, zero
	}
	t.live = 0
}
