package network

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vi-fighter/internal/event"
)

// ProtocolVersion is the session wire contract: the message numbering, the
// handshake order, and the shape of everything that travels inside it.
//
// It is checked before anything else on both sides, because it is the one
// disagreement that makes every other check meaningless — two builds that do not
// agree on what a frame is cannot usefully compare what is in one. Bump it when a
// change would be misread rather than rejected by the older side.
const ProtocolVersion uint32 = 1

// IdentityRefusalTag marks a join refused because the peer would not simulate the
// same session. It travels inside the refusal text because that is what the join
// handshake carries back, and it is a tag rather than a sentence so a joiner can
// recognise it without matching prose — the same arrangement HandoffRefusalTag
// uses, and for the same reason.
//
// Unlike a handoff refusal, this one is not worth retrying: nothing about the peer
// will be different a second later. It is recognisable so a joiner can say what is
// wrong rather than what to do about it.
const IdentityRefusalTag = "identity-mismatch"

// IsIdentityRefusal reports whether a join was refused for a build or session
// identity the coordinator would not accept.
func IsIdentityRefusal(err error) bool {
	return err != nil && strings.Contains(err.Error(), IdentityRefusalTag)
}

// PeerIdentity is what a participant must reproduce for a session to be shared.
//
// It exists because the join used to be self-policed: the coordinator sent its
// anchor, the joiner compared it against its own build and refused itself, and a
// joiner that did not perform that comparison was admitted on its word. The
// authority is the host, so the host has to be able to refuse — which means the
// joiner reports what it actually is, and the coordinator, not the joiner, decides
// whether that is the same session.
//
// Three of the fields are properties of the build and cannot be derived from an
// anchor: the wire contract, the simulation the manifest assembles, and the layout
// a capture is written in. The rest are the session identity an anchor already
// carries, restated here so one comparison covers both.
type PeerIdentity struct {
	Protocol      uint32 `json:"protocol"`
	Simulation    string `json:"simulation"`
	CaptureSchema int    `json:"capture_schema"`

	JournalSchema  uint64 `json:"journal_schema"`
	TickIntervalNS int64  `json:"tick_ns"`
	Seed           uint64 `json:"seed"`
	Session        uint64 `json:"session"`
	ConfigID       string `json:"config_id"`
	ContentID      string `json:"content_id"`
	ContentPin     string `json:"content_pin,omitempty"`
	ContentFiles   uint64 `json:"content_files"`
	ContentBlocks  uint64 `json:"content_blocks"`
	ContentLines   uint64 `json:"content_lines"`
}

// SessionFrom fills the session half from an anchor, leaving the build half to the
// caller. An anchor describes a session, not the binary that produced it, and the
// build values to compare against are always this build's own — reading them out of
// something a peer sent would compare a claim with itself.
func (local PeerIdentity) SessionFrom(an event.JournalAnchor) PeerIdentity {
	local.Seed = an.Seed
	local.Session = an.Session
	local.ConfigID = an.ConfigID
	local.ContentID = an.ContentID
	local.ContentPin = an.ContentPin
	local.ContentFiles = an.ContentFiles
	local.ContentBlocks = an.ContentBlocks
	local.ContentLines = an.ContentLines
	return local
}

// identityField is one comparable pair, named for the refusal message.
type identityField struct {
	name      string
	want, got any
}

// buildFields are what a peer knows about itself before it has a world: the wire
// contract, the simulation the manifest assembles, and the two layout schemas.
// They are checked first because a disagreement in any of them makes the rest
// meaningless — two builds that do not agree on what a frame is cannot usefully
// compare what is in one.
func (local PeerIdentity) buildFields(remote PeerIdentity) []identityField {
	return []identityField{
		{"protocol", local.Protocol, remote.Protocol},
		{"simulation", local.Simulation, remote.Simulation},
		{"capture_schema", local.CaptureSchema, remote.CaptureSchema},
		{"journal_schema", local.JournalSchema, remote.JournalSchema},
		{"tick_ns", local.TickIntervalNS, remote.TickIntervalNS},
	}
}

// sessionFields are what the two are simulating: the same seed over the same
// configuration and the same corpus. They can only be compared once the peer has
// built a world, which is why they are separate from the build half.
func (local PeerIdentity) sessionFields(remote PeerIdentity) []identityField {
	return []identityField{
		{"seed", local.Seed, remote.Seed},
		{"session", local.Session, remote.Session},
		{"config_id", local.ConfigID, remote.ConfigID},
		{"content_id", local.ContentID, remote.ContentID},
		{"content_pin", local.ContentPin, remote.ContentPin},
		{"content_files", local.ContentFiles, remote.ContentFiles},
		{"content_blocks", local.ContentBlocks, remote.ContentBlocks},
		{"content_lines", local.ContentLines, remote.ContentLines},
	}
}

// VerifyBuild compares only the build half, which is what a joiner can check
// against an offer before it has constructed anything. Its own seed, config and
// corpus do not exist yet; the coordinator checks those when the joiner reports
// what they turned out to be.
func (local PeerIdentity) VerifyBuild(remote PeerIdentity) error {
	return firstDifference(local.buildFields(remote))
}

// Verify compares the whole identity: the build a peer is running and the session
// it built. This is the authority's check, made against the offer it sent.
func (local PeerIdentity) Verify(remote PeerIdentity) error {
	if err := local.VerifyBuild(remote); err != nil {
		return err
	}
	return firstDifference(local.sessionFields(remote))
}

// firstDifference names one field rather than all of them: they are usually one
// cause, the message goes to a person deciding what to reinstall, and a peer that
// differs in nine fields is a different build rather than nine problems.
func firstDifference(fields []identityField) error {
	for _, f := range fields {
		if f.want != f.got {
			return fmt.Errorf("%s: %s is %v here and %v on the other side",
				IdentityRefusalTag, f.name, f.want, f.got)
		}
	}
	return nil
}
