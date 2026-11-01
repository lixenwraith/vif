//go:build !vif_headless && !vif_noaudio && !wasm

package soundlab

// The song: a rip's bars matched to a dictionary of one-bar patterns and the
// timeline that plays them (compress), the timeline shown, rendered or conducted
// through the live sequencer (song), and the score of what the compression kept.

import (
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
	return audio.CompareRips(got, want), nil
}

// renderSong plays a song offline, the dirty patterns it names applied first
func (s *Session) renderSong(d *audio.SongDef) ([]float64, error) {
	tl, err := s.songTimeline(d)
	if err != nil {
		return nil, err
	}
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
	buf, err := s.renderSong(d)
	if err != nil {
		return err
	}
	f, err := os.Create(a[1])
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
