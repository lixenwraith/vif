package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/tui"
	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/filter"
	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/keys"
	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/logfile"
	"github.com/lixenwraith/vi-fighter/tool/vif-log/internal/ui"
)

const (
	firstPassBudget = 40 * time.Millisecond // filter time spent on a filter change
	stepBudget      = 8 * time.Millisecond  // filter time spent per tick
	budgetCheck     = 2048                  // records tested between deadline checks
	followScanCap   = 1 << 21
	toastFrames     = 40

	minW = 80
	minH = 24
)

type overlayKind uint8

const (
	ovNone overlayKind = iota
	ovHelp
	ovOpen
	ovPrompt
)

// viewBuilder tracks the incremental filter pass. A filter change resets it;
// index growth extends it.
type viewBuilder struct {
	next int
	busy bool
}

// App holds all viewer state.
type App struct {
	term terminal.Terminal
	th   ui.Theme
	lay  ui.Layout
	res  *keys.Resolver

	idx   *logfile.Index
	rd    *logfile.Reader // render path
	frd   *logfile.Reader // filter pass; disjoint window, no thrash
	title string

	stack   filter.Stack
	lvl     *filter.Level
	snap    *filter.Collapse
	find    *filter.Find
	dom     *filter.Domain
	pins    *filter.PinSet
	pinOnly *filter.Pinned

	build viewBuilder
	view  []int32 // record indices, always ascending
	order []int32 // display order when sorted

	col     logfile.Column
	sortCol logfile.Column
	sortDir sortDir

	cursor  int
	scroll  int
	dscroll int
	listH   int

	overlay    overlayKind
	prompt     *tui.TextFieldState
	promptKind promptKind
	browse     browser
	help       ui.Panel
	helpLines  []helpLine
	toast      tui.ToastState
	scanShown  bool

	w, h  int
	frame int
	quit  bool
	cells []terminal.Cell // reused frame buffer

	rec  logfile.Record
	fctx filter.Ctx
}

// New builds the viewer. start may be log files, one directory to browse, or
// empty for the working directory. specs are "kind:arg" filters applied up front.
func New(t terminal.Terminal, start []string, specs []string) (*App, error) {
	w, h := t.Size()
	a := &App{
		term: t, th: ui.DefaultTheme, lay: ui.DefaultLayout(), res: keys.NewResolver(),
		w: w, h: h, col: logfile.ColAll, sortCol: logfile.ColTime,
		helpLines: buildHelpLines(),
	}
	a.help = ui.Panel{W: 70}
	a.browse.panel.Cursor = true

	// Snapshots collapse by default: stat members are ~half the records in a
	// typical log and arrive in blocks of ~40. enter expands.
	a.lvl, a.snap, a.find = filter.NewLevel(), filter.NewCollapse(true), filter.NewFind()
	a.dom = filter.NewDomain()
	a.pins = filter.NewPinSet()
	a.pinOnly = filter.NewPinned(a.pins)
	a.stack.Add(a.lvl)
	a.stack.Add(a.snap)
	a.stack.Add(a.dom)
	a.stack.Add(a.pinOnly)
	a.stack.Add(a.find)

	for _, s := range specs {
		if err := a.applyFilterSpec(s); err != nil {
			return nil, err
		}
	}

	a.initStart(start)
	return a, nil
}

// applyFilterSpec parses "kind:arg" and installs it. Kinds owned by a key
// binding are routed to their owner so the app keeps a single instance.
func (a *App) applyFilterSpec(spec string) error {
	kind, arg, ok := strings.Cut(spec, ":")
	if !ok {
		return fmt.Errorf("filter: expected kind:arg, got %q", spec)
	}
	kind = strings.TrimSpace(kind)
	switch kind {
	case "level":
		f, err := filter.New(kind, arg)
		if err != nil {
			return err
		}
		a.lvl.Mask = f.(*filter.Level).Mask
		return nil
	case "dom":
		f, err := filter.New(kind, arg)
		if err != nil {
			return err
		}
		a.dom.Set(f.(*filter.Domain).State)
		return nil
	case "find":
		return a.find.Set(arg, a.col)
	case "snap", "pin":
		return fmt.Errorf("filter: %s is bound to a key, not a spec", kind)
	}
	if strings.TrimSpace(arg) == "" {
		a.stack.Remove(kind)
		return nil
	}
	f, err := filter.New(kind, arg)
	if err != nil {
		return err
	}
	a.stack.Set(f)
	return nil
}

// initStart loads the named files, or opens the browser at the right directory.
func (a *App) initStart(start []string) {
	dir := "."
	if len(start) == 1 {
		if fi, err := os.Stat(start[0]); err == nil && fi.IsDir() {
			a.browse.dir = start[0]
			a.openBrowser()
			return
		}
	}
	if len(start) > 0 {
		a.browse.dir = filepath.Dir(start[0])
		if err := a.openFiles(start); err == nil {
			return
		} else {
			a.say(tui.ToastError, err.Error())
		}
	}
	if a.browse.dir == "" {
		a.browse.dir = dir
	}
	a.openBrowser()
}

