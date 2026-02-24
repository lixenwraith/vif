package component

// TargetType defines how a target group resolves its position
type TargetType uint8

const (
	// TargetCursor tracks the player cursor entity (group 0 default)
	TargetCursor TargetType = iota
	// TargetEntity tracks a specific world entity (tower, objective)
	TargetEntity
	// TargetPosition targets fixed world coordinates
	TargetPosition
)

// MaxTargetGroups bounds the fixed-size group array in TargetResource
const MaxTargetGroups = 16

// TargetComponent assigns an entity to a navigation target group
// Entities without this component default to group 0 (cursor)
type TargetComponent struct {
	GroupID uint8 // 0 = cursor, 1+ = custom groups set by level/script
}

// TargetAnchorComponent marks an entity as the navigation destination for a group
// Set by spawning systems when target_group_id > 0 in spawn payload
// NavigationSystem discovers these and auto-registers/deregisters groups
// Component lifecycle follows entity lifecycle — no manual cleanup required
type TargetAnchorComponent struct {
	GroupID uint8
}