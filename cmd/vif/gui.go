//go:build (linux || freebsd || windows) && !vif_headless && !vif_nogui

package main

import (
	"fmt"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/atlas"
	"github.com/lixenwraith/terminal/glyph"
	"github.com/lixenwraith/terminal/window"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// The window is in every Linux, FreeBSD and Windows build: -g opens it, a vif_gui
// build by default (gui_default.go), and vif_nogui leaves it out of the binary
func init() { openWindow = func() { platformHost.Presentation = &windowHost{} } }

// windowHost is a desktop window as the run's Presentation. A scenario change
// rebuilds the run, not the window: it opens on the first Init and closes when
// the process exits.
type windowHost struct {
	win    *window.Window
	aspect vmath.Aspect
}

func (h *windowHost) Name() string           { return "window" }
func (h *windowHost) Dependencies() []string { return nil }
func (h *windowHost) Start() error           { return nil }
func (h *windowHost) Stop() error            { return nil }

func (h *windowHost) Init() error {
	if h.win != nil {
		return nil
	}
	font := atlas.Hack()
	strikes := font.Strikes() // the largest is the face's own size
	h.aspect = vmath.Aspect(strikes[len(strikes)-1].Aspect())
	cols, rows := parameter.WindowCols, parameter.WindowRows
	if c, r, err := parseSize(flagSession.size); err == nil {
		cols, rows = c, r // the size its run names, which the window opens holding
	}
	var err error
	h.win, err = window.Open(window.Config{Title: parameter.WindowTitle, Font: font,
		Cols: cols, Rows: rows, Scale: *flagScale,
		Cell:      settings.Presentation.Cell,
		Zoom:      glyph.Zoom{Least: parameter.CellLeast, Most: parameter.CellMost, Step: parameter.CellStep},
		Maximized: windowMaximized, Fullscreen: windowFullscreen})
	if err != nil && !windowOnly {
		return fmt.Errorf("%w (-cli plays in the terminal)", err)
	}
	return err
}

func (h *windowHost) Surface() render.Surface             { return h.win }
func (h *windowHost) Size() (int, int)                    { return h.win.Size() }
func (h *windowHost) Blocked() time.Duration              { return h.win.Blocked() }
func (h *windowHost) CellAspect() vmath.Aspect            { return h.aspect }
func (h *windowHost) Cell() float64                       { return h.win.Cell() }
func (h *windowHost) Events() <-chan terminal.Event       { return h.win.Events() }
func (h *windowHost) SetPointerMode(enabled, motion bool) { h.win.SetPointerMode(enabled, motion) }

func (h *windowHost) SetKeyReport(report terminal.KeyReport, allow terminal.KeySource) error {
	return h.win.SetKeyReport(report, allow)
}
