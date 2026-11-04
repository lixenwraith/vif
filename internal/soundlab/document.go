//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

import (
	"cmp"
	"errors"
	"fmt"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/lixenwraith/audio"
)

// doc is an ordered, name-keyed working set in document order, which save emits
// and ls shows. dirty marks an entry diverging from the registry ('*'): any
// mutation sets it, apply or revert of the entry clears it. modified latches a
// document diverging from disk ('● unsaved'): any mutation sets it and only save
// and replaceAll clear it, revert included, so it may over-warn, never under-warn.
type doc[T any] struct {
	order   []string
	byName  map[string]T
	dirty   map[string]bool
	src     string   // provenance for bare `save`
	include []string // sound documents only; patterns have no include form

	word     string // the kind, as undo counts it
	nameOf   func(T) string
	setName  func(T, string)
	clone    func(T) T
	brief    func(T) string // what undo says an entry is back to
	modified bool           // modified tracks divergence from disk

	// shadow is each entry as the last edit left it: a command mutates in place
	// before it marks, so an edit's first touch of a name records the shadow's.
	// applied counts clearDirty, after which a recorded mark may be stale.
	shadow  map[string]T
	edit    *docEdit[T]
	applied uint64
}

func newDoc[T any](word string, nameOf func(T) string, setName func(T, string), clone func(T) T, brief func(T) string) *doc[T] {
	return &doc[T]{
		byName: map[string]T{}, dirty: map[string]bool{}, shadow: map[string]T{},
		word: word, nameOf: nameOf, setName: setName, clone: clone, brief: brief,
	}
}

func newSoundDoc() *doc[*audio.SoundDef] {
	return newDoc("sound", func(d *audio.SoundDef) string { return d.Name },
		func(d *audio.SoundDef, n string) { d.Name = n }, (*audio.SoundDef).Clone,
		func(d *audio.SoundDef) string { return plural(len(d.Layer), "layer") })
}

func newVoiceDoc() *doc[*audio.VoiceDef] {
	return newDoc("voice", func(d *audio.VoiceDef) string { return d.Name },
		func(d *audio.VoiceDef, n string) { d.Name = n }, (*audio.VoiceDef).Clone,
		func(d *audio.VoiceDef) string { return cmp.Or(d.Wave, "sine") + " wave" })
}

func newPatternDoc() *doc[*audio.PatternDef] {
	return newDoc("pattern", func(d *audio.PatternDef) string { return d.Name },
		func(d *audio.PatternDef, n string) { d.Name = n }, clonePatternDef,
		func(d *audio.PatternDef) string { return plural(len(d.Track), "track") })
}

func newSongDoc() *doc[*audio.SongDef] {
	return newDoc("song", func(d *audio.SongDef) string { return d.Name },
		func(d *audio.SongDef, n string) { d.Name = n }, (*audio.SongDef).Clone,
		func(d *audio.SongDef) string { return plural(len(d.Bars), "bar") })
}

func plural(n int, what string) string {
	if n == 1 {
		return "1 " + what
	}
	return fmt.Sprintf("%d %ss", n, what)
}

// clonePatternDef is the deep copy PatternDef lacks. StepDef is a value
// struct, so cloning the slices is sufficient.
func clonePatternDef(d *audio.PatternDef) *audio.PatternDef {
	c := *d
	c.Groups = slices.Clone(d.Groups)
	c.Tiers = slices.Clone(d.Tiers)
	c.Track = slices.Clone(d.Track)
	for i := range c.Track {
		c.Track[i].Event = slices.Clone(d.Track[i].Event)
	}
	return &c
}

func (d *doc[T]) has(n string) bool     { _, ok := d.byName[n]; return ok }
func (d *doc[T]) isDirty(n string) bool { return d.dirty[n] }

func (d *doc[T]) get(n string) (T, bool) { v, ok := d.byName[n]; return v, ok }

// put inserts or replaces, preserving position on replace.
func (d *doc[T]) put(v T, dirty bool) {
	n := d.nameOf(v)
	d.touch(n)
	if !d.has(n) {
		d.order = append(d.order, n)
	}
	d.byName[n] = v
	if dirty {
		d.dirty[n] = true
		d.modified = true
	} else {
		delete(d.dirty, n)
	}
}

func (d *doc[T]) del(n string) bool {
	if !d.has(n) {
		return false
	}
	d.touch(n)
	delete(d.byName, n)
	delete(d.dirty, n)
	if i := slices.Index(d.order, n); i >= 0 {
		d.order = slices.Delete(d.order, i, i+1)
	}
	d.modified = true
	return true
}

