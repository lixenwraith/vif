//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

// The song: a rip's bars matched to a dictionary of one-bar patterns and the
// timeline that plays them (compress), the timeline shown, rendered or conducted
// through the live sequencer (song), and the score of what the compression kept.

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/lixenwraith/vif/pkg/audio"
	"github.com/lixenwraith/vif/pkg/genetic"
)

// compressOptions sizes the search over the assignment of bars to patterns
type compressOptions struct {
	generations, pool int
	seed              uint64
}

var defaultCompress = compressOptions{generations: 40, pool: 32, seed: 1}

func cmdCompress(s *Session, a []string) error {
	rp, ok := s.rips[a[0]]
	if !ok {
		return fmt.Errorf("%q: nothing ripped under that name this session (rip first)", a[0])
	}
	pct := 5.0
	if len(a) > 1 {
		f, err := strconv.ParseFloat(a[1], 64)
		if err != nil || f <= 0 || f > 100 {
			return fmt.Errorf("percent %q: want more than 0, up to 100", a[1])
		}
		pct = f
	}
	return s.compress(a[0], rp.rip, pct/100, s.compressOpt)
}

// compress matches a rip's bars to as few patterns as the threshold allows, part
// by part: the greedy grouping first, then a search over the assignment with a
// term for the patterns in use, and writes the dictionary and the song into the
// document in place of what an earlier compress of the name made. The song is
// rendered and ripped back, and that rip compared with the first, so what the
// compression lost is heard as a score.
func (s *Session) compress(name string, r *audio.Rip, threshold float64, o compressOptions) error {
	var assign [audio.RipParts][]int
	parts := 0
	for part := range audio.RipParts {
		greedy := r.Cluster(part, threshold)
		bars := 0
		for _, rep := range greedy {
			if rep >= 0 {
				bars++
			}
		}
		if bars == 0 {
			fmt.Fprintf(s.out, "%s: silent\n", audio.RipPartNames[part])
			continue
		}
		parts++
		refined := refineAssignment(r, part, greedy, threshold, o)
		assign[part] = refined
		before, after := distinct(greedy), distinct(refined)
		line := fmt.Sprintf("%s: %d bar(s) in %d pattern(s)", audio.RipPartNames[part], bars, after)
		if after != before {
			line += fmt.Sprintf(" (the greedy grouping had %d)", before)
		}
		fmt.Fprintln(s.out, line)
	}
	pats, song := r.Song(name, assign)
	if len(pats) == 0 {
		return fmt.Errorf("%s: nothing heard to compress", name)
	}
	for _, p := range pats {
		if s.has(p.Name) && !s.pats.has(p.Name) {
			return fmt.Errorf("%q: already exists and is not a pattern", p.Name)
		}
	}
	// an earlier dictionary of the name goes, so another threshold leaves no strays
	fresh := map[string]bool{}
	for _, p := range pats {
		fresh[p.Name] = true
	}
	for _, n := range slices.Clone(s.pats.order) {
		if dictionaryName(name, n) && !fresh[n] {
			s.pats.del(n)
		}
	}
	for _, p := range pats {
		s.pats.put(p, true)
	}
	s.songs.put(song, true)
	fmt.Fprintf(s.out, "%s: %d bar(s) of %d part(s) in %d pattern(s); song %s plays them at %d bpm, %s %s\n",
		name, len(r.Bars), parts, len(pats), song.Name, song.BPM, noteName(song.Root), song.Scale)
	score, err := s.songScore(song, r)
	if err != nil {
		return err
	}
	fmt.Fprintf(s.out, "the song rips back at %.2f of the recording's rip on its own grid: drums %.2f melody %.2f bass %.2f\n",
		score.Total, score.Drums, score.Melody, score.Bass)
	return nil
}

// dictionaryName reports a pattern name a compress of name makes: the name, a
// part and letters
func dictionaryName(name, n string) bool {
	rest, ok := strings.CutPrefix(n, name+"_")
	if !ok {
		return false
	}
	for _, part := range audio.RipPartNames {
		if tail, ok := strings.CutPrefix(rest, part+"_"); ok && tail != "" && strings.Trim(tail, "abcdefghijklmnopqrstuvwxyz") == "" {
			return true
		}
	}
	return false
}

// distinct counts the bars an assignment uses as representatives
func distinct(assign []int) int {
	seen := map[int]bool{}
	for _, a := range assign {
		if a >= 0 {
			seen[a] = true
		}
	}
	return len(seen)
}

