package network

import (
	"net"
	"testing"
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
