package main

import (
	"cmp"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/lixenwraith/ascimage"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
)

type sortMode uint8

const (
	sortName sortMode = iota
	sortSize
	sortTime
	sortModes
)

func (s sortMode) String() string { return [...]string{"name", "size", "time"}[s] }

// entry is a row of the browser: a directory, or a file it lists
type entry struct {
	name  string
	dir   bool
	size  int64
	mtime time.Time
}

// browser is a directory's listing: its entries, the rows the hidden toggle
// and the filter leave of them, the cursor and the marks
type browser struct {
	dir    string
	all    []entry
	index  map[string]int // entries by name
	rows   []int          // indices into all
	cursor int
	scroll int
	marks  map[string]bool
	filter string
	sortBy sortMode
	hidden bool
	err    error
}

type overlay uint8

const (
	ovNone overlay = iota
	ovHelp
	ovFilter
	ovExport
	ovConfirm
)

// wakeEvent and tickEvent are what the app posts itself, a decode done and an
// animation frame due: the terminal's reader never sends KeyNone
var (
	wakeEvent = terminal.Event{Type: terminal.EventKey, Key: terminal.KeyNone, Rune: 'w'}
	tickEvent = terminal.Event{Type: terminal.EventKey, Key: terminal.KeyNone, Rune: 't'}
)

const (
	tickEvery = 100 * time.Millisecond
	toastFor  = 2500 * time.Millisecond
)

// rect is where a frame drew a pane, for the mouse
type rect struct{ x, y, w, h int }

func (r rect) has(x, y int) bool { return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h }

func rectOf(r tui.Region) rect { return rect{r.X, r.Y, r.W, r.H} }

type app struct {
	term  terminal.Terminal
	w, h  int
	cells []terminal.Cell
	th    tui.Theme
	pal   palette
	start time.Time
	quit  bool

	lk       look
	backdrop int
	status   bool

	br          browser
	ld          *loader
	viewing     string // the image the full view shows, "" in the browser
	vw          view
	startView   view // what a newly viewed image starts at
	vwin, pwin  window
	showPreview bool // a layout with room for one pane shows the preview

	overlay    overlay
	filterTF   *tui.TextFieldState
	ex         *exportForm
	confirm    *tui.ConfirmState
	confirmMsg string
	onConfirm  func()

	toast     tui.ToastOpts
	toastTill time.Time
	ticking   bool
	pendingG  bool
	drag      struct {
		on   bool
		x, y int
	}
	geom struct{ list, preview rect }
}

// runTUI browses path, a directory, or views it, an image, until q
func runTUI(path string, lk look, start view, status bool, termMode terminal.ColorMode, explicit bool) error {
	var term terminal.Terminal
	if explicit {
		term = terminal.New(termMode)
	} else {
		term = terminal.New()
	}
	defer func() {
		if r := recover(); r != nil {
			terminal.EmergencyReset(os.Stdout)
			panic(r)
		}
	}()
	if err := term.Init(); err != nil {
		return fmt.Errorf("terminal: %w", err)
	}
	defer term.Fini()
	_ = term.SetMouseMode(terminal.MouseModeClick | terminal.MouseModeDrag)

	if !explicit {
		lk.depth = term.ColorMode()
	}
	w, h := term.Size()
	a := &app{
		term: term, w: w, h: h, start: time.Now(),
		th: tui.ThemeFor(term.ColorMode()), pal: paletteFor(term.ColorMode()),
		lk: lk, status: status, startView: start, vw: start,
		br: browser{marks: map[string]bool{}},
	}
	a.ld = newLoader(func() { term.PostEvent(wakeEvent) })
	if err := a.open(path); err != nil {
		return err
	}
	for !a.quit {
		a.ld.want(a.wanted()...)
		a.render()
		a.schedule()
		a.handle(term.PollEvent())
	}
	return nil
}

// open browses a directory, or views an image with its directory behind it
func (a *app) open(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return a.loadDir(abs, "")
	}
	if err := a.loadDir(filepath.Dir(abs), filepath.Base(abs)); err != nil {
		return err
	}
	a.viewing = abs
	return nil
}

func readEntries(dir string) ([]entry, error) {
	des, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := make([]entry, 0, len(des))
	for _, de := range des {
		fi, err := de.Info()
		if de.Type()&fs.ModeSymlink != 0 {
			fi, err = os.Stat(filepath.Join(dir, de.Name())) // a link counts as what it names
		}
		if err != nil {
			continue
		}
		if fi.IsDir() || fi.Mode().IsRegular() && listed(de.Name()) {
			out = append(out, entry{name: de.Name(), dir: fi.IsDir(), size: fi.Size(), mtime: fi.ModTime()})
		}
	}
	return out, nil
}