// refineAssignment searches the assignment of a part's bars to the bars that
// stand for them, from the greedy grouping: each entry of the genome names a
// bar's representative among the bars with the part, scored by the
// representatives in use, heavily by any bar farther from its own than the
// threshold and lightly by the mean distance. An assignment better than the
// greedy one is taken, each group's medoid standing for it.
func refineAssignment(r *audio.Rip, part audio.RipPart, greedy []int, threshold float64, o compressOptions) []int {
	var bars []int
	for b, rep := range greedy {
		if rep >= 0 {
			bars = append(bars, b)
		}
	}
	n := len(bars)
	if n < 3 || distinct(greedy) < 2 {
		return greedy
	}
	at := make(map[int]int, n)
	for i, b := range bars {
		at[b] = i
	}
	d := make([][]float64, n)
	for i := range d {
		d[i] = make([]float64, n)
		for j := range i {
			d[i][j] = audio.BarDistance(r.PartBar(part, bars[i]), r.PartBar(part, bars[j]))
			d[j][i] = d[i][j]
		}
	}
	seed := make([]int, n)
	for i, b := range bars {
		seed[i] = at[greedy[b]]
	}
	eval := genetic.Memoize(func(g []int) string { return fmt.Sprint(g) }, func(g []int) float64 {
		used, k := make([]bool, n), 0
		var over, fit float64
		for i, rep := range g {
			if rep < 0 || rep >= n {
				return math.Inf(-1)
			}
			if !used[rep] {
				used[rep], k = true, k+1
			}
			fit += d[i][rep]
			if d[i][rep] > threshold {
				over += d[i][rep] - threshold
			}
		}
		return -float64(k)/float64(n) - 10*over - 0.1*fit/float64(n)
	})
	cfg := genetic.DefaultConfig()
	cfg.PoolSize, cfg.EliteCount, cfg.MaxIterations = max(o.pool, 4), max(o.pool/8, 1), o.generations
	cfg.Parallelism, cfg.Seed = 1, o.seed
	cfg.PerturbationRate, cfg.PerturbationStrength = 1/float64(n), 0
	e := genetic.NewEngine[[]int, float64](eval, func(rng *rand.Rand) []int {
		g := slices.Clone(seed) // the seed a few moves away
		for range 1 + rng.IntN(3) {
			g[rng.IntN(n)] = rng.IntN(n)
		}
		return g
	}, &genetic.TournamentSelector[[]int, float64]{TournamentSize: 3},
		&genetic.UniformCombiner[[]int, int, float64]{MixProbability: 0.9},
		&genetic.CategoricalPerturbator{Categories: n}, cfg)
	e.SetSeeds([][]int{slices.Clone(seed)})
	pool, err := e.Run(context.Background())
	if err != nil || len(pool.Members) == 0 {
		return greedy
	}
	best := pool.Members[0].Data
	if eval(best) <= eval(seed) {
		return greedy
	}
	groups := map[int][]int{}
	for i, rep := range best {
		groups[rep] = append(groups[rep], i)
	}
	out := slices.Clone(greedy)
	for _, members := range groups {
		med, medSum := members[0], math.Inf(1)
		for _, i := range members {
			var sum float64
			for _, j := range members {
				sum += d[i][j]
			}
			if sum < medSum {
				med, medSum = i, sum
			}
		}
		for _, i := range members {
			out[bars[i]] = bars[med]
		}
	}
	return out
}

// songScore renders a song through the engine and rips the render on the
// song's own grid, its tempo and its downbeat at the start pinned, so the score
// is of the hits and the notes and not of the tempo and bar-line decoders, and
// compares that rip with want: how much of a rip survives as the song
func (s *Session) songScore(d *audio.SongDef, want *audio.Rip) (audio.RipScore, error) {
	buf, err := s.renderSong(d)
	if err != nil {
		return audio.RipScore{}, err
	}
	rp := s.ripper()
	rp.Grid = &audio.RipGrid{BPM: d.BPM}
	got, err := rp.Samples(buf, audio.AudioSampleRate)
	if err != nil {
		return audio.RipScore{}, err
	}
	// a rip without a kit (nodrums) is compared without one, or the render's
	// plucks read as kicks and grade the song on drums it has none of
	if !hasDrums(want) {
		for b := range got.Bars {
			got.Bars[b].Drums = [audio.InstrClap][]audio.Step{}
		}
	}
	return audio.CompareRips(got, want), nil
}

