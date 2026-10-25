package network

import link "github.com/lixenwraith/vif/pkg/network"

// The kinds a session exchanges, and the table a link reads them by. The reserved
// codes hold their place in the numbering for capabilities the transport does not
// have yet; nothing sends or accepts them, and NetworkSystem counts an unrecognised
// type as a drop rather than translating it.
const (
	// MsgSessionRoute is the one frame a dialer sends before it reads: the session
	// name it was given, empty for none, so one address can carry several. A front
	// door reads it to place the connection, and the session refuses a name not its
	// own, so a stale link is refused rather than seated in a stranger's match. It is
	// a routing key, not a credential: the port is unauthenticated by decision.
	MsgSessionRoute link.MessageType = 0x05 // Live: the session name a dialer is holding

	// MsgJoinClaims is the coordinator asking a dialer, before it assigns a place,
	// which places it can come back to, and the dialer's answer: the token of each
	// claim it keeps. Asked rather than sent with the route, so a door that refuses
	// the route has read all a dialer sent and its refusal arrives whole.
	MsgJoinClaims link.MessageType = 0x06 // Live: the claims a dialer comes back with

	// Preparing holds nothing in the session. A preview is the offer an assignment
	// would make, with no identity in it, which the dialer builds its world on.
	// Prepared opens the coordinator's ready period for the player's press; seat is
	// the press, or a dialer that needs none, and only then is a place assigned. A
	// ready period that runs out ends the handshake with nothing to give back.
	MsgJoinPreview  link.MessageType = 0x07 // Live: the session a dialer prepares for
	MsgJoinPrepared link.MessageType = 0x08 // Live: the dialer is ready and waits for its press
	MsgJoinSeat     link.MessageType = 0x09 // Live: the dialer asks for its place now

	// Game. 0x10 is reserved and stays so: raw participant input is not a message
	// kind — a peer sends the resolved D-3 artifact, never the keystroke.
	MsgStateSync link.MessageType = 0x11 // Live: one cursor's owner-authored state (D-13)
	MsgEvent     link.MessageType = 0x12 // Live: one closed barrier production epoch

	// MsgScenarioRequest and MsgScenarioBody carry the session's scenario to a joiner
	// whose roots lack it: the request names the preview's digest, and the answer is
	// the deflated canonical form, chunked like a capture. They sit before the joiner
	// reports an identity the coordinator would refuse it on, while it prepares or,
	// seated at once, before its reply, and one request fits a handshake.
	MsgScenarioRequest link.MessageType = 0x16 // Live: a joiner asking for the session's scenario
	MsgScenarioBody    link.MessageType = 0x17 // Live: one chunk of the deflated scenario

	// MsgSessionRestart is the authority saying this session is rebuilding on
	// another scenario. Its payload is the address to redial, where the authority
	// listens or the one it declared on inheriting the session, so a restart follows
	// a migrated authority; the offer given on redial names what is being played.
	// Both sides restart the run, so there is no state to hand over.
	MsgSessionRestart link.MessageType = 0x18 // Live: the authority is rebuilding the session

	// MsgStateSnapshot carries one chunk of an authoritative shared-world capture
	// (D-19). Its size is a function of the world rather than of the format, so it is
	// split; see snapshot.go for the chunk header.
	MsgStateSnapshot link.MessageType = 0x26 // Live: one chunk of a shared-world capture

	// MsgStateCorrection carries one chunk of a periodic authoritative correction, a
	// whole capture or a delta against the last keyframe. It is chunked as
	// MsgStateSnapshot but is its own kind: a capture is part of a handshake on a
	// stream nothing else owns yet, a correction arrives mid-session beside epochs,
	// syncs and digests.
	MsgStateCorrection link.MessageType = 0x27 // Live: one chunk of an authoritative correction

	// The selective-correction exchange: a manifest indexes the capture a correction
	// would carry, sent on each peer's cadence; a request, sent back to the peer the
	// manifest came from, names the pages a receiver could not reproduce; a shard set
	// is that peer's repair, chunked as a correction body is and bounded by the
	// keyframe it stands in for, which answers a wider one.
	MsgStateManifest link.MessageType = 0x28 // Live: a correction manifest, root and section hashes
	MsgStateRequest  link.MessageType = 0x29 // Live: one receiver's answer to a manifest
	MsgStateShard    link.MessageType = 0x2A // Live: the pages one request asked for

	// MsgStateUnserved answers a request a retention holder cannot serve: it holds no
	// index for the tick or none that answers the pages, the repair is too wide, or
	// the request asks a relay for a whole world. Silence would cost the receiver a
	// cadence, and a body from another baseline is the mixed-baseline assembly the
	// supersession rules make unreachable.
	MsgStateUnserved link.MessageType = 0x2B // Live: a request this peer's retention cannot answer

	// Succession, in the order it runs. A report is the news that the authority is
	// gone, flooded so a participant two links away learns of a loss only its
	// neighbour observed; a handoff, a membership change, lets a receiver adopt a
	// term it has never seen. There is no vote: the successor is a function of the
	// chain and the roster (see authority.go), so 0x2D is retired rather than reused.
	MsgAuthorityReport  link.MessageType = 0x2C // Live: a survivor's notice that the authority is gone
	MsgAuthorityHandoff link.MessageType = 0x2E // Live: the record a successor authors under

	// MsgPeerList is the succession chain, flooded whole: the eligible participants
	// in join order and where each listens, so a survivor can dial the successor a
	// handoff names. It rides the authority's term and is refused below the term the
	// receiver holds, like every other authoritative artifact. See reach.go.
	MsgPeerList link.MessageType = 0x20 // Live: the succession chain and its addresses

	// MsgRelayJoin and MsgRelayAdmit admit a participant on the link of the one it
	// dialled rather than on the coordinator's: a guest's bot joins through its
	// holder. The holder runs the handshake and serves the world; the coordinator
	// still decides, asked on the holder's own link at the two steps that change the
	// roster — allocating the identity and crossing the arrival.
	MsgRelayJoin  link.MessageType = 0x21 // Live: a relay asking the coordinator to decide one step
	MsgRelayAdmit link.MessageType = 0x2F // Live: the coordinator's answer, on the same link

	// MsgConnect and MsgAck are the peer link: two participants that already hold
	// identities the coordinator assigned opening a stream between themselves.
	// Deliberately not a join — nothing is allocated, offered or captured — and
	// deliberately not the coordinator's handshake, which a guest could not serve.
	MsgConnect link.MessageType = 0x02 // Live: a peer opening a link, or confirming a bind
	MsgAck     link.MessageType = 0x04 // Live: the peer link answered

	// Membership. A departure is observed only by a direct neighbour, so a neighbour
	// that is not the coordinator forwards a notice rather than acting on it.
	MsgDisconnect link.MessageType = 0x03 // Live: a participant's link was lost

	// Coordination, in the order the startup handshake runs them
	MsgJoinOffer link.MessageType = 0x22 // Live: host offers the session anchor and roster assignment
	MsgJoinReply link.MessageType = 0x23 // Live: joiner accepts or rejects the offered identity
	MsgStart     link.MessageType = 0x24 // Live: host releases a joiner into the capture that follows
	MsgReady     link.MessageType = 0x25 // Live: joiner confirms it received the start gate

	// Reserved, unused: authentication.
	MsgAuthRequest  link.MessageType = 0x30
	MsgAuthResponse link.MessageType = 0x31
)

