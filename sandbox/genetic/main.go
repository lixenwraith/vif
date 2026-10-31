// Command genetic evolves lander controllers with genetic.StreamingEngine. Two
// streams of landers, a population each, are released in step from opposite
// corners toward one pad, and two landers of different streams that meet in
// the air are both destroyed. Arrow keys or hjkl move the pad and re-score the
// archives; q quits; -headless N runs N ticks without the view and tabulates.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"os/signal"
	"slices"
	"syscall"
	"time"

	"github.com/lixenwraith/vif/pkg/genetic"
)

const (
	fieldWidth  = 80
	fieldHeight = 18
	maxTicks    = 300
	frame       = 16 * time.Millisecond

	gravity   = 0.04
	maxThrust = 0.08
	startY    = fieldHeight - 2.0

	padHalf  = 4
	markLife = 150

	// Two landers of different streams within crashRadius are both destroyed
	crashRadius = 1.5
	burstLife   = 8

	releaseEvery    = 12
	maxLive         = 24
	flightsPerTrial = 4 // Releases a genotype flies in; its score is their mean
	refreshEvery    = 2 // Every other trial re-flies an archive member
	recent          = 32 // Flights the outcome figures average over
	maxSources      = 4
)

// The genome is the four PD gains. Signs are fixed by the control law in step,
// so the search space is positive gains only
const (
	geneKpX = iota
	geneKdX
	geneKpY
	geneKdY
	geneCount
)

var bounds = []genetic.ParameterBounds{
	{Min: 0, Max: 4}, {Min: 0, Max: 8}, {Min: 0, Max: 4}, {Min: 0, Max: 24},
}

type lander struct {
	src     int
	trial   int
	genes   []float64
	x, y    float64
	vx, vy  float64
	fuel    float64
	ticks   int
	crashed bool
}

func (l *lander) launch(src, trial int, genes []float64, x float64) {
	*l = lander{src: src, trial: trial, genes: genes, x: x, y: startY}
}

func (l *lander) flying() bool { return l.genes != nil }

// step advances one tick and reports whether the flight ended. Thrust acts in
// both directions on each axis, so a controller can dive as well as brake
func (l *lander) step(padX float64) bool {
	g := l.genes
	reqX := g[geneKpX]*(padX-l.x)/fieldWidth - g[geneKdX]*l.vx/2
	reqY := -g[geneKpY]*l.y/fieldHeight - g[geneKdY]*l.vy/2
	tx := clamp(reqX, -1, 1) * maxThrust
	ty := clamp(reqY, -1, 1) * maxThrust
	l.fuel += math.Abs(tx) + math.Abs(ty)
	l.vx += tx
	l.vy += ty - gravity
	l.x += l.vx
	l.y += l.vy
	l.ticks++
	return l.y <= 0 || l.x < 0 || l.x >= fieldWidth || l.y > fieldHeight || l.ticks >= maxTicks
}

func (l *lander) landed() bool { return l.y <= 0 && l.x >= 0 && l.x < fieldWidth }

func (l *lander) onPad(padX float64) bool { return math.Abs(l.x-padX) <= padHalf }

func (l *lander) speed() float64 { return math.Hypot(l.vx, l.vy) }

// score rewards a soft, cheap touchdown anywhere on the pad: short of the
// centre or past it costs nothing, past the edge decays, and a lander destroyed
// in a collision scores nothing. Only a landing earns the softness and fuel
// terms, so hovering until timeout or leaving the field is a miss at the final
// distance, altitude included
func (l *lander) score(padX float64) float64 {
	if l.crashed {
		return 0
	}
	over := max(math.Abs(l.x-padX)-padHalf, 0)
	if !l.landed() {
		d := math.Hypot(over, l.y) / 3
		return 0.5 / (1 + d*d)
	}
	d := over / 3
	v := l.speed() / 0.1
	return 0.5/(1+d*d) + 0.35/(1+v*v) + 0.15/(1+l.fuel/20)
}

