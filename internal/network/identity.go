package network

import (
	"fmt"

	"github.com/lixenwraith/vif/internal/event"
)

// ProtocolVersion changes when peers cannot share the session wire contract.
const ProtocolVersion uint32 = 27

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

	// Build names the binary to a person reading a refusal, and is never compared:
	// the fields above decide whether two builds share a session, this one only
	// says which builds they were.
	Build string `json:"build,omitempty"`
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
	if diff := firstDifference(local.buildFields(remote)); diff != "" {
		return &Refusal{Code: RefusalIdentity, Reason: fmt.Sprintf("another build, %s here and %s on the other side: %s",
			local.named(), remote.named(), diff)}
	}
	return nil
}

// Verify compares the whole identity: the build a peer is running and the session
// it built. This is the authority's check, made against the offer it sent.
func (local PeerIdentity) Verify(remote PeerIdentity) error {
	if err := local.VerifyBuild(remote); err != nil {
		return err
	}
	if diff := firstDifference(local.sessionFields(remote)); diff != "" {
		return &Refusal{Code: RefusalIdentity, Reason: "another session: " + diff}
	}
	return nil
}

// named is how a refusal names a build; one with no name predates naming itself.
func (id PeerIdentity) named() string {
	if id.Build == "" {
		return "an older vif"
	}
	return "vif " + id.Build
}

// firstDifference names one field rather than all of them: they are usually one
// cause, the message goes to a person deciding what to reinstall, and a peer that
// differs in nine fields is a different build rather than nine problems.
func firstDifference(fields []identityField) string {
	for _, f := range fields {
		if f.want != f.got {
			return fmt.Sprintf("%s is %v here and %v there", f.name, f.want, f.got)
		}
	}
	return ""
}
