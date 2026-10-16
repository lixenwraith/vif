//go:build !vif_headless

package app

import (
	"io"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/internal/service"
)

// Presentation is a host's screen and the player's device: a service the hub runs,
// the surface frames are flushed to, its size, how long it has held a frame, and the
// events it reports in the terminal's vocabulary. The terminal is one; a window or
// an Android view is another, posting the keys and pointer cells it maps.
type Presentation interface {
	service.Service
	Surface() render.Surface
	Size() (width, height int)
	Blocked() time.Duration
	Events() <-chan terminal.Event
	SetPointerMode(enabled, motion bool)
}

// Host is what a platform supplies to a run. A nil field is the build's own: the
// terminal, and speakers reached by a backend's name.
type Host struct {
	Presentation Presentation
	Audio        io.WriteCloser // takes the mixer's 44.1 kHz 16-bit stereo PCM
}

const defaultMouseMode = terminal.MouseModeClick | terminal.MouseModeDrag

// terminalHost is the terminal as a Presentation, around the service that opens it
type terminalHost struct{ *service.TerminalService }

func (t terminalHost) Surface() render.Surface { return t.Terminal() }
func (t terminalHost) Size() (int, int)        { return t.Terminal().Size() }
func (t terminalHost) Blocked() time.Duration  { return t.Terminal().Blocked() }

// Fini restores the tty, which the crash handler calls before it prints
func (t terminalHost) Fini() { t.Terminal().Fini() }

func (t terminalHost) SetPointerMode(enabled, motion bool) {
	if !enabled {
		t.Terminal().SetMouseMode(0)
		return
	}
	mode := defaultMouseMode
	if motion {
		mode |= terminal.MouseModeMotion
	}
	t.Terminal().SetMouseMode(mode)
}
