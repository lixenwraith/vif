//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"cmp"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/lixenwraith/vif/pkg/audio"
)

func cmdSet(s *Session, a []string) error {
	segs := strings.Split(a[0], ".")
	name, rest := segs[0], segs[1:]
	if len(rest) == 0 {
		return fmt.Errorf("set: path must address a field, not a document entry")
	}
	// The document key and the Name field must agree — registration is by
	// Name. mv is the only path that changes both.
	if len(rest) == 1 && rest[0] == "name" {
		return fmt.Errorf("set: use mv to rename")
	}
	root, mark, err := s.docRoot(name)
	if err != nil {
		return err
	}
	v, err := resolve(reflect.ValueOf(root), rest, true)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if err := setLeaf(v, strings.Join(a[1:], " ")); err != nil {
		return fmt.Errorf("%s: %w", a[0], err)
	}
	mark()
	fmt.Fprintf(s.out, "%s = %s\n", a[0], formatLeaf(v))
	return nil
}

func cmdAdd(s *Session, a []string) error {
	segs := strings.Split(a[0], ".")
	if len(segs) < 2 {
		return fmt.Errorf("add: path must address a list field")
	}
	root, mark, err := s.docRoot(segs[0])
	if err != nil {
		return err
	}
	idx, err := addAt(root, segs[1:])
	if err != nil {
		return fmt.Errorf("%s: %w", segs[0], err)
	}
	mark()
	fmt.Fprintf(s.out, "added %s.%d\n", a[0], idx)
	return nil
}

func cmdDel(s *Session, a []string) error {
	segs := strings.Split(a[0], ".")
	if len(segs) == 1 {
		n := segs[0]
		switch {
		case s.sounds.del(n), s.voices.del(n), s.pats.del(n), s.songs.del(n):
			// Registry deletion does not exist: IDs are table indices sized
			// at Start. The registered entry, if any, keeps playing until a
			// registry rebuild.
			fmt.Fprintf(s.out, "removed %q from document (registry entry, if any, unaffected)\n", n)
			return nil
		}
		return fmt.Errorf("%q: not in any document", n)
	}
	root, mark, err := s.docRoot(segs[0])
	if err != nil {
		return err
	}
	if err := delAt(root, segs[1:]); err != nil {
		return fmt.Errorf("%s: %w", segs[0], err)
	}
	mark()
	fmt.Fprintf(s.out, "deleted %s\n", a[0])
	return nil
}

func cmdShow(s *Session, a []string) error {
	segs := strings.Split(a[0], ".")
	name := segs[0]
	if len(segs) == 1 {
		// The marshaller is the pretty-printer: output is paste-able back
		// into a file, and any divergence is a package bug surfaced free.
		if d, ok := s.sounds.get(name); ok {
			data, err := audio.MarshalSounds([]*audio.SoundDef{d})
			if err != nil {
				return err
			}
			s.out.Write(data)
			return nil
		}
		if d, ok := s.voices.get(name); ok {
			data, err := audio.MarshalVoices([]*audio.VoiceDef{d})
			if err != nil {
				return err
			}
			s.out.Write(data)
			return nil
		}
		if d, ok := s.pats.get(name); ok {
			data, err := audio.MarshalPatternDefs([]*audio.PatternDef{d})
			if err != nil {
				return err
			}
			s.out.Write(data)
			return nil
		}
		if d, ok := s.songs.get(name); ok {
			data, err := audio.MarshalSongs([]*audio.SongDef{d})
			if err != nil {
				return err
			}
			s.out.Write(data)
			return nil
		}
		return fmt.Errorf("%q: not in any document", name)
	}
	root, _, err := s.docRoot(name)
	if err != nil {
		return err
	}
	v, err := resolve(reflect.ValueOf(root), segs[1:], false)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	fmt.Fprintf(s.out, "%s = %s\n", a[0], formatLeaf(v))
	return nil
}

