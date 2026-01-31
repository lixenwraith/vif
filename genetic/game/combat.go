package game

import (
	"time"

	"github.com/lixenwraith/vi-fighter/genetic"
	"github.com/lixenwraith/vi-fighter/parameter"
	"github.com/lixenwraith/vi-fighter/vmath"
)

// DrainGenotype indices
const (
	DrainGeneHomingAccel = iota
	DrainGeneShieldApproach
	DrainGeneAggression
	DrainGeneCount
)

type DrainGenotype = []float64

type DrainPhenotype struct {
	HomingAccel    int64
	ShieldApproach int64 // Future
	AggressionMult int64
}

var DrainBounds = []genetic.ParameterBounds{
	{Min: parameter.GADrainHomingAccelMin, Max: parameter.GADrainHomingAccelMax},
	{Min: parameter.GADrainShieldApproachMin, Max: parameter.GADrainShieldApproachMax},
	{Min: parameter.GADrainAggressionMin, Max: parameter.GADrainAggressionMax},
}

type DrainCodec struct {
	bounds *genetic.BoundedPerturbator
}

func NewDrainCodec() *DrainCodec {
	return &DrainCodec{
		bounds: &genetic.BoundedPerturbator{
			Bounds:            DrainBounds,
			StandardDeviation: parameter.GADrainPerturbationStdDev,
		},
	}
}

func (c *DrainCodec) Encode(p DrainPhenotype) DrainGenotype {
	return DrainGenotype{
		vmath.ToFloat(p.HomingAccel),
		vmath.ToFloat(p.ShieldApproach),
		vmath.ToFloat(p.AggressionMult),
	}
}

func (c *DrainCodec) Decode(g DrainGenotype) DrainPhenotype {
	if len(g) < DrainGeneCount {
		return DrainPhenotype{}
	}
	return DrainPhenotype{
		HomingAccel:    vmath.FromFloat(g[DrainGeneHomingAccel]),
		ShieldApproach: vmath.FromFloat(g[DrainGeneShieldApproach]),
		AggressionMult: vmath.FromFloat(g[DrainGeneAggression]),
	}
}

func (c *DrainCodec) Clamp(g DrainGenotype) DrainGenotype {
	return c.bounds.Clamp(g)
}

// SwarmGenotype indices
const (
	SwarmGeneChaseSpeed = iota
	SwarmGeneLockDuration
	SwarmGeneChargeDuration
	SwarmGeneChargeInterval
	SwarmGeneGroupSpacing
	SwarmGeneChargeAngleBias
	SwarmGeneCount
)

type SwarmGenotype = []float64

type SwarmPhenotype struct {
	ChaseSpeedMult  int64
	LockDuration    time.Duration
	ChargeDuration  time.Duration
	ChargeInterval  time.Duration
	GroupSpacing    int64
	ChargeAngleBias int64
}

var SwarmBounds = []genetic.ParameterBounds{
	{Min: 2.0, Max: 6.0},
	{Min: 0.5, Max: 4.0},
	{Min: 0.3, Max: 1.5},
	{Min: 3.0, Max: 10.0},
	{Min: 2.0, Max: 8.0},
	{Min: -1.0, Max: 1.0},
}

type SwarmCodec struct {
	bounds *genetic.BoundedPerturbator
}

func NewSwarmCodec() *SwarmCodec {
	return &SwarmCodec{
		bounds: &genetic.BoundedPerturbator{
			Bounds:            SwarmBounds,
			StandardDeviation: 0.1,
		},
	}
}

func (c *SwarmCodec) Encode(p SwarmPhenotype) SwarmGenotype {
	return SwarmGenotype{
		vmath.ToFloat(p.ChaseSpeedMult),
		p.LockDuration.Seconds(),
		p.ChargeDuration.Seconds(),
		p.ChargeInterval.Seconds(),
		vmath.ToFloat(p.GroupSpacing),
		vmath.ToFloat(p.ChargeAngleBias),
	}
}

func (c *SwarmCodec) Decode(g SwarmGenotype) SwarmPhenotype {
	if len(g) < SwarmGeneCount {
		return SwarmPhenotype{}
	}
	return SwarmPhenotype{
		ChaseSpeedMult:  vmath.FromFloat(g[SwarmGeneChaseSpeed]),
		LockDuration:    time.Duration(g[SwarmGeneLockDuration] * float64(time.Second)),
		ChargeDuration:  time.Duration(g[SwarmGeneChargeDuration] * float64(time.Second)),
		ChargeInterval:  time.Duration(g[SwarmGeneChargeInterval] * float64(time.Second)),
		GroupSpacing:    vmath.FromFloat(g[SwarmGeneGroupSpacing]),
		ChargeAngleBias: vmath.FromFloat(g[SwarmGeneChargeAngleBias]),
	}
}

func (c *SwarmCodec) Clamp(g SwarmGenotype) SwarmGenotype {
	return c.bounds.Clamp(g)
}