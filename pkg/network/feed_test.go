package network

import (
	"errors"
	"fmt"
	"net"
	"slices"
	"testing"
	"time"
)

// feedConfig is a feed whose data records close every second mark and which sends
// no keepalive, so a test reads exactly what it wrote.
func feedConfig() FeedConfig {
	cfg := DefaultFeedConfig()
	cfg.Marks, cfg.Keepalive = 2, 0
	return cfg
}

// feedOrigin is a hub with its writer, ended with the test.
func feedOrigin(t *testing.T, cfg FeedConfig) (*FeedHub, *FeedWriter) {
	t.Helper()
	hub := NewFeedHub(cfg)
	t.Cleanup(func() { hub.End("the test is over") })
	return hub, NewFeedWriter(hub)
}

// feedViewer subscribes a port to hub over an in-process link.
func feedViewer(t *testing.T, hub *FeedHub, cfg FeedConfig) *FeedPort {
	t.Helper()
	client, server := net.Pipe()
	if err := hub.Subscribe(server, false); err != nil {
		t.Fatal(err)
	}
	port := NewFeedPort(client, testConfig().Kinds, cfg)
	t.Cleanup(func() { _ = port.Close() })
	return port
}

func feedEventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); !ok(); time.Sleep(time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("%s never happened", what)
		}
	}
}

// feedHead waits for the port's starting point and takes it.
func feedHead(t *testing.T, port *FeedPort) string {
	t.Helper()
	select {
	case <-port.Heads():
	case <-time.After(2 * time.Second):
		t.Fatalf("no starting point arrived: %v", port.Err())
	}
	return string(port.TakeHead())
}

// feedTickFrames waits for the port to release tick and lists its frames as
// kind:payload, a bundle's apart.
func feedTickFrames(t *testing.T, port *FeedPort, tick uint64) []string {
	t.Helper()
	feedEventually(t, fmt.Sprintf("the release of tick %d (port: %v)", tick, port.Err()), func() bool { return port.Ready(tick) })
	got := []string{}
	var buf [4]Inbound
	for n := port.Drain(buf[:]); n > 0; n = port.Drain(buf[:]) {
		for _, in := range buf[:n] {
			list := func(kind MessageType, body []byte) { got = append(got, fmt.Sprintf("%#x:%s", kind, body)) }
			if in.Msg.Type != MsgBundle {
				list(in.Msg.Type, in.Msg.Payload)
			} else if err := Unbundle(in.Msg.Payload, list); err != nil {
				t.Fatal(err)
			}
		}
	}
	return got
}

func feedWant(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Fatalf("the port released %q, want %q", got, want)
	}
}

// TestAFeedPortReleasesATickOnlyOnItsMark: a tick's frames reach the run in the
// order written and only once that tick is asked for and its mark has arrived, so a
// run that follows a feed applies what its publisher applied, tick for tick. A run
// at another tick than the feed's next has lost step, which ends the port.
func TestAFeedPortReleasesATickOnlyOnItsMark(t *testing.T) {
	hub, w := feedOrigin(t, feedConfig())
	w.Sync(w.Cut(7), []byte("world@7"))
	port := feedViewer(t, hub, feedConfig())
	if head := feedHead(t, port); head != "world@7" {
		t.Fatalf("the starting point is %q, want world@7", head)
	}

	w.Frame(testEpoch, []byte("a"))
	w.Frame(testControl, []byte("b"))
	w.Frame(testSync, []byte("c"))
	w.Flush()
	time.Sleep(20 * time.Millisecond)
	var buf [4]Inbound
	if port.Ready(8) || port.Drain(buf[:]) != 0 {
		t.Fatal("tick 8 was released before its mark")
	}
	w.Mark(8)
	w.Frame(testEpoch, []byte("d"))
	w.Mark(9)
	feedWant(t, feedTickFrames(t, port, 8), "0x12:a", "0x18:b", "0x11:c")
	if !port.Ready(8) || port.Drain(buf[:]) != 0 {
		t.Fatal("asking again for the tick released refused it, or released the next")
	}
	if n := port.Backlog(); n != 1 {
		t.Fatalf("%d ticks wait, want tick 9", n)
	}
	if port.Ready(11) {
		t.Fatal("a run two ticks past its feed was let on")
	}
	feedEventually(t, "the port ending", func() bool {
		select {
		case <-port.Done():
			return port.Err() != nil
		default:
			return false
		}
	})
}

