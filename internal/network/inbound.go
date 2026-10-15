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

	// Served marks a connect from a participant that joined through this one: it
	// is behind this instance from its admission, before its first epoch says so.
	Served bool
}