// kinds is what a link knows of the session's kinds: the family each counts under,
// which travel bundled and which bodies the snapshot codec deflated already. A kind
// not named counts as control, the link's probe and echo among them.
var kinds = link.NewKinds(map[link.MessageType]link.Traits{
	MsgEvent:           {Kind: link.KindEpoch, Bundles: true},
	MsgStateSync:       {Kind: link.KindSync, Bundles: true},
	link.MsgBundle:     {Kind: link.KindEpoch},
	MsgStateSnapshot:   {Kind: link.KindCorrection, Precompressed: true},
	MsgStateCorrection: {Kind: link.KindCorrection, Precompressed: true},
	MsgStateManifest:   {Kind: link.KindRepair},
	MsgStateRequest:    {Kind: link.KindRepair},
	MsgStateShard:      {Kind: link.KindRepair, Precompressed: true},
	MsgStateUnserved:   {Kind: link.KindRepair},
	MsgScenarioBody:    {Kind: link.KindControl, Precompressed: true},
}, MsgReady, MsgConnect, MsgSessionRoute)

// Bundles reports whether frames of kind t travel bundled: what a link is sent of a
// tick's epochs and syncs leaves as one MsgBundle frame.
func Bundles(t link.MessageType) bool { return kinds.Traits(t).Bundles }
