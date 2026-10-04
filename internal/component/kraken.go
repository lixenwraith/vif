package component

import (
	"math"
	"time"

	"github.com/lixenwraith/vif/internal/parameter"
)

type KrakenState uint8

const (
	KrakenIdle KrakenState = iota
	KrakenAttack
	KrakenMove
	KrakenSpin
)

// The entire animation is simulation state so hitboxes and rendering survive snapshots together.
type KrakenComponent struct {
	State             KrakenState
	StateRemaining    time.Duration
	IdleTurnRemaining time.Duration
	LastAction        KrakenState
	ActionStreak      int
	AttackLegs        int
	TargetX, TargetY  float64
	DirX, DirY        float64
	TurnDir           float64
	Angle             float64
	Time              float64
	RotSpeed          float64
	MoveBlend         float64
	AttackT           float64
}

// TentacleSamples shares the sandbox's shape between solid hitboxes and shaded rendering.
func (k *KrakenComponent) TentacleSamples(x, y float64, visit func(x, y, radius, step float64, attacking bool)) {
	for leg := range 8 {
		attacking := k.State == KrakenAttack && leg%2 == k.AttackLegs
		extension := 0.0
		if attacking {
			extension = k.AttackT
		}
		length := parameter.KrakenLegLength + extension*parameter.KrakenAttackReach + k.MoveBlend*parameter.KrakenMoveReach
		base := k.Angle + float64(leg)*math.Pi/4
		amplitude := parameter.KrakenWiggleAmp * (1 - k.MoveBlend*0.7) * (1 - extension*0.8)
		for r := 0.0; r <= length; r += parameter.KrakenSampleStep {
			step := r / length
			drag := -k.RotSpeed * parameter.KrakenInertiaBend * 15 * math.Pow(r/10, parameter.KrakenInertiaCurve)
			wiggle := math.Sin(r*parameter.KrakenWiggleFreq-k.Time*parameter.KrakenWiggleSpeed) * amplitude
			theta := base + drag + wiggle
			radius := parameter.KrakenLegThickness * math.Pow(1-step, 0.7)
			visit(x+r*math.Cos(theta), y+r*math.Sin(theta)/2, radius, step, attacking)
		}
	}
}
