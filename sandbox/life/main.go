// Command life evolves patterns that cross a Game of Life field to a goal. Eight
// sources around the field each run a genetic.StreamingEngine over a 5x5 cell
// pattern; a pattern stamped at its source lives by the Life rules among every
// other source's cells and scores by how close its descendants come to the goal.
// hjkl move the goal and re-score every archive; z zooms; -headless N tabulates.
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
	fieldW, fieldH = 160, 80
	cols, rows     = 80, 20
	cellW, cellH   = fieldW / cols, fieldH / rows
	stride         = fieldW + 2 // The field is padded by a dead border
	side           = 5
	genes          = side * side
	maxAge         = 600
	reachR2        = 5
	launchEvery    = 16
	margin         = 3 // The box around a pad that a stamp needs clear
	maxLive        = 16
	sourceCount    = 8
	maxProbes      = sourceCount * maxLive
	wild           = maxProbes + 1 // The lineage of a cell born to mixed parents
	flashTicks     = 30
	hashSeed       = 1469598103934665603
	hashPrime      = 1099511628211
	frame          = 16 * time.Millisecond
)

// A source is a population and the pad it stamps its patterns on. The four
// edge sources face the centre along a row or column, the four others along a
// diagonal, so every source has a lane to the starting goal and a moved goal
// keeps some and loses others
type source struct {
	name   string
	x, y   int
	colour string
}

const (
	padMid  = fieldH/2 - side/2
	padNear = 2
	padFar  = fieldH - 2 - side
	padDiag = fieldW/2 - side/2 - (fieldH/2 - padNear - side/2)
)

var sources = [sourceCount]source{
	{"L", padNear, padMid, "196"},
	{"R", fieldW - 2 - side, padMid, "33"},
	{"T", fieldW/2 - side/2, padNear, "46"},
	{"B", fieldW/2 - side/2, padFar, "201"},
	{"TL", padDiag, padNear, "226"},
	{"TR", fieldW - side - padDiag, padNear, "51"},
	{"BL", padDiag, padFar, "252"},
	{"BR", fieldW - side - padDiag, padFar, "208"},
}

func idx(x, y int) int { return (y+1)*stride + x + 1 }

// bitFlip is a Perturbator over bit patterns: each bit flips with probability
// rate. strength scales a real gene's step and has no meaning for a bit
type bitFlip struct{}

func (bitFlip) Perturb(g *[]bool, rate, _ float64, rng *rand.Rand) {
	for i := range *g {
		if rng.Float64() < rate {
			(*g)[i] = !(*g)[i]
		}
	}
}

// bitSpread is the archive's diversity: the mean over bits of 4p(1-p), 0 when
// every member is the same pattern and 1 when every bit splits the archive evenly
func bitSpread(members []genetic.Candidate[[]bool, float64]) float64 {
	if len(members) < 2 {
		return 0
	}
	total := 0.0
	for j := range genes {
		ones := 0
		for _, m := range members {
			if m.Data[j] {
				ones++
			}
		}
		p := float64(ones) / float64(len(members))
		total += 4 * p * (1 - p)
	}
	return total / genes
}

func newEngine(seed uint64) *genetic.StreamingEngine[[]bool, float64] {
	cfg := genetic.DefaultStreamingConfig()
	cfg.PoolSize = 16
	// A shallow queue keeps proposals bred from the current archive
	cfg.ProposalCapacity = 8
	// Four times the patterns a source may have alive, so a window never fills
	cfg.PendingCapacity = 4 * maxLive
	cfg.MinOutcomesPerGen = 8
	// One bit flips per child on average
	cfg.PerturbationRate = 1.0 / genes
	cfg.Seed = seed

	e := genetic.NewStreamingEngine[[]bool, float64](
		func(rng *rand.Rand) []bool {
			g := make([]bool, genes)
			for i := range g {
				g[i] = rng.IntN(3) == 0
			}
			return g
		},
		&genetic.TournamentSelector[[]bool, float64]{TournamentSize: 3},
		&genetic.UniformCombiner[[]bool, bool, float64]{MixProbability: 0.95},
		bitFlip{},
		genetic.SliceCloner[[]bool, bool]{},
		cfg,
	)
	// Without duplicate rejection an elitist archive fills with copies of its best
	e.SetEqual(slices.Equal[[]bool])
	e.SetDiversity(bitSpread)
	e.Start()
	return e
}

