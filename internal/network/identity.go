package network

import (
	"fmt"
	"strings"

	"github.com/lixenwraith/vif/internal/event"
)

// ProtocolVersion changes when peers cannot share the session wire contract.
const ProtocolVersion uint32 = 4

// IdentityRefusalTag distinguishes incompatible simulation identity from a
// retryable admission failure without matching user-facing prose.
const IdentityRefusalTag = "identity-mismatch"

// IsIdentityRefusal reports whether a join was refused for a build or session
// identity the coordinator would not accept.
func IsIdentityRefusal(err error) bool {
	return err != nil && strings.Contains(err.Error(), IdentityRefusalTag)
}

// PeerIdentity is the simulation contract verified by both sides before admission.
// Build identity is checked before construction; session identity after construction.
type PeerIdentity struct {
	Protocol      uint32 `json:"protocol"`
	Simulation    string `json:"simulation"`
	CaptureSchema int    `json:"capture_schema"`

	JournalSchema  uint64 `json:"journal_schema"`
	TickIntervalNS int64  `json:"tick_ns"`
	Seed           uint64 `json:"seed"`
	Session        uint64 `json:"session"`
	ScenarioDigest string `json:"scenario_digest"`
}

// SessionFrom fills the session half from an anchor, leaving the build half to the
// caller. An anchor describes a session, not the binary that produced it, and the
// build values to compare against are always this build's own — reading them out of
// something a peer sent would compare a claim with itself.
func (local PeerIdentity) SessionFrom(an event.JournalAnchor) PeerIdentity {
	local.Seed = an.Seed
	local.Session = an.Session
	local.ScenarioDigest = an.ScenarioDigest
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
// scenario. They can only be compared once the peer has built a world, which is
// why they are separate from the build half. The corpus is deliberately absent —
// it is the player's, resolved locally and never reconciled, so a peer with a
// different one is not a peer running a different session.
func (local PeerIdentity) sessionFields(remote PeerIdentity) []identityField {
	return []identityField{
		{"seed", local.Seed, remote.Seed},
		{"session", local.Session, remote.Session},
		{"scenario_digest", local.ScenarioDigest, remote.ScenarioDigest},
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