// openFiles replaces the loaded set with one merged index. Level, search, sort
// and stack filters survive; pins, snapshot exceptions and the cursor reset.
func (a *App) openFiles(paths []string) error {
	idx, err := logfile.Open(paths...)
	if err != nil {
		return err
	}
	rd, err := idx.NewReader()
	if err != nil {
		return err
	}
	frd, err := idx.NewReader()
	if err != nil {
		rd.Close()
		return err
	}
	if a.rd != nil {
		a.rd.Close()
	}
	if a.frd != nil {
		a.frd.Close()
	}
	a.idx, a.rd, a.frd = idx, rd, frd
	a.title = idx.SrcName(0)
	if n := idx.SrcCount(); n > 1 {
		a.title = fmt.Sprintf("%s +%d", a.title, n-1)
	}
	a.pins.Clear()
	a.pinOnly.On = false
	a.snap.ResetGroups()
	a.view, a.order = a.view[:0], a.order[:0]
	a.cursor, a.scroll, a.dscroll = 0, 0, 0
	a.build = viewBuilder{}
	a.scanShown = false
	a.rebuild()
	a.say(tui.ToastSuccess, fmt.Sprintf("opened %d file(s)", idx.SrcCount()))
	return nil
}

// Close releases the line readers.
func (a *App) Close() {
	if a.rd != nil {
		a.rd.Close()
	}
	if a.frd != nil {
		a.frd.Close()
	}
}

// Quit reports whether the event loop should stop.
func (a *App) Quit() bool { return a.quit }

// --- event handling --------------------------------------------------------

// Handle processes one terminal event.
func (a *App) Handle(ev terminal.Event) {
	switch ev.Type {
	case terminal.EventResize:
		a.w, a.h = ev.Width, ev.Height
		return
	case terminal.EventError, terminal.EventClosed:
		a.quit = true
		return
	case terminal.EventKey:
		if ev.Key == terminal.KeyNone { // synthetic tick
			a.tick()
			return
		}
	default:
		return
	}

	if a.overlay == ovPrompt {
		a.handlePrompt(ev)
		return
	}

	mode := keys.ModeNormal
	if a.overlay != ovNone {
		mode = keys.ModeOverlay
	}
	act := a.res.Resolve(mode, keys.FromEvent(ev))
	if act == keys.ActNone {
		return
	}
	table := actions
	if mode == keys.ModeOverlay {
		table = overlayActions
	}
	if fn := table[act]; fn != nil {
		fn(a)
	}
}

// handlePrompt routes text entry; the binding table has no place for it.
func (a *App) handlePrompt(ev terminal.Event) {
	switch ev.Key {
	case terminal.KeyEscape:
		a.overlay = ovNone
	case terminal.KeyEnter:
		a.commitPrompt()
	default:
		a.prompt.HandleKey(ev.Key, ev.Rune, ev.Modifiers)
	}
}

// tick advances animation and extends the filter pass into newly indexed rows.
func (a *App) tick() {
	a.frame++
	if a.toast.Visible {
		a.toast.Tick()
	}
	if a.build.busy || a.build.next < a.idx.Len() {
		a.build.busy = true
		a.filterStep(stepBudget)
		if !a.build.busy {
			a.applySort()
		}
	}
	if !a.scanShown {
		if err := a.idx.Err(); err != nil {
			a.scanShown = true
			a.say(tui.ToastError, "scan: "+err.Error())
		}
	}
	a.clamp()
}

// panel returns the scrollable body of the open overlay, nil when none is.
func (a *App) panel() *ui.Panel {
	switch a.overlay {
	case ovHelp:
		return &a.help
	case ovOpen:
		return &a.browse.panel
	}
	return nil
}

func (a *App) toggleLevel(l logfile.Level) {
	a.lvl.Toggle(l)
	a.rebuild()
}

func (a *App) threshold(l logfile.Level) {
	a.lvl.ThresholdToggle(l)
	a.rebuild()
}

func (a *App) say(sev tui.ToastSeverity, msg string) {
	o := tui.DefaultToastOpts(msg, sev)
	o.Position = tui.ToastBottomRight
	o.Style = tui.ToastStyleRounded
	a.toast.Show(o, toastFrames)
}