// A probe is one stamped pattern under evaluation: every cell descended from it
// carries its lineage, the probe slot plus one, until the lineage dies out.
// Life is deterministic, so a lineage no other cell ever touched ran exactly as
// it would alone; one that was touched is scored by that lone run instead
type probe struct {
	source  int
	id      genetic.EvalID
	born    int
	cells   int
	minD2   int
	d0      float64
	hist    [3]uint64 // The lone run's last three cell sets, hashed
	genes   []bool
	touched bool // A cell of another lineage was adjacent at some tick
	reached bool
	abandon bool
	live    bool
}

// settle folds a lone pattern's cell set into h and reports whether it is still
// or oscillates with a period of two or three, when it can come no closer.
// Cells arrive in scan order, so the hash is the same for the same set
func (p *probe) settle(h uint64) bool {
	still := slices.Contains(p.hist[:], h)
	p.hist = [3]uint64{h, p.hist[0], p.hist[1]}
	return still
}

// score is closeness: how much of the distance from pad to goal the lineage
// covered, 0 for none and 1 for all. Arrival scores 2 plus a bonus for speed
func (p *probe) score(now int) float64 {
	if p.reached {
		return 2 + 0.5*(1-float64(now-p.born)/maxAge)
	}
	if p.minD2 == math.MaxInt || p.d0 <= 0 {
		return 0
	}
	return max(0, 1-math.Sqrt(float64(p.minD2))/p.d0)
}

// The field is a ring of five grids, tick t's at t mod 5: the current one, the
// two and three back that the test for what stands still reads, and the oldest
// as the next to write
type world struct {
	engines   [sourceCount]*genetic.StreamingEngine[[]bool, float64]
	grids     [5][]uint16
	cur       []uint16
	probes    [maxProbes]probe
	liveOf    [sourceCount]int
	nextStamp [sourceCount]int
	arrivals  [sourceCount]int
	hit       [sourceCount]int
	since     [sourceCount]int // arrivals when the goal last moved
	tick      int
	goalX     int
	goalY     int
	flash     int // The goal shows the last arrival's colour until this tick
	flashSrc  int
	zoom      bool
	lone      lone
}

func newWorld(seed uint64) *world {
	w := &world{goalX: fieldW / 2, goalY: fieldH / 2}
	for i := range w.grids {
		w.grids[i] = make([]uint16, stride*(fieldH+2))
	}
	w.cur = w.grids[0]
	for i := range w.engines {
		w.engines[i] = newEngine(seed*sourceCount + uint64(i))
	}
	w.lone.init()
	return w
}

// distance is from a source's pad centre to the goal
func (w *world) distance(s int) float64 {
	return math.Hypot(float64(sources[s].x+side/2-w.goalX), float64(sources[s].y+side/2-w.goalY))
}

func stamp(field []uint16, s int, g []bool, lineage uint16) {
	for y := range side {
		for x := range side {
			if g[y*side+x] {
				field[idx(sources[s].x+x, sources[s].y+y)] = lineage
			}
		}
	}
}