func cmdLs(s *Session, a []string) error {
	glob := "*"
	if len(a) > 1 {
		glob = a[1]
	}
	if a[0] == "song" {
		names, err := s.songs.names(glob)
		if err != nil {
			return err
		}
		for _, n := range names {
			d, _ := s.songs.get(n)
			fmt.Fprintf(s.out, "%s %-24s %3d bar(s) %3d bpm %s %s\n",
				dirtyMark(s.songs.isDirty(n)), n, len(d.Bars), d.BPM, noteName(d.Root), d.Scale)
		}
		return nil
	}
	k, err := kindArg(a[0])
	if err != nil {
		return err
	}
	switch k {
	case kindSound:
		names, err := s.sounds.names(glob)
		if err != nil {
			return err
		}
		for _, n := range names {
			d, _ := s.sounds.get(n)
			fmt.Fprintf(s.out, "%s %-24s %2d layer(s) %5.2fs\n",
				dirtyMark(s.sounds.isDirty(n)), n, len(d.Layer), soundDur(d))
		}
		return nil
	case kindVoice:
		names, err := s.voices.names(glob)
		if err != nil {
			return err
		}
		for _, n := range names {
			d, _ := s.voices.get(n)
			fmt.Fprintf(s.out, "%s %-24s %-6s level %.2f\n",
				dirtyMark(s.voices.isDirty(n)), n, cmp.Or(d.Wave, "sine"), cmp.Or(d.Level, 1))
		}
		return nil
	}
	names, err := s.pats.names(glob)
	if err != nil {
		return err
	}
	for _, n := range names {
		d, _ := s.pats.get(n)
		fmt.Fprintf(s.out, "%s %-24s %2d step(s) %2d track(s)\n",
			dirtyMark(s.pats.isDirty(n)), n, d.Steps, len(d.Track))
	}
	return nil
}

func dirtyMark(d bool) string {
	if d {
		return "*"
	}
	return " "
}

func cmdNew(s *Session, a []string) error {
	k, err := kindArg(a[0])
	if err != nil {
		return err
	}
	name := a[1]
	if err := badName(name); err != nil {
		return err
	}
	if s.has(name) {
		return fmt.Errorf("%q: already exists", name)
	}

	switch k {
	case kindSound:
		// Minimal valid spec: a new entry must be playable immediately, not
		// a validation error. 0.2s sine with a click-free release.
		s.sounds.put(&audio.SoundDef{
			Name: name, Duration: 0.2,
			Layer: []audio.Layer{{
				Source: audio.Source{Kind: "osc", Wave: "sine", Freq: 440},
				Chain:  []audio.Proc{{Kind: "ar", Attack: 0.005, Release: 0.05}},
			}},
		}, true)
	case kindVoice:
		// A plain sine with a click-free envelope: an instrument from its first note
		s.voices.put(&audio.VoiceDef{Name: name, Wave: "sine", Attack: 0.01, Decay: 0.1, Sustain: 0.6, Release: 0.2}, true)
	default:
		s.pats.put(&audio.PatternDef{
			Name: name, Steps: 16,
			Track: []audio.TrackDef{{
				Instr: "kick",
				Event: []audio.StepDef{{Pos: 0, Vel: 0.9}},
			}},
		}, true)
	}
	fmt.Fprintf(s.out, "created %s %q (minimal valid seed; edit away)\n", a[0], name)
	return nil
}

func cmdCp(s *Session, a []string) error {
	src, dst := a[0], a[1]
	if err := badName(src); err != nil {
		return err
	}
	if err := badName(dst); err != nil {
		return err
	}
	if s.has(dst) {
		return fmt.Errorf("%q: already exists", dst)
	}
	if d, ok := s.sounds.get(src); ok {
		c := d.Clone()
		c.Name = dst
		s.sounds.put(c, true)
		fmt.Fprintf(s.out, "copied sound %q -> %q\n", src, dst)
		return nil
	}
	if d, ok := s.voices.get(src); ok {
		c := d.Clone()
		c.Name = dst
		s.voices.put(c, true)
		fmt.Fprintf(s.out, "copied voice %q -> %q\n", src, dst)
		return nil
	}
	if d, ok := s.pats.get(src); ok {
		c := clonePatternDef(d)
		c.Name = dst
		s.pats.put(c, true)
		fmt.Fprintf(s.out, "copied pattern %q -> %q\n", src, dst)
		return nil
	}
	return fmt.Errorf("%q: not in any document", src)
}

