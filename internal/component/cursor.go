package component

// ControlKind is which producer on this instance drives a cursor. A bot is its
// own instance whose policy feeds the router, so it is ControlLocal there and
// ControlRemote everywhere else. Values are journaled in cursor spawn payloads.
type ControlKind uint8

const (
	ControlLocal  ControlKind = 0 // Intents through this instance's mode router
	ControlRemote ControlKind = 2 // Network peer
)

// CursorComponent marks an entity as a player cursor
type CursorComponent struct {
	// Slot is the roster index this cursor occupies
	Slot uint8

	// Control identifies what drives this cursor
	Control ControlKind

	// PeerID names the remote owner when Control is ControlRemote
	PeerID uint32
}