// launch stamps a source's next proposal once it is due, a slot is free and the
// pad box is clear, so a stamp never meets what came before it
func (w *world) launch(s int) {
	if w.tick < w.nextStamp[s] || w.liveOf[s] >= maxLive {
		return
	}
	for y := max(sources[s].y-margin, 0); y < min(sources[s].y+side+margin, fieldH); y++ {
		for x := max(sources[s].x-margin, 0); x < min(sources[s].x+side+margin, fieldW); x++ {
			if w.cur[idx(x, y)] != 0 {
				return
			}
		}
	}
	g, id := w.engines[s].Propose()
	if id == 0 {
		return
	}
	slot := slices.IndexFunc(w.probes[:], func(p probe) bool { return !p.live })
	w.probes[slot] = probe{source: s, id: id, born: w.tick, minD2: math.MaxInt, d0: w.distance(s), genes: g, live: true}
	w.liveOf[s]++
	w.nextStamp[s] = w.tick + launchEvery
	stamp(w.cur, s, g, uint16(slot+1))
}

// step applies the Life rules with one addition: a live cell whose 5x5 window
// is what it was two or three ticks ago stands in a still life, a blinker or a
// pulsar and dies, so debris clears while ships, of period four, never match.
// A cell born to three parents of one lineage joins it; one born to mixed
// parents is wild, owned by nobody, so a collision cancels patterns
func (w *world) step() {
	cur, nxt := w.cur, w.grids[w.tick%5]
	old2, old3 := w.grids[(w.tick+2)%5], w.grids[(w.tick+1)%5]
	for y := range fieldH {
		for x := range fieldW {
			i := idx(x, y)
			n := 0
			own, mixed := cur[i], false
			var first uint16
			for _, j := range [8]int{i - stride - 1, i - stride, i - stride + 1, i - 1, i + 1, i + stride - 1, i + stride, i + stride + 1} {
				l := cur[j]
				if l == 0 {
					continue
				}
				if n == 0 {
					first = l
				}
				n++
				if l != first {
					mixed = true
				}
				if own != 0 && own != wild && l != own {
					w.probes[own-1].touched = true
				}
			}
			switch {
			case own != 0 && (n == 2 || n == 3) && !still(cur, old2, x, y) && !still(cur, old3, x, y):
				nxt[i] = own
			case own == 0 && n == 3 && mixed:
				nxt[i] = wild
			case own == 0 && n == 3:
				nxt[i] = first
			default:
				nxt[i] = 0
			}
		}
	}
	w.cur = nxt
}

// still reports whether a cell's 5x5 window reads the same in both grids
func still(cur, old []uint16, x, y int) bool {
	for yy := max(y-2, 0); yy <= min(y+2, fieldH-1); yy++ {
		x0, x1 := max(x-2, 0), min(x+2, fieldW-1)
		if !slices.Equal(cur[idx(x0, yy):idx(x1, yy)+1], old[idx(x0, yy):idx(x1, yy)+1]) {
			return false
		}
	}
	return true
}

// account reads every lineage's cell count and closest approach off the field,
// then retires the probes that died, arrived or aged out
func (w *world) account() {
	for i := range w.probes {
		w.probes[i].cells = 0
	}
	for y := range fieldH {
		for x := range fieldW {
			l := w.cur[idx(x, y)]
			if l == 0 || l == wild {
				continue
			}
			p := &w.probes[l-1]
			p.cells++
			dx, dy := x-w.goalX, y-w.goalY
			if d2 := dx*dx + dy*dy; d2 < p.minD2 {
				p.minD2 = d2
				p.reached = p.reached || d2 <= reachR2
			}
		}
	}
	for slot := range w.probes {
		p := &w.probes[slot]
		if !p.live || (p.cells > 0 && !p.reached && w.tick-p.born < maxAge) {
			continue
		}
		if p.cells > 0 {
			w.clearLineage(uint16(slot + 1))
		}
		p.live = false
		w.liveOf[p.source]--
		if p.reached {
			w.arrivals[p.source]++
			w.flash, w.flashSrc = w.tick+flashTicks, p.source
		}
		if p.abandon {
			continue
		}
		score := p.score(w.tick)
		if p.touched {
			w.hit[p.source]++
			score = w.lone.score(p.source, p.genes, w.goalX, w.goalY)
		}
		w.engines[p.source].CompleteEvaluation(p.id, score)
	}
}