// fly runs genes to completion alone, with nothing to collide with
func fly(genes []float64, startX, padX float64) lander {
	var l lander
	l.launch(0, 0, genes, startX)
	for !l.step(padX) {
	}
	return l
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

type outcome struct {
	score   float64
	offset  float64 // Touchdown relative to the pad centre
	landed  bool    // On the pad
	soft    bool
	crashed bool
}

// A trial is one evaluation: a genotype flown in flightsPerTrial consecutive
// releases, each against another of the other stream's landers, and scored by
// the mean, so the archive holds what a controller can expect rather than its
// luckiest flight
type trial struct {
	id      genetic.EvalID
	genes   []float64
	flights int
	done    int
	sum     float64
	live    bool
}

// A source is one stream's population and its corner of the field
type source struct {
	name   string
	colour string
	startX float64
	engine *genetic.StreamingEngine[[]float64, float64]

	live       int
	trials     []trial
	current    int // The trial whose flights are being released, or -1
	started    int
	generation int
	members    []genetic.Candidate[[]float64, float64] // The archive at its last generation, re-flown in turn
	refresh    int
	sweep      int // Members still to re-fly before breeding resumes, after the pad moved

	// The last recent flights' outcomes: the archive holds each genotype's last
	// score, not how the stream fares now
	outcomes [recent]outcome
	flights  int
}

var names = [maxSources + 1][]string{
	1: {"solo"}, 2: {"left", "right"}, 3: {"left", "mid", "right"}, 4: {"left", "mid-l", "mid-r", "right"},
}

var colours = [maxSources]string{"196", "33", "46", "226"}

func newSource(name, colour string, startX float64, seed uint64) source {
	cfg := genetic.DefaultStreamingConfig()
	cfg.PoolSize = 24
	// A shallow queue keeps proposals bred from the current archive, not a stale one
	cfg.ProposalCapacity = 8
	// Four times the flights a stream may have in the air, so a window never fills
	cfg.PendingCapacity = 4 * maxLive
	cfg.MinOutcomesPerGen = 8
	// Four genes: mutate two per child on average, at a tenth of each range
	cfg.PerturbationRate = 0.5
	cfg.PerturbationStrength = 0.1
	cfg.Seed = seed

	e := genetic.NewStreamingEngine[[]float64, float64](
		func(rng *rand.Rand) []float64 {
			g := make([]float64, geneCount)
			for i, b := range bounds {
				g[i] = b.Min + rng.Float64()*(b.Max-b.Min)
			}
			return g
		},
		&genetic.TournamentSelector[[]float64, float64]{TournamentSize: 3},
		&genetic.UniformCombiner[[]float64, float64, float64]{MixProbability: 0.5},
		&genetic.BoundedPerturbator{Bounds: bounds, Boundary: genetic.BoundaryReflect},
		genetic.SliceCloner[[]float64, float64]{},
		cfg,
	)
	// A re-flown member takes its new score; without this an elitist archive
	// fills with clones of its best, scored when the other stream flew otherwise
	e.SetEqual(slices.Equal[[]float64])
	e.SetDiversity(genetic.GeneSpread[float64](bounds))
	e.Start()
	return source{name: name, colour: colour, startX: startX, engine: e, trials: make([]trial, maxLive+1), current: -1}
}

// nextTrial returns the trial the next release flies for: the current one while
// it has flights left, else a new one, bred by the engine or, every
// refreshEvery-th and throughout a sweep, an archive member re-flown since its
// score is as old as the other stream's behaviour when it was set. Nil when
// the engine gives nothing
func (s *source) nextTrial() *trial {
	if s.current >= 0 && s.trials[s.current].flights < flightsPerTrial {
		return &s.trials[s.current]
	}
	i := slices.IndexFunc(s.trials, func(t trial) bool { return !t.live })
	if i < 0 {
		return nil
	}
	t := &s.trials[i]
	s.started++
	if (s.sweep > 0 || s.started%refreshEvery == 0) && len(s.members) > 0 {
		s.sweep = max(s.sweep-1, 0)
		genes := slices.Clone(s.members[s.refresh%len(s.members)].Data)
		s.refresh++
		*t = trial{id: s.engine.BeginEvaluation(genes), genes: genes, live: true}
	} else {
		genes, id := s.engine.Propose()
		if id == 0 {
			return nil
		}
		*t = trial{id: id, genes: genes, live: true}
	}
	s.current = i
	return t
}

// finish adds a flight's score to its trial and reports the mean once the
// trial's last flight is in
func (s *source) finish(trial int, score float64) {
	t := &s.trials[trial]
	t.sum += score
	t.done++
	if t.done < flightsPerTrial {
		return
	}
	s.engine.CompleteEvaluation(t.id, t.sum/flightsPerTrial)
	t.live = false
	if s.current == trial {
		s.current = -1
	}
}

func (s *source) record(o outcome) {
	s.outcomes[s.flights%recent] = o
	s.flights++
}

// rate is the share of the recent flights that met the condition
func (s *source) rate(ok func(outcome) bool) float64 {
	n := min(s.flights, recent)
	if n == 0 {
		return 0
	}
	hits := 0
	for _, o := range s.outcomes[:n] {
		if ok(o) {
			hits++
		}
	}
	return float64(hits) / float64(n)
}

func (s *source) recentScore() float64 {
	n := min(s.flights, recent)
	if n == 0 {
		return 0
	}
	total := 0.0
	for _, o := range s.outcomes[:n] {
		total += o.score
	}
	return total / float64(n)
}

// zone is where the stream's recent pad landings touched down, relative to the
// pad centre: the side of the pad a stream settles on
func (s *source) zone() float64 {
	n, total := 0, 0.0
	for _, o := range s.outcomes[:min(s.flights, recent)] {
		if o.landed {
			n++
			total += o.offset
		}
	}
	if n == 0 {
		return 0
	}
	return total / float64(n)
}

// refreshMembers copies the archive once per generation for re-flights
func (s *source) refreshMembers() {
	if g := s.engine.Generation(); s.members == nil || g != s.generation {
		s.generation = g
		s.members = s.engine.Snapshot().Members
	}
}

type mark struct {
	until int
	src   int
	soft  bool
}

type burst struct {
	x, y  int
	until int
}

type world struct {
	sources []source
	landers []lander
	padX    float64
	tick    int
	marks   [fieldWidth]mark
	bursts  []burst
}

func newWorld(seed uint64, n int) *world {
	w := &world{padX: fieldWidth / 2, landers: make([]lander, n*maxLive)}
	for i := range n {
		x := 8.0
		if n > 1 {
			x += float64(fieldWidth-16) * float64(i) / float64(n-1)
		}
		w.sources = append(w.sources, newSource(names[n][i], colours[i], x, seed*maxSources+uint64(i)))
	}
	return w
}

// release launches one lander from every source on the same tick, so each
// release's landers meet, or none when a stream has no slot free
func (w *world) release() {
	if w.tick%releaseEvery != 0 {
		return
	}
	for s := range w.sources {
		if w.sources[s].live >= maxLive {
			return
		}
	}
	for s := range w.sources {
		src := &w.sources[s]
		t := src.nextTrial()
		if t == nil {
			continue
		}
		slot := slices.IndexFunc(w.landers, func(l lander) bool { return !l.flying() })
		w.landers[slot].launch(s, src.current, t.genes, src.startX)
		t.flights++
		src.live++
	}
}

// collide destroys both landers of any two streams that meet; a stream's own
// landers pass each other, since a stream cannot choose its release cadence
func (w *world) collide() {
	for i := range w.landers {
		a := &w.landers[i]
		if !a.flying() || a.crashed {
			continue
		}
		for j := i + 1; j < len(w.landers); j++ {
			b := &w.landers[j]
			if !b.flying() || b.crashed || b.src == a.src || math.Hypot(b.x-a.x, b.y-a.y) >= crashRadius {
				continue
			}
			a.crashed, b.crashed = true, true
			w.bursts = append(w.bursts, burst{x: int((a.x + b.x) / 2), y: int((a.y + b.y) / 2), until: w.tick + burstLife})
			break
		}
	}
}

// advance releases, flies every lander one tick, settles collisions and scores
// the flights that ended. Flights end at different ticks, so outcomes reach the
// engines out of release order
func (w *world) advance() {
	w.tick++
	for s := range w.sources {
		w.sources[s].refreshMembers()
	}
	w.release()
	w.collide()
	for i := range w.landers {
		l := &w.landers[i]
		if !l.flying() || (!l.crashed && !l.step(w.padX)) {
			continue
		}
		src := &w.sources[l.src]
		score := l.score(w.padX)
		src.finish(l.trial, score)
		landed := l.landed() && l.onPad(w.padX)
		src.record(outcome{score: score, offset: l.x - w.padX, landed: landed, soft: landed && l.speed() < 0.15, crashed: l.crashed})
		if l.landed() {
			w.marks[int(l.x)] = mark{until: w.tick + markLife, src: l.src, soft: l.speed() < 0.15}
		}
		src.live--
		l.genes = nil
	}
	w.bursts = slices.DeleteFunc(w.bursts, func(b burst) bool { return b.until <= w.tick })
}

// retarget moves the pad, abandons the flights scored against the old one and
// re-scores each archive by flying its members alone against the new one, so
// the learned gains survive the objective change instead of being thrown away;
// a sweep then re-flies every member live, where the other stream is
func (w *world) retarget(dx int) {
	x := clamp(w.padX+float64(dx), padHalf+1, fieldWidth-padHalf-2)
	if x == w.padX {
		return
	}
	w.padX = x
	for i := range w.landers {
		w.landers[i].genes = nil
	}
	for s := range w.sources {
		src := &w.sources[s]
		for i := range src.trials {
			if t := &src.trials[i]; t.live {
				src.engine.AbandonEvaluation(t.id)
				t.live = false
			}
		}
		src.current, src.live = -1, 0
		pool := src.engine.Snapshot()
		for i := range pool.Members {
			run := fly(pool.Members[i].Data, src.startX, x)
			pool.Members[i].Score = run.score(x)
		}
		src.engine.Inject(pool.Members, pool.Generation)
		src.members = nil
		src.sweep = len(pool.Members)
		src.flights = 0
	}
	w.marks = [fieldWidth]mark{}
	w.bursts = w.bursts[:0]
}

func main() {
	seed := flag.Uint64("seed", 1, "PCG seed; the same seed replays the same run")
	headless := flag.Int("headless", 0, "run this many ticks without the terminal view, moving the pad once halfway")
	sources := flag.Int("sources", 2, "streams of landers, 1 to 4")
	speed := flag.Int("speed", 4, "ticks per frame")
	flag.Parse()

	w := newWorld(*seed, min(max(*sources, 1), maxSources))
	if *headless > 0 {
		runHeadless(w, *headless)
		return
	}
	runInteractive(w, max(*speed, 1))
}

func runHeadless(w *world, ticks int) {
	fmt.Println("  tick src     gen evals  best   avg spread | recent  soft crash  zone |   KpX   KdX   KpY   KdY")
	for w.tick < ticks {
		w.advance()
		if w.tick%1000 != 0 && w.tick != ticks {
			continue
		}
		for s := range w.sources {
			src := &w.sources[s]
			st := src.engine.Stats()
			best, _ := src.engine.Best()
			fmt.Printf("%6d %-7s %4d %5d %.3f %.3f  %.3f | %.3f  %3.0f%%  %3.0f%% %+5.1f | %5.2f %5.2f %5.2f %5.2f\n",
				w.tick, src.name, st.Generation, st.Evaluations, st.BestScore, st.AverageScore, st.Diversity,
				src.recentScore(), 100*src.rate(func(o outcome) bool { return o.soft }),
				100*src.rate(func(o outcome) bool { return o.crashed }), src.zone(),
				best.Data[geneKpX], best.Data[geneKdX], best.Data[geneKpY], best.Data[geneKdY])
		}
		if w.tick == ticks/2 {
			w.retarget(-20)
			fmt.Printf("pad moved to %d\n", int(w.padX))
		}
	}
}

func runInteractive(w *world, speed int) {
	defer rawTerminal()()

	keys := make(chan byte, 8)
	go readKeys(keys)
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	ticker := time.NewTicker(frame)
	defer ticker.Stop()

	buf := bytes.NewBuffer(make([]byte, 0, 16<<10))
	for {
		select {
		case <-sig:
			return
		case k := <-keys:
			switch k {
			case 'q':
				return
			case '<':
				w.retarget(-3)
			case '>':
				w.retarget(3)
			}
		case <-ticker.C:
			for range speed {
				w.advance()
			}
			render(buf, w)
		}
	}
}

// rawTerminal switches stdin to unbuffered input through stty, which keeps the
// sandbox on the standard library. It returns the function that undoes it
func rawTerminal() func() {
	stty := func(args ...string) {
		c := exec.Command("stty", args...)
		c.Stdin = os.Stdin
		_ = c.Run()
	}
	stty("-icanon", "-echo")
	fmt.Print("\033[?25l\033[2J")
	return func() {
		stty("icanon", "echo")
		fmt.Print("\033[0m\033[?25h\033[2J\033[H")
	}
}

// readKeys maps q, h/l, a/d and the arrow keys onto 'q', '<' and '>'
func readKeys(keys chan<- byte) {
	b := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(b)
		if err != nil {
			return
		}
		arrow := n == 3 && b[0] == 27 && b[1] == '['
		switch {
		case b[0] == 'q' || b[0] == 'Q':
			keys <- 'q'
		case b[0] == 'h' || b[0] == 'a' || (arrow && b[2] == 'D'):
			keys <- '<'
		case b[0] == 'l' || b[0] == 'd' || (arrow && b[2] == 'C'):
			keys <- '>'
		}
	}
}

