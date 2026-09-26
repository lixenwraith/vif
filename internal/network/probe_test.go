package network

import (
	"testing"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
)

func TestLinkReportRoundTripsThroughItsFrame(t *testing.T) {
	want := LinkReport{Tick: 1 << 40, LagTicks: 7, Magnitude: 19, CursorX: -3, CursorY: 42, HasCursor: true}
	got, ok := decodeLinkReport(want.encode())
	if !ok || got != want {
		t.Fatalf("report round trip gave %+v (ok=%v), want %+v", got, ok, want)
	}
	if _, ok := decodeLinkReport(make([]byte, linkReportSize-1)); ok {
		t.Fatal("a short frame decoded as a report")
	}
	blank, ok := decodeLinkReport(LinkReport{CursorX: 5, CursorY: 5}.encode())
	if !ok || blank.HasCursor {
		t.Fatalf("a report with no cursor claimed one: %+v", blank)
	}
}

func TestEchoCarriesTheProbeBackUntouched(t *testing.T) {
	sent := time.Unix(0, 1_700_000_000_123_456_789)
	probe := encodeProbe(9, sent)
	report := LinkReport{Tick: 500, LagTicks: 2, Magnitude: 4, CursorX: 11, CursorY: 12, HasCursor: true}

	seq, back, in, got, ok := decodeEcho(encodeEcho(probe, 8192, report))
	if !ok {
		t.Fatal("a well-formed echo did not decode")
	}
	if seq != 9 || !back.Equal(sent) {
		t.Fatalf("echo returned seq %d at %s, want 9 at %s", seq, back, sent)
	}
	if in != 8192 || got != report {
		t.Fatalf("echo carried %d bytes and %+v", in, got)
	}
	if encodeEcho(probe[:4], 0, report) != nil {
		t.Fatal("a truncated probe produced an echo")
	}
	if _, _, _, _, ok := decodeEcho(probe); ok {
		t.Fatal("a bare probe decoded as an echo")
	}
}

// TestOnlyAProbeThatStopsComingBackIsLost: an echo slower than the probe interval
// is a slow link, not a lost probe; a link that answers nothing for
// NetworkProbeLostAfter is.
func TestOnlyAProbeThatStopsComingBackIsLost(t *testing.T) {
	m := newLinkMeter()
	now := time.Unix(0, 0)
	late := 3 * parameter.NetworkProbeInterval / 2
	for i := range 20 {
		m.nextProbe()
		if i > 1 {
			m.observe(now, now.Add(-late), uint32(i-1), 0, 0, LinkReport{})
		}
		now = now.Add(parameter.NetworkProbeInterval)
	}
	if got := m.link.Metrics().Loss; got != 0 {
		t.Fatalf("echoes %v late counted as %.3f loss", late, got)
	}

	for range probeLostAfter + 1 {
		m.nextProbe()
	}
	if got := m.link.Metrics().Loss; got == 0 {
		t.Fatal("a link silent past NetworkProbeLostAfter counted no loss")
	}
}

func TestTheMeterTurnsTwoEchoesIntoADeliveryRate(t *testing.T) {
	m := newLinkMeter()
	at := time.Unix(0, 0)
	step := func(delivered, sent uint64) {
		seq := m.nextProbe()
		at = at.Add(time.Second)
		m.observe(at, at.Add(-30*time.Millisecond), seq, delivered, sent, LinkReport{})
	}
	step(0, 0)
	for i := range 10 {
		step(uint64(i+1)*50_000, uint64(i+1)*50_000)
	}
	got := m.link.Metrics()
	if got.Throughput < 45_000 || got.Throughput > 55_000 {
		t.Fatalf("50 KB per second measured %.0f B/s", got.Throughput)
	}
	if got.Saturated {
		t.Fatal("a link with nothing queued reported itself as the limit")
	}
}

// TestAJoinersReadyRestartsTheBacklogOrigin: the gate answers a probe before the
// capture, and the port that takes over after it counts from zero, so without the
// origin the ready sets the capture would stand as backlog for the whole session.
func TestAJoinersReadyRestartsTheBacklogOrigin(t *testing.T) {
	m := newLinkMeter()
	at := time.Unix(0, 0)
	echo := func(delivered, sent uint64) {
		seq := m.nextProbe()
		at = at.Add(parameter.NetworkProbeInterval)
		m.observe(at, at.Add(-5*time.Millisecond), seq, delivered, sent, LinkReport{})
	}
	echo(24, 68)
	const capture = 125_000
	m.rebase()
	for i := range 10 {
		n := uint64(i+1) * 5_000
		echo(n, 68+capture+n)
	}
	if m.link.Metrics().Saturated {
		t.Fatal("the join capture stood as backlog after the joiner's ready")
	}
}

