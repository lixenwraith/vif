package component

import "github.com/lixenwraith/vif/pkg/position"

// PositionComponent is an entity's cell, the position package's point: a column,
// a row and, in a volume, a layer
type PositionComponent = position.Point
