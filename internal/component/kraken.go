package component

import (
	"math"
	"slices"
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
	MaxHitPoints      int // the hit points it rose with, its health bar's full length
	State             KrakenState
	StateRemaining    time.Duration
	IdleTurnRemaining time.Duration
	LastAction        KrakenState
	ActionStreak      int
	AttackLegs        int
	TargetX, TargetY  float64
	TurnDir           float64
	Angle             float64
	Time              float64
	RotSpeed          float64
	MoveBlend         float64
	AttackT           float64
}

// krakenBend is the inertia curve at each sample radius up to the longest reach
var krakenBend = func() []float64 {
	reach := parameter.KrakenLegLength + parameter.KrakenAttackReach + parameter.KrakenMoveReach
	curve := make([]float64, int(reach/parameter.KrakenSampleStep)+2)
	for i := range curve {
		curve[i] = math.Pow(float64(i)*parameter.KrakenSampleStep/parameter.KrakenBendRadiusScale, parameter.KrakenInertiaCurve)
	}
	return curve
}()

// KrakenSample is one disc along a tentacle, the Index-th from the root; Step runs
// from 0 at the root to 1 at the tip.
type KrakenSample struct {
	X, Y, Radius, Step float64
	Index              int
	Attacking          bool
}

// AppendTentacleSamples appends the sandbox's shape leg by leg from the root, the one
// shape solid hitboxes and shaded rendering share. Legs that attack, and legs that
// rest, share a length, so each index's wiggle, step and radius are computed once.
func (k *KrakenComponent) AppendTentacleSamples(dst []KrakenSample, x, y float64) []KrakenSample {
	groups := 1 // group 0 rests, group 1 attacks
	if k.State == KrakenAttack {
		groups = 2
	}
	var length, amplitude [2]float64
	var count [2]int
	for g := range groups {
		extension := 0.0
		if g == 1 {
			extension = k.AttackT
		}
		length[g] = parameter.KrakenLegLength + extension*parameter.KrakenAttackReach + k.MoveBlend*parameter.KrakenMoveReach
		amplitude[g] = parameter.KrakenWiggleAmp * (1 - k.MoveBlend*parameter.KrakenWiggleMoveDamp) * (1 - extension*parameter.KrakenWiggleAttackDamp)
		for count[g] < len(krakenBend) && float64(count[g])*parameter.KrakenSampleStep <= length[g] {
			count[g]++
		}
	}
	var group, start [8]int
	n := len(dst)
	for leg := range 8 {
		if groups == 2 && leg%2 == k.AttackLegs {
			group[leg] = 1
		}
		start[leg] = n
		n += count[group[leg]]
	}
	dst = slices.Grow(dst, n-len(dst))[:n]
	for i := range max(count[0], count[1]) {
		r := float64(i) * parameter.KrakenSampleStep
		drag := -k.RotSpeed * parameter.KrakenInertiaBend * parameter.KrakenInertiaScale * krakenBend[i]
		wave := math.Sin(r*parameter.KrakenWiggleFreq - k.Time*parameter.KrakenWiggleSpeed)
		var step, radius [2]float64
		for g := range groups {
			if i < count[g] {
				step[g] = r / length[g]
				radius[g] = parameter.KrakenLegThickness * math.Pow(1-step[g], parameter.KrakenTaper)
			}
		}
		for leg, g := range group {
			if i >= count[g] {
				continue
			}
			sin, cos := math.Sincos(k.Angle + float64(leg)*math.Pi/4 + drag + wave*amplitude[g])
			dst[start[leg]+i] = KrakenSample{
				X: x + r*cos, Y: y + r*sin/parameter.KrakenRowSquash, Radius: radius[g], Step: step[g], Index: i, Attacking: g == 1,
			}
		}
	}
	return dst
}