func hasDrums(r *audio.Rip) bool {
	for _, b := range r.Bars {
		for d := range b.Drums {
			if len(b.Drums[d]) > 0 {
				return true
			}
		}
	}
	return false
}

// reportSong prints how a song sounds against the recording it was ripped from
// and how it rips back, drums, melody and bass apart; the fidelity's total is
// what the searches maximize
func (s *Session) reportSong(name string, d *audio.SongDef, rp *ripped, when string) (float64, error) {
	fid, err := s.songJudge(d, rp)
	if err != nil {
		return 0, err
	}
	score, err := s.songScore(d, rp.rip)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(s.out, "%s sounds %.2f like the recording (pitch %.2f timbre %.2f onset %.2f) and rips back at %.2f (drums %.2f melody %.2f bass %.2f)%s\n",
		name, fid.Total, fid.Pitch, fid.Timbre, fid.Onset, score.Total, score.Drums, score.Melody, score.Bass, when)
	return fid.Total, nil
}

// songJudge is how much a song sounds like the recording it was ripped from: its
// render heard through the front end beside the recording, bar by bar, with no
// rip between them to mishear both the same way.
func (s *Session) songJudge(d *audio.SongDef, rp *ripped) (audio.RipFidelity, error) {
	want, err := s.soundOf(rp)
	if err != nil {
		return audio.RipFidelity{}, err
	}
	buf, err := s.renderSong(d)
	if err != nil {
		return audio.RipFidelity{}, err
	}
	r := s.ripper()
	r.Grid = &audio.RipGrid{BPM: d.BPM}
	got, err := r.Sound(buf, audio.AudioSampleRate, nil)
	if err != nil {
		return audio.RipFidelity{}, err
	}
	return audio.Fidelity(got, want), nil
}

// songJudgeBars is the fidelity of the song's bars given, each rendered on its
// own against the recording's same bar and averaged: what a move in a one-bar
// pattern can change, at a bar's cost rather than the song's. A bar's render
// starts its patterns afresh, so a pattern longer than a bar is judged whole.
func (s *Session) songJudgeBars(d *audio.SongDef, rp *ripped, bars []int) (float64, error) {
	want, err := s.soundOf(rp)
	if err != nil {
		return 0, err
	}
	tl, err := s.songTimeline(d)
	if err != nil {
		return 0, err
	}
	r := s.ripper()
	r.Grid = &audio.RipGrid{BPM: d.BPM}
	var sum float64
	n := 0
	for _, b := range bars {
		lo := audio.BeatsPerBar * b
		if b < 0 || b >= len(tl) || lo+audio.BeatsPerBar+1 > len(want.Beats) {
			continue
		}
		buf, err := s.renderBars(d, tl[b:b+1])
		if err != nil {
			return 0, err
		}
		got, err := r.Sound(buf, audio.AudioSampleRate, nil)
		if err != nil {
			return 0, err
		}
		bar := *want
		bar.Beats = want.Beats[lo : lo+audio.BeatsPerBar+1]
		sum += audio.Fidelity(got, &bar).Total
		n++
	}
	if n == 0 {
		fid, err := s.songJudge(d, rp)
		return fid.Total, err
	}
	return sum / float64(n), nil
}

// soundOf is the recording as Fidelity hears it, heard once per rip
func (s *Session) soundOf(rp *ripped) (*audio.RipSound, error) {
	if rp.sound == nil {
		snd, err := s.ripper().Sound(rp.x, rp.rate, rp.rip.Beats)
		if err != nil {
			return nil, err
		}
		rp.sound = snd
	}
	return rp.sound, nil
}

// renderSong plays a song offline, the dirty patterns it names applied first
func (s *Session) renderSong(d *audio.SongDef) ([]float64, error) {
	tl, err := s.songTimeline(d)
	if err != nil {
		return nil, err
	}
	return s.renderBars(d, tl)
}

// renderBars renders bars of a song's timeline at the song's tuning, the
// transport's put back after
func (s *Session) renderBars(d *audio.SongDef, tl [][audio.MusicSlots]audio.PatternID) ([]float64, error) {
	s.eng.SetTuning(d.Tuning)
	defer s.eng.SetTuning(s.tuning)
	return s.eng.RenderTimeline(tl, d.BPM, d.Root, d.ScaleID(), d.Chords, 1)
}

