package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
)

// A sink encodes frames headless and counts the bytes it would send
type sink interface {
	// frame encodes cells, changed saying whether they differ from the last frame
	frame(cells []terminal.Cell, changed bool) (caller time.Duration)
	bytes() uint64
	close()
}

func newSink(lib string, w, h int, mode terminal.ColorMode) (sink, error) {
	switch lib {
	case "terminal":
		return newTermSink(w, h, mode)
	case "tcell", "tcell-full":
		return newTcellSink(w, h, mode, lib == "tcell-full")
	}
	return nil, fmt.Errorf("unknown lib %q", lib)
}

// --- the terminal module, over a backend that drops what it is given ---

type nullBackend struct {
	w, h   int
	n      atomic.Uint64
	writes atomic.Uint64
	wake   chan struct{}
}

func (b *nullBackend) Init() error                     { return nil }
func (b *nullBackend) Fini()                           {}
func (b *nullBackend) Size() (int, int)                { return b.w, b.h }
func (b *nullBackend) SetResizeHandler(func(int, int)) {}
func (b *nullBackend) Write(p []byte) error {
	b.n.Add(uint64(len(p)))
	b.writes.Add(1)
	return nil
}
func (b *nullBackend) Read(wait time.Duration) ([]byte, error) {
	var passed <-chan time.Time
	if wait >= 0 {
		t := time.NewTimer(wait)
		defer t.Stop()
		passed = t.C
	}
	select {
	case <-b.wake:
	case <-passed:
	}
	return nil, nil
}

func (b *nullBackend) Wake() {
	select {
	case b.wake <- struct{}{}:
	default:
	}
}

type termSink struct {
	b *nullBackend
	t terminal.Terminal
}

func newTermSink(w, h int, mode terminal.ColorMode) (*termSink, error) {
	b := &nullBackend{w: w, h: h, wake: make(chan struct{}, 1)}
	t := terminal.NewWithBackend(b, mode)
	if err := t.Init(); err != nil {
		return nil, err
	}
	return &termSink{b: b, t: t}, nil
}

// frame flushes and, as a frame that changed a cell always writes, waits for the
// writer goroutine to take it, so every frame is encoded and none is held
func (s *termSink) frame(cells []terminal.Cell, changed bool) time.Duration {
	w0 := s.b.writes.Load()
	t0 := time.Now()
	s.t.Flush(cells, s.b.w, s.b.h)
	d := time.Since(t0)
	if changed {
		for deadline := time.Now().Add(time.Second); s.b.writes.Load() == w0 && time.Now().Before(deadline); {
			runtime.Gosched()
		}
	}
	return d
}

func (s *termSink) bytes() uint64 { return s.b.n.Load() }
func (s *termSink) close()        { s.t.Fini() }

// --- tcell, over a tty that drops what it is given ---

type nullTty struct {
	w, h int
	n    atomic.Uint64
	stop chan struct{}
	once sync.Once
}

func (t *nullTty) Start() error        { return nil }
func (t *nullTty) Stop() error         { return nil }
func (t *nullTty) Close() error        { return nil }
func (t *nullTty) NotifyResize(func()) {}
func (t *nullTty) Drain() error {
	t.once.Do(func() { close(t.stop) })
	return nil
}
func (t *nullTty) WindowSize() (tcell.WindowSize, error) {
	return tcell.WindowSize{Width: t.w, Height: t.h}, nil
}
func (t *nullTty) Read([]byte) (int, error) {
	<-t.stop
	return 0, io.EOF
}
func (t *nullTty) Write(p []byte) (int, error) {
	t.n.Add(uint64(len(p)))
	return len(p), nil
}

type tcellSink struct {
	tty  *nullTty
	s    tcell.Screen
	put  tcellPutter
	full bool
}

// newTcellSink builds tcell's terminfo screen as it runs on xterm-256color, with
// or without the RGB sequences COLORTERM=truecolor adds; the shared terminfo entry
// is copied, since tcell amends it in place
func newTcellSink(w, h int, mode terminal.ColorMode, full bool) (*tcellSink, error) {
	tcellEnv()
	ti, err := tcell.LookupTerminfo("xterm-256color")
	if err != nil {
		return nil, err
	}
	tc := *ti
	tc.SetFgRGB, tc.SetBgRGB, tc.SetFgBgRGB = "", "", ""
	switch mode {
	case terminal.ColorModeTrueColor:
		tc.SetFgRGB = "\x1b[38;2;%p1%d;%p2%d;%p3%dm"
		tc.SetBgRGB = "\x1b[48;2;%p1%d;%p2%d;%p3%dm"
		tc.SetFgBgRGB = "\x1b[38;2;%p1%d;%p2%d;%p3%d;48;2;%p4%d;%p5%d;%p6%dm"
	case terminal.ColorMode256:
	default:
		return nil, fmt.Errorf("tcell sink: mode %d not run", mode)
	}
	tty := &nullTty{w: w, h: h, stop: make(chan struct{})}
	s, err := tcell.NewTerminfoScreenFromTtyTerminfo(tty, &tc)
	if err != nil {
		return nil, err
	}
	if err := s.Init(); err != nil {
		return nil, err
	}
	return &tcellSink{tty: tty, s: s, put: newTcellPutter(w, h), full: full}, nil
}

