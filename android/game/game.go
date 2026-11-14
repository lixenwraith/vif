// Package game is vif's Android host, bound to Java by gomobile: the activity
// starts a run with its view's size and its speakers, posts touches and keys,
// and pulls the frames its drawer paints. A process runs one game at a time.
package game

import (
	"cmp"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/resource"
	"github.com/lixenwraith/vif/internal/vlog"
	"github.com/lixenwraith/vif/pkg/glyph"
)

// Speaker is the AudioTrack the mixer streams 44.1 kHz 16-bit stereo PCM to
type Speaker interface {
	Write(pcm []byte) error
	Close() error
}

// Listener hears the run end, err being what ended it, empty when the player quit,
// and takes the configuration menu as JSON each time it changes (native-menu.md §4),
// empty once it closed; the app draws it with its own widgets and answers with Menu.
// Subnets is the IPv4 networks the device is on, each address/prefix-length and
// joined by commas, and Listening the run starting or stopping to listen on them.
type Listener interface {
	Exited(err string)
	Menu(json string)
	Subnets() string
	Listening(on bool)
}

var (
	mu  sync.Mutex
	run *view // the running game's screen, nil between runs

	// zone is the device's time zone, which the first Start makes local
	zone      string
	localZone sync.Once

	loadTexture = sync.OnceValues(func() (*glyph.Texture, error) {
		a, err := glyph.Parse(asset.DefaultFont)
		if err != nil {
			return nil, err
		}
		return glyph.NewTexture(a), nil
	})
)

// Zone names the device's time zone as tzdata does (TimeZone.getDefault().getID()),
// called before Start: Go takes UTC for local time on Android, and the console stamps
// its rows in local time, as the log and profile file names are.
func Zone(id string) {
	mu.Lock()
	defer mu.Unlock()
	zone = id
}

// Start runs the game on the view's grid, in cells sized by the screen's density.
// files is the app's own directory, holding the user root, logs and journals; wad,
// the APK's assets, is the system root; vif.toml and the keymap are brought up to
// date as a terminal run's are. raster paints with the software raster. away holds
// the game behind the start screen until Resume, starting over if the grid changes.
func Start(files, wad string, width, height int32, density float32, raster, away bool, speaker Speaker, listener Listener) error {
	tex, err := loadTexture()
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if run != nil {
		return errors.New("a game is already running")
	}
	// Once, before any run reads the time: a run's goroutines can outlive it
	localZone.Do(func() {
		if loc, err := time.LoadLocation(zone); zone != "" && err == nil {
			time.Local = loc
		}
	})
	for k, v := range map[string]string{
		"HOME":               files,
		"XDG_CONFIG_HOME":    filepath.Join(files, "config"),
		"XDG_STATE_HOME":     filepath.Join(files, "state"),
		paths.SystemRootsEnv: wad,
	} {
		if err := os.Setenv(k, v); err != nil {
			return err
		}
	}
	settings, file, notes, err := paths.LoadSettings("")
	if err != nil {
		return err
	}
	p := settings.Paths
	vlog.Configure(vlog.Config{Dir: cmp.Or(p.Log, paths.DefaultLogDir()),
		JournalDir: cmp.Or(p.Journal, paths.DefaultJournalDir()), Spawn: core.Go})
	v := newView(tex, raster, density, settings.Presentation.Cell)
	v.listener = listener
	v.setGrid(int(width), int(height))
	if away {
		v.setAway(true) // before the run reads its reports, so it never starts going
	}
	own := paths.OwnSettings("", file)
	cfg := app.Config{Play: settings.Play, Settings: paths.SettingsFor("", file),
		AudioBuffer: time.Duration(settings.Audio.BufferMs) * time.Millisecond}
	cfg.Resources.ApplySettings(settings, own)
	// The app's private files, where the state root was put above: the system ends
	// a backgrounded process, and its places come back with the next.
	if settings.Session.ResumeAfterRestart {
		cfg.Membership.PlaceStore = paths.PlaceFile{Path: paths.PlacesFile()}
		cfg.Membership.SignInStore = paths.PlaceFile{Path: paths.SignInsFile()}
	}
	if len(notes) > 0 && own {
		cfg.Notice = paths.UpgradeStore(cfg.Settings)
	}
	cfg.Notice = app.JoinNotice(cfg.Notice, resource.UpgradeKeymap(cfg.Resources))
	// Android keeps a device's networks from the Go runtime and its Wi-Fi drops
	// broadcasts with no lock held, so the app lists the one and holds the other
	host := app.Host{Presentation: v, StartScreen: true, Listening: listener.Listening,
		Subnets: func() []netip.Prefix { return subnets(listener.Subnets()) }}
	if speaker != nil {
		host.Audio = sink{speaker}
	}
	run = v
	go func() {
		err := app.Run(cfg, host)
		mu.Lock()
		run = nil
		mu.Unlock()
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		listener.Exited(msg)
	}()
	return nil
}

