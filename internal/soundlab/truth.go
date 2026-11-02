//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/lixenwraith/vif/pkg/audio"
)

// cmdAgree compares the transcripts of two rips of one piece, two witnesses
// where no score is to hand: what both hear is likely, what one hears is to
// doubt
func cmdAgree(s *Session, a []string) error {
	ra, ok := s.rips[a[0]]
	if !ok {
		return fmt.Errorf("%q: nothing ripped under that name this session (rip first)", a[0])
	}
	rb, ok := s.rips[a[1]]
	if !ok {
		return fmt.Errorf("%q: nothing ripped under that name this session (rip first)", a[1])
	}
	ag := audio.MatchNotes(ra.rip.Notes, rb.rip.Notes)
	fmt.Fprintf(s.out, "%s against %s, %s transposed %+d: %s's seconds = %.3f × %s's %+.2f\n", a[0], a[1], a[1], ag.Shift, a[0], ag.Stretch, a[1], ag.Offset)
	reportAgreement(s.out, ag, a[0], a[1], "heard by both")
	return nil
}

// cmdTruth scores a rip's transcript against one given as text in the notes
// verb's own form, corrected by hand or written out, or as a MIDI file, whose
// drum channel scores the drums: how many of the notes and hits given the rip
// heard, and how many it heard that were not given
func cmdTruth(s *Session, a []string) error {
	rp, ok := s.rips[a[0]]
	if !ok {
		return fmt.Errorf("%q: nothing ripped under that name this session (rip first)", a[0])
	}
	data, err := os.ReadFile(s.path(a[1]))
	if err != nil {
		return err
	}
	var truth []audio.RipNote
	var hits []audio.RipHit
	if isMIDI(a[1]) {
		truth, hits, err = audio.ReadMIDI(bytes.NewReader(data))
	} else {
		// a rhythm tapped out without a start begins where the rip's melody
		// register first sounds
		origin := math.Inf(1)
		for _, n := range rp.rip.Notes {
			if !n.Bass {
				origin = math.Min(origin, n.Start)
			}
		}
		if math.IsInf(origin, 1) {
			origin = 0
		}
		truth, err = audio.ParseTranscript(string(data), rp.rip.BPM, origin)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", a[1], err)
	}
	if len(truth) > 0 {
		ag := audio.MatchNotes(rp.rip.Notes, truth)
		fmt.Fprintf(s.out, "%s against %d note(s) given, transposed %+d: the rip's seconds = %.3f × the given %+.2f\n", a[0], len(truth), ag.Shift, ag.Stretch, ag.Offset)
		reportAgreement(s.out, ag, a[0], "given", "heard as given")
	}
	if len(hits) > 0 {
		reportHits(s.out, a[0], rp.rip, hits)
	}
	return nil
}

// reportHits prints each drum's count of a rip's hits against the hits given,
// matched within half a step, and the hits only one side has
func reportHits(w io.Writer, name string, r *audio.Rip, given []audio.RipHit) {
	hg := audio.MatchHits(r.Hits(), given, 30/float64(r.BPM)/audio.StepsPerBeat)
	fmt.Fprintf(w, "%s against %d hit(s) given: the rip's seconds = %.3f × the given %+.2f\n", name, len(given), hg.Stretch, hg.Offset)
	brief := func(hits []audio.RipHit) string {
		notes := make([]audio.RipNote, len(hits))
		for i, h := range hits {
			notes[i] = audio.RipNote{Start: h.Start, MIDI: -1}
		}
		return notesInBrief(notes)
	}
	for d, l := range hg.Drums {
		if l.A > 0 || l.B > 0 {
			fmt.Fprintf(w, "%-6s %s %d, given %d, %d heard as given, only %s %d, only given %d\n", audio.InstrumentName(audio.InstrumentType(d)), name, l.A, l.B, l.Matched, name, len(l.OnlyA), len(l.OnlyB))
		}
	}
	for d, l := range hg.Drums {
		if len(l.OnlyA) > 0 {
			fmt.Fprintf(w, "  %s only %s: %s\n", audio.InstrumentName(audio.InstrumentType(d)), name, brief(l.OnlyA))
		}
		if len(l.OnlyB) > 0 {
			fmt.Fprintf(w, "  %s only given: %s\n", audio.InstrumentName(audio.InstrumentType(d)), brief(l.OnlyB))
		}
	}
}

// isMIDI is whether a file names a Standard MIDI File by its extension
func isMIDI(file string) bool {
	ext := strings.ToLower(filepath.Ext(file))
	return ext == ".mid" || ext == ".midi"
}

// reportAgreement prints each line's counts and the notes only one side has
func reportAgreement(w io.Writer, ag audio.Agreement, nameA, nameB, both string) {
	words := [...]string{"melody", "under", "bass"}
	for i, l := range ag.Lines {
		if l.A == 0 && l.B == 0 {
			continue
		}
		fmt.Fprintf(w, "%-6s %s %d, %s %d, %d %s, only %s %d, only %s %d", words[i], nameA, l.A, nameB, l.B, l.Matched, both, nameA, len(l.OnlyA), nameB, len(l.OnlyB))
		if l.Rhythm {
			fmt.Fprintf(w, "; by onset: %d of the given cut in two or more, %d of %s's span two or more", l.Split, l.Merged, nameA)
		}
		fmt.Fprintln(w)
	}
	if r, apart := ag.Register, ag.Lines[0].Matched+ag.Lines[1].Matched; r.Matched > apart {
		fmt.Fprintf(w, "melody register as one line: %d %s, %d more than the lines apart, so the two part the lines differently\n", r.Matched, both, r.Matched-apart)
	}
	for i, l := range ag.Lines {
		if len(l.OnlyA) > 0 {
			fmt.Fprintf(w, "  %s only %s: %s\n", words[i], nameA, notesInBrief(l.OnlyA))
		}
		if len(l.OnlyB) > 0 {
			fmt.Fprintf(w, "  %s only %s: %s\n", words[i], nameB, notesInBrief(l.OnlyB))
		}
		if len(l.Cut) > 0 {
			fmt.Fprintf(w, "  %s %s cut in two or more: %s\n", words[i], nameB, notesInBrief(l.Cut))
		}
		if len(l.Spanning) > 0 {
			fmt.Fprintf(w, "  %s %s spanning two or more: %s\n", words[i], nameA, notesInBrief(l.Spanning))
		}
	}
}

// notesInBrief names notes by pitch and second, a rhythm's by second alone
func notesInBrief(notes []audio.RipNote) string {
	var b strings.Builder
	for i, n := range notes {
		if i > 0 {
			b.WriteString(" ")
		}
		if n.MIDI < 0 {
			fmt.Fprintf(&b, "%.2f", n.Start)
		} else {
			fmt.Fprintf(&b, "%s@%.2f", noteName(n.MIDI), n.Start)
		}
	}
	return b.String()
}