func (w *world) clearLineage(lineage uint16) {
	for i := range w.cur {
		if w.cur[i] == lineage {
			w.cur[i] = 0
		}
	}
}

func (w *world) advance() {
	w.tick++
	for s := range sources {
		w.launch(s)
	}
	w.step()
	w.account()
}

// retarget moves the goal. Patterns in flight were scored against the old goal
// and are abandoned; each archive is re-scored by running its members alone
// against the new one, so what was learned survives the objective change
func (w *world) retarget(dx, dy int) {
	x := min(max(w.goalX+dx, 0), fieldW-1)
	y := min(max(w.goalY+dy, 0), fieldH-1)
	if x == w.goalX && y == w.goalY {
		return
	}
	w.goalX, w.goalY = x, y
	for i := range w.probes {
		if p := &w.probes[i]; p.live && !p.abandon {
			p.abandon = true
			w.engines[p.source].AbandonEvaluation(p.id)
		}
	}
	for s, e := range w.engines {
		pool := e.Snapshot()
		for i := range pool.Members {
			pool.Members[i].Score = w.lone.score(s, pool.Members[i].Data, x, y)
		}
		e.Inject(pool.Members, pool.Generation)
		w.since[s] = w.arrivals[s]
	}
}

// lone runs one pattern alone on an empty field, stepping only the box around
// its live cells, and scores it as the field would: a moved goal re-ranks an
// archive at once, and a lineage another pattern touched is scored by the run
// it would have made
type lone struct {
	a, b []uint8
}

func (l *lone) init() {
	l.a = make([]uint8, stride*(fieldH+2))
	l.b = make([]uint8, stride*(fieldH+2))
}

func (l *lone) score(s int, g []bool, goalX, goalY int) float64 {
	x0, y0 := sources[s].x, sources[s].y
	for y := range side {
		for x := range side {
			if g[y*side+x] {
				l.a[idx(x0+x, y0+y)] = 1
			}
		}
	}
	p := probe{minD2: math.MaxInt, d0: math.Hypot(float64(x0+side/2-goalX), float64(y0+side/2-goalY))}
	bx0, by0, bx1, by1 := x0, y0, x0+side, y0+side
	age := 0
	for ; age < maxAge; age++ {
		// The region is the live box grown by one, where births can happen
		rx0, ry0 := max(bx0-1, 0), max(by0-1, 0)
		rx1, ry1 := min(bx1+1, fieldW), min(by1+1, fieldH)
		cells, h := 0, uint64(hashSeed)
		bx0, by0, bx1, by1 = fieldW, fieldH, 0, 0
		for y := ry0; y < ry1; y++ {
			for x := rx0; x < rx1; x++ {
				if l.a[idx(x, y)] == 0 {
					continue
				}
				cells++
				h = (h ^ uint64(idx(x, y))) * hashPrime
				bx0, by0, bx1, by1 = min(bx0, x), min(by0, y), max(bx1, x+1), max(by1, y+1)
				dx, dy := x-goalX, y-goalY
				if d2 := dx*dx + dy*dy; d2 < p.minD2 {
					p.minD2 = d2
					p.reached = p.reached || d2 <= reachR2
				}
			}
		}
		// A dead, settled or arrived pattern can come no closer
		if cells == 0 || p.settle(h) || p.reached {
			break
		}
		for y := ry0; y < ry1; y++ {
			for x := rx0; x < rx1; x++ {
				i := idx(x, y)
				n := l.a[i-stride-1] + l.a[i-stride] + l.a[i-stride+1] + l.a[i-1] + l.a[i+1] +
					l.a[i+stride-1] + l.a[i+stride] + l.a[i+stride+1]
				l.b[i] = 0
				if n == 3 || (n == 2 && l.a[i] == 1) {
					l.b[i] = 1
				}
			}
		}
		for y := ry0; y < ry1; y++ {
			clear(l.a[idx(rx0, y):idx(rx1, y)])
		}
		l.a, l.b = l.b, l.a
	}
	for y := max(by0-1, 0); y < min(by1+1, fieldH); y++ {
		clear(l.a[idx(0, y):idx(fieldW, y)])
	}
	return p.score(age)
}