func (a *app) sortEntries(es []entry) {
	by := a.br.sortBy
	slices.SortFunc(es, func(x, y entry) int {
		if x.dir != y.dir {
			if x.dir {
				return -1
			}
			return 1
		}
		switch by {
		case sortSize:
			if c := cmp.Compare(y.size, x.size); c != 0 {
				return c
			}
		case sortTime:
			if c := y.mtime.Compare(x.mtime); c != 0 {
				return c
			}
		}
		return cmp.Compare(strings.ToLower(x.name), strings.ToLower(y.name))
	})
}

// loadDir lists a directory, the cursor on keep where it is there
func (a *app) loadDir(dir, keep string) error {
	es, err := readEntries(dir)
	if err != nil {
		return err
	}
	if dir != a.br.dir {
		a.br.filter = ""
		clear(a.br.marks)
	}
	a.br.dir, a.br.all, a.br.err = dir, es, nil
	a.sortEntries(a.br.all)
	a.br.index = make(map[string]int, len(es))
	for i, e := range a.br.all {
		a.br.index[e.name] = i
	}
	a.rebuild(keep)
	return nil
}

// reload lists the directory again, keeping the cursor's entry
func (a *app) reload() {
	if err := a.loadDir(a.br.dir, a.cursorName()); err != nil {
		a.br.err = err
	}
}

func (a *app) rebuild(keep string) {
	b := &a.br
	b.rows = b.rows[:0]
	f := strings.ToLower(b.filter)
	for i, e := range b.all {
		if !b.hidden && strings.HasPrefix(e.name, ".") {
			continue
		}
		if f != "" && !strings.Contains(strings.ToLower(e.name), f) {
			continue
		}
		b.rows = append(b.rows, i)
	}
	b.cursor = 0
	for r, i := range b.rows {
		if b.all[i].name == keep {
			b.cursor = r
			break
		}
	}
}

func (a *app) cursorEntry() (entry, bool) {
	if b := &a.br; b.cursor >= 0 && b.cursor < len(b.rows) {
		return b.all[b.rows[b.cursor]], true
	}
	return entry{}, false
}

func (a *app) cursorName() string {
	e, _ := a.cursorEntry()
	return e.name
}

func (a *app) pathOf(e entry) string { return filepath.Join(a.br.dir, e.name) }

// imageRows are the rows that hold images, in the browser's order
func (a *app) imageRows() []int {
	var out []int
	for r, i := range a.br.rows {
		if !a.br.all[i].dir {
			out = append(out, r)
		}
	}
	return out
}

