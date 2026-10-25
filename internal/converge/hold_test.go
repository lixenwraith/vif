package converge

import (
	"testing"
	"time"
)

// TestAHeldSessionGatesAtItsTick: a join that meets a hold is handed the held tick's
// world rather than waiting for a lead the session never reaches: the host's
// keyframe is taken at that tick, the hold's tick goes out to every link once so a
// relay proves it, and a relay hands the world it proved there to a join waiting for
// a lead as to one arriving after the proof; a running session keeps no such world.
func TestAHeldSessionGatesAtItsTick(t *testing.T) {
	t.Parallel()
	deadline := func() time.Time { return time.Now().Add(5 * time.Second) }
	type handed struct {
		tick uint64
		err  error
	}
	t.Run("the host's keyframe", func(t *testing.T) {
		t.Parallel()
		host := session(t, 2, [][2]int{{1, 2}})[0]
		at := host.world.Position().Tick
		got := make(chan handed, 1)
		go func() {
			_, tick, err := host.c.KeyframeAt(at+5, deadline())
			got <- handed{tick, err}
		}()
		time.Sleep(20 * time.Millisecond)
		select {
		case k := <-got:
			t.Fatalf("a keyframe at tick %d (%v) was handed out before the lead or a hold", k.tick, k.err)
		default:
		}
		host.world.setHeld(true)
		select {
		case k := <-got:
			if k.err != nil || k.tick != at {
				t.Fatalf("the held host handed a keyframe at tick %d (%v), want the held tick %d", k.tick, k.err, at)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the held host handed out no keyframe")
		}
	})
	t.Run("the hold's tick goes out once", func(t *testing.T) {
		t.Parallel()
		runs := session(t, 2, [][2]int{{1, 2}})
		host, guest := runs[0], runs[1]
		if err := host.c.PublishDue(); err != nil {
			t.Fatalf("keyframe: %v", err)
		}
		deliver(runs, 1)
		for _, r := range runs {
			r.world.advance(1)
		}
		sent := func() int64 { return host.stat("snapshot.corrections_sent") + host.stat("snapshot.manifests_sent") }
		base := sent()
		publish := func() int64 {
			if err := host.c.PublishDue(); err != nil {
				t.Fatalf("publish: %v", err)
			}
			deliver(runs, 1)
			return sent() - base
		}
		if n := publish(); n != 0 {
			t.Fatalf("a tick after the keyframe %d rounds went out, want none before the cadence", n)
		}
		host.world.setHeld(true)
		one := publish()
		if one == 0 {
			t.Fatal("nothing went out at the hold, want the held tick's round")
		}
		if n := publish(); n != one {
			t.Fatalf("the hold's round went out again while the hold stood: %d then %d", one, n)
		}
		host.world.setHeld(false)
		for _, r := range runs {
			r.world.advance(1) // a resumed host takes a tick before a hold lands again
		}
		host.world.setHeld(true)
		if n := publish(); n != 2*one {
			t.Fatalf("over two holds %d went out, want one round each, %d", n, 2*one)
		}
		if guest.world.installs()+int(guest.stat("snapshot.manifests_received")) < 2 {
			t.Fatal("the held tick's round reached nobody")
		}
	})
	t.Run("a relay's proved world", func(t *testing.T) {
		t.Parallel()
		relay := session(t, 2, [][2]int{{1, 2}})[1]
		relay.world.setHeld(true)
		cap := relay.world.world
		at := cap.Header.Tick
		got := make(chan handed, 1)
		go func() {
			_, tick, err := relay.c.ProvedWorld(at+5, deadline())
			got <- handed{tick, err}
		}()
		time.Sleep(20 * time.Millisecond)
		relay.c.offerProved(cap)
		select {
		case p := <-got:
			if p.err != nil || p.tick != at {
				t.Fatalf("a join waiting for a lead was handed tick %d (%v), want the held tick %d", p.tick, p.err, at)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a join waiting for a lead was handed nothing at the hold")
		}
		if _, tick, err := relay.c.ProvedWorld(at, deadline()); err != nil || tick != at {
			t.Fatalf("a join arriving after the proof was handed tick %d (%v), want %d at once", tick, err, at)
		}
		relay.world.setHeld(false)
		relay.c.offerProved(cap)
		if _, _, err := relay.c.ProvedWorld(at, time.Now().Add(50*time.Millisecond)); err == nil {
			t.Fatal("a running session kept a proved world for a join that came after it")
		}
	})
}