// TestANewcomerBeginsAtTheNewestStartingPoint: a subscriber is sent the newest
// starting point and the records since its cut, never the feed before it, and a
// subscriber already reading is sent the stream through the cut and no starting
// point of anyone else's. A starting point whose cut was replaced is not published.
func TestANewcomerBeginsAtTheNewestStartingPoint(t *testing.T) {
	hub, w := feedOrigin(t, feedConfig())
	w.Sync(w.Cut(0), []byte("world@0"))
	early := feedViewer(t, hub, feedConfig())
	if head := feedHead(t, early); head != "world@0" {
		t.Fatalf("the first subscriber began at %q", head)
	}
	tick := func(n uint64) {
		w.Frame(testEpoch, fmt.Appendf(nil, "t%d", n))
		w.Mark(n)
	}
	tick(1)
	tick(2)
	tick(3)
	stale := w.Cut(3)
	cut := w.Cut(3)
	tick(4)
	if w.Sync(stale, []byte("stale")) {
		t.Fatal("a starting point was published under a cut a later one replaced")
	}
	w.Sync(cut, []byte("world@3"))
	tick(5)
	w.Flush()

	late := feedViewer(t, hub, feedConfig())
	if head := feedHead(t, late); head != "world@3" {
		t.Fatalf("the newcomer began at %q, want world@3", head)
	}
	feedWant(t, feedTickFrames(t, late, 4), "0x12:t4")
	feedWant(t, feedTickFrames(t, late, 5), "0x12:t5")
	for n := uint64(1); n <= 5; n++ {
		feedWant(t, feedTickFrames(t, early, n), fmt.Sprintf("0x12:t%d", n))
	}
	if head := early.TakeHead(); head != nil {
		t.Fatalf("a subscriber already reading was sent the starting point %q", head)
	}
}

// TestAResetReachesEverySubscriber: a reset is the one starting point a subscriber
// already reading is sent, and nothing after it is released until the run took it.
func TestAResetReachesEverySubscriber(t *testing.T) {
	hub, w := feedOrigin(t, feedConfig())
	w.Sync(w.Cut(0), []byte("world@0"))
	port := feedViewer(t, hub, feedConfig())
	feedHead(t, port)
	w.Frame(testEpoch, []byte("old"))
	w.Mark(1)
	w.Reset(100, []byte("world@100"))
	w.Frame(testEpoch, []byte("new"))
	w.Mark(101)
	w.Flush()

	select {
	case <-port.Heads():
	case <-time.After(2 * time.Second):
		t.Fatalf("the reset never arrived: %v", port.Err())
	}
	if port.Ready(1) || port.Ready(101) {
		t.Fatal("a tick was released across a reset the run has not taken")
	}
	if head := string(port.TakeHead()); head != "world@100" {
		t.Fatalf("the reset carried %q", head)
	}
	feedWant(t, feedTickFrames(t, port, 101), "0x12:new")
}

// TestASubscriberThatFallsBehindOrSendsIsDropped: the publisher never waits on a
// subscriber, and a feed runs one way. The others read on.
func TestASubscriberThatFallsBehindOrSendsIsDropped(t *testing.T) {
	cfg := feedConfig()
	cfg.Queue, cfg.Marks = 8, 1
	hub, w := feedOrigin(t, cfg)
	w.Sync(w.Cut(0), []byte("world@0"))
	port := feedViewer(t, hub, cfg)
	feedHead(t, port)

	stalled, server := net.Pipe()
	defer stalled.Close()
	if err := hub.Subscribe(server, false); err != nil {
		t.Fatal(err)
	}
	talker, server := net.Pipe()
	defer talker.Close()
	if err := hub.Subscribe(server, false); err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 1<<10)
		for {
			if _, err := talker.Read(buf); err != nil {
				return
			}
		}
	}()
	if _, err := talker.Write([]byte{1}); err != nil {
		t.Fatal(err)
	}
	feedEventually(t, "the drop of a subscriber that sent", func() bool { return hub.Subscribers() == 2 })

	for n := uint64(1); n <= 20; n++ {
		w.Frame(testEpoch, fmt.Appendf(nil, "t%d", n))
		w.Mark(n)
		feedWant(t, feedTickFrames(t, port, n), fmt.Sprintf("0x12:t%d", n))
	}
	if n := hub.Subscribers(); n != 1 {
		t.Fatalf("%d subscribers remain, want only the one that reads", n)
	}
}