// source is the decoded image at path, or its error, or neither while pending;
// the listing gives a file's size and time, which say whether it changed
func (a *app) source(path string) (*source, error) {
	if i, ok := a.br.index[filepath.Base(path)]; ok && filepath.Dir(path) == a.br.dir {
		return a.ld.get(path, a.br.all[i].size, a.br.all[i].mtime)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	return a.ld.get(path, fi.Size(), fi.ModTime())
}

// viewingRow is the row of the image the view shows, -1 for none
func (a *app) viewingRow() int {
	if a.viewing == "" || filepath.Dir(a.viewing) != a.br.dir {
		return -1
	}
	i, ok := a.br.index[filepath.Base(a.viewing)]
	if !ok {
		return -1
	}
	return slices.Index(a.br.rows, i)
}

// wanted is what the loader should decode: the image shown, then those
// either side of it, which n and p or j and k show next
func (a *app) wanted() []string {
	cur := a.viewing
	if cur == "" {
		if e, ok := a.cursorEntry(); ok && !e.dir {
			cur = a.pathOf(e)
		}
	}
	out := []string{cur}
	rows := a.imageRows()
	row := a.br.cursor
	if a.viewing != "" {
		row = a.viewingRow()
	}
	at := slices.Index(rows, row)
	if at < 0 {
		return out // a folder or nothing under the cursor: nothing to read ahead of
	}
	for _, k := range []int{at + 1, at - 1} {
		if k >= 0 && k < len(rows) {
			out = append(out, a.pathOf(a.br.all[a.br.rows[rows[k]]]))
		}
	}
	return out
}

func (a *app) say(sev tui.ToastSeverity, msg string) {
	o := tui.DefaultToastOpts(msg, sev)
	o.Position, o.Style, o.MaxWidth = tui.ToastBottomRight, tui.ToastStyleRounded, max(20, a.w-4)
	a.toast, a.toastTill = o, time.Now().Add(toastFor)
}

// schedule asks for an animation frame while a decode runs or a toast shows;
// idle, the app takes no wakeups
func (a *app) schedule() {
	if a.ticking || a.ld.busy == "" && time.Now().After(a.toastTill) {
		return
	}
	a.ticking = true
	time.AfterFunc(tickEvery, func() { a.term.PostEvent(tickEvent) })
}

func (a *app) handle(ev terminal.Event) {
	switch ev.Type {
	case terminal.EventResize:
		a.w, a.h = ev.Width, ev.Height
		return
	case terminal.EventClosed, terminal.EventError:
		a.quit = true
		return
	case terminal.EventMouse:
		if a.overlay == ovNone {
			a.handleMouse(ev)
		}
		return
	case terminal.EventPaste:
		if a.overlay == ovFilter {
			a.filterTF.Paste(ev.Text)
			a.setFilter(a.filterTF.Value())
		} else if a.overlay == ovExport {
			a.ex.paste(ev.Text)
		}
		return
	}
	if ev.Type != terminal.EventKey {
		return
	}
	if ev.Key == terminal.KeyNone {
		if ev.Rune == tickEvent.Rune {
			a.ticking = false
		}
		return
	}
	switch ev.Key {
	case terminal.KeyCtrlC, terminal.KeyCtrlQ:
		a.quit = true
		return
	case terminal.KeyCtrlL:
		a.term.Sync()
		return
	}
	switch a.overlay {
	case ovHelp:
		a.overlay = ovNone
	case ovConfirm:
		if a.confirm.HandleKey(ev.Key, ev.Rune) {
			a.overlay = ovNone
			if a.confirm.Result == tui.ConfirmYes && a.onConfirm != nil {
				a.onConfirm()
			}
			a.onConfirm = nil
		}
	case ovFilter:
		switch ev.Key {
		case terminal.KeyEscape:
			a.setFilter("")
			a.overlay = ovNone
		case terminal.KeyEnter:
			a.overlay = ovNone
		default:
			if a.filterTF.HandleKey(ev.Key, ev.Rune, ev.Modifiers) {
				a.setFilter(a.filterTF.Value())
			}
		}
	case ovExport:
		a.ex.handleKey(a, ev)
	default:
		if a.viewing != "" {
			a.viewKey(ev)
		} else {
			a.browseKey(ev)
		}
	}
}

func (a *app) setFilter(f string) {
	keep := a.cursorName()
	a.br.filter = f
	a.rebuild(keep)
}

func (a *app) move(delta int) {
	if n := len(a.br.rows); n > 0 {
		a.br.cursor = max(0, min(n-1, a.br.cursor+delta))
	}
}

// enter opens the cursor's entry: a directory to browse, an image to view
func (a *app) enter() {
	e, ok := a.cursorEntry()
	if !ok {
		return
	}
	if !e.dir {
		a.viewing = a.pathOf(e)
		a.vw = a.startView
		return
	}
	if err := a.loadDir(a.pathOf(e), ""); err != nil {
		a.say(tui.ToastError, err.Error())
	}
}

func (a *app) parent() {
	up := filepath.Dir(a.br.dir)
	if up == a.br.dir {
		return
	}
	if err := a.loadDir(up, filepath.Base(a.br.dir)); err != nil {
		a.say(tui.ToastError, err.Error())
	}
}

// page is the rows a page of the list moves
func (a *app) page() int { return max(1, a.geom.list.h-1) }

func (a *app) browseKey(ev terminal.Event) {
	b := &a.br
	pendG := a.pendingG
	a.pendingG = false
	switch ev.Key {
	case terminal.KeyUp:
		a.move(-1)
	case terminal.KeyDown:
		a.move(1)
	case terminal.KeyLeft, terminal.KeyBackspace:
		a.parent()
	case terminal.KeyRight, terminal.KeyEnter:
		a.enter()
	case terminal.KeyHome:
		b.cursor = 0
	case terminal.KeyEnd:
		a.move(len(b.rows))
	case terminal.KeyPageUp:
		a.move(-a.page())
	case terminal.KeyPageDown:
		a.move(a.page())
	case terminal.KeyCtrlU:
		a.move(-tui.PageDelta(a.geom.list.h))
	case terminal.KeyCtrlD:
		a.move(tui.PageDelta(a.geom.list.h))
	case terminal.KeyTab, terminal.KeyBacktab:
		a.showPreview = !a.showPreview
	case terminal.KeyEscape:
		if b.filter != "" {
			a.setFilter("")
		} else {
			clear(b.marks)
		}
	case terminal.KeyRune:
		switch ev.Rune {
		case 'q':
			a.quit = true
		case 'j':
			a.move(1)
		case 'k':
			a.move(-1)
		case 'h':
			a.parent()
		case 'l':
			a.enter()
		case 'g':
			if pendG {
				b.cursor = 0
			} else {
				a.pendingG = true
			}
		case 'G':
			a.move(len(b.rows))
		case '~':
			if home, err := os.UserHomeDir(); err == nil {
				if err := a.loadDir(home, ""); err != nil {
					a.say(tui.ToastError, err.Error())
				}
			}
		case ' ':
			if e, ok := a.cursorEntry(); ok && !e.dir {
				if b.marks[e.name] {
					delete(b.marks, e.name)
				} else {
					b.marks[e.name] = true
				}
			}
			a.move(1)
		case 'a':
			for _, i := range b.rows {
				if !b.all[i].dir {
					b.marks[b.all[i].name] = true
				}
			}
		case 'u':
			clear(b.marks)
		case '.':
			b.hidden = !b.hidden
			a.rebuild(a.cursorName())
		case 's':
			b.sortBy = (b.sortBy + 1) % sortModes
			keep := a.cursorName()
			a.sortEntries(b.all)
			for i, e := range b.all {
				b.index[e.name] = i
			}
			a.rebuild(keep)
		case '/':
			a.filterTF = tui.NewTextFieldState(b.filter)
			a.overlay = ovFilter
		case 'w':
			a.openExport(a.exportTargets())
		default:
			a.lookKey(ev.Rune)
		}
	}
}

// lookKey changes how images show, the same in the browser and the view
func (a *app) lookKey(r rune) {
	switch r {
	case 'm':
		if a.lk.mode == ascimage.ModeQuadrant {
			a.lk.mode = ascimage.ModeBackgroundOnly
		} else {
			a.lk.mode = ascimage.ModeQuadrant
		}
	case 'c':
		i := slices.IndexFunc(depths, func(d depth) bool { return d.mode == a.lk.depth })
		a.lk.depth = depths[(i+1)%len(depths)].mode // none comes back to true color
	case 'b':
		a.backdrop = (a.backdrop + 1) % len(backdrops)
	case '?':
		a.overlay = ovHelp
	}
}

// viewArea is the full view's viewport, the status bar's row left out
func (a *app) viewArea() (int, int) {
	if a.status {
		return a.w, max(1, a.h-1)
	}
	return a.w, a.h
}

// viewFrame is the frame the full view shows, nil while its image is pending
func (a *app) viewFrame() (*source, frame, bool) {
	s, _ := a.source(a.viewing)
	if s == nil {
		return nil, frame{}, false
	}
	vw, vh := a.viewArea()
	return s, a.vw.frame(s, vw, vh, a.lk), true
}

// stepImage views the image n rows of images on, stopping at either end
func (a *app) stepImage(n int) {
	rows := a.imageRows()
	at := slices.Index(rows, a.viewingRow())
	next := at + n
	if at < 0 {
		next = 0
	}
	if next < 0 || next >= len(rows) {
		a.say(tui.ToastInfo, map[bool]string{true: "the last image", false: "the first image"}[n > 0])
		return
	}
	a.br.cursor = rows[next]
	a.viewing = a.pathOf(a.br.all[a.br.rows[rows[next]]])
	a.vw.cx, a.vw.cy = 0.5, 0.5
}

func (a *app) viewKey(ev terminal.Event) {
	s, f, ok := a.viewFrame()
	pan := func(dx, dy int) {
		if ok {
			a.vw.pan(f, dx, dy)
		}
	}
	step := 1
	if ev.Modifiers&terminal.ModShift != 0 {
		step = 10
	}
	switch ev.Key {
	case terminal.KeyEscape:
		a.viewing = ""
	case terminal.KeyBackspace:
		a.stepImage(-1)
	case terminal.KeyLeft:
		pan(-step, 0)
	case terminal.KeyRight:
		pan(step, 0)
	case terminal.KeyUp:
		pan(0, -step)
	case terminal.KeyDown:
		pan(0, step)
	case terminal.KeyPageUp:
		pan(0, -f.vh)
	case terminal.KeyPageDown:
		pan(0, f.vh)
	case terminal.KeyCtrlU:
		pan(0, -f.vh/2)
	case terminal.KeyCtrlD:
		pan(0, f.vh/2)
	case terminal.KeyHome:
		pan(-f.gw, 0)
	case terminal.KeyEnd:
		pan(f.gw, 0)
	case terminal.KeyRune:
		switch ev.Rune {
		case 'q':
			a.quit = true
		case 'h':
			pan(-1, 0)
		case 'l':
			pan(1, 0)
		case 'k':
			pan(0, -1)
		case 'j':
			pan(0, 1)
		case 'H':
			pan(-10, 0)
		case 'L':
			pan(10, 0)
		case 'K':
			pan(0, -10)
		case 'J':
			pan(0, 10)
		case 'g':
			pan(-f.gw, -f.gh)
		case 'G':
			pan(-f.gw, f.gh)
		case 'n', ' ', ']':
			a.stepImage(1)
		case 'p', '[':
			a.stepImage(-1)
		case '+', '=':
			if ok {
				a.vw.step(s, f, 1, f.vw/2, f.vh/2, a.lk)
			}
		case '-', '_':
			if ok {
				a.vw.step(s, f, -1, f.vw/2, f.vh/2, a.lk)
			}
		case '0':
			if ok {
				a.vw.zoomTo(s, f, 100, f.vw/2, f.vh/2, a.lk)
			}
		case 'f':
			if a.vw.fit {
				if ok {
					a.vw.zoomTo(s, f, 100, f.vw/2, f.vh/2, a.lk)
				}
			} else {
				a.vw.fit = true
			}
		case 's':
			a.status = !a.status
		case 'w':
			a.openExport([]string{a.viewing})
			if a.ex != nil && ok && s.dual == nil {
				a.ex.width.SetValue(fmt.Sprint(min(f.gw, ascimage.MaxSide))) // what the view shows
			}
		default:
			a.lookKey(ev.Rune)
		}
	}
}

func (a *app) handleMouse(ev terminal.Event) {
	if a.viewing != "" {
		a.viewMouse(ev)
		return
	}
	if ev.MouseAction != terminal.MouseActionPress {
		return
	}
	switch ev.MouseBtn {
	case terminal.MouseBtnWheelUp:
		a.move(-3)
	case terminal.MouseBtnWheelDown:
		a.move(3)
	case terminal.MouseBtnBack:
		a.parent()
	case terminal.MouseBtnLeft:
		switch {
		case a.geom.list.has(ev.MouseX, ev.MouseY):
			row := a.br.scroll + ev.MouseY - a.geom.list.y
			if row == a.br.cursor {
				a.enter()
			} else if row >= 0 && row < len(a.br.rows) {
				a.br.cursor = row
			}
		case a.geom.preview.has(ev.MouseX, ev.MouseY):
			if e, ok := a.cursorEntry(); ok && !e.dir {
				a.enter()
			}
		}
	}
}

func (a *app) viewMouse(ev terminal.Event) {
	s, f, ok := a.viewFrame()
	if !ok {
		return
	}
	switch ev.MouseAction {
	case terminal.MouseActionPress:
		zoom := ev.Modifiers&terminal.ModCtrl != 0
		switch ev.MouseBtn {
		case terminal.MouseBtnWheelUp:
			if zoom {
				a.vw.step(s, f, 1, ev.MouseX, ev.MouseY, a.lk)
			} else {
				a.vw.pan(f, 0, -3)
			}
		case terminal.MouseBtnWheelDown:
			if zoom {
				a.vw.step(s, f, -1, ev.MouseX, ev.MouseY, a.lk)
			} else {
				a.vw.pan(f, 0, 3)
			}
		case terminal.MouseBtnWheelLeft:
			a.vw.pan(f, -6, 0)
		case terminal.MouseBtnWheelRight:
			a.vw.pan(f, 6, 0)
		case terminal.MouseBtnBack:
			a.stepImage(-1)
		case terminal.MouseBtnForward:
			a.stepImage(1)
		case terminal.MouseBtnLeft:
			a.drag.on, a.drag.x, a.drag.y = true, ev.MouseX, ev.MouseY
		}
	case terminal.MouseActionDrag:
		if a.drag.on {
			a.vw.pan(f, a.drag.x-ev.MouseX, a.drag.y-ev.MouseY)
			a.drag.x, a.drag.y = ev.MouseX, ev.MouseY
		}
	case terminal.MouseActionRelease:
		a.drag.on = false
	}
}
