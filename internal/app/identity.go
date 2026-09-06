// What this instance is, as the other side of a session needs to hear it.
//
// The join used to be self-policed. The coordinator sent its anchor, the joiner
// compared it against its own build, and a joiner that did not perform that
// comparison — an older client, a modified one, one that simply skipped it — was
// admitted on its word. The host is the authority over the session, so the host has
// to be the one that refuses: the joiner reports what it actually is, and the
// coordinator decides whether that is the same game.
//
// Two of the three fields the anchor cannot supply are new here. The wire contract
// is network's own constant. The simulation is the manifest's fingerprint: which
// components exist, which systems run, in which order and domain, and which of them
// carry state a capture moves — the thing that actually decides whether two
// participants converge. The third is the capture layout, which snapshot has always
// versioned but which nothing checked until a world was already being installed.

package app

import (
	"github.com/lixenwraith/vi-fighter/internal/event"
	"github.com/lixenwraith/vi-fighter/internal/manifest"
	"github.com/lixenwraith/vi-fighter/internal/network"
	"github.com/lixenwraith/vi-fighter/internal/parameter"
	"github.com/lixenwraith/vi-fighter/internal/snapshot"
)

// buildIdentity is what this binary is, with no world required: five compile-time
// constants. It is what a dialer compares against an offer before it constructs
// anything, and it is the half of the identity no configuration changes.
func buildIdentity() network.PeerIdentity {
	return network.PeerIdentity{
		Protocol:       network.ProtocolVersion,
		Simulation:     manifest.FingerprintString(),
		CaptureSchema:  snapshot.Schema,
		JournalSchema:  event.JournalSchema,
		TickIntervalNS: int64(parameter.GameUpdateInterval),
	}
}

// sessionIdentity is the whole of it: this build, and the session this instance has
// actually built. It reads the live anchor rather than the configuration, because
// what matters is the corpus that loaded and the seed the world is running, not the
// ones that were asked for.
func (a *App) sessionIdentity() network.PeerIdentity {
	return identityFromAnchor(a.JoinAnchor())
}

// identityFromAnchor is the same for an anchor already in hand — the offer a
// coordinator sent, which is what it later compares a joiner's report against.
func identityFromAnchor(j event.JoinAnchor) network.PeerIdentity {
	return buildIdentity().SessionFrom(j.Anchor)
}

// joinerReport is what this instance tells a coordinator about itself when it
// accepts an offer: the geometry a dedicated host may size its map from, and the
// identity that host refuses the join on.
func (a *App) joinerReport() network.JoinerReport {
	return network.JoinerReport{
		Width:    a.ctx.Width,
		Height:   a.ctx.Height,
		Identity: a.sessionIdentity(),
	}
}