// TestTheMeshMeasuresARoundTripOnItsOwnClock is the deterministic half of the
// measurement: a mesh has no wall time, so its round trip is counted in ticks and
// the same ticks give the same answer on every machine.
func TestTheMeshMeasuresARoundTripOnItsOwnClock(t *testing.T) {
	measure := func(shape LinkShape) time.Duration {
		mesh := NewMesh()
		mesh.Link(1, 2)
		if shape != (LinkShape{}) {
			mesh.Shape(1, 2, shape)
		}
		host, guest := mesh.Node(1), mesh.Node(2)
		buf := make([]Inbound, 16)
		for range 60 {
			host.Drain(buf)
			guest.Drain(buf)
		}
		return host.LinkMetric(2).RTT
	}

	fast := measure(LinkShape{})
	if fast <= 0 {
		t.Fatal("an unshaped mesh link measured no round trip at all")
	}
	slow := measure(LinkShape{LatencyTicks: 4})
	if slow <= fast {
		t.Fatalf("a four-tick link measured %s against an unshaped %s", slow, fast)
	}
	if again := measure(LinkShape{LatencyTicks: 4}); again != slow {
		t.Fatalf("the same shape measured %s then %s", slow, again)
	}
	// Eight ticks of round trip is 400 ms, and the estimator smooths toward it.
	if slow < 200*time.Millisecond {
		t.Fatalf("a four-tick each-way link measured only %s", slow)
	}
}

func TestTheMeshLinkShapeHoldsBackAndDropsFrames(t *testing.T) {
	mesh := NewMesh()
	mesh.Link(1, 2)
	host, guest := mesh.Node(1), mesh.Node(2)
	guest.SetShape(LinkShape{BytesPerTick: 64})

	body := make([]byte, 200)
	for range 8 {
		host.Broadcast(0x12, body)
	}
	buf := make([]Inbound, 32)
	if first := countMessages(buf[:guest.Drain(buf)]); first != 0 {
		t.Fatalf("a 64-byte budget released %d frames of 212 bytes on its first tick", first)
	}
	// The budget is head-of-line rather than a filter: the frames are not lost,
	// they are paced. Eight of them at 212 bytes need 27 ticks of a 64-byte
	// budget, so forty is comfortably enough for all of them and nowhere near
	// enough for a budget that reset each tick.
	released := 0
	for range 40 {
		released += countMessages(buf[:guest.Drain(buf)])
	}
	if released != 8 {
		t.Fatalf("forty ticks of a 64-byte budget released %d of 8 frames", released)
	}

	dropper := NewMesh()
	dropper.Link(1, 2)
	sender, receiver := dropper.Node(1), dropper.Node(2)
	receiver.SetShape(LinkShape{LossEvery: 2})
	for range 10 {
		sender.Broadcast(0x12, []byte{1})
	}
	if got := countMessages(buf[:receiver.Drain(buf)]); got != 5 {
		t.Fatalf("one frame in two dropped left %d of 10", got)
	}
}

// countMessages ignores the connect and disconnect notifications a link's own
// lifecycle produces, which are not what a shape is shaping.
func countMessages(in []Inbound) int {
	n := 0
	for i := range in {
		if in[i].Kind == InboundMessage {
			n++
		}
	}
	return n
}

// TestTheMeshProbeNeverReachesTheGame is the boundary the round trip lives
// inside: a probe and its echo are answered in the transport, so a tick never
// sees one and network timing cannot enter the simulation through them.
func TestTheMeshProbeNeverReachesTheGame(t *testing.T) {
	mesh := NewMesh()
	mesh.Link(1, 2)
	host, guest := mesh.Node(1), mesh.Node(2)
	buf := make([]Inbound, 64)
	for range 40 {
		for _, in := range buf[:host.Drain(buf)] {
			if in.Msg != nil && (in.Msg.Type == MsgLinkProbe || in.Msg.Type == MsgLinkEcho) {
				t.Fatal("a link probe reached the game-side drain")
			}
		}
		for _, in := range buf[:guest.Drain(buf)] {
			if in.Msg != nil && (in.Msg.Type == MsgLinkProbe || in.Msg.Type == MsgLinkEcho) {
				t.Fatal("a link echo reached the game-side drain")
			}
		}
	}
	if host.LinkMetric(2).Samples == 0 {
		t.Fatal("forty ticks produced no measurement at all")
	}
}

func TestTheReportReachesTheProbingPeer(t *testing.T) {
	mesh := NewMesh()
	mesh.Link(1, 2)
	host, guest := mesh.Node(1), mesh.Node(2)
	guest.SetLinkReport(LinkReport{LagTicks: 5, Magnitude: 12, CursorX: 30, CursorY: 9, HasCursor: true})

	buf := make([]Inbound, 16)
	for range 20 {
		host.Drain(buf)
		guest.Drain(buf)
	}
	m := host.LinkMetric(2)
	if m.LagTicks != 5 || m.Magnitude != 12 {
		t.Fatalf("the host read lag %d magnitude %d from the guest", m.LagTicks, m.Magnitude)
	}
	if !m.Interest.Valid || m.Interest.X != 30 || m.Interest.Y != 9 {
		t.Fatalf("the host read interest %+v", m.Interest)
	}
}

func TestMeshProbeIntervalMatchesTheParameter(t *testing.T) {
	if meshProbeDrains == 0 {
		t.Fatal("the mesh would never probe")
	}
	if want := uint64(parameter.NetworkProbeInterval / parameter.GameUpdateInterval); meshProbeDrains != want {
		t.Fatalf("mesh probes every %d drains, the interval is %d ticks", meshProbeDrains, want)
	}
}