// songTimeline resolves a song's bars, applying the dirty patterns it names so
// they are registered as the document has them, quietly
func (s *Session) songTimeline(d *audio.SongDef) ([][audio.MusicSlots]audio.PatternID, error) {
	if err := audio.ValidateSong(d); err != nil {
		return nil, err
	}
	for _, bar := range d.Bars {
		for _, n := range bar {
			if n == "" || !s.pats.has(n) || !s.pats.isDirty(n) {
				continue
			}
			pd, _ := s.pats.get(n)
			p, err := pd.Pattern()
			if err == nil {
				_, err = s.eng.DefinePattern(p)
			}
			if err != nil {
				return nil, fmt.Errorf("auto-apply %q: %w", n, err)
			}
			s.pats.clearDirty(n)
		}
	}
	return d.Timeline(audio.PatternIDByName)
}

func cmdSong(s *Session, a []string) error {
	d, ok := s.songs.get(a[0])
	if !ok {
		return fmt.Errorf("%q: no such song (compress makes one; load pattern brings a file's in)", a[0])
	}
	if len(a) == 1 {
		fmt.Fprintf(s.out, "%s: %d bar(s) at %d bpm, %s %s\n", d.Name, len(d.Bars), d.BPM, noteName(d.Root), d.Scale)
		for i, bar := range d.Bars {
			names := make([]string, audio.MusicSlots)
			for slot := range names {
				names[slot] = "-"
				if slot < len(bar) && bar[slot] != "" {
					names[slot] = bar[slot]
				}
			}
			fmt.Fprintf(s.out, "%3d: %s\n", i+1, strings.Join(names, "  "))
		}
		return nil
	}
	if a[1] == "play" {
		return s.playSong(d)
	}
	if a[1] == "score" {
		rp, ok := s.rips[a[0]]
		if !ok {
			return fmt.Errorf("%q: nothing ripped under that name this session (rip first; the song's name is its rip's)", a[0])
		}
		_, err := s.reportSong(a[0], d, rp, "")
		return err
	}
	buf, err := s.renderSong(d)
	if err != nil {
		return err
	}
	f, err := os.Create(s.path(a[1]))
	if err != nil {
		return err
	}
	werr := audio.WriteWAV(f, buf)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	fmt.Fprintf(s.out, "wrote %s: %s, %d bar(s) at %d bpm (%.1fs)\n", a[1], d.Name, len(d.Bars), d.BPM, float64(len(buf))/audio.AudioSampleRate)
	return nil
}

// songPlay is a song being conducted through the live sequencer
type songPlay struct {
	name  string
	bars  [][audio.MusicSlots]audio.PatternID
	next  int  // the first bar not yet scheduled
	begun bool // the transport has been seen at the song's first bar
	until time.Time
}

// playSong conducts a song through the live sequencer: the tempo and key set,
// the transport reset, the first bar's patterns placed and each next bar's
// scheduled to land on its bar line while the one before plays; the last bar
// stops the transport. A front end that ticks drives it through conduct and
// play returns at once; without one, play returns when the song ends.
func (s *Session) playSong(d *audio.SongDef) error {
	switch {
	case s.startErr != nil:
		return errors.New("no audio device: song <name> <file.wav> renders it instead")
	case s.eng.IsMusicMuted():
		return errors.New("music is muted: mute none first")
	}
	tl, err := s.songTimeline(d)
	if err != nil {
		return err
	}
	s.bpm = d.BPM
	s.eng.SetMusicBPM(d.BPM)
	s.root, s.scale = d.Root, d.ScaleID()
	if len(d.Chords) > 0 {
		s.prog = slices.Clone(d.Chords)
	}
	s.eng.SetHarmony(d.Root, d.ScaleID(), d.Chords)
	s.tuning = d.Tuning // the song's pitch stays the transport's after, as its tempo and key do
	s.eng.SetTuning(d.Tuning)
	s.eng.StopMusic()
	s.eng.ResetMusic()
	for slot, id := range tl[0] {
		s.eng.SetPattern(slot, id, 0, false)
		s.slots[slot] = id
	}
	length := float64(len(tl)*audio.SamplesPerBar(d.BPM)) / audio.AudioSampleRate
	s.song = &songPlay{name: d.Name, bars: tl, next: 1, until: time.Now().Add(time.Duration(length*float64(time.Second)) + 3*time.Second)}
	s.eng.StartMusic()
	fmt.Fprintf(s.out, "playing %s: %d bar(s), %.1fs; music stop ends it\n", d.Name, len(tl), length)
	if s.ticked {
		return nil
	}
	for s.song != nil {
		time.Sleep(20 * time.Millisecond)
		s.conduct()
	}
	return nil
}

