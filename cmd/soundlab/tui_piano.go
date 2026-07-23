package main

// Piano mode: a two-row strip above the input line, tracker key layout.
// Notes go through Execute("note ...") like everything else — quiet echo,
// but full script parity. The strip is mode-owned: a future beat/step-grid
// editor claims the same rows, one mode active at a time.

import (
	"fmt"
	"slices"

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
	if !a.s.eng.IsMusicPlaying() {
		if a.s.eng.IsMusicMuted() {
			fmt.Fprintln(a.log, "music is muted — :mute none, then p")
			return
		}
		a.exec("music start")
		a.pianoStarted = true // piano owns this start; Esc reverts it
	}
	a.mode = modePiano
}

func (a *tuiApp) pianoKey(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		if a.rec {
			a.finishRecording()
		}
		a.mode = modeNormal
		// Stop only a transport piano itself started: exiting must not kill
		// patterns the user had running underneath.
		if a.pianoStarted {
			a.exec("music stop")
		}
		a.pianoStarted = false
		return
	case terminal.KeyCtrlS:
		if a.rec {
			a.finishRecording()
		} else {
			a.rec = true
			a.recSteps = a.recSteps[:0]
			a.recInstr = a.pianoInstr
			fmt.Fprintln(a.log, "recording — notes quantize to the 16th; Ctrl+S to commit")
		}
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
		if a.rec {
			a.captureNote(midi)
		}
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

// captureNote quantizes a played note onto the 16th grid. deg is relative to
// the harmony default root (E, DefaultRootNote) so recordings land in the
// scale the sequencer resolves against; FollowChord stays off on the
// recorded track, so absolute pitch is preserved bar over bar.
func (a *tuiApp) captureNote(midi int) {
	bar, step, running := a.s.eng.Transport()
	if !running {
		return
	}
	_ = bar
	a.recSteps = append(a.recSteps, audio.StepDef{
		Pos: step % audio.StepsPerBar,
		Vel: a.pianoVel,
		Deg: midi - audio.DefaultRootNote, // chromatic offset; resolve() folds octaves
		Oct: 0,
		Dur: 2,
	})
}

// finishRecording writes the take as a document pattern. One bar, merged by
// pos (later note wins a collision), dirty — the normal a/0-2 flow applies
// and assigns it.
func (a *tuiApp) finishRecording() {
	a.rec = false
	if len(a.recSteps) == 0 {
		fmt.Fprintln(a.log, "recording empty — nothing written")
		return
	}
	name := ""
	for {
		a.recN++
		name = fmt.Sprintf("rec_%d", a.recN)
		if !a.s.pats.has(name) && !a.s.sounds.has(name) {
			break
		}
	}
	byPos := map[int]audio.StepDef{}
	for _, st := range a.recSteps {
		byPos[st.Pos] = st
	}
	evs := make([]audio.StepDef, 0, len(byPos))
	for _, st := range byPos {
		evs = append(evs, st)
	}
	slices.SortFunc(evs, func(x, y audio.StepDef) int { return x.Pos - y.Pos })
	a.s.pats.put(&audio.PatternDef{
		Name: name, Steps: audio.StepsPerBar,
		Track: []audio.TrackDef{{Instr: a.recInstr.String(), Humanize: 0.15, Event: evs}},
	}, true)
	fmt.Fprintf(a.log, "recorded %d step(s) -> pattern %q (a to apply, 1 to hear in the melody slot)\n", len(evs), name)
}
