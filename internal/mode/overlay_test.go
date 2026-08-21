package mode

import (
	"testing"

	"github.com/lixenwraith/vi-fighter/internal/engine"
	"github.com/lixenwraith/vi-fighter/internal/input"
)

func TestSelectedCardScrollDeltaTraversesClippedCard(t *testing.T) {
	cards := []engine.OverlayCardRef{
		{Key: "before", Y: 0, H: 4},
		{Key: "tall", Y: 10, H: 25},
		{Key: "after", Y: 36, H: 4},
	}
	tests := []struct {
		name      string
		selected  string
		offset    int
		viewportH int
		motion    input.MotionOp
		want      int
	}{
		{"down reveals next row", "tall", 10, 10, input.MotionDown, 1},
		{"down stops at card bottom", "tall", 25, 10, input.MotionDown, 0},
		{"up reveals previous row", "tall", 25, 10, input.MotionUp, -1},
		{"up stops at card top", "tall", 10, 10, input.MotionUp, 0},
		{"horizontal motion selects", "tall", 10, 10, input.MotionRight, 0},
		{"missing selection", "missing", 10, 10, input.MotionDown, 0},
		{"empty viewport", "tall", 10, 0, input.MotionDown, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := selectedCardScrollDelta(cards, tt.selected, tt.offset, tt.viewportH, tt.motion)
			if got != tt.want {
				t.Fatalf("delta = %d, want %d", got, tt.want)
			}
		})
	}
}