func cmdMv(s *Session, a []string) error {
	src, dst := a[0], a[1]
	if err := badName(src); err != nil {
		return err
	}
	if err := badName(dst); err != nil {
		return err
	}
	// doc.rename checks only its own kind; docRoot shadows patterns behind
	// sounds on collision — mv must not create the state new/cp refuse.
	if s.has(dst) {
		return fmt.Errorf("%q: already exists", dst)
	}
	if s.sounds.has(src) {
		if err := s.sounds.rename(src, dst); err != nil {
			return err
		}
		fmt.Fprintf(s.out, "renamed sound %q -> %q; noise variants will re-roll (rng seeds from name)\n", src, dst)
		return nil
	}
	if s.voices.has(src) {
		if err := s.voices.rename(src, dst); err != nil {
			return err
		}
		fmt.Fprintf(s.out, "renamed voice %q -> %q; applied, the new name is a new instrument\n", src, dst)
		return nil
	}
	if s.pats.has(src) {
		if err := s.pats.rename(src, dst); err != nil {
			return err
		}
		fmt.Fprintf(s.out, "renamed pattern %q -> %q\n", src, dst)
		return nil
	}
	return fmt.Errorf("%q: not in any document", src)
}

// cmdMix appends src's structure into dst. Deep copies via Clone /
// clonePatternDef: owned memory moves into dst, src keeps its own. Domain
// bounds (events past dst's grid, track/layer caps, bus refs) belong to
// validate, per house style — except sound bus name collisions, which would
// silently reroute src layers into dst's bus instead of failing validation.
func cmdMix(s *Session, a []string) error {
	src, dst := a[0], a[1]
	if src == dst {
		return fmt.Errorf("mix: src and dst are the same entry")
	}
	if sd, ok := s.sounds.get(src); ok {
		dd, ok := s.sounds.get(dst)
		if !ok {
			return fmt.Errorf("%q: not a sound in the document (mix is same-kind)", dst)
		}
		c := sd.Clone()
		for i := range c.Bus {
			for j := range dd.Bus {
				if c.Bus[i].Name == dd.Bus[j].Name {
					return fmt.Errorf("bus %q exists in both; rename one first", c.Bus[i].Name)
				}
			}
		}
		dd.Bus = append(dd.Bus, c.Bus...)
		dd.Layer = append(dd.Layer, c.Layer...)
		s.sounds.markDirty(dst)
		fmt.Fprintf(s.out, "mixed sound %q into %q: +%d layer(s) +%d bus(es); dst master chain/duration win — validate %s\n",
			src, dst, len(c.Layer), len(c.Bus), dst)
		return nil
	}
	if pd, ok := s.pats.get(src); ok {
		dd, ok := s.pats.get(dst)
		if !ok {
			return fmt.Errorf("%q: not a pattern in the document (mix is same-kind)", dst)
		}
		c := clonePatternDef(pd)
		dd.Track = append(dd.Track, c.Track...)
		s.pats.markDirty(dst)
		fmt.Fprintf(s.out, "mixed pattern %q into %q: +%d track(s); dst steps=%d — validate flags events past the grid\n",
			src, dst, len(c.Track), dd.Steps)
		return nil
	}
	if s.voices.has(src) {
		return fmt.Errorf("%q: a voice has no parts to mix; cp copies it", src)
	}
	return fmt.Errorf("%q: not in any document", src)
}

// has reports a name in any document; new, cp and mv refuse one, since the
// command grammar addresses an entry by name alone
func (s *Session) has(name string) bool {
	return s.sounds.has(name) || s.voices.has(name) || s.pats.has(name) || s.songs.has(name)
}

