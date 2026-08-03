package network

type InboundKind uint8

const (
	InboundConnect InboundKind = iota
	InboundDisconnect
	InboundMessage
)

// Inbound is a transport notification for the game-side consumer
type Inbound struct {
	Kind InboundKind
	Peer PeerID
	Msg  *Message // nil for Connect/Disconnect
}