// subnets reads the networks the app listed, leaving out what does not parse
func subnets(list string) []netip.Prefix {
	var out []netip.Prefix
	for s := range strings.SplitSeq(list, ",") {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// sink is the speaker as the writer the mixer streams to
type sink struct{ Speaker }

func (s sink) Write(p []byte) (int, error) {
	if err := s.Speaker.Write(p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func current() *view {
	mu.Lock()
	defer mu.Unlock()
	return run
}

// Resize follows the view's pixels; a grid that changes is reported to the run
func Resize(width, height int32) {
	if v := current(); v != nil && v.setGrid(int(width), int(height)) {
		cols, rows := v.Size()
		v.post(terminal.Event{Type: terminal.EventResize, Width: cols, Height: rows})
	}
}

// Tap is a finger going down or up at the view's pixel x, y: the left button
// pressed or released on the cell under it
func Tap(x, y int32, down bool) {
	if v := current(); v != nil {
		if ev, ok := v.touch(int(x), int(y), down); ok {
			v.post(ev)
		}
	}
}

// Drag is a pressed finger moving to the view's pixel x, y: the left button dragged
// to the cell under it, once per cell
func Drag(x, y int32) {
	if v := current(); v != nil {
		if ev, ok := v.drag(int(x), int(y)); ok {
			v.post(ev)
		}
	}
}

// OnStatusBar reports whether the view's pixel row y is drawn on the status bar,
// where a finger presses with the pointer off and three taps raise or drop the
// system keyboard
func OnStatusBar(y int32) bool {
	v := current()
	if v == nil {
		return false
	}
	_, cy := v.cellAt(0, int(y))
	return v.onBar(cy)
}

// KeyDown is a key the view received going down, or repeating while held:
// Android's key code, the character it types without Ctrl or Alt (0 for none) and
// its meta state. It reports whether the game takes the key; one it does not, a
// volume key, is left to the system.
func KeyDown(code, typed, meta int32, repeat bool) bool {
	ev, ok := keyEvent(code, rune(typed), meta)
	if v := current(); v != nil && ok {
		v.keys.Down(uint32(code), ev, repeat)
	}
	return ok
}

// KeyUp is a key the view received coming up, taken as its KeyDown was
func KeyUp(code, typed, meta int32) bool {
	_, ok := keyEvent(code, rune(typed), meta)
	if v := current(); v != nil && ok {
		v.keys.Up(uint32(code))
	}
	return ok
}

// Key is a key pressed and let go at once, with no up of its own to come: Back, or
// the system keyboard's delete
func Key(code, typed, meta int32) bool {
	ev, ok := keyEvent(code, rune(typed), meta)
	if v := current(); v != nil && ok {
		v.keys.Tap(ev)
	}
	return ok
}

// Text is what the system keyboard committed or a paste brought, which the run types
// a character at a time: one event carries it, so a long paste fits the queue whole
func Text(s string) {
	if v := current(); v != nil && s != "" {
		v.post(terminal.Event{Type: terminal.EventPaste, Text: s})
	}
}

// FocusLost ends every key held: the window's keys go elsewhere, their ups too
func FocusLost() {
	if v := current(); v != nil {
		v.keys.Lost()
	}
}

// Pause and Resume follow the activity: a solo run pauses while it is hidden,
// and the drawer is handed the whole grid when it comes back
func Pause() {
	if v := current(); v != nil {
		v.setAway(true)
	}
}

func Resume() {
	if v := current(); v != nil {
		v.Sync()
		v.setAway(false)
	}
}

// Zoom scales the cell by a pinch's scale, within the range the browser's zoom
// keeps too; a grid that changes is reported as a resize, which a crop_on_resize
// map follows, and the run keeps the cell in vif.toml for the next
func Zoom(scale float32) {
	v := current()
	if v == nil {
		return
	}
	v.zoom(float64(scale))
	v.mu.Lock()
	w, h := v.w, v.h
	v.mu.Unlock()
	Resize(int32(w), int32(h))
}

// Menu is what the player asked of the menu the app draws: an op on the node id
// names (set, step, activate, submit, search or close) with its argument, applied by
// the run as the key path would (native-menu.md §5). The UI thread never waits: with
// the queue full the op is lost, as a touch would be.
func Menu(op, id, arg string) {
	if v := current(); v != nil {
		select {
		case v.menuOps <- mode.MenuOp{Op: op, ID: id, Arg: arg}:
		default:
		}
	}
}

// Keyboard is the pixels at the view's foot the system keyboard covers, 0 while it
// is down: the status bar keeps its input to the bottom row's left half, which the
// view repeats above it, and the console stays above both
func Keyboard(px int32) {
	if v := current(); v != nil {
		v.covered.Store(max(px, 0))
	}
}

// Quit ends the run as a terminal closing does
func Quit() {
	if v := current(); v != nil {
		v.post(terminal.Event{Type: terminal.EventClosed})
	}
}

// Frame is the newest frame since the last taken, nil when no cell changed, laid
// out as glyph.Frames.Take describes: columns, rows, then for a cell drawer the
// changed cells with their slot in Atlas and ARGB colors; for the raster, RGBA rows
func Frame() []byte {
	if v := current(); v != nil {
		return v.Take()
	}
	return nil
}

// Atlas is the texture a cell drawer paints glyphs from, as glyph.NewTexture lays
// it out: cell width, height and slot count as words, each slot's rows one below
// the last with a byte a pixel, 255 where the glyph is set, then each slot's rune
func Atlas() ([]byte, error) {
	tex, err := loadTexture()
	if err != nil {
		return nil, err
	}
	return tex.Bytes(), nil
}
