//go:build !vif_headless

package app

import (
	"github.com/lixenwraith/vif/pkg/vmath"
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
	CellAspect() vmath.Aspect // width over height of the cells it draws
	Events() <-chan terminal.Event
	SetPointerMode(enabled, motion bool)
}

// awayPresentation is one its player can leave without quitting, an app sent to the
// background: it reports leaving and returning, and a solo run pauses meanwhile
type awayPresentation interface{ Away() <-chan bool }

// coveredPresentation is one a keyboard can cover: while it does, the host shows
// the left half of the status bar's bottom row again above the keyboard, and the
// bar keeps its input to that half
type coveredPresentation interface{ Covered() bool }

// keyReportPresentation is one that can report key releases when asked, answering
// each ask with an EventKeyReport on Events; any other reports presses alone
type keyReportPresentation interface {
	SetKeyReport(report terminal.KeyReport, allow terminal.KeySource) error
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

func (t terminalHost) Surface() render.Surface  { return t.Terminal() }
func (t terminalHost) Size() (int, int)         { return t.Terminal().Size() }
func (t terminalHost) Blocked() time.Duration   { return t.Terminal().Blocked() }
func (t terminalHost) CellAspect() vmath.Aspect { return vmath.RefAspect }

// Fini restores the tty, which the crash handler calls before it prints
func (t terminalHost) Fini() { t.Terminal().Fini() }

func (t terminalHost) SetKeyReport(report terminal.KeyReport, allow terminal.KeySource) error {
	return t.Terminal().SetKeyReport(report, allow)
}

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