// rename keeps document position and marks dirty: a renamed entry always
// diverges from whatever the registry holds under either name.
func (d *doc[T]) rename(from, to string) error {
	v, ok := d.byName[from]
	if !ok {
		return fmt.Errorf("%q: not in document", from)
	}
	if d.has(to) {
		return fmt.Errorf("%q: already exists", to)
	}
	d.touch(from)
	d.touch(to)
	d.setName(v, to)
	d.byName[to] = v
	delete(d.byName, from)
	delete(d.dirty, from)
	if i := slices.Index(d.order, from); i >= 0 {
		d.order[i] = to
	}
	d.dirty[to] = true
	d.modified = true
	return nil
}

func (d *doc[T]) markDirty(n string) {
	if d.has(n) {
		d.touch(n)
		d.dirty[n] = true
		d.modified = true
	}
}

func (d *doc[T]) clearDirty(n string) {
	delete(d.dirty, n)
	d.applied++
}

func (d *doc[T]) dirtyNames() []string {
	out := make([]string, 0, len(d.dirty))
	for _, n := range d.order {
		if d.dirty[n] {
			out = append(out, n)
		}
	}
	return out
}

func (d *doc[T]) all() []T {
	out := make([]T, 0, len(d.order))
	for _, n := range d.order {
		out = append(out, d.byName[n])
	}
	return out
}

func (d *doc[T]) names(glob string) ([]string, error) {
	if glob == "" || glob == "*" {
		return slices.Clone(d.order), nil
	}
	var out []string
	for _, n := range d.order {
		ok, err := path.Match(glob, n)
		if err != nil {
			return nil, fmt.Errorf("bad glob %q: %w", glob, err)
		}
		if ok {
			out = append(out, n)
		}
	}
	return out, nil
}

func (d *doc[T]) replaceAll(vs []T, src string, dirty bool) {
	d.begin().srcSet = true
	for _, n := range d.order {
		d.touch(n)
	}
	d.order = d.order[:0]
	clear(d.byName)
	clear(d.dirty)
	d.src = src
	for _, v := range vs {
		d.put(v, dirty) // per-entry put latches modified when dirty…
	}
	d.modified = false // …but bulk replacement is a baseline, not an edit
}

func (d *doc[T]) mergeAll(vs []T, dirty bool) {
	for _, v := range vs {
		d.put(v, dirty)
	}
}

// undoDepth bounds the history. An edit holds the order it replaced and the old
// values of the entries it touched, moved out of the shadow: on a ripped song's
// 191 patterns a set holds 4.4 KB and a load replacing them all 0.19 MB, so the
// bound costs 0.4 MB of sets and 19 MB of loads at most.
const undoDepth = 100

// undoListed is how many entries an undo names one by one before it counts them
const undoListed = 4

// docEdit is a document before one edit: its order and provenance, and each entry
// the edit touched as it was, one the edit made recorded absent
type docEdit[T any] struct {
	d       *doc[T]
	order   []string
	src     string
	include []string
	srcSet  bool   // the edit replaced the document, provenance and all
	applied uint64 // d.applied when the edit ended
	names   []string
	old     map[string]prior[T]
}

type prior[T any] struct {
	v          T
	had, dirty bool
}

func (d *doc[T]) begin() *docEdit[T] {
	if d.edit == nil {
		d.edit = &docEdit[T]{d: d, order: slices.Clone(d.order), src: d.src,
			include: slices.Clone(d.include), old: map[string]prior[T]{}}
	}
	return d.edit
}

// touch records a name's entry as the last edit left it, once per edit
func (d *doc[T]) touch(n string) {
	e := d.begin()
	if _, ok := e.old[n]; ok {
		return
	}
	v, had := d.shadow[n]
	e.old[n] = prior[T]{v, had, d.dirty[n]}
	e.names = append(e.names, n)
}

// settle ends the edit in progress, nil when there was none, and brings the
// shadow of each entry it touched up to the document
func (d *doc[T]) settle() undoer {
	e := d.edit
	if e == nil {
		return nil
	}
	d.edit = nil
	for _, n := range e.names {
		if v, ok := d.byName[n]; ok {
			d.shadow[n] = d.clone(v)
		} else {
			delete(d.shadow, n)
		}
	}
	e.applied = d.applied
	return e
}

// restored is what one undo did to one document
type restored struct {
	word       string
	back, gone []string
}

