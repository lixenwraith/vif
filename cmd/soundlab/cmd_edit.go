package main

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/lixenwraith/vi-fighter/audio"
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
		case s.sounds.del(n), s.pats.del(n):
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
		if d, ok := s.pats.get(name); ok {
			data, err := audio.MarshalPatternDefs([]*audio.PatternDef{d})
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
	snd, err := kindArg(a[0])
	if err != nil {
		return err
	}
	glob := "*"
	if len(a) > 1 {
		glob = a[1]
	}
	if snd {
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
	snd, err := kindArg(a[0])
	if err != nil {
		return err
	}
	name := a[1]
	if err := badName(name); err != nil {
		return err
	}
	if s.sounds.has(name) || s.pats.has(name) {
		return fmt.Errorf("%q: already exists", name)
	}

	if snd {
		// Minimal valid spec: a new entry must be playable immediately, not
		// a validation error. 0.2s sine with a click-free release.
		s.sounds.put(&audio.SoundDef{
			Name: name, Duration: 0.2,
			Layer: []audio.Layer{{
				Source: audio.Source{Kind: "osc", Wave: "sine", Freq: 440},
				Chain:  []audio.Proc{{Kind: "ar", Attack: 0.005, Release: 0.05}},
			}},
		}, true)
	} else {
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
	if s.sounds.has(dst) || s.pats.has(dst) {
		return fmt.Errorf("%q: already exists", dst)
	}
	if d, ok := s.sounds.get(src); ok {
		c := d.Clone()
		c.Name = dst
		s.sounds.put(c, true)
		fmt.Fprintf(s.out, "copied sound %q -> %q\n", src, dst)
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
	if s.sounds.has(dst) || s.pats.has(dst) {
		return fmt.Errorf("%q: already exists", dst)
	}
	if s.sounds.has(src) {
		if err := s.sounds.rename(src, dst); err != nil {
			return err
		}
		fmt.Fprintf(s.out, "renamed sound %q -> %q; noise variants will re-roll (rng seeds from name)\n", src, dst)
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
	return fmt.Errorf("%q: not in any document", src)
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