func (s *tcellSink) frame(cells []terminal.Cell, _ bool) time.Duration {
	t0 := time.Now()
	s.put.draw(s.s, cells, s.full)
	s.s.Show()
	return time.Since(t0)
}

func (s *tcellSink) bytes() uint64 { return s.tty.n.Load() }
func (s *tcellSink) close()        { s.s.Fini() }

// tcellEnv pins what tcell reads from the environment: a UTF-8 charset, and color
// left to the terminfo entry
func tcellEnv() {
	os.Setenv("LC_ALL", "C.UTF-8")
	os.Unsetenv("NO_COLOR")
	os.Unsetenv("TCELL_TRUECOLOR")
}

// tcellPutter ports a []Cell frame to tcell as a game would: Put for each cell that
// changed since the last frame (or every cell when full), runes as cached strings
type tcellPutter struct {
	w, h  int
	prev  []terminal.Cell
	first bool
	strs  map[rune]string
}

var asciiStr = func() (t [128]string) {
	for i := range t {
		t[i] = string(rune(i))
	}
	return t
}()

func newTcellPutter(w, h int) tcellPutter {
	return tcellPutter{w: w, h: h, prev: make([]terminal.Cell, w*h), first: true, strs: map[rune]string{}}
}

func (p *tcellPutter) resize(w, h int) {
	p.w, p.h, p.prev, p.first = w, h, make([]terminal.Cell, w*h), true
}

func (p *tcellPutter) draw(s tcell.Screen, cells []terminal.Cell, full bool) {
	for i, c := range cells {
		if !full && !p.first && c == p.prev[i] {
			continue
		}
		p.prev[i] = c
		s.Put(i%p.w, i/p.w, p.str(c.Rune), tcellStyle(c))
	}
	p.first = false
}

func (p *tcellPutter) str(r rune) string {
	if r >= 0 && r < 128 {
		if r < ' ' {
			return " "
		}
		return asciiStr[r]
	}
	s, ok := p.strs[r]
	if !ok {
		s = string(r)
		p.strs[r] = s
	}
	return s
}

func tcellStyle(c terminal.Cell) tcell.Style {
	return tcell.StyleDefault.
		Foreground(tcellColor(c.Fg, c.Attrs&terminal.AttrFg256 != 0, c.Attrs&terminal.AttrFgDefault != 0)).
		Background(tcellColor(c.Bg, c.Attrs&terminal.AttrBg256 != 0, c.Attrs&terminal.AttrBgDefault != 0)).
		Attributes(tcellAttrs(c.Attrs))
}

func tcellColor(c color.RGB, palette, deflt bool) tcell.Color {
	switch {
	case deflt:
		return tcell.ColorDefault
	case palette:
		return tcell.PaletteColor(int(c.R))
	}
	return tcell.NewRGBColor(int32(c.R), int32(c.G), int32(c.B))
}

var attrMap = [...]struct {
	t terminal.Attr
	c tcell.AttrMask
}{
	{terminal.AttrBold, tcell.AttrBold}, {terminal.AttrDim, tcell.AttrDim}, {terminal.AttrItalic, tcell.AttrItalic},
	{terminal.AttrUnderline, tcell.AttrUnderline}, {terminal.AttrBlink, tcell.AttrBlink}, {terminal.AttrReverse, tcell.AttrReverse},
}

func tcellAttrs(a terminal.Attr) tcell.AttrMask {
	var m tcell.AttrMask
	for _, e := range attrMap {
		if a&e.t != 0 {
			m |= e.c
		}
	}
	return m
}

// --- live screens: each library on its own platform backend ---

type liveEvent struct {
	text                string
	quit, resize, mouse bool
}

type screen interface {
	size() (int, int)
	mouse()
	event() (liveEvent, bool)
	// frame draws cells and returns what the call cost the loop and, for the
	// terminal module, how long the write in flight had waited when it was made
	frame(cells []terminal.Cell, w, h int) (time.Duration, time.Duration)
	fini()
}

