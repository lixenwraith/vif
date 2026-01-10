package component

import (
	"github.com/lixenwraith/vi-fighter/core"
)

// MemberComponent provides O(1) anchor resolution from any child entity
type MemberComponent struct {
	HeaderEntity core.Entity // Phantom Head
}