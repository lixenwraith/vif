package component

import (
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/parameter"
)

// ButtonComponent is one of a row of buttons a scenario lays across the map. A press
// of its key or a click on it reaches the scenario as the same input, and Slot alone
// places it: the rectangle follows from the slot and the map, so it is never stored.
type ButtonComponent struct {
	Slot  int  // 1-based place in the row, unique
	Key   rune // the input it presses
	Color color.RGB
	Sound string // sound bank name it plays as it lights

	// PressedBy is the cursor whose press lit it, zero when the scenario lit it; Held
	// keeps a pointer press lit until that cursor releases it
	PressedBy core.Entity
	Held      bool
}

// ButtonRect places slot of count buttons in one row centred on a mapW by mapH map;
// a map too narrow for the row lets it overhang, which the renderer clips
func ButtonRect(slot, count, mapW, mapH int) (x, y, w, h int) {
	gap := parameter.ButtonGap
	w = max(1, min(parameter.ButtonMaxWidth, (mapW-(count+1)*gap)/max(count, 1)))
	h = max(1, min(parameter.ButtonMaxHeight, mapH/parameter.ButtonHeightShare))
	x = (mapW-count*w-(count-1)*gap)/2 + (slot-1)*(w+gap)
	y = (mapH - h) / 2
	return x, y, w, h
}
