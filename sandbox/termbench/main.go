// Command termbench compares the terminal module with tcell v2 on what a game
// pays per frame: encoding (CPU, bytes, allocations) headless, the loop's own
// cost against a live terminal, and the raw input a console delivers.
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"
	"unsafe"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

const usage = `termbench encode [flags]   headless: CPU, bytes and allocations a frame
termbench live -lib L [flags]  a real terminal: the loop's cost, events received
termbench rawin [flags]    the bytes or console records input arrives as
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "encode":
		err = runEncode(os.Args[2:])
	case "live":
		err = runLive(os.Args[2:])
	case "rawin":
		err = runRawIn(os.Args[2:])
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "termbench:", err)
		os.Exit(1)
	}
}

// --- scenes ---

// A scene is a looped sequence of whole frames, built before timing starts
type scene struct {
	name   string
	w, h   int
	frames [][]terminal.Cell
}

var sceneNames = []string{"idle", "sparse", "plasma", "text", "pal256"}

var (
	bgField  = color.RGB{R: 10, G: 12, B: 18}
	fgWall   = color.RGB{R: 90, G: 96, B: 110}
	bgStatus = color.RGB{R: 28, G: 30, B: 40}
	fgStatus = color.RGB{R: 200, G: 200, B: 210}
	bgBadge  = color.RGB{R: 40, G: 110, B: 60}
	bgFps    = color.RGB{R: 30, G: 80, B: 120}
)

// buildScene makes n frames of kind at w x h, deterministic for a seed
func buildScene(kind string, w, h, n int) (*scene, error) {
	if !slices.Contains(sceneNames, kind) {
		return nil, fmt.Errorf("unknown scene %q (have %s)", kind, strings.Join(sceneNames, ","))
	}
	s := &scene{name: kind, w: w, h: h}
	rng := rand.New(rand.NewPCG(1, 2))
	base := field(w, h, rng)
	sprites := make([]sprite, w*h/40)
	for i := range sprites {
		sprites[i] = sprite{x: rng.Float64() * float64(w), y: rng.Float64() * float64(h-1),
			dx: rng.Float64()*1.6 - 0.8, dy: rng.Float64()*0.8 - 0.4,
			c: color.RGB{R: uint8(128 + rng.IntN(128)), G: uint8(rng.IntN(256)), B: uint8(64 + rng.IntN(192))}}
	}
	trail := make([]float64, w*h)
	for f := range n {
		cells := make([]terminal.Cell, w*h)
		switch kind {
		case "idle":
			copy(cells, base)
		case "sparse":
			copy(cells, base)
			moveSprites(cells, sprites, trail, w, h)
		case "plasma":
			plasma(cells, w, h, f, false)
		case "pal256":
			plasma(cells, w, h, f, true)
		case "text":
			text(cells, w, h, f)
		}
		statusBar(cells, w, h, f)
		s.frames = append(s.frames, cells)
	}
	return s, nil
}

// field is a dark map with walls, the backdrop most game frames share
func field(w, h int, rng *rand.Rand) []terminal.Cell {
	cells := make([]terminal.Cell, w*h)
	for i := range cells {
		cells[i] = terminal.Cell{Rune: ' ', Fg: fgWall, Bg: bgField}
	}
	for range w * h / 25 {
		x, y, l := rng.IntN(w), rng.IntN(h-1), 3+rng.IntN(12)
		for k := range l {
			if x+k < w {
				cells[y*w+x+k].Rune = '▓'
			}
		}
	}
	return cells
}

type sprite struct {
	x, y, dx, dy float64
	c            color.RGB
}

// moveSprites steps each sprite and draws it over a decaying trail, the shape of
// bullets, particles and their glow: many cells, many distinct colors
func moveSprites(cells []terminal.Cell, sp []sprite, trail []float64, w, h int) {
	for i := range trail {
		trail[i] *= 0.82
	}
	for i := range sp {
		s := &sp[i]
		s.x, s.y = s.x+s.dx, s.y+s.dy
		if s.x < 0 || s.x >= float64(w) {
			s.dx, s.x = -s.dx, math.Max(0, math.Min(float64(w)-1, s.x))
		}
		if s.y < 0 || s.y >= float64(h-1) {
			s.dy, s.y = -s.dy, math.Max(0, math.Min(float64(h-2), s.y))
		}
		trail[int(s.y)*w+int(s.x)] = 1
	}
	for i, t := range trail {
		if t > 0.05 {
			cells[i].Bg = color.Blend(bgField, color.RGB{R: 255, G: 140, B: 40}, t*0.7)
		}
	}
	for _, s := range sp {
		c := &cells[int(s.y)*w+int(s.x)]
		c.Rune, c.Fg, c.Attrs = '●', s.c, terminal.AttrBold
	}
}

// plasma changes every cell every frame with a smooth field of colors, the worst
// case of a full-screen effect; pal gives it as palette indices, as the 256-color
// renderers draw
func plasma(cells []terminal.Cell, w, h, f int, pal bool) {
	t := float64(f) * 0.08
	shades := []rune{' ', '░', '▒', '▓'}
	for y := range h - 1 {
		for x := range w {
			v := math.Sin(float64(x)*0.09+t) + math.Sin(float64(y)*0.17-t*1.3) + math.Sin(float64(x+y)*0.05+t*0.7)
			v = (v + 3) / 6
			c := &cells[y*w+x]
			c.Rune = shades[int(v*3.99)]
			c.Bg = color.RGB{R: uint8(40 + 180*v), G: uint8(30 + 120*(1-v)), B: uint8(90 + 120*math.Abs(math.Sin(t+v*3)))}
			c.Fg = color.Scale(c.Bg, 1.6)
			if pal {
				c.Fg, c.Bg = color.RGB{R: color.RGBTo256(c.Fg)}, color.RGB{R: color.RGBTo256(c.Bg)}
				c.Attrs = terminal.AttrFg256 | terminal.AttrBg256
			}
		}
	}
}

const lorem = "the quick brown fox jumps over the lazy dog 0123456789 sphinx of black quartz judge my vow "

// text scrolls a line a frame: every cell's rune changes, one style throughout
func text(cells []terminal.Cell, w, h, f int) {
	for y := range h - 1 {
		line := (y + f) * 7
		for x := range w {
			cells[y*w+x] = terminal.Cell{Rune: rune(lorem[(line+x)%len(lorem)]), Fg: fgStatus, Bg: bgField}
		}
	}
}

// statusBar draws the last row: a mode badge, a frame counter that changes every
// frame, a clock in tenths and an FPS figure, as the game's status bar ticks
func statusBar(cells []terminal.Cell, w, h, f int) {
	row := cells[(h-1)*w : h*w]
	for i := range row {
		row[i] = terminal.Cell{Rune: ' ', Fg: fgStatus, Bg: bgStatus}
	}
	put := func(x int, s string, fg, bg color.RGB, a terminal.Attr) int {
		for _, r := range s {
			if x >= 0 && x < w {
				row[x] = terminal.Cell{Rune: r, Fg: fg, Bg: bg, Attrs: a}
			}
			x++
		}
		return x
	}
	put(0, " NORMAL ", color.RGB{R: 230, G: 255, B: 230}, bgBadge, terminal.AttrBold)
	right := fmt.Sprintf(" frame %06d  %5.1fs  FPS: %d ", f, float64(f)/60, 58+f/30%3)
	put(w-len(right), right, fgStatus, bgFps, 0)
}

// loadCapture reads frames dumped from a game: each record is width and height as
// little-endian uint16, then width*height cells as they lie in memory (12 bytes)
func loadCapture(path string) (*scene, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	s := &scene{name: "capture"}
	for {
		var dim [2]uint16
		if err := binary.Read(f, binary.LittleEndian, &dim); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, err
		}
		w, h := int(dim[0]), int(dim[1])
		if s.w != 0 && (w != s.w || h != s.h) {
			return nil, fmt.Errorf("capture changes size %dx%d to %dx%d", s.w, s.h, w, h)
		}
		s.w, s.h = w, h
		cells := make([]terminal.Cell, w*h)
		if _, err := io.ReadFull(f, unsafe.Slice((*byte)(unsafe.Pointer(&cells[0])), len(cells)*int(unsafe.Sizeof(terminal.Cell{})))); err != nil {
			return nil, err
		}
		s.frames = append(s.frames, cells)
	}
	if len(s.frames) == 0 {
		return nil, errors.New("capture holds no frame")
	}
	return s, nil
}

// --- encode ---

type encodeResult struct {
	nsFrame, nsCaller     float64 // median over runs
	minNs                 float64
	bytesFrame, allocsFrm float64
	allocBytes            float64
}

func runEncode(args []string) error {
	fs := flag.NewFlagSet("encode", flag.ExitOnError)
	scenes := fs.String("scenes", strings.Join(sceneNames, ","), "scenes to run")
	sizes := fs.String("sizes", "120x40,240x66", "WxH sizes")
	modes := fs.String("modes", "tc,256", "color modes: tc, 256")
	libs := fs.String("libs", "terminal,tcell", "terminal, tcell (app diffs, Puts changed cells), tcell-full (Puts every cell)")
	frames := fs.Int("frames", 600, "frames a run")
	runs := fs.Int("runs", 7, "runs; the median is reported")
	procs := fs.Int("procs", 1, "GOMAXPROCS: 1 counts the terminal module's writer goroutine in the frame")
	capture := fs.String("capture", "", "a frame dump to run instead of the scenes (see loadCapture)")
	fs.Parse(args)
	runtime.GOMAXPROCS(*procs)

	var sc []*scene
	if *capture != "" {
		s, err := loadCapture(*capture)
		if err != nil {
			return err
		}
		sc = append(sc, s)
	} else {
		for _, sz := range strings.Split(*sizes, ",") {
			w, h, err := parseSize(sz)
			if err != nil {
				return err
			}
			for _, k := range strings.Split(*scenes, ",") {
				s, err := buildScene(k, w, h, 120)
				if err != nil {
					return err
				}
				sc = append(sc, s)
			}
		}
	}

	fmt.Printf("%s %s/%s GOMAXPROCS=%d, %d frames x %d runs, median\n", runtime.Version(), runtime.GOOS, runtime.GOARCH, *procs, *frames, *runs)
	fmt.Printf("%-8s %-7s %-4s %-10s %10s %10s %10s %9s %9s %11s %7s\n",
		"scene", "size", "mode", "lib", "us/frame", "min", "caller", "B/frame", "allocs/f", "allocB/f", "x term")
	for _, s := range sc {
		for _, m := range strings.Split(*modes, ",") {
			mode, err := parseMode(m)
			if err != nil {
				return err
			}
			var ref float64
			for _, l := range strings.Split(*libs, ",") {
				r, err := encode(l, s, mode, *frames, *runs)
				if err != nil {
					return err
				}
				if l == "terminal" {
					ref = r.nsFrame
				}
				ratio := "-"
				if ref > 0 {
					ratio = fmt.Sprintf("%.2f", r.nsFrame/ref)
				}
				fmt.Printf("%-8s %-7s %-4s %-10s %10.1f %10.1f %10.1f %9.0f %9.1f %11.0f %7s\n",
					s.name, fmt.Sprintf("%dx%d", s.w, s.h), m, l, r.nsFrame/1e3, r.minNs/1e3, r.nsCaller/1e3,
					r.bytesFrame, r.allocsFrm, r.allocBytes, ratio)
			}
		}
	}
	return nil
}

// encode runs frames of s through lib against a sink that counts and drops bytes
func encode(lib string, s *scene, mode terminal.ColorMode, frames, runs int) (encodeResult, error) {
	sink, err := newSink(lib, s.w, s.h, mode)
	if err != nil {
		return encodeResult{}, err
	}
	defer sink.close()

	// One index runs through warm-up and every run, so frame k always follows k-1
	// and whether it changed a cell is known before timing starts
	nf := len(s.frames)
	changed := make([]bool, nf)
	for i := range nf {
		changed[i] = !slices.Equal(s.frames[i], s.frames[(i+nf-1)%nf])
	}
	k := 0
	next := func() time.Duration {
		i := k % nf
		c := changed[i] || k == 0
		k++
		return sink.frame(s.frames[i], c)
	}
	for range min(60, nf) {
		next()
	}
	var times, callers []float64
	var bytes, mallocs, allocB uint64
	var ms0, ms1 runtime.MemStats
	for range runs {
		runtime.GC()
		runtime.ReadMemStats(&ms0)
		b0 := sink.bytes()
		var caller time.Duration
		t0 := time.Now()
		for range frames {
			caller += next()
		}
		el := time.Since(t0)
		runtime.ReadMemStats(&ms1)
		times = append(times, float64(el.Nanoseconds())/float64(frames))
		callers = append(callers, float64(caller.Nanoseconds())/float64(frames))
		bytes += sink.bytes() - b0
		mallocs += ms1.Mallocs - ms0.Mallocs
		allocB += ms1.TotalAlloc - ms0.TotalAlloc
	}
	n := float64(frames * runs)
	return encodeResult{
		nsFrame: median(times), nsCaller: median(callers), minNs: slices.Min(times),
		bytesFrame: float64(bytes) / n, allocsFrm: float64(mallocs) / n, allocBytes: float64(allocB) / n,
	}, nil
}

func median(v []float64) float64 {
	s := slices.Clone(v)
	slices.Sort(s)
	return s[len(s)/2]
}

func parseSize(s string) (int, int, error) {
	a, b, ok := strings.Cut(s, "x")
	w, e1 := strconv.Atoi(a)
	h, e2 := strconv.Atoi(b)
	if !ok || e1 != nil || e2 != nil || w < 20 || h < 3 {
		return 0, 0, fmt.Errorf("bad size %q", s)
	}
	return w, h, nil
}

func parseMode(s string) (terminal.ColorMode, error) {
	switch s {
	case "tc":
		return terminal.ColorModeTrueColor, nil
	case "256":
		return terminal.ColorMode256, nil
	case "16":
		return terminal.ColorMode16, nil
	}
	return 0, fmt.Errorf("bad mode %q (tc, 256, 16)", s)
}

// --- live ---

func runLive(args []string) error {
	fs := flag.NewFlagSet("live", flag.ExitOnError)
	lib := fs.String("lib", "terminal", "terminal or tcell")
	kind := fs.String("scene", "sparse", "scene: "+strings.Join(sceneNames, ","))
	m := fs.String("mode", "tc", "color mode: tc, 256")
	fps := fs.Int("fps", 60, "target frames a second, 0 for as fast as the loop goes")
	dur := fs.Duration("dur", 10*time.Second, "how long to run; q, Esc or Ctrl+C ends it sooner")
	mouse := fs.Bool("mouse", true, "ask for click, drag and motion reports")
	fs.Parse(args)
	mode, err := parseMode(*m)
	if err != nil {
		return err
	}

	scr, err := openScreen(*lib, mode)
	if err != nil {
		return err
	}
	if *mouse {
		scr.mouse()
	}
	modes := consoleModes()
	w, h := scr.size()
	s, err := buildScene(*kind, w, h, 120)
	if err != nil {
		scr.fini()
		return err
	}

	var interval time.Duration
	if *fps > 0 {
		interval = time.Second / time.Duration(*fps)
	}
	st := liveStats{}
	frame := make([]terminal.Cell, w*h)
	start := time.Now()
	next := start
	for f := 0; time.Since(start) < *dur; f++ {
		for ev, ok := scr.event(); ok; ev, ok = scr.event() {
			switch {
			case ev.quit:
				*dur = 0
			case ev.resize:
				w, h = scr.size()
				if s, err = buildScene(*kind, w, h, 120); err != nil {
					scr.fini()
					return err
				}
				frame = make([]terminal.Cell, w*h)
				st.resizes++
			case ev.mouse:
				st.mouse++
			default:
				st.keys++
			}
			if ev.text != "" {
				st.last = ev.text
			}
		}
		copy(frame, s.frames[f%len(s.frames)])
		overlay(frame, w, fmt.Sprintf(" %s %s | keys %d mouse %d | %s | last: %s ", *lib, *m, st.keys, st.mouse, st.summary(time.Since(start)), st.last))
		d, held := scr.frame(frame, w, h)
		st.add(d, held)
		if interval > 0 {
			next = next.Add(interval)
			if wait := time.Until(next); wait > 0 {
				time.Sleep(wait)
			} else {
				next = time.Now()
			}
		}
	}
	el := time.Since(start)
	scr.fini()

	fmt.Printf("%s %s/%s lib=%s scene=%s mode=%s size=%dx%d target=%dfps ran %v\n",
		runtime.Version(), runtime.GOOS, runtime.GOARCH, *lib, *kind, *m, w, h, *fps, el.Round(time.Millisecond))
	fmt.Println(st.report(el))
	fmt.Printf("events: keys %d, mouse %d, resizes %d; last: %s\n", st.keys, st.mouse, st.resizes, st.last)
	if modes != "" {
		fmt.Println(modes)
	}
	return nil
}

// overlay writes s over the first row, so a run shows its events and figures
func overlay(cells []terminal.Cell, w int, s string) {
	x := 0
	for _, r := range s {
		if x >= w {
			break
		}
		cells[x] = terminal.Cell{Rune: r, Fg: color.RGB{R: 255, G: 255, B: 255}, Bg: color.RGB{R: 90, G: 20, B: 60}}
		x++
	}
}

// liveStats keeps what the loop paid a frame and, for the terminal module, how
// many frames it held behind a write in flight and how many writes it started
type liveStats struct {
	lat             []time.Duration
	held, delivered int
	lastWrite       time.Time
	keys, mouse     int
	resizes         int
	last            string
	sumLat          time.Duration
}

// add takes one frame's caller latency and, from the terminal module, how long
// its write in flight had waited (zero when the frame was rendered at once)
func (s *liveStats) add(d time.Duration, held time.Duration) {
	s.lat = append(s.lat, d)
	s.sumLat += d
	switch {
	case held == 0:
		s.delivered++
	default:
		s.held++
		// A write that began since the last held frame carries that frame
		if ws := time.Now().Add(-held); ws.Sub(s.lastWrite) > 200*time.Microsecond {
			if !s.lastWrite.IsZero() {
				s.delivered++
			}
			s.lastWrite = ws
		}
	}
}

func (s *liveStats) summary(el time.Duration) string {
	if len(s.lat) == 0 {
		return ""
	}
	return fmt.Sprintf("loop %.0f fps, frame call %.0fus avg", float64(len(s.lat))/el.Seconds(), float64(s.sumLat.Microseconds())/float64(len(s.lat)))
}

func (s *liveStats) report(el time.Duration) string {
	if len(s.lat) == 0 {
		return "no frame"
	}
	l := slices.Clone(s.lat)
	slices.Sort(l)
	q := func(p float64) time.Duration { return l[min(len(l)-1, int(p*float64(len(l))))] }
	r := fmt.Sprintf("frames %d, loop %.1f fps; frame call p50 %v p90 %v p99 %v max %v",
		len(l), float64(len(l))/el.Seconds(), q(0.5), q(0.9), q(0.99), l[len(l)-1])
	if s.held > 0 {
		r += fmt.Sprintf("\nheld behind a write: %d frames; delivered about %d (%.1f fps)", s.held, s.delivered, float64(s.delivered)/el.Seconds())
	}
	return r
}