// TestARelayCopiesAFeedForItsOwnSubscribers: a relay publishes the records it reads
// without inflating them, is sent every starting point so its newcomers begin at
// the newest, and passes on why the feed ended.
func TestARelayCopiesAFeedForItsOwnSubscribers(t *testing.T) {
	origin, w := feedOrigin(t, feedConfig())
	relay := NewFeedHub(feedConfig())
	client, server := net.Pipe()
	if err := origin.Subscribe(server, true); err != nil {
		t.Fatal(err)
	}
	go func() {
		r := NewFeedReader(client, feedConfig())
		for {
			rec, err := r.Next()
			if err != nil {
				relay.End(err.Error())
				return
			}
			relay.Publish(rec)
		}
	}()

	w.Sync(w.Cut(0), []byte("world@0"))
	early := feedViewer(t, relay, feedConfig())
	if head := feedHead(t, early); head != "world@0" {
		t.Fatalf("the relay's first subscriber began at %q", head)
	}
	w.Frame(testEpoch, []byte("t1"))
	w.Mark(1)
	cut := w.Cut(1)
	w.Frame(testEpoch, []byte("t2"))
	w.Mark(2)
	w.Sync(cut, []byte("world@1"))
	w.Flush()
	feedWant(t, feedTickFrames(t, early, 1), "0x12:t1")
	feedWant(t, feedTickFrames(t, early, 2), "0x12:t2")

	late := feedViewer(t, relay, feedConfig())
	if head := feedHead(t, late); head != "world@1" {
		t.Fatalf("the relay's newcomer began at %q, want world@1", head)
	}
	feedWant(t, feedTickFrames(t, late, 2), "0x12:t2")
	if head := early.TakeHead(); head != nil {
		t.Fatalf("the relay sent a subscriber already reading the starting point %q", head)
	}

	origin.End("the host left")
	for _, port := range []*FeedPort{early, late} {
		select {
		case <-port.Done():
		case <-time.After(2 * time.Second):
			t.Fatal("a relay's subscriber never learned the feed ended")
		}
		if ended := (FeedEnded{}); !errors.As(port.Err(), &ended) || ended.Reason != "the host left" {
			t.Fatalf("the feed ended as %v, want the publisher's reason", port.Err())
		}
	}
}

// TestAQuietFeedIsAliveAndASilentOneLost: a hub with nothing to carry still sends,
// so a reader can tell a paused publisher from a dead link, which it gives up on.
func TestAQuietFeedIsAliveAndASilentOneLost(t *testing.T) {
	cfg := feedConfig()
	cfg.Keepalive, cfg.Silence = 10*time.Millisecond, 150*time.Millisecond
	hub, w := feedOrigin(t, cfg)
	w.Sync(w.Cut(0), []byte("world@0"))
	quiet := feedViewer(t, hub, cfg)
	feedHead(t, quiet)

	mute := feedConfig()
	mute.Silence = 50 * time.Millisecond
	silentHub, sw := feedOrigin(t, mute)
	sw.Sync(sw.Cut(0), []byte("world@0"))
	silent := feedViewer(t, silentHub, mute)
	feedHead(t, silent)

	select {
	case <-silent.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("a reader waited on a silent feed past its bound")
	}
	time.Sleep(2 * cfg.Silence)
	select {
	case <-quiet.Done():
		t.Fatalf("a quiet feed was given up on: %v", quiet.Err())
	default:
	}
}
