package main

// Piano mode: a two-row strip above the input line, tracker key layout.
// Notes go through Execute("note ...") like everything else — quiet echo,
// but full script parity. The strip is mode-owned: a future beat/step-grid
// editor claims the same rows, one mode active at a time.

import (
	"fmt"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vi-fighter/audio"
)

// pkey is one keycap on the strip: cell x, strip row (0 = blacks, 1 =
// whites), semitone offset from the base octave's C.
type pkey struct {
	x, row, off int
	cap         rune
}

// Tracker layout, two octaves: z-row whites + home-row blacks, q-row whites
// + number-row blacks. Black x positions sit between their neighbors, with
// the E-F and B-C gaps left empty.
var pianoLayout = []pkey{
	{1, 0, 1, 's'}, {3, 0, 3, 'd'}, {7, 0, 6, 'g'}, {9, 0, 8, 'h'}, {11, 0, 10, 'j'},
	{19, 0, 13, '2'}, {21, 0, 15, '3'}, {25, 0, 18, '5'}, {27, 0, 20, '6'}, {29, 0, 22, '7'},
	{0, 1, 0, 'z'}, {2, 1, 2, 'x'}, {4, 1, 4, 'c'}, {6, 1, 5, 'v'},
	{8, 1, 7, 'b'}, {10, 1, 9, 'n'}, {12, 1, 11, 'm'}, {14, 1, 12, ','},
	{18, 1, 12, 'q'}, {20, 1, 14, 'w'}, {22, 1, 16, 'e'}, {24, 1, 17, 'r'},
	{26, 1, 19, 't'}, {28, 1, 21, 'y'}, {30, 1, 23, 'u'}, {32, 1, 24, 'i'},
}

var pianoOffsets = func() map[rune]int {
	m := make(map[rune]int, len(pianoLayout))
	for _, k := range pianoLayout {
		m[k.cap] = k.off
	}
	return m
}()

func (a *tuiApp) enterPiano() {
	// Tonal voices render only inside Sequencer.Generate: no transport, no
	// sound. Start it rather than let the first note land in silence.
	if !a.s.eng.IsMusicPlaying() {
		if a.s.eng.IsMusicMuted() {
			fmt.Fprintln(a.log, "music is muted — :mute none, then p")
			return
		}
		a.exec("music start")
	}
	a.mode = modePiano
}

func (a *tuiApp) pianoKey(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		a.mode = modeNormal
		return
	case terminal.KeyTab:
		switch a.pianoInstr {
		case audio.InstrBass:
			a.pianoInstr = audio.InstrPiano
		case audio.InstrPiano:
			a.pianoInstr = audio.InstrPad
		default:
			a.pianoInstr = audio.InstrBass
		}
		return
	case terminal.KeyUp:
		a.pianoVel = min(a.pianoVel+0.05, 1.0)
		return
	case terminal.KeyDown:
		a.pianoVel = max(a.pianoVel-0.05, 0.05)
		return
	case terminal.KeyRune:
	default:
		return
	}
	switch ev.Rune {
	case '[':
		if a.pianoOct > 0 {
			a.pianoOct--
		}
	case ']':
		// Cap so the top playable key (base+24) stays in sane register.
		if a.pianoOct < 6 {
			a.pianoOct++
		}
	default:
		off, ok := pianoOffsets[ev.Rune]
		if !ok {
			return
		}
		midi := audio.MIDINote(audio.NoteC, a.pianoOct) + off
		a.execQ(fmt.Sprintf("note %d %.2f 2 %s", midi, a.pianoVel, a.pianoInstr))
		a.pianoLit[ev.Rune] = 3 // ~300ms at the ticker rate
	}
}

func (a *tuiApp) renderPiano(r tui.Region) {
	r.Fill(a.theme.Bg)
	for _, k := range pianoLayout {
		fg, bg := a.theme.Fg, a.theme.FocusBg // whites: raised blocks
		if k.row == 0 {
			fg, bg = a.theme.HintFg, a.theme.Bg // blacks: recessed
		}
		if a.pianoLit[k.cap] > 0 {
			fg, bg = a.theme.Bg, a.theme.Selected
		}
		r.Cell(1+k.x, k.row, k.cap, fg, bg, terminal.AttrNone)
	}
	r.Text(36, 1, fmt.Sprintf("C%d .. C%d", a.pianoOct, a.pianoOct+2),
		a.theme.HintFg, a.theme.Bg, terminal.AttrNone)
}
