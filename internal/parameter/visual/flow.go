package visual

import "github.com/lixenwraith/color"

// Flow-field debug view: a point field reads cyan to blue and a composite one
// orange to yellow, each brightening from its base toward the target
var (
	RgbFlowTarget          = color.RGB{R: 255, G: 255, B: 255}
	RgbFlowUnreached       = color.RGB{R: 60, G: 60, B: 60}
	RgbFlowCompositeTarget = color.RGB{R: 255, G: 200, B: 50}
	RgbFlowPassable        = color.RGB{R: 40, G: 100, B: 40}
	RgbFlowBlocked         = color.RGB{R: 120, G: 40, B: 40}

	FlowPointGradient          = FlowGradient{Base: [3]float64{40, 80, 120}, Span: [3]float64{60, 175, 135}}
	FlowCompositeGradient      = FlowGradient{Base: [3]float64{180, 80, 20}, Span: [3]float64{75, 120, 30}}
	FlowCompositeFieldGradient = FlowGradient{Base: [3]float64{200, 120, 20}, Span: [3]float64{55, 100, 40}}
)

// FlowGradient colours a cell at t, 0 at the field's far edge and 1 at its target
type FlowGradient struct{ Base, Span [3]float64 }

func (g FlowGradient) At(t float64) color.RGB {
	return color.RGB{R: uint8(g.Base[0] + t*g.Span[0]), G: uint8(g.Base[1] + t*g.Span[1]), B: uint8(g.Base[2] + t*g.Span[2])}
}
