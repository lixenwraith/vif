package machine

import "time"

// Queue holds values, each behind its own relative timer, for any number of
// pending spawns or effects. An empty queue encodes as JSON the same way whether
// it never held a value or was emptied, so a captured one digests alike.
type Queue[T any] struct {
	Items []Item[T] `json:"items,omitempty"`
}

type Item[T any] struct {
	Remaining time.Duration `json:"remaining"`
	Value     T             `json:"value"`
}

// Push queues v to fire once after has been spent
func (q *Queue[T]) Push(after time.Duration, v T) {
	q.Items = append(q.Items, Item[T]{Remaining: after, Value: v})
}

// Tick spends dt on every item and fires the ones it expires in push order,
// whatever their remaining times were. A value fire pushes waits for the next
// Tick.
func (q *Queue[T]) Tick(dt time.Duration, fire func(T)) {
	n, kept := len(q.Items), 0
	for i := range n {
		it := q.Items[i]
		if it.Remaining -= dt; it.Remaining > 0 {
			q.Items[kept] = it
			kept++
			continue
		}
		fire(it.Value)
	}
	kept += copy(q.Items[kept:], q.Items[n:])
	clear(q.Items[kept:])
	q.Items = q.Items[:kept]
}

func (q *Queue[T]) Len() int { return len(q.Items) }

func (q *Queue[T]) Clear() {
	clear(q.Items)
	q.Items = q.Items[:0]
}

// Any reports whether a queued value matches
func (q *Queue[T]) Any(match func(T) bool) bool {
	for _, it := range q.Items {
		if match(it.Value) {
			return true
		}
	}
	return false
}