// conduct moves a playing song on: once the transport is seen at its first bar,
// each bar's patterns are scheduled while the bar before plays, to land on the
// bar line, and the transport stops after the last bar, or when the song has
// run three seconds past its length
func (s *Session) conduct() {
	p := s.song
	if p == nil {
		return
	}
	bar, _, running := s.eng.Transport()
	at := int(bar)
	switch {
	case time.Now().After(p.until):
		at = len(p.bars)
	case !running:
		return
	case !p.begun && at != 0:
		return
	}
	p.begun = true
	if at >= len(p.bars) {
		s.eng.StopMusic()
		s.song = nil
		fmt.Fprintf(s.out, "%s done\n", p.name)
		return
	}
	for p.next <= at+1 && p.next < len(p.bars) {
		for slot, id := range p.bars[p.next] {
			if id != p.bars[p.next-1][slot] {
				s.eng.SetPattern(slot, id, 0, p.next == at+1)
				s.slots[slot] = id
			}
		}
		p.next++
	}
}

// refineOptions sizes the pattern search of a refinement round
type refineOptions struct {
	generations, pool int
	seed              uint64
	steps             int // the most steps a sweep visits, every one when zero; a test's budget
}

var defaultRefine = refineOptions{generations: 6, pool: 12, seed: 1}

func cmdRefine(s *Session, a []string) error {
	rp, ok := s.rips[a[0]]
	if !ok {
		return fmt.Errorf("%q: nothing ripped under that name this session (rip first)", a[0])
	}
	d, ok := s.songs.get(a[0])
	if !ok {
		return fmt.Errorf("%q: no song of that name (compress first)", a[0])
	}
	rounds := 3
	if len(a) > 1 {
		n, err := strconv.Atoi(a[1])
		if err != nil || n < 1 || n > 50 {
			return fmt.Errorf("rounds %q: want 1 to 50", a[1])
		}
		rounds = n
	}
	return s.refine(a[0], rp, d, rounds, s.refineOpt)
}

// refine closes the loop over a ripped song: each round searches each
// register's voice and then the velocities and lengths of the song's tonal
// steps, every candidate judged by the song's render ripped back against the
// recording's rip, so a voice or a step stays only when the song rips back
// nearer; it stops when a round gains too little
func (s *Session) refine(name string, rp *ripped, d *audio.SongDef, rounds int, o refineOptions) error {
	total, err := s.reportSong(name, d, rp, " before refining")
	if err != nil {
		return err
	}
	for round := 1; round <= rounds; round++ {
		start := total
		voiced, err := s.refineVoices(name, rp, d, total, o)
		if err != nil {
			return err
		}
		fmt.Fprintf(s.out, "round %d: voices %.2f -> %.2f\n", round, total, voiced)
		total = voiced
		stepped, err := s.refineSteps(rp, d, total, o)
		if err != nil {
			return err
		}
		fmt.Fprintf(s.out, "round %d: steps %.2f -> %.2f\n", round, total, stepped)
		total = stepped
		if total-start < 0.005 {
			fmt.Fprintf(s.out, "converged after %d round(s)\n", round)
			break
		}
	}
	_, err = s.reportSong(name, d, rp, " after refining")
	return err
}

