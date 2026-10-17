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

	loadFont = sync.OnceValues(func() (*font, error) {
		a, err := glyph.Parse(asset.DefaultFont)
		if err != nil {
			return nil, err
		}
		return newFont(a), nil
	})
)

// Start runs the game on the grid the view's pixels hold. files is the app's own
// directory, holding the user root, logs and journals; wad is where the APK's
// assets were copied, read as the system root. raster paints frames with the
// software raster and hands the drawer pixels instead of cells.
func Start(files, wad string, width, height int32, raster bool, speaker Speaker, listener Listener) error {
	f, err := loadFont()
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

	v := newView(f, raster)
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

// Touch is a finger going down or up at the view's pixel x, y: the left button
// pressed or released on the cell under it
func Touch(x, y int32, down bool) {
	if v := current(); v != nil {
		if ev, ok := v.touch(int(x), int(y), down); ok {
			v.post(ev)
		}
	}
}

// Key is a key the view received: Android's key code, the character it types
// without Ctrl or Alt (0 for none) and its meta state. It reports whether the game
// takes the key; one it does not, a volume key, is left to the system.
func Key(code, char, meta int32) bool {
	ev, ok := keyEvent(code, rune(char), meta)
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

// Frame is the newest frame since the last taken, nil when there is none. It is
// little-endian 32-bit words: columns, rows, then for a cell drawer the count of
// changed cells and per cell its index, glyph slot in Atlas (-1 for background
// only), foreground and background ARGB; for the raster, RGBA rows top first.
func Frame() []byte {
	if v := current(); v != nil {
		return v.take()
	}
	return nil
}

// Atlas is the texture a cell drawer paints glyphs from: cell width, height and
// glyph count as words, then each glyph's rows one below the last, a byte a pixel,
// 255 where the glyph is set
func Atlas() ([]byte, error) {
	f, err := loadFont()
	if err != nil {
		return nil, err
	}
	return f.mask, nil
}
