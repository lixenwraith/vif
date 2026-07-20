package audio

import (
	"io"

	"github.com/lixenwraith/vi-fighter/core"
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
)

// audioCmd is the unified control/play message consumed by the mixer goroutine
// Single channel preserves ordering between playback and control commands
type audioCmd struct {
	op      cmdOp
	sound   core.SoundType
	pattern core.PatternID
	instr   core.InstrumentType
	f1      float64 // play volume | swing | music volume | note velocity
	i1      int     // bpm | crossfade | midi note | harmony root | slot (mask)
	i2      int     // slot (pattern) | duration samples | scale | mask value
	ints    []int   // harmony progression
	b       bool    // quantize
	w       io.Writer
}
