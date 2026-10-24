package network

import (
	"fmt"
	"net"
	"slices"
	"testing"
	"time"
)

// TestBroadcastReportsRefusedFrames pins the outbound loss signal. A peer whose
// send queue is full refuses the frame, and for a D-3 crossing that is a permanent
// lockstep divergence — so the count has to leave the transport rather than be
// swallowed by a discarded return value.
func TestBroadcastReportsRefusedFrames(t *testing.T) {
	cfg := DefaultConfig()
	cfg.SendQueueSize = 1

	pm := NewPeerManager(cfg)
	client, server := net.Pipe()
	defer client.Close()

	// Registered without its I/O loops: nothing drains the send queue, so the
	// second frame has nowhere to go.
	peer := newPeer(1, server, cfg)
	defer peer.Close()
	pm.peers[peer.ID] = peer

	if refused := pm.Broadcast(NewMessage(MsgEvent, nil)); refused != 0 {
		t.Fatalf("first broadcast refused %d frames, want 0", refused)
	}
	if refused := pm.Broadcast(NewMessage(MsgEvent, nil)); refused != 1 {
		t.Fatalf("broadcast into a full queue refused %d frames, want 1", refused)
	}

	// A closed peer refuses rather than blocking, so a lost link is also counted.
	peer.Close()
	if refused := pm.Broadcast(NewMessage(MsgEvent, nil)); refused != 1 {
		t.Fatalf("broadcast to a closed peer refused %d frames, want 1", refused)
	}
}

// TestAFrameQueuedBeforeCloseStillArrives: closing a link flushes what was queued on
// it, so a notice broadcast just before a shutdown reaches the peers it was meant for.
func TestAFrameQueuedBeforeCloseStillArrives(t *testing.T) {
	pm := NewPeerManager(DefaultConfig())
	client, server := net.Pipe()
	defer client.Close()
	if _, err := pm.AddConnectionAs(server, 1, false); err != nil {
		t.Fatal(err)
	}
	received := make(chan *Message, 1)
	go func() {
		var header [HeaderSize]byte
		msg, _ := openFlateStream(client, 0, 0).readFrame(MaxPayloadSize, header[:])
		received <- msg
	}()
	pm.Broadcast(NewMessage(MsgSessionRestart, []byte("host:7777")))
	start := time.Now()
	pm.Close()
	if elapsed := time.Since(start); elapsed > closeFlush {
		t.Errorf("Close took %s, past its %s flush bound", elapsed, closeFlush)
	}
	if msg := <-received; msg == nil || msg.Type != MsgSessionRestart {
		t.Fatalf("the peer read %+v, want the queued restart notice", msg)
	}
}

// TestALinkBundlesEpochsAndKeepsTheirOrder: what a link is sent of epochs and syncs
// leaves as one MsgBundle when the port releases it, and a frame of another kind
// releases what the link is owed before it, so a link reads its frames in the order
// they were sent.
func TestALinkBundlesEpochsAndKeepsTheirOrder(t *testing.T) {
	port := NewSocketPort(DebugConfig(RoleHost, "127.0.0.1:0"))
	defer port.transport.peers.Close()
	client, server := net.Pipe()
	defer client.Close()
	if _, err := port.transport.peers.AddConnectionAs(server, 2, false); err != nil {
		t.Fatal(err)
	}
	for _, f := range []struct {
		kind MessageType
		body string
	}{{MsgEvent, "a"}, {MsgStateSync, "b"}, {MsgDisconnect, "c"}, {MsgEvent, "d"}} {
		if !port.Send(2, uint8(f.kind), []byte(f.body)) {
			t.Fatalf("the link refused %#x", f.kind)
		}
	}
	port.Release()
	_ = client.SetReadDeadline(time.Now().Add(5 * time.Second))
	stream := openFlateStream(client, 0, 0)
	var got []string
	for range 3 {
		var header [HeaderSize]byte
		msg, err := stream.readFrame(MaxPayloadSize, header[:])
		if err != nil {
			t.Fatalf("after %v: %v", got, err)
		}
		frame := fmt.Sprintf("%#x:%s", msg.Type, msg.Payload)
		if msg.Type == MsgBundle {
			frame = "bundle:"
			_ = Unbundle(msg.Payload, func(kind MessageType, body []byte) { frame += fmt.Sprintf(" %#x:%s", kind, body) })
		}
		got = append(got, frame)
	}
	want := []string{
		fmt.Sprintf("bundle: %#x:a %#x:b", MsgEvent, MsgStateSync),
		fmt.Sprintf("%#x:c", MsgDisconnect),
		fmt.Sprintf("bundle: %#x:d", MsgEvent),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("the link read %v, want %v", got, want)
	}
}