func openScreen(lib string, mode terminal.ColorMode) (screen, error) {
	switch lib {
	case "terminal":
		t := terminal.New(mode)
		if err := t.Init(); err != nil {
			return nil, err
		}
		s := &termScreen{t: t, ev: make(chan liveEvent, 256)}
		go s.read()
		return s, nil
	case "tcell":
		os.Unsetenv("NO_COLOR")
		if mode == terminal.ColorMode256 {
			os.Setenv("TCELL_TRUECOLOR", "disable")
		} else if os.Getenv("COLORTERM") == "" {
			os.Setenv("COLORTERM", "truecolor")
		}
		sc, err := tcell.NewScreen()
		if err != nil {
			return nil, err
		}
		if err := sc.Init(); err != nil {
			return nil, err
		}
		w, h := sc.Size()
		s := &tcellScreen{s: sc, ev: make(chan liveEvent, 256), put: newTcellPutter(w, h)}
		go s.read()
		return s, nil
	}
	return nil, fmt.Errorf("unknown lib %q", lib)
}

type termScreen struct {
	t  terminal.Terminal
	ev chan liveEvent
}

func (s *termScreen) size() (int, int) { return s.t.Size() }
func (s *termScreen) mouse() {
	s.t.SetMouseMode(terminal.MouseModeClick | terminal.MouseModeDrag | terminal.MouseModeMotion)
}
func (s *termScreen) fini() { s.t.Fini() }

func (s *termScreen) event() (liveEvent, bool) {
	select {
	case e := <-s.ev:
		return e, true
	default:
		return liveEvent{}, false
	}
}

func (s *termScreen) frame(cells []terminal.Cell, w, h int) (time.Duration, time.Duration) {
	held := s.t.Blocked()
	t0 := time.Now()
	s.t.Flush(cells, w, h)
	return time.Since(t0), held
}

func (s *termScreen) read() {
	for {
		ev := s.t.PollEvent()
		var e liveEvent
		switch ev.Type {
		case terminal.EventKey:
			e.text = fmt.Sprintf("key %s %q mod %d", terminal.KeyName(ev.Key), ev.Rune, ev.Modifiers)
			e.quit = ev.Key == terminal.KeyEscape || ev.Key == terminal.KeyCtrlC || ev.Rune == 'q'
		case terminal.EventMouse:
			e.text = fmt.Sprintf("mouse %d,%d %s %s mod %d", ev.MouseX, ev.MouseY, ev.MouseBtn, ev.MouseAction, ev.Modifiers)
			e.mouse = true
		case terminal.EventResize:
			e.text, e.resize = fmt.Sprintf("resize %dx%d", ev.Width, ev.Height), true
		case terminal.EventClosed, terminal.EventError:
			s.ev <- liveEvent{text: "input closed", quit: true}
			return
		default:
			e.text = fmt.Sprintf("event %d", ev.Type)
		}
		s.ev <- e
	}
}

type tcellScreen struct {
	s   tcell.Screen
	ev  chan liveEvent
	put tcellPutter
}

func (s *tcellScreen) size() (int, int) { return s.s.Size() }
func (s *tcellScreen) mouse()           { s.s.EnableMouse() }
func (s *tcellScreen) fini()            { s.s.Fini() }

func (s *tcellScreen) event() (liveEvent, bool) {
	select {
	case e := <-s.ev:
		return e, true
	default:
		return liveEvent{}, false
	}
}

func (s *tcellScreen) frame(cells []terminal.Cell, w, h int) (time.Duration, time.Duration) {
	if w != s.put.w || h != s.put.h {
		s.put.resize(w, h)
	}
	t0 := time.Now()
	s.put.draw(s.s, cells, false)
	s.s.Show()
	return time.Since(t0), 0
}

func (s *tcellScreen) read() {
	for {
		var e liveEvent
		switch ev := s.s.PollEvent().(type) {
		case nil:
			return
		case *tcell.EventKey:
			e.text = "key " + ev.Name()
			e.quit = ev.Key() == tcell.KeyEscape || ev.Key() == tcell.KeyCtrlC || ev.Rune() == 'q'
		case *tcell.EventMouse:
			x, y := ev.Position()
			e.text = fmt.Sprintf("mouse %d,%d buttons %#x mod %d", x, y, ev.Buttons(), ev.Modifiers())
			e.mouse = true
		case *tcell.EventResize:
			w, h := ev.Size()
			e.text, e.resize = fmt.Sprintf("resize %dx%d", w, h), true
		case *tcell.EventError:
			s.ev <- liveEvent{text: "input error: " + ev.Error(), quit: true}
			return
		default:
			e.text = fmt.Sprintf("event %T", ev)
		}
		s.ev <- e
	}
}