// cmdTake adds a track of one tonal voice playing the notes of a take, each at its
// step on the degree of the mirrored key nearest its pitch with an accidental for
// the rest, the later of two on one step and pitch kept: what the keys write when
// a recording ends
func cmdTake(s *Session, a []string) error {
	d, ok := s.pats.get(a[0])
	if !ok {
		return fmt.Errorf("%q: no such pattern in the document", a[0])
	}
	instr, ok := audio.InstrumentByName(a[1])
	if !ok || instr.IsDrum() {
		return fmt.Errorf("%q: not a tonal voice", a[1])
	}
	type at struct{ pos, midi int }
	notes, accidentals := map[at]audio.StepDef{}, 0
	for _, n := range a[2:] {
		f := strings.Split(n, ":")
		e := audio.StepDef{Dur: 1, Vel: 0.8}
		var midi int
		_, err := fmt.Sscan(f[0], &e.Pos)
		if err == nil && len(f) > 1 {
			_, err = fmt.Sscan(f[1], &midi)
		}
		if err == nil && len(f) > 2 {
			_, err = fmt.Sscan(f[2], &e.Dur)
		}
		if err == nil && len(f) > 3 {
			_, err = fmt.Sscan(f[3], &e.Vel)
		}
		if err != nil || len(f) < 2 || len(f) > 4 || e.Pos < 0 || e.Pos >= d.Steps {
			return fmt.Errorf("note %q: want <pos>:<midi>[:<steps>[:<vel>]], pos under %d", n, d.Steps)
		}
		if e.Deg, e.Oct, e.Acc = audio.PitchStep(s.root, s.scale, midi); e.Acc != 0 {
			accidentals++
		}
		notes[at{e.Pos, midi}] = e
	}
	tr := audio.TrackDef{Instr: audio.InstrumentName(instr), Humanize: 0.15}
	for _, e := range notes {
		tr.Event = append(tr.Event, e)
	}
	slices.SortFunc(tr.Event, func(x, y audio.StepDef) int { return cmpInts(x.Pos, y.Pos, x.Oct, y.Oct, x.Deg, y.Deg) })
	d.Track = append(d.Track, tr)
	s.pats.markDirty(a[0])
	fmt.Fprintf(s.out, "take: %d note(s) into %s, track %d (%s)\n", len(tr.Event), a[0], len(d.Track)-1, tr.Instr)
	if accidentals > 0 {
		fmt.Fprintf(s.out, "%d note(s) outside %s carry an accidental\n", accidentals, s.keyName())
	}
	return nil
}

func cmdUnset(s *Session, a []string) error {
	segs := strings.Split(a[0], ".")
	if len(segs) < 2 {
		return fmt.Errorf("unset: path must address a field, not a document entry (del removes entries)")
	}
	if len(segs) == 2 && segs[1] == "name" {
		return fmt.Errorf("unset: name is the registration identity; use mv")
	}
	root, mark, err := s.docRoot(segs[0])
	if err != nil {
		return err
	}
	// create=false: a chain that is already unset errors here with "unset",
	// which is the correct report — there is nothing to clear.
	v, err := resolve(reflect.ValueOf(root), segs[1:], false)
	if err != nil {
		return fmt.Errorf("%s: %w", segs[0], err)
	}
	if !v.CanSet() {
		return fmt.Errorf("%s: not settable", a[0])
	}
	v.Set(reflect.Zero(v.Type()))
	mark()
	fmt.Fprintf(s.out, "unset %s\n", a[0])
	return nil
}

func cmdValidate(s *Session, a []string) error {
	if len(a) == 0 || a[0] == "all" {
		return s.validateAll()
	}
	if err := s.validateName(a[0]); err != nil {
		return err
	}
	fmt.Fprintf(s.out, "%s: ok\n", a[0])
	return nil
}

func cmdApply(s *Session, a []string) error {
	if len(a) == 0 || a[0] == "all" {
		return s.applyAll()
	}
	return s.applyName(a[0])
}

func cmdRevert(s *Session, a []string) error {
	if len(a) == 0 || a[0] == "all" {
		return s.revertAll()
	}
	return s.revertName(a[0])
}

// badName rejects names the command grammar cannot address: '.' collides with
// path segments, whitespace with tokenization, '#' with comments. Validation
// allows all of them, so the guard lives at creation.
func badName(n string) error {
	if strings.ContainsAny(n, ". \t#") {
		return fmt.Errorf("%q: dots, spaces and '#' are reserved by the command grammar", n)
	}
	return nil
}
