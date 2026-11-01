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
	cmdTuning
	cmdMusicVol
	cmdPattern
	cmdMask
	cmdSlotMute
	cmdHarmony
	cmdNote
	cmdMusicStart
	cmdMusicStop
	cmdMusicReset
	cmdSwapOutput
	cmdSeed
	cmdIntensity
	cmdReloadSound   // swap a rendered variant set; grows per-ID tables
	cmdReloadPattern // re-resolve slots pointing at a replaced pattern
	cmdReloadVoice   // install a voice; grows the kit's table for a new instrument
	cmdPlayBuffer    // audition a caller-rendered buffer, unregistered
	cmdAutoFill
	cmdMusicTap // w receives the music bus alone, as it plays; nil detaches
)

// audioCmd is the unified control/play message consumed by the mixer goroutine
// Single channel preserves ordering between playback and control commands
type audioCmd struct {
	op      cmdOp
	sound   SoundID
	pattern PatternID
	instr   InstrumentType
	tier    Intensity // arrangement tier
	slot    int8      // sequencer slot
	f1      float64   // play volume | swing | music volume | note velocity
	i1      int       // bpm | crossfade | midi note | harmony root
	i2      int       // duration samples | scale | mask value
	ints    []int     // harmony progression
	seed    int64     // sequencer rng seed (explicit width, no int-size assumption)
	b       bool      // quantize | auto-fill on
	reveal  bool      // per-bar track build-up on the incoming pattern
	w       io.Writer
	bufs    []floatBuffer // cmdReloadSound: rendered variant set, mixer-owned on receipt
	buf     floatBuffer   // cmdPlayBuffer: single take, mixer-owned on receipt
	voice   *VoiceDef     // cmdReloadVoice: the registered entry, read-only once sent
}
