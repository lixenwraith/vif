package component

import "github.com/lixenwraith/vif/internal/core"

// NavigationComponent provides pathfinding state for kinetic entities
type NavigationComponent struct {
	// Snakes keep aim and routing on the same shared cursor between switches.
	LockedTarget core.Entity
	// LockTicks is how long since a snake head last weighed the cursors; the
	// spawn seeds it with the cooldown so a fresh head locks at once.
	LockTicks int

	// True when straight-line path to target is walkable
	HasDirectPath bool

	// Flow direction, valid when HasDirectPath is false
	FlowX float64
	FlowY float64

	// The body's footprint and its header's offset from the top-left, set at spawn:
	// area line of sight reads the size, and the passability tier it steers by all four
	Width, Height    int
	HeaderX, HeaderY int

	// FlowLookahead is how far along the flow vector, in cells, the homing target is
	// placed while HasDirectPath is false
	FlowLookahead float64

	// UseRouteGraph enables per-route flow field navigation instead of shared optimal field
	// Default false: entity uses shared group flow field
	UseRouteGraph bool

	// RouteGraphID selects which pre-computed route graph to use (0 = none)
	// Set by spawning system when route graph is available for entity's gateway-target pair
	RouteGraphID uint32

	// RouteID selects which route's flow field to follow within the graph (-1 = unassigned, use shared field)
	RouteID int
}
