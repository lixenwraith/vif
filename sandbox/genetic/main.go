// Command genetic evolves lander controllers with genetic.StreamingEngine.
// Landers are proposed, fly for up to maxTicks while others are still in the
// air, and report one scalar score on touchdown. Arrow keys move the pad and
// re-score the archive against it; q quits. -headless N runs N ticks without
// the terminal view and prints a convergence table instead.
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
	landerCount = 32
	maxTicks    = 300
	frame       = 16 * time.Millisecond

	gravity   = 0.04
	maxThrust = 0.08
	startX    = 10.0
	startY    = fieldHeight - 2.0

	padHalf  = 4
	markLife = 150
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
	id     genetic.EvalID
	genes  []float64
	x, y   float64
	vx, vy float64
	fuel   float64
	ticks  int
}

func (l *lander) launch(genes []float64, id genetic.EvalID) {
	*l = lander{id: id, genes: genes, x: startX, y: startY}
}

func (l *lander) flying() bool { return l.genes != nil }

// step advances one tick and reports whether the flight ended. Thrust acts in
// both directions on each axis, so a controller can dive as well as brake
func (l *lander) step(targetX float64) bool {
	g := l.genes
	reqX := g[geneKpX]*(targetX-l.x)/fieldWidth - g[geneKdX]*l.vx/2
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

func (l *lander) speed() float64 { return math.Hypot(l.vx, l.vy) }

// score rewards touchdown near the pad centre. Only a landing earns the softness
// and fuel terms, so hovering until timeout or leaving the field is a miss at
// the final distance, altitude included
func (l *lander) score(targetX float64) float64 {
	dx := l.x - targetX
	if !l.landed() {
		d := math.Hypot(dx, l.y) / 3
		return 0.5 / (1 + d*d)
	}
	d := dx / 3
	v := l.speed() / 0.1
	return 0.5/(1+d*d) + 0.35/(1+v*v) + 0.15/(1+l.fuel/20)
}

// fly runs genes to completion synchronously
func fly(genes []float64, targetX float64) lander {
	var l lander
	l.launch(genes, 0)
	for !l.step(targetX) {
	}
	return l
}

func clamp(v, lo, hi float64) float64 { return math.Min(math.Max(v, lo), hi) }

type mark struct {
	until int
	soft  bool
}

type world struct {
	engine  *genetic.StreamingEngine[[]float64, float64]
	landers [landerCount]lander
	targetX float64
	tick    int
	marks   [fieldWidth]mark

	// champion replays the archive's best genes so the current optimum is visible
	champion lander
	best     genetic.Candidate[[]float64, float64]
	bestRun  lander
}

func newWorld(seed uint64) *world {
	cfg := genetic.DefaultStreamingConfig()
	cfg.PoolSize = 48
	// A shallow queue keeps proposals bred from the current archive, not a stale one
	cfg.ProposalCapacity = 8
	// A pending slot is reclaimed once PendingCapacity further evaluations have
	// started. Flights last 20..300 ticks with 32 in the air, so one slow flight
	// can see ~500 launches; 1024 keeps it from being evicted before it lands
	cfg.PendingCapacity = 1024
	cfg.MinOutcomesPerGen = landerCount
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
	// Without duplicate rejection an elitist archive fills with clones of its best
	e.SetEqual(slices.Equal[[]float64])
	e.SetDiversity(genetic.GeneSpread[float64](bounds))
	e.Start()
	return &world{engine: e, targetX: fieldWidth / 2}
}

// advance launches a lander into every idle slot, flies them one tick, and
// scores the ones that ended. Flights end at different ticks, so outcomes reach
// the engine out of proposal order
func (w *world) advance() {
	w.tick++
	for i := range w.landers {
		l := &w.landers[i]
		if l.flying() {
			continue
		}
		if genes, id := w.engine.Propose(); id != 0 {
			l.launch(genes, id)
		}
	}
	for i := range w.landers {
		l := &w.landers[i]
		if !l.flying() || !l.step(w.targetX) {
			continue
		}
		w.engine.CompleteEvaluation(l.id, l.score(w.targetX))
		if l.landed() {
			w.marks[int(l.x)] = mark{until: w.tick + markLife, soft: l.speed() < 0.15}
		}
		l.genes = nil
	}
	w.flyChampion()
}

func (w *world) flyChampion() {
	best, ok := w.engine.Best()
	if !ok {
		return
	}
	if best.Score != w.best.Score || !slices.Equal(best.Data, w.best.Data) {
		w.best = best
		w.bestRun = fly(best.Data, w.targetX)
	}
	if !w.champion.flying() || w.champion.step(w.targetX) {
		w.champion.launch(w.best.Data, 0)
	}
}

// retarget moves the pad and re-scores the archive against it, so the learned
// gains survive the objective change instead of being thrown away
func (w *world) retarget(dx int) {
	x := clamp(w.targetX+float64(dx), padHalf+1, fieldWidth-padHalf-2)
	if x == w.targetX {
		return
	}
	w.targetX = x
	for i := range w.landers {
		if l := &w.landers[i]; l.flying() {
			w.engine.AbandonEvaluation(l.id)
			l.genes = nil
		}
	}
	pool := w.engine.Snapshot()
	for i := range pool.Members {
		run := fly(pool.Members[i].Data, x)
		pool.Members[i].Score = run.score(x)
	}
	w.engine.Inject(pool.Members, pool.Generation)
	w.marks = [fieldWidth]mark{}
	w.best = genetic.Candidate[[]float64, float64]{}
}

func main() {
	seed := flag.Uint64("seed", 1, "PCG seed; the same seed replays the same run")
	headless := flag.Int("headless", 0, "run this many ticks without the terminal view")
	flag.Parse()

	w := newWorld(*seed)
	if *headless > 0 {
		runHeadless(w, *headless)
		return
	}
	runInteractive(w)
}

func runHeadless(w *world, ticks int) {
	fmt.Println("  tick   gen  evals evicted   best    avg  worst spread |   KpX   KdX   KpY   KdY |  miss speed  fuel ticks")
	for w.tick < ticks {
		w.advance()
		if (w.tick%500 != 0 && w.tick != ticks) || w.best.Data == nil {
			continue
		}
		s, r := w.engine.Stats(), w.bestRun
		fmt.Printf("%6d %5d %6d %7d  %.3f  %.3f  %.3f  %.3f | %5.2f %5.2f %5.2f %5.2f | %5.2f %.3f %5.1f %5d\n",
			w.tick, s.Generation, s.Evaluations, s.Evicted, s.BestScore, s.AverageScore, s.WorstScore, s.Diversity,
			w.best.Data[geneKpX], w.best.Data[geneKdX], w.best.Data[geneKpY], w.best.Data[geneKdY],
			math.Abs(r.x-w.targetX), r.speed(), r.fuel, r.ticks)
	}
}

func runInteractive(w *world) {
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
			w.advance()
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
		fmt.Print("\033[?25h\033[2J\033[H")
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
	ansiReset  = "\033[0m"
	ansiDim    = "\033[2m"
	ansiBold   = "\033[1;37m"
	ansiCyan   = "\033[36m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiRed    = "\033[31m"
	ansiClear  = "\033[K\n"
)

func render(buf *bytes.Buffer, w *world) {
	buf.Reset()
	buf.WriteString("\033[H")

	s := w.engine.Stats()
	fmt.Fprintf(buf, "%s genetic lander %s| gen %s%d%s | evals %s%d%s | pending %s%d%s | evicted %s%d%s | archive %s%d%s%s",
		ansiBold, ansiReset, ansiCyan, s.Generation, ansiReset, ansiCyan, s.Evaluations, ansiReset,
		ansiCyan, s.Pending, ansiReset, ansiRed, s.Evicted, ansiReset, ansiCyan, s.Size, ansiReset, ansiClear)
	fmt.Fprintf(buf, " best %s%.3f%s | avg %s%.3f%s | worst %s%.3f%s | spread %s%.3f%s%s",
		ansiGreen, s.BestScore, ansiReset, ansiCyan, s.AverageScore, ansiReset,
		ansiRed, s.WorstScore, ansiReset, ansiCyan, s.Diversity, ansiReset, ansiClear)
	if w.best.Data == nil {
		buf.WriteString(" waiting for the first touchdown" + ansiClear)
	} else {
		g, r := w.best.Data, w.bestRun
		fmt.Fprintf(buf, " champion %s@%s KpX %.2f KdX %.2f KpY %.2f KdY %.2f -> %.1f off pad at %.3f/tick, %.1f fuel, %d ticks%s",
			ansiCyan, ansiReset, g[geneKpX], g[geneKdX], g[geneKpY], g[geneKdY],
			math.Abs(r.x-w.targetX), r.speed(), r.fuel, r.ticks, ansiClear)
	}
	fmt.Fprintf(buf, " %s[<] [>]%s move pad   %s[q]%s quit%s%s", ansiYellow, ansiReset, ansiYellow, ansiReset, ansiClear, ansiClear)

	var field [fieldHeight][fieldWidth]byte
	plot := func(l *lander, glyph byte) {
		if !l.flying() {
			return
		}
		col, row := int(l.x), fieldHeight-1-int(l.y)
		if col >= 0 && col < fieldWidth && row >= 0 && row < fieldHeight {
			field[row][col] = glyph
		}
	}
	for i := range w.landers {
		plot(&w.landers[i], 'A')
	}
	plot(&w.champion, '@')

	for row := range field {
		for _, c := range field[row] {
			switch c {
			case 'A':
				buf.WriteString(ansiYellow + "A" + ansiReset)
			case '@':
				buf.WriteString(ansiBold + "@" + ansiReset)
			default:
				buf.WriteByte(' ')
			}
		}
		buf.WriteString(ansiClear)
	}

	// Ground row: the pad, and recent touchdowns as '.' (soft) or 'x' (hard)
	pad := int(w.targetX)
	for col := range fieldWidth {
		m := w.marks[col]
		switch {
		case col == pad:
			buf.WriteString(ansiRed + "o" + ansiReset)
		case col == pad-padHalf || col == pad+padHalf:
			buf.WriteString(ansiBold + "|" + ansiReset)
		case col > pad-padHalf && col < pad+padHalf:
			buf.WriteString(ansiGreen + "=" + ansiReset)
		case m.until > w.tick && m.soft:
			buf.WriteString(ansiGreen + "." + ansiReset)
		case m.until > w.tick:
			buf.WriteString(ansiRed + "x" + ansiReset)
		default:
			buf.WriteString(ansiDim + "_" + ansiReset)
		}
	}
	buf.WriteString(ansiClear)

	_, _ = os.Stdout.Write(buf.Bytes())
}