// refineVoices searches each register's voice by the song's score: a genome of
// the voice's genes and wave, seeded with the voice in hand and every registered
// one, each candidate registered under the register's name while the song's
// render is ripped back; the voice that rips back nearest stays, the one in hand
// when none beats it. The registry holds each candidate, so one worker.
func (s *Session) refineVoices(name string, rp *ripped, d *audio.SongDef, base float64, o refineOptions) (float64, error) {
	for reg, at := range lineVoices(rp.rip) {
		if *at == nil {
			continue
		}
		word := ripLines[reg].word
		vname := name + "_" + word + "_voice"
		define := func(v *audio.VoiceDef) error {
			c := v.Clone()
			c.Name = vname
			_, err := s.eng.DefineVoice(c)
			return err
		}
		var failure error
		eval := genetic.Memoize(genomeKey, func(g []float64) float64 {
			v := voiceFromGenome(g)
			if audio.ValidateVoice(v) != nil {
				return math.Inf(-1)
			}
			if err := define(v); err != nil {
				failure = err
				return math.Inf(-1)
			}
			fid, err := s.songJudge(d, rp)
			if err != nil {
				failure = err
				return math.Inf(-1)
			}
			return fid.Total
		})
		seeds := [][]float64{voiceGenome(*at)}
		for _, v := range audio.RegisteredVoices() {
			if v.Name != vname {
				seeds = append(seeds, voiceGenome(v))
			}
		}
		n := audio.VoiceGeneCount + 1
		bounds := make([]genetic.ParameterBounds, n)
		for i := range bounds {
			bounds[i] = genetic.ParameterBounds{Min: 0, Max: 1}
		}
		cfg := genetic.DefaultConfig()
		cfg.PoolSize, cfg.EliteCount, cfg.MaxIterations = max(o.pool, 4), max(o.pool/4, 1), o.generations
		cfg.Parallelism, cfg.Seed = 1, o.seed+uint64(reg)
		cfg.PerturbationRate, cfg.PerturbationStrength = 0.15, 0.1
		e := genetic.NewEngine[[]float64, float64](eval, func(rng *rand.Rand) []float64 {
			g := make([]float64, n)
			for i := range g {
				g[i] = rng.Float64()
			}
			return g
		}, &genetic.TournamentSelector[[]float64, float64]{TournamentSize: 3},
			&genetic.UniformCombiner[[]float64, float64, float64]{MixProbability: 0.5},
			&genetic.BoundedPerturbator{Bounds: bounds, Boundary: genetic.BoundaryReflect}, cfg)
		e.SetSeeds(seeds)
		e.SetSchedule(genetic.AdaptiveStrength[float64](0.15, 0.1, 0.01, 0.4, 1.5))
		e.SetTerminator(func(p *genetic.Pool[[]float64, float64], gen int) bool {
			fmt.Fprintf(s.out, "generation %d: %s voice %.3f\n", gen, word, p.Stats.BestScore)
			return failure != nil
		})
		pool, err := e.Run(context.Background())
		if err == nil {
			err = failure
		}
		if err != nil || len(pool.Members) == 0 || pool.Members[0].Score <= base {
			if derr := define(*at); err == nil {
				err = derr
			}
			if err != nil {
				return base, err
			}
			fmt.Fprintf(s.out, "the %s voice stays (%.2f)\n", word, base)
			continue
		}
		v := voiceFromGenome(pool.Members[0].Data)
		if err := define(v); err != nil {
			return base, err
		}
		*at = v
		named := v.Clone()
		named.Name = vname
		s.voices.put(named, false)
		fmt.Fprintf(s.out, "%s voice: %s, %.2f -> %.2f\n", word, cmp.Or(v.Wave, "sine"), base, pool.Members[0].Score)
		base = pool.Members[0].Score
	}
	return base, nil
}

