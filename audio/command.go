package audio

import (
	"io"
)

// cmdOp discriminates audioCmd variants
type cmdOp uint8

const (
	cmdPlay cmdOp = iota
	cmdBPM
	cmdSwing
	cmdMusicVol
	cmdPattern
	cmdMask
	cmdHarmony
	cmdNote
	cmdMusicStart
	cmdMusicStop
	cmdMusicReset
	cmdSwapOutput
	cmdSeed
	cmdArrangement
	cmdIntensity
	cmdReloadSound   // swap a rendered variant set; grows per-ID tables
	cmdReloadPattern // re-resolve slots pointing at a replaced pattern
	cmdPlayBuffer    // audition a caller-rendered buffer, unregistered
)

// audioCmd is the unified control/play message consumed by the mixer goroutine
// Single channel preserves ordering between playback and control commands
type audioCmd struct {
	op      cmdOp
	sound   SoundID
	pattern PatternID
	instr   InstrumentType
	tier    Intensity // arrangement tier (§5)
	slot    int8      // sequencer slot; was overloaded onto i1/i2
	f1      float64   // play volume | swing | music volume | note velocity
	i1      int       // bpm | crossfade | midi note | harmony root | slot (mask)
	i2      int       // slot (pattern) | duration samples | scale | mask value
	ints    []int     // harmony progression
	seed    int64     // sequencer rng seed (explicit width, no int-size assumption)
	b       bool      // quantize
	reveal  bool      // per-bar track build-up on the incoming pattern
	w       io.Writer
	bufs    []floatBuffer // cmdReloadSound: rendered variant set, mixer-owned on receipt
	buf     floatBuffer   // cmdPlayBuffer: single take, mixer-owned on receipt
}