// actions maps key actions to behaviour; keys knows nothing about App.
var actions = map[keys.Action]func(*App){
	keys.ActQuit:         func(a *App) { a.quit = true },
	keys.ActRedraw:       func(a *App) { a.term.Sync() },
	keys.ActHelp:         func(a *App) { a.overlay = ovHelp },
	keys.ActCloseOverlay: func(a *App) { a.overlay = ovNone },

	keys.ActDown:     func(a *App) { a.move(1) },
	keys.ActUp:       func(a *App) { a.move(-1) },
	keys.ActPageDown: func(a *App) { a.move(max(1, a.listH)) },
	keys.ActPageUp:   func(a *App) { a.move(-max(1, a.listH)) },
	keys.ActHalfDown: func(a *App) { a.move(tui.PageDelta(a.listH)) },
	keys.ActHalfUp:   func(a *App) { a.move(-tui.PageDelta(a.listH)) },
	keys.ActTop:      func(a *App) { a.cursor, a.dscroll = 0, 0; a.clamp() },
	keys.ActBottom:   func(a *App) { a.cursor, a.dscroll = len(a.rows())-1, 0; a.clamp() },

	keys.ActColNext:    func(a *App) { a.cycleColumn(1) },
	keys.ActColPrev:    func(a *App) { a.cycleColumn(-1) },
	keys.ActSort:       (*App).cycleSort,
	keys.ActDetailDown: func(a *App) { a.dscroll++ },
	keys.ActDetailUp:   func(a *App) { a.dscroll = max(0, a.dscroll-1) },

	keys.ActSearch:     (*App).openSearch,
	keys.ActFollowNext: func(a *App) { a.followJump(1) },
	keys.ActFollowPrev: func(a *App) { a.followJump(-1) },
	keys.ActFilter:     (*App).openFilter,
	keys.ActClear:      (*App).clearState,

	keys.ActExpand:   (*App).toggleSnapshot,
	keys.ActSnapNext: (*App).nextSnapshot,
	keys.ActSnapPrev: (*App).prevSnapshot,

	keys.ActPinToggle: (*App).togglePin,
	keys.ActPinOnly:   (*App).togglePinOnly,
	keys.ActPinClear:  (*App).clearPins,

	keys.ActDomain: (*App).cycleDomain,

	keys.ActOpen:   (*App).openBrowser,
	keys.ActExport: (*App).openExport,

	keys.ActLvlTrace: func(a *App) { a.toggleLevel(logfile.LevelTrace) },
	keys.ActLvlDebug: func(a *App) { a.toggleLevel(logfile.LevelDebug) },
	keys.ActLvlInfo:  func(a *App) { a.toggleLevel(logfile.LevelInfo) },
	keys.ActLvlWarn:  func(a *App) { a.toggleLevel(logfile.LevelWarn) },
	keys.ActLvlError: func(a *App) { a.toggleLevel(logfile.LevelError) },
	keys.ActLvlProc:  func(a *App) { a.toggleLevel(logfile.LevelProc) },
	keys.ActLvlBad:   func(a *App) { a.toggleLevel(logfile.LevelBad) },
	keys.ActLvlAll:   func(a *App) { a.lvl.SetAll(true); a.rebuild() },
	keys.ActLvlRaise: func(a *App) { a.lvl.Shift(1); a.rebuild() },
	keys.ActLvlLower: func(a *App) { a.lvl.Shift(-1); a.rebuild() },
	keys.ActThresh1:  func(a *App) { a.threshold(logfile.LevelTrace) },
	keys.ActThresh2:  func(a *App) { a.threshold(logfile.LevelDebug) },
	keys.ActThresh3:  func(a *App) { a.threshold(logfile.LevelInfo) },
	keys.ActThresh4:  func(a *App) { a.threshold(logfile.LevelWarn) },
	keys.ActThresh5:  func(a *App) { a.threshold(logfile.LevelError) },
}

// overlayActions drive the focused panel; movement keys are shared with the
// record list and resolve here only while an overlay is open.
var overlayActions = map[keys.Action]func(*App){
	keys.ActCloseOverlay: func(a *App) { a.overlay = ovNone },
	keys.ActQuit:         func(a *App) { a.overlay = ovNone },

	keys.ActDown:     func(a *App) { a.panelDo(func(p *ui.Panel) { p.Move(1) }) },
	keys.ActUp:       func(a *App) { a.panelDo(func(p *ui.Panel) { p.Move(-1) }) },
	keys.ActPageDown: func(a *App) { a.panelDo(func(p *ui.Panel) { p.Page(1) }) },
	keys.ActPageUp:   func(a *App) { a.panelDo(func(p *ui.Panel) { p.Page(-1) }) },
	keys.ActHalfDown: func(a *App) { a.panelDo(func(p *ui.Panel) { p.Half(1) }) },
	keys.ActHalfUp:   func(a *App) { a.panelDo(func(p *ui.Panel) { p.Half(-1) }) },
	keys.ActTop:      func(a *App) { a.panelDo((*ui.Panel).First) },
	keys.ActBottom:   func(a *App) { a.panelDo((*ui.Panel).Last) },

	keys.ActMark: func(a *App) {
		if a.overlay == ovOpen {
			a.browse.toggleMark()
		}
	},
	keys.ActConfirm: func(a *App) {
		if a.overlay == ovOpen {
			a.browserEnter()
		}
	},
	keys.ActBack: func(a *App) {
		if a.overlay == ovOpen {
			a.browserUp()
		}
	},
}

func (a *App) panelDo(fn func(*ui.Panel)) {
	if p := a.panel(); p != nil {
		fn(p)
	}
}
