package machine

import (
	"encoding/json"
	"slices"
	"testing"
	"time"
)

type spawn struct{ X, Y int }

// TestQueueFiresExpiredItemsInPushOrder expires three items in one tick, pushed
// with remaining times in no order, and has the first one fired push a fourth,
// which waits for the next tick; Any sees only what is still queued
func TestQueueFiresExpiredItemsInPushOrder(t *testing.T) {
	var q Queue[spawn]
	q.Push(30*time.Millisecond, spawn{1, 0})
	q.Push(50*time.Millisecond, spawn{2, 0})
	q.Push(10*time.Millisecond, spawn{3, 0})
	q.Push(80*time.Millisecond, spawn{4, 0})

	var fired []spawn
	fire := func(v spawn) {
		if fired = append(fired, v); v.X == 1 {
			q.Push(0, spawn{5, 0})
		}
	}
	q.Tick(50*time.Millisecond, fire)
	if want := []spawn{{1, 0}, {2, 0}, {3, 0}}; !slices.Equal(fired, want) || q.Len() != 2 {
		t.Fatalf("fired %v leaving %d, want %v leaving 2", fired, q.Len(), want)
	}
	if q.Any(func(v spawn) bool { return v.X == 2 }) || !q.Any(func(v spawn) bool { return v.X == 5 }) {
		t.Fatal("Any sees a fired value or misses a queued one")
	}
	fired = nil
	q.Tick(0, fire)
	if want := []spawn{{5, 0}}; !slices.Equal(fired, want) || q.Len() != 1 {
		t.Fatalf("next tick fired %v leaving %d, want %v leaving 1", fired, q.Len(), want)
	}
}

// TestQueueItemFiresOnTheTickThatSpendsIt fires an item on the tick that brings
// its remaining time to zero exactly, and not on the tick before
func TestQueueItemFiresOnTheTickThatSpendsIt(t *testing.T) {
	var q Queue[spawn]
	q.Push(100*time.Millisecond, spawn{1, 0})
	ticks := 0
	for fired := false; !fired; {
		ticks++
		q.Tick(25*time.Millisecond, func(spawn) { fired = true })
	}
	if ticks != 4 || q.Len() != 0 {
		t.Fatalf("fired on tick %d leaving %d, want tick 4 leaving 0", ticks, q.Len())
	}
}

// TestQueueRoundTripsThroughJSON decodes what it encodes, and encodes an emptied
// queue as it does one that never held a value, so a capture digests alike
func TestQueueRoundTripsThroughJSON(t *testing.T) {
	var q Queue[spawn]
	q.Push(10*time.Millisecond, spawn{1, 2})
	q.Push(20*time.Millisecond, spawn{3, 4})
	data, err := json.Marshal(q)
	if err != nil {
		t.Fatal(err)
	}
	var back Queue[spawn]
	if err := json.Unmarshal(data, &back); err != nil || !slices.Equal(back.Items, q.Items) {
		t.Fatalf("%s decoded to %+v, %v", data, back.Items, err)
	}

	q.Clear()
	back.Tick(time.Second, func(spawn) {})
	never, _ := json.Marshal(Queue[spawn]{})
	cleared, _ := json.Marshal(q)
	spent, _ := json.Marshal(back)
	if string(cleared) != string(never) || string(spent) != string(never) {
		t.Fatalf("cleared %s and spent %s, never filled %s", cleared, spent, never)
	}
}
