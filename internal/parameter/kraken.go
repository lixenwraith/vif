package parameter

import "time"

const (
	KrakenBodyRadius   = 4.0
	KrakenLegLength    = 30.0
	KrakenLegThickness = 3.4
	KrakenAttackReach  = 40.0
	KrakenMoveReach    = 12.0
	KrakenWiggleAmp    = 0.2
	KrakenWiggleFreq   = 0.2
	KrakenWiggleSpeed  = 4.0
	KrakenRotSpeed     = 0.2
	KrakenSpinRotSpeed = 4.0
	KrakenMoveSpeed    = 60.0
	KrakenInertiaBend  = 0.008 // keeps a spinning tip from bending past the adjacent leg
	KrakenInertiaCurve = 0.1
	KrakenRotResponse  = 6.0 // 1/s; how quickly rotation reaches a new state's speed

	// Tentacle shape: the wiggle damps by KrakenWiggleMoveDamp of the move blend
	// and KrakenWiggleAttackDamp of the attack extension, inertia bends the tip
	// KrakenInertiaScale times the bend, thickness tapers by (1-step)^KrakenTaper,
	// the bend curve runs over radius/KrakenBendRadiusScale, and rows are
	// KrakenRowSquash times shorter than columns
	KrakenWiggleMoveDamp   = 0.7
	KrakenWiggleAttackDamp = 0.8
	KrakenInertiaScale     = 15
	KrakenTaper            = 0.7
	KrakenBendRadiusScale  = 10
	KrakenRowSquash        = 2

	KrakenAttackDuration = 1500 * time.Millisecond
	KrakenMoveDuration   = 3 * time.Second
	KrakenSpinDuration   = 2500 * time.Millisecond
	KrakenWaitMin        = 1 * time.Second
	KrakenWaitMax        = 3 * time.Second
	KrakenIdleTurnMin    = 600 * time.Millisecond
	KrakenIdleTurnMax    = 1400 * time.Millisecond
	KrakenIdleRotSpeed   = 0.25
	KrakenInitialHP      = 10000
	KrakenShieldDrain    = 10 * QuasarShieldDrain
	KrakenDamageHeat     = 10 * QuasarDamageHeat
)

// A tentacle samples a disc every KrakenSampleStep from its root, and within each
// Reach of the root, where it is thickest, only every Stride-th: discs that wide
// overlap so far that the ones skipped cover next to nothing of their own.
const KrakenSampleStep = 0.35

var KrakenSampleStrides = [...]struct {
	Reach  float64
	Stride int
}{{10, 3}, {20, 2}}
