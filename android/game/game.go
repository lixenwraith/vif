// Package game is vif's Android host, bound to Java by gomobile: the activity
// starts a run with its view's size and its speakers, posts touches and keys,
// and pulls the frames its drawer paints. A process runs one game at a time.
package game

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/vlog"
	"github.com/lixenwraith/vif/pkg/glyph"
)

// Speaker is the AudioTrack the mixer streams 44.1 kHz 16-bit stereo PCM to
type Speaker interface {
	Write(pcm []byte) error
	Close() error
}

// Listener hears the run end: err is what ended it, empty when the player quit
type Listener interface {
	Exited(err string)
}

var (
	mu  sync.Mutex
	run *view // the running game's screen, nil between runs

	loadTexture = sync.OnceValues(func() (*glyph.Texture, error) {
		a, err := glyph.Parse(asset.DefaultFont)
		if err != nil {
			return nil, err
		}
		return glyph.NewTexture(a), nil
	})
)

// Start runs the game on the grid the view's pixels hold in cells sized by the
// screen's density. files is the app's own directory, holding the user root, logs
// and journals; wad is where the APK's assets were copied, read as the system root.
// raster paints frames with the software raster and hands the drawer pixels.
func Start(files, wad string, width, height int32, density float32, raster bool, speaker Speaker, listener Listener) error {
	tex, err := loadTexture()
	if err != nil {
		return err
	}
	mu.Lock()
	defer mu.Unlock()
	if run != nil {
		return errors.New("a game is already running")
	}
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
	vlog.Configure(vlog.Config{Dir: paths.DefaultLogDir(), JournalDir: paths.DefaultJournalDir(), Spawn: core.Go})

	v := newView(tex, raster, density)
	v.setGrid(int(width), int(height))
	cfg := app.Config{Host: app.Host{Presentation: v}}
	if speaker != nil {
		cfg.Host.Audio = sink{speaker}
	}
	run = v
	go func() {
		err := app.Run(cfg)
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

// Key is a key the view received: Android's key code, the character it types
// without Ctrl or Alt (0 for none) and its meta state. It reports whether the game
// takes the key; one it does not, a volume key, is left to the system.
func Key(code, typed, meta int32) bool {
	ev, ok := keyEvent(code, rune(typed), meta)
	if v := current(); v != nil && ok {
		v.post(ev)
	}
	return ok
}

// Text is what the system keyboard committed, typed a character at a time
func Text(s string) {
	if v := current(); v != nil {
		for _, r := range s {
			v.post(textEvent(r))
		}
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