// refineSteps sweeps the song's tonal steps: each is tried a semitone and an
// octave either way, the recording's strongest pitch at its step, a step
// earlier and later, a step longer and shorter, and silent, judged on the bars
// that play its pattern, and the best move is kept when it gains; then a step a
// line leaves empty tries that strongest pitch, the one way in for a note the
// rip missed. A step left silent is dropped at the end, and a sweep that does
// not carry to the whole song is undone. One sweep is a round's share. A move
// touches one note's frames in a thousand, which a search over every step's
// genes at once could not see in its budget.
func (s *Session) refineSteps(rp *ripped, d *audio.SongDef, base float64, o refineOptions) (float64, error) {
	type step struct {
		pat     string
		trk, ev int
	}
	var steps []step
	var names []string
	originals := map[string]*audio.PatternDef{}
	uses := map[string][]int{} // the bars each pattern plays in
	slotOf := map[string]int{} // the slot it plays in, the bass's register or the melody's
	local := true              // every pattern a bar long, so a bar's render plays it whole
	for b, bar := range d.Bars {
		for slot, n := range bar {
			if slot == 0 || n == "" {
				continue
			}
			pd, ok := s.pats.get(n)
			if !ok {
				continue
			}
			uses[n] = append(uses[n], b)
			if originals[n] != nil {
				continue
			}
			originals[n] = clonePatternDef(pd)
			slotOf[n] = slot
			names = append(names, n)
			local = local && pd.Steps <= audio.StepsPerBar
			for ti := range pd.Track {
				if instr, _ := audio.InstrumentByName(pd.Track[ti].Instr); instr.IsDrum() {
					continue
				}
				for ei := range pd.Track[ti].Event {
					steps = append(steps, step{n, ti, ei})
				}
			}
		}
	}
	if len(steps) == 0 {
		return base, nil
	}
	if o.steps > 0 && len(steps) > o.steps {
		steps = steps[:o.steps]
	}
	register := func(n string) error {
		pd, _ := s.pats.get(n)
		p, err := pd.Pattern()
		if err == nil {
			_, err = s.eng.DefinePattern(p)
		}
		if err != nil {
			return fmt.Errorf("pattern %q: %w", n, err)
		}
		s.pats.clearDirty(n)
		return nil
	}
	judge := func(pat string) (float64, error) {
		if local { // on the bars that play it, two at most: a bar stands for the pattern's others
			return s.songJudgeBars(d, rp, uses[pat][:min(len(uses[pat]), 2)])
		}
		fid, err := s.songJudge(d, rp)
		return fid.Total, err
	}
	root, scale := d.Root, d.ScaleID()
	want, err := s.soundOf(rp)
	if err != nil {
		return base, err
	}
	// proposal is the recording's strongest pitch over a step of the bar a
	// pattern first plays in, in the register of the pattern's slot
	proposal := func(pat string, pos int) (int, bool) {
		bar := uses[pat][0] + pos/audio.StepsPerBar
		i := audio.BeatsPerBar*bar + pos%audio.StepsPerBar/audio.StepsPerBeat
		if i < 0 || i+1 >= len(rp.rip.Beats) {
			return 0, false
		}
		span := (rp.rip.Beats[i+1] - rp.rip.Beats[i]) / audio.StepsPerBeat
		t0 := rp.rip.Beats[i] + span*float64(pos%audio.StepsPerBeat)
		f0, f1 := int(t0/want.Frames.Hop), int((t0+span)/want.Frames.Hop)
		lo, hi := audio.RipMelodyLow, audio.RipMaxMIDI
		if slotOf[pat] == 3 {
			lo, hi = audio.RipMinMIDI, audio.RipBassTop
		}
		best, bestV := -1, 0.0
		for m := lo; m < hi; m++ {
			var v float64
			for t := max(f0, 0); t < f1 && (t+1)*audio.RipPitches <= len(want.Frames.Salience); t++ {
				v += float64(want.Frames.Salience[t*audio.RipPitches+m-audio.RipMinMIDI])
			}
			if v > bestV {
				best, bestV = m, v
			}
		}
		return best, best >= 0
	}
	moved, current, judged := 0, 0.0, ""
	for _, st := range steps {
		pd, _ := s.pats.get(st.pat)
		if judged != st.pat {
			score, err := judge(st.pat)
			if err != nil {
				return base, err
			}
			current, judged = score, st.pat
		}
		ev := &pd.Track[st.trk].Event[st.ev]
		was := *ev
		midi := audio.StepPitch(root, scale, was.Deg, was.Oct, was.Acc)
		taken := func(pos int) bool {
			return slices.ContainsFunc(pd.Track[st.trk].Event, func(e audio.StepDef) bool { return e.Pos == pos && e != was })
		}
		pitch := func(m int) func(*audio.StepDef) {
			return func(e *audio.StepDef) { e.Deg, e.Oct, e.Acc = audio.PitchStep(root, scale, m) }
		}
		var moves []func(*audio.StepDef)
		for _, m := range [...]int{midi - 1, midi + 1, midi - 12, midi + 12} {
			if m >= audio.RipMinMIDI && m < audio.RipMaxMIDI {
				moves = append(moves, pitch(m))
			}
		}
		if m, ok := proposal(st.pat, was.Pos); ok && m != midi {
			moves = append(moves, pitch(m))
		}
		for _, pos := range [...]int{was.Pos - 1, was.Pos + 1} {
			if pos >= 0 && pos < pd.Steps && !taken(pos) {
				moves = append(moves, func(e *audio.StepDef) { e.Pos = pos })
			}
		}
		for _, dur := range [...]int{max(was.Dur, 1) - 1, max(was.Dur, 1) + 1} {
			if dur >= 1 && dur <= audio.MaxPatternLen {
				moves = append(moves, func(e *audio.StepDef) { e.Dur = dur })
			}
		}
		moves = append(moves, func(e *audio.StepDef) { e.Vel = 0 }) // silent, dropped at the end if it stays so
		best, bestEv := current, was
		for _, move := range moves {
			*ev = was
			move(ev)
			if err := register(st.pat); err != nil {
				return base, err
			}
			score, err := judge(st.pat)
			if err != nil {
				return base, err
			}
			if score > best+1e-4 {
				best, bestEv = score, *ev
			}
		}
		*ev = bestEv
		if err := register(st.pat); err != nil {
			return base, err
		}
		if best > current {
			moved++
			fmt.Fprintf(s.out, "%s track %d step %d: %s -> %s, %.3f -> %.3f\n", st.pat, st.trk, was.Pos, stepName(root, scale, was), stepName(root, scale, bestEv), current, best)
			current = best
		}
	}
	// a step a line leaves empty tries the recording's strongest pitch there,
	// at the line's loudness, kept when the bar sounds more like the recording
	added, tries := 0, 0
	for _, n := range names {
		pd, _ := s.pats.get(n)
		if !local || len(uses[n]) == 0 {
			continue
		}
		score, err := judge(n)
		if err != nil {
			return base, err
		}
		for ti := range pd.Track {
			tr := &pd.Track[ti]
			if instr, _ := audio.InstrumentByName(tr.Instr); instr.IsDrum() || len(tr.Event) == 0 {
				continue
			}
			var vel float64
			for _, e := range tr.Event {
				vel += e.Vel
			}
			vel /= float64(len(tr.Event))
			sounding := func(pos, m int) bool {
				for _, t := range pd.Track {
					if slices.ContainsFunc(t.Event, func(e audio.StepDef) bool {
						return e.Pos == pos && e.Vel > 0 && audio.StepPitch(root, scale, e.Deg, e.Oct, e.Acc) == m
					}) {
						return true
					}
				}
				return false
			}
			for pos := 0; pos < pd.Steps; pos++ {
				if o.steps > 0 && tries >= o.steps {
					break
				}
				m, ok := proposal(n, pos)
				if !ok || slices.ContainsFunc(tr.Event, func(e audio.StepDef) bool { return e.Pos == pos }) || sounding(pos, m) {
					continue
				}
				tries++
				deg, oct, acc := audio.PitchStep(root, scale, m)
				tr.Event = append(tr.Event, audio.StepDef{Pos: pos, Vel: vel, Deg: deg, Oct: oct, Acc: acc, Dur: 1})
				if err := register(n); err != nil {
					return base, err
				}
				sc, err := judge(n)
				if err != nil {
					return base, err
				}
				if sc > score+1e-4 {
					added++
					fmt.Fprintf(s.out, "%s track %d: %s added at %d, %.3f -> %.3f\n", n, ti, noteName(m), pos, score, sc)
					score = sc
					continue
				}
				tr.Event = tr.Event[:len(tr.Event)-1]
				if err := register(n); err != nil {
					return base, err
				}
			}
			slices.SortFunc(tr.Event, func(a, b audio.StepDef) int { return a.Pos - b.Pos })
		}
	}
	dropped := 0
	for _, n := range names {
		pd, _ := s.pats.get(n)
		for ti := range pd.Track {
			before := len(pd.Track[ti].Event)
			pd.Track[ti].Event = slices.DeleteFunc(pd.Track[ti].Event, func(e audio.StepDef) bool { return e.Vel == 0 })
			dropped += before - len(pd.Track[ti].Event)
		}
		if err := register(n); err != nil {
			return base, err
		}
	}
	fid, err := s.songJudge(d, rp)
	if err != nil {
		return base, err
	}
	if fid.Total < base {
		for _, pd := range originals {
			s.pats.put(pd, false)
			if err := register(pd.Name); err != nil {
				return base, err
			}
		}
		fmt.Fprintf(s.out, "%d step(s) moved and %d added on their bars, but the song sounds less like the recording (%.3f against %.3f): undone\n", moved, added, fid.Total, base)
		return base, nil
	}
	for _, n := range names {
		if moved+added > 0 {
			s.pats.markDirty(n) // the document moved; a save keeps it, apply already did
		}
	}
	fmt.Fprintf(s.out, "%d step(s) moved, %d added, %d dropped\n", moved, added, dropped)
	return fid.Total, nil
}

// stepName is a step's pitch as a note name with its step and length, or silence
func stepName(root int, scale audio.ScaleID, e audio.StepDef) string {
	if e.Vel == 0 {
		return "silence"
	}
	return fmt.Sprintf("%s at %d for %d", noteName(audio.StepPitch(root, scale, e.Deg, e.Oct, e.Acc)), e.Pos, max(e.Dur, 1))
}