// undo puts each touched entry back with its mark, which an apply since may have
// made stale: the registry then holds what the edit wrote, so the entry is dirty
func (e *docEdit[T]) undo() restored {
	d := e.d
	r := restored{word: d.word}
	for _, n := range e.names {
		p := e.old[n]
		if !p.had {
			if d.has(n) {
				r.gone = append(r.gone, n)
			}
			delete(d.byName, n)
			delete(d.dirty, n)
			delete(d.shadow, n)
			continue
		}
		d.byName[n], d.shadow[n] = p.v, d.clone(p.v)
		if p.dirty || d.applied != e.applied {
			d.dirty[n] = true
		} else {
			delete(d.dirty, n)
		}
		r.back = append(r.back, n+" back to "+d.brief(p.v))
	}
	d.order = e.order
	if e.srcSet {
		d.src, d.include = e.src, e.include
	}
	d.modified = true
	return r
}

type undoer interface{ undo() restored }

// history is the undo stack over the documents. An edit is one command line that
// changed a document, a nested script's lines folded into it, or every line one
// TUI key ran; press counts the keys.
type history struct {
	edits            []edit
	depth            int
	verbs            []string // the verbs of the edit in progress
	open             bool     // the documents' edits in progress are lines'
	press, openPress uint64
}

type edit struct {
	verbs []string
	docs  []undoer
}

func (s *Session) settleDocs() []undoer {
	var out []undoer
	for _, u := range []undoer{s.pats.settle(), s.songs.settle(), s.sounds.settle(), s.voices.settle()} {
		if u != nil {
			out = append(out, u)
		}
	}
	return out
}

// beginLine opens an edit for a command line Execute runs, unless it is nested or
// another line of the open edit's key. A change made outside any line is taken as
// it stands, and the history before it dropped, since undo cannot step over it.
func (s *Session) beginLine(verb string) {
	h := &s.hist
	if h.depth++; h.depth > 1 {
		return
	}
	if h.open && h.press != 0 && h.press == h.openPress {
		if !slices.Contains(h.verbs, verb) {
			h.verbs = append(h.verbs, verb)
		}
		return
	}
	s.closeEdit(h.open)
	h.verbs = []string{verb}
}

// endLine closes the outermost line's edit, or leaves it open for the rest of a
// TUI key's lines
func (s *Session) endLine() {
	h := &s.hist
	if h.depth--; h.depth > 0 {
		return
	}
	if h.press == 0 {
		s.closeEdit(true)
		return
	}
	if !h.open {
		h.open, h.openPress = true, h.press
	}
}

func (s *Session) closeEdit(keep bool) {
	h := &s.hist
	h.open = false
	docs := s.settleDocs()
	switch {
	case len(docs) == 0:
	case !keep:
		h.edits = nil
	default:
		if len(h.edits) == undoDepth {
			h.edits = slices.Delete(h.edits, 0, 1)
		}
		h.edits = append(h.edits, edit{slices.Clone(h.verbs), docs})
	}
}

// undo restores the documents as they were before the edit and says what it put
// back: each entry, or a count per kind when more than undoListed
func (e edit) undo() string {
	var rs []restored
	total := 0
	for _, u := range e.docs {
		r := u.undo()
		rs = append(rs, r)
		total += len(r.back) + len(r.gone)
	}
	var parts []string
	for _, r := range rs {
		if total <= undoListed {
			parts = append(parts, r.back...)
			for _, n := range r.gone {
				parts = append(parts, n+" removed")
			}
			continue
		}
		if len(r.back) > 0 {
			parts = append(parts, plural(len(r.back), r.word)+" back")
		}
		if len(r.gone) > 0 {
			parts = append(parts, plural(len(r.gone), r.word)+" removed")
		}
	}
	if len(parts) == 0 {
		parts = []string{"nothing left to restore"}
	}
	return "undid " + strings.Join(e.verbs, ", ") + ": " + strings.Join(parts, ", ")
}

func cmdUndo(s *Session, a []string) error {
	n := 1
	if len(a) == 1 {
		var err error
		if n, err = strconv.Atoi(a[0]); err != nil || n < 1 {
			return fmt.Errorf("undo: want a count of 1 or more, got %q", a[0])
		}
	}
	s.closeEdit(true) // a nested script's lines so far are an edit of their own
	h := &s.hist
	if len(h.edits) == 0 {
		return errors.New("nothing to undo")
	}
	k := min(n, len(h.edits))
	for range k {
		e := h.edits[len(h.edits)-1]
		h.edits = h.edits[:len(h.edits)-1]
		fmt.Fprintln(s.out, e.undo())
	}
	if k < n {
		fmt.Fprintf(s.out, "the history held %s\n", plural(k, "edit"))
	}
	return nil
}
