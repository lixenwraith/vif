// TODO: merge into manifest and codegen
package engine

import (
	"sync/atomic"

	"github.com/lixenwraith/vi-fighter/internal/core"
	"github.com/lixenwraith/vi-fighter/internal/vlog"
)

// componentRule names the entity domain a component bit may attach to.
// Only single-domain components are listed; an unlisted bit is legal in either.
type componentRule struct {
	name   string
	domain core.Domain
}

// componentDomains is the audit table for AddComponentMask. Glyph, Sigil,
// Kinetic, Protection, Combat, Navigation, Death, Timer and Position attach in
// both domains and are deliberately absent, as do Cleaner, Materialize, Spirit
// and Marker, which their systems stamp from the requesting domain.
var componentDomains = map[uint64]componentRule{
	CursorBit:       {"cursor", core.DomainShared},
	NuggetBit:       {"nugget", core.DomainShared},
	WallBit:         {"wall", core.DomainShared},
	GatewayBit:      {"gateway", core.DomainShared},
	EnergyBit:       {"energy", core.DomainShared},
	HeatBit:         {"heat", core.DomainShared},
	ShieldBit:       {"shield", core.DomainShared},
	BoostBit:        {"boost", core.DomainShared},
	WeaponBit:       {"weapon", core.DomainShared},
	CursorViewBit:   {"cursor_view", core.DomainShared}, // Shared cursor view, written by one instance
	PingBit:         {"ping", core.DomainShared},
	PulseBit:        {"pulse", core.DomainShared},
	GenotypeBit:     {"genotype", core.DomainShared},
	TargetBit:       {"target", core.DomainShared},
	TargetAnchorBit: {"target_anchor", core.DomainShared},
	QuasarBit:       {"quasar", core.DomainShared},
	SwarmBit:        {"swarm", core.DomainShared},
	StormBit:        {"storm", core.DomainShared},
	StormCircleBit:  {"storm_circle", core.DomainShared},
	PylonBit:        {"pylon", core.DomainShared},
	SnakeBit:        {"snake", core.DomainShared},
	SnakeHeadBit:    {"snake_head", core.DomainShared},
	SnakeBodyBit:    {"snake_body", core.DomainShared},
	SnakeMemberBit:  {"snake_member", core.DomainShared},
	EyeBit:          {"eye", core.DomainShared},
	TowerBit:        {"tower", core.DomainShared},
	HeaderBit:       {"header", core.DomainShared},
	MemberBit:       {"member", core.DomainShared},

	LootBit:      {"loot", core.DomainPlayer},
	DustBit:      {"dust", core.DomainPlayer},
	DrainBit:     {"drain", core.DomainPlayer},
	DecayBit:     {"decay", core.DomainPlayer},
	BlossomBit:   {"blossom", core.DomainPlayer},
	BulletBit:    {"bullet", core.DomainPlayer},
	OrbBit:       {"orb", core.DomainPlayer},
	MissileBit:   {"missile", core.DomainPlayer},
	LightningBit: {"lightning", core.DomainPlayer},
	FlashBit:     {"flash", core.DomainPlayer},
	FadeoutBit:   {"fadeout", core.DomainPlayer},
	SplashBit:    {"splash", core.DomainPlayer},
}

var domainAudit atomic.Bool

// SetDomainAudit enables the per-attachment domain check; a per-tick decision
func SetDomainAudit(on bool) { domainAudit.Store(on) }

// auditComponentDomain reports an attachment contradicting the entity's domain
// tag. Diagnostic only; it never blocks the write.
func auditComponentDomain(e core.Entity, bit uint64) {
	rule, ok := componentDomains[bit]
	if !ok || rule.domain == e.Domain() {
		return
	}
	vlog.Warn("domain", "msg", "component domain mismatch",
		"component", rule.name,
		"want", rule.domain.String(),
		"got", e.Domain().String(),
		"id", e.ID())
}