const (
	ansiReset = "\033[0m"
	ansiDim   = "\033[2m"
	ansiBold  = "\033[1m"
	ansiBurst = "\033[1;93m"
	ansiClear = "\033[K\n"
)

func (s *source) sgr() string { return "\033[38;5;" + s.colour + "m" }

func render(buf *bytes.Buffer, w *world) {
	buf.Reset()
	buf.WriteString("\033[H")

	fmt.Fprintf(buf, "%s genetic landers%s | tick %d | pad %d | %s[<] [>]%s move pad  %s[q]%s quit%s",
		ansiBold, ansiReset, w.tick, int(w.padX), ansiBold, ansiReset, ansiBold, ansiReset, ansiClear)
	for s := range w.sources {
		src := &w.sources[s]
		fmt.Fprintf(buf, "%s■ %-6s%s gen %-3d flights %-5d recent %.2f  soft %3.0f%%  crash %3.0f%%  zone %+.1f%s",
			src.sgr(), src.name, ansiReset, src.engine.Generation(), src.flights, src.recentScore(),
			100*src.rate(func(o outcome) bool { return o.soft }),
			100*src.rate(func(o outcome) bool { return o.crashed }), src.zone(), ansiClear)
	}
	buf.WriteString(ansiClear)

	var field [fieldHeight][fieldWidth]string
	for i := range w.landers {
		l := &w.landers[i]
		if !l.flying() {
			continue
		}
		if col, row := int(l.x), fieldHeight-1-int(l.y); col >= 0 && col < fieldWidth && row >= 0 && row < fieldHeight {
			field[row][col] = w.sources[l.src].sgr() + "A" + ansiReset
		}
	}
	for _, b := range w.bursts {
		if row := fieldHeight - 1 - b.y; b.x >= 0 && b.x < fieldWidth && row >= 0 && row < fieldHeight {
			field[row][b.x] = ansiBurst + "*" + ansiReset
		}
	}
	for row := range field {
		for _, c := range field[row] {
			if c == "" {
				buf.WriteByte(' ')
			} else {
				buf.WriteString(c)
			}
		}
		buf.WriteString(ansiClear)
	}

	// Ground row: the pad, and recent touchdowns as '.' (soft) or 'x' (hard)
	// in their stream's colour
	pad := int(w.padX)
	for col := range fieldWidth {
		m := w.marks[col]
		switch {
		case m.until > w.tick && m.soft:
			buf.WriteString(w.sources[m.src].sgr() + "." + ansiReset)
		case m.until > w.tick:
			buf.WriteString(w.sources[m.src].sgr() + "x" + ansiReset)
		case col == pad:
			buf.WriteString(ansiBold + "o" + ansiReset)
		case col == pad-padHalf || col == pad+padHalf:
			buf.WriteString(ansiBold + "|" + ansiReset)
		case col > pad-padHalf && col < pad+padHalf:
			buf.WriteString("=")
		default:
			buf.WriteString(ansiDim + "_" + ansiReset)
		}
	}
	buf.WriteString(ansiClear)

	_, _ = os.Stdout.Write(buf.Bytes())
}