func main() {
	seed := flag.Uint64("seed", 1, "PCG seed; the same seed and keys replay the same run")
	headless := flag.Int("headless", 0, "run this many ticks without the view, moving the goal once halfway")
	speed := flag.Int("speed", 4, "Life generations per frame")
	flag.Parse()

	w := newWorld(*seed)
	if *headless > 0 {
		runHeadless(w, *headless)
		return
	}
	runInteractive(w, max(*speed, 1))
}

// pattern prints a genome as one group per row, '#' for a live cell
func pattern(g []bool) string {
	b := make([]byte, 0, genes+side)
	for i, v := range g {
		if i > 0 && i%side == 0 {
			b = append(b, ' ')
		}
		if v {
			b = append(b, '#')
		} else {
			b = append(b, '.')
		}
	}
	return string(b)
}

func runHeadless(w *world, ticks int) {
	var last, lastHit [sourceCount]int
	for w.tick < ticks {
		w.advance()
		if w.tick == ticks/2 {
			// Down the diagonal two sources face; the others lose their lane
			w.retarget(16, 16)
			fmt.Printf("goal moved to %d,%d\n", w.goalX, w.goalY)
		}
		if w.tick%2000 != 0 && w.tick != ticks {
			continue
		}
		fmt.Printf("  tick src  gen evals evicted   best    avg spread   hit arrivals  best pattern\n")
		for s, e := range w.engines {
			st := e.Stats()
			best, _ := e.Best()
			fmt.Printf("%6d %-3s %4d %5d %7d  %.3f  %.3f  %.3f %5d %8d  %s\n",
				w.tick, sources[s].name, st.Generation, st.Evaluations, st.Evicted, st.BestScore, st.AverageScore, st.Diversity,
				w.hit[s]-lastHit[s], w.arrivals[s]-last[s], pattern(best.Data))
			last[s], lastHit[s] = w.arrivals[s], w.hit[s]
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

	buf := bytes.NewBuffer(make([]byte, 0, 32<<10))
	for {
		select {
		case <-sig:
			return
		case k := <-keys:
			dx, dy := cellW, cellH
			if w.zoom {
				dx, dy = 1, 1
			}
			switch k {
			case 'q':
				return
			case 'z':
				w.zoom = !w.zoom
			case 'h':
				w.retarget(-dx, 0)
			case 'l':
				w.retarget(dx, 0)
			case 'k':
				w.retarget(0, -dy)
			case 'j':
				w.retarget(0, dy)
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

// readKeys maps q, z, hjkl and the arrow keys onto single bytes
func readKeys(keys chan<- byte) {
	b := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(b)
		if err != nil {
			return
		}
		k := b[0]
		if n == 3 && b[0] == 27 && b[1] == '[' {
			k = map[byte]byte{'A': 'k', 'B': 'j', 'C': 'l', 'D': 'h'}[b[2]]
		}
		switch k {
		case 'q', 'Q':
			keys <- 'q'
		case 'z', 'Z':
			keys <- 'z'
		case 'h', 'j', 'k', 'l':
			keys <- k
		}
	}
}

const (
	ansiReset = "\033[0m"
	ansiBold  = "\033[1m"
	ansiDim   = "\033[2m"
	ansiClear = "\033[K\n"
)

func colour(s int) string { return "\033[38;5;" + sources[s].colour + "m" }

func render(buf *bytes.Buffer, w *world) {
	buf.Reset()
	buf.WriteString("\033[H")

	view := "heat"
	if w.zoom {
		view = "zoom"
	}
	fmt.Fprintf(buf, "%s life%s | tick %d | goal %d,%d | %s | %shjkl%s move goal  %sz%s view  %sq%s quit%s",
		ansiBold, ansiReset, w.tick, w.goalX, w.goalY, view, ansiBold, ansiReset, ansiBold, ansiReset, ansiBold, ansiReset, ansiClear)
	for s, e := range w.engines {
		st := e.Stats()
		fmt.Fprintf(buf, "%s%s%-2s%s g%-3d %.2f/%.2f %s^%-3d%s",
			colour(s), "■", sources[s].name, ansiReset, st.Generation, st.BestScore, st.AverageScore,
			colour(s), w.arrivals[s]-w.since[s], ansiReset)
		if s%4 == 3 {
			buf.WriteString(ansiClear)
		} else {
			buf.WriteByte(' ')
		}
	}
	buf.WriteString(ansiClear)

	if w.zoom {
		renderZoom(buf, w)
	} else {
		renderHeat(buf, w)
	}
	buf.WriteString(ansiReset)
	_, _ = os.Stdout.Write(buf.Bytes())
}

// renderHeat draws each 2x4 block of cells as one character: the glyph is how
// many are alive, the colour is the source holding most of them
func renderHeat(buf *bytes.Buffer, w *world) {
	glyphs := [...]string{" ", "░", "░", "▒", "▒", "▓", "▓", "█", "█"}
	goalCX, goalCY := w.goalX/cellW, w.goalY/cellH
	var pads [sourceCount][2]int
	for s := range sources {
		pads[s] = [2]int{(sources[s].x + side/2) / cellW, (sources[s].y + side/2) / cellH}
	}
	last := ""
	for cy := range rows {
		for cx := range cols {
			var count [sourceCount + 1]int
			total, dominant := 0, 0
			for y := cy * cellH; y < (cy+1)*cellH; y++ {
				for x := cx * cellW; x < (cx+1)*cellW; x++ {
					if l := w.cur[idx(x, y)]; l != 0 {
						s := sourceCount
						if l != wild {
							s = w.probes[l-1].source
						}
						count[s]++
						total++
						if count[s] > count[dominant] {
							dominant = s
						}
					}
				}
			}
			sgr, glyph := ansiDim, glyphs[total]
			switch {
			case cx == goalCX && cy == goalCY:
				sgr, glyph = "\033[7m", "+"
				if w.tick < w.flash {
					sgr += colour(w.flashSrc)
				}
			case total > 0 && dominant == sourceCount:
				sgr = ansiDim
			case total > 0:
				sgr = colour(dominant)
			default:
				for s := range sources {
					if pads[s] == [2]int{cx, cy} {
						sgr, glyph = colour(s), "S"
					}
				}
			}
			if sgr != last {
				buf.WriteString(ansiReset + sgr)
				last = sgr
			}
			buf.WriteString(glyph)
		}
		last = ""
		buf.WriteString(ansiReset + ansiClear)
	}
}

// renderZoom draws the cells around the goal one to a character, in their
// source's colour, so the patterns that converge can be read
func renderZoom(buf *bytes.Buffer, w *world) {
	x0 := min(max(w.goalX-cols/2, 0), fieldW-cols)
	y0 := min(max(w.goalY-rows/2, 0), fieldH-rows)
	last := ""
	for y := y0; y < y0+rows; y++ {
		for x := x0; x < x0+cols; x++ {
			sgr, glyph := ansiDim, " "
			switch l := w.cur[idx(x, y)]; {
			case x == w.goalX && y == w.goalY:
				sgr, glyph = "\033[7m", "+"
				if w.tick < w.flash {
					sgr += colour(w.flashSrc)
				}
			case l == wild:
				glyph = "█"
			case l != 0:
				sgr, glyph = colour(w.probes[l-1].source), "█"
			}
			if sgr != last {
				buf.WriteString(ansiReset + sgr)
				last = sgr
			}
			buf.WriteString(glyph)
		}
		last = ""
		buf.WriteString(ansiReset + ansiClear)
	}
}
