package component

import "time"

// TimerSlot is one of an entity's countdowns. A lifetime's expiry destroys the
// entity; every other slot stays expired until its owner acts and re-arms it.
type TimerSlot uint8

const (
	TimerLifetime TimerSlot = iota
	TimerDeadline           // one deadline its owner acts on: missile flight, materialize, spirit flight, gold sequence, swarm life, button light
	TimerCooldown           // a ready gate: mount discharge, drain energy drain, nugget beacon
	TimerSlotCount
)

// Countdown is idle while Duration is zero
type Countdown struct {
	Remaining time.Duration
	Duration  time.Duration
}

// TimerComponent holds the countdowns TimerSystem ages at the start of each tick
type TimerComponent struct {
	Slots [TimerSlotCount]Countdown
}

// Running reports slot s counting down; an idle or expired slot is not
func (t *TimerComponent) Running(s TimerSlot) bool {
	return t.Slots[s].Remaining > 0
}

func (t *TimerComponent) Expired(s TimerSlot) bool {
	return t.Slots[s].Duration > 0 && t.Slots[s].Remaining <= 0
}

// Elapsed is how long slot s has run since it was armed
func (t *TimerComponent) Elapsed(s TimerSlot) time.Duration {
	c := t.Slots[s]
	return c.Duration - max(c.Remaining, 0)
}

// Progress is the elapsed fraction of slot s, zero while idle
func (t *TimerComponent) Progress(s TimerSlot) float64 {
	c := t.Slots[s]
	if c.Duration <= 0 {
		return 0
	}
	return 1 - float64(max(c.Remaining, 0))/float64(c.Duration)
}
