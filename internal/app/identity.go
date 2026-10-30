package app

import (
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/manifest"
	"github.com/lixenwraith/vif/internal/network"
	"github.com/lixenwraith/vif/internal/snapshot"
)

// buildIdentity is what this binary is, with no world required: four compile-time
// constants and the name a refusal gives them. It is what a dialer compares against
// an offer before it constructs anything, and it is the half of the identity no
// configuration changes.
func buildIdentity() network.PeerIdentity {
	return network.PeerIdentity{
		Protocol:      network.ProtocolVersion,
		Simulation:    manifest.FingerprintString(),
		CaptureSchema: snapshot.Schema,
		JournalSchema: event.JournalSchema,
		Build:         asset.Build(),
	}
}

// sessionIdentity is the whole of it: this build, and the session this instance has
// actually built. It reads the live anchor rather than the configuration, because
// what matters is the corpus that loaded and the seed the world is running, not the
// ones that were asked for.
func (m *membership) sessionIdentity() network.PeerIdentity {
	return identityFromAnchor(m.JoinAnchor())
}

// identityFromAnchor is the same for an anchor already in hand — the offer a
// coordinator sent, which is what it later compares a joiner's report against.
func identityFromAnchor(j event.JoinAnchor) network.PeerIdentity {
	return buildIdentity().SessionFrom(j.Anchor)
}

// joinerReport is what this instance tells a coordinator about itself when it
// accepts an offer: the geometry a dedicated host may size its map from, the
// identity that host refuses the join on, and the claim it comes back with, with
// what it brings back when the offer is the place that claim waits under.
func (m *membership) joinerReport() network.JoinerReport {
	r := network.JoinerReport{
		Holder:   m.cfg.holder,
		Width:    m.ctx.Width,
		Height:   m.ctx.Height,
		Identity: m.sessionIdentity(),
		Listen:   m.reach.DeclaredAddr(),
	}
	if c := m.claim.Load(); c != nil {
		r.Claim, r.Record = network.ClaimDigest(c.Token), c.Record
	}
	return r
}
