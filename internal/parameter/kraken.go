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
	KrakenSpinRotSpeed = 1.6
	KrakenMoveSpeed    = 60.0
	KrakenInertiaBend  = 0.02
	KrakenInertiaCurve = 0.1
	KrakenSampleStep   = 0.35

	KrakenAttackDuration = 1500 * time.Millisecond
	KrakenMoveDuration   = 3 * time.Second
	KrakenSpinDuration   = 3 * time.Second
	KrakenWaitMin        = 2 * time.Second
	KrakenWaitMax        = 5 * time.Second
	KrakenIdleTurnMin    = 600 * time.Millisecond
	KrakenIdleTurnMax    = 1400 * time.Millisecond
	KrakenIdleRotSpeed   = 0.15
	KrakenInitialHP      = 10000
	KrakenShieldDrain    = 10 * QuasarShieldDrain
	KrakenDamageHeat     = 10 * QuasarDamageHeat
)
