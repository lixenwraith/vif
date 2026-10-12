package component

import "time"

// TimerSlot is one of an entity's countdowns. A lifetime's expiry destroys the
// entity; every other slot stays expired until its owner acts and re-arms it.
type TimerSlot uint8

const (
	TimerLifetime TimerSlot = iota
	TimerDeadline           // one deadline its owner acts on: missile flight, materialize beam, gold sequence
	TimerCooldown           // a ready gate: mount discharge, drain energy drain
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

// Arm starts slot s over d
func (t *TimerComponent) Arm(s TimerSlot, d time.Duration) {
	t.Slots[s] = Countdown{Remaining: d, Duration: d}
}

func (t *TimerComponent) Stop(s TimerSlot) {
	t.Slots[s] = Countdown{}
}

func (t *TimerComponent) Expired(s TimerSlot) bool {
	return t.Slots[s].Duration > 0 && t.Slots[s].Remaining <= 0
}

// Progress is the elapsed fraction of slot s, zero while idle
func (t *TimerComponent) Progress(s TimerSlot) float64 {
	c := t.Slots[s]
	if c.Duration <= 0 {
		return 0
	}
	return 1 - float64(max(c.Remaining, 0))/float64(c.Duration)
}
