// Named domain-boundary exemptions.
//
// The domain rules in doc/domain-design.md are enforced mechanically almost
// everywhere: the event class table and the system profile are compared by
// TestEventClassMatchesSystemProfile, the owner-authored set by
// TestRemoteCursorRejectsOwnerAuthoredWrites, entity domains by the allocator
// itself. What is left over is a handful of places where the boundary is crossed
// on purpose, and the problem with those was never that they were wrong — each is
// individually defensible — but that an unremarked one is indistinguishable from a
// mistake. A named, commented, tested exemption is a boundary. A bare cast is debt.
//
// This file is where they are named. An exemption added anywhere else is one nobody
// agreed to.

package system

import "github.com/lixenwraith/vi-fighter/internal/core"

// routeAnchorID is a domain-boundary exemption, named here rather than left as a
// bare cast at each of its sites.
//
// A route graph is addressed by a uint32 on the wire and in NavigationComponent,
// and the anchor it belongs to is a core.Entity: 56 bits of id with the
// replication domain packed above them. This keeps 32 of those id bits and none of
// the tag, which is exact rather than lucky under two conditions and no others.
// Every route anchor is created in the shared domain — GatewaySystem.spawnGateway
// is the only producer and TestRouteAnchorsAreShared pins it — so the dropped tag
// carries no information; and one domain never issues 2^32 ids in a run, so the
// dropped id bits carry none either.
//
// If either stops holding, the payload field widens to uint64 rather than this
// getting cleverer: an identity compared across instances is the whole identity or
// it is a different entity's.
func routeAnchorID(anchor core.Entity) uint32 { return uint32(anchor) }
