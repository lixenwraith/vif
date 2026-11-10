package component

import (
	"math"
	"slices"
	"time"

	"github.com/lixenwraith/vif/internal/core"
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

// KrakenBurn is a killed Kraken's last pose burning away, Burn running from 0 as it
// dies to 1 as the last of it goes. Entity is the Kraken it was, whose return ends it.
type KrakenBurn struct {
	Entity core.Entity
	Kraken KrakenComponent
	X, Y   float64
	Burn   float64
}

// krakenLattice is each sample's distance from the root, and the inertia curve
// there, out past the longest reach
var krakenLattice = func() (lattice []struct{ r, bend float64 }) {
	reach := parameter.KrakenLegLength + parameter.KrakenAttackReach + parameter.KrakenMoveReach
	for i := 0; ; {
		r := float64(i) * parameter.KrakenSampleStep
		lattice = append(lattice, struct{ r, bend float64 }{r, math.Pow(r/parameter.KrakenBendRadiusScale, parameter.KrakenInertiaCurve)})
		if r > reach {
			return lattice
		}
		stride := 1
		for _, zone := range parameter.KrakenSampleStrides {
			if r < zone.Reach {
				stride = zone.Stride
				break
			}
		}
		i += stride
	}
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
// The living have burnt 0; a dead Kraken's legs draw in, crinkle and swing wider as
// its burn runs to 1.
func (k *KrakenComponent) AppendTentacleSamples(dst []KrakenSample, x, y, burn float64) []KrakenSample {
	groups := 1 // group 0 rests, group 1 attacks
	if k.State == KrakenAttack {
		groups = 2
	}
	shrink, frequency := 1-burn*parameter.KrakenBurnShrink, parameter.KrakenWiggleFreq*(1+burn*parameter.KrakenBurnCrinkle)
	var length, amplitude [2]float64
	var count [2]int
	for g := range groups {
		extension := 0.0
		if g == 1 {
			extension = k.AttackT
		}
		length[g] = (parameter.KrakenLegLength + extension*parameter.KrakenAttackReach + k.MoveBlend*parameter.KrakenMoveReach) * shrink
		amplitude[g] = parameter.KrakenWiggleAmp*(1-k.MoveBlend*parameter.KrakenWiggleMoveDamp)*(1-extension*parameter.KrakenWiggleAttackDamp) + burn*parameter.KrakenBurnSwing
		for count[g] < len(krakenLattice) && krakenLattice[count[g]].r <= length[g] {
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
		r := krakenLattice[i].r
		drag := -k.RotSpeed * parameter.KrakenInertiaBend * parameter.KrakenInertiaScale * krakenLattice[i].bend
		wave := math.Sin(r*frequency - k.Time*parameter.KrakenWiggleSpeed)
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
