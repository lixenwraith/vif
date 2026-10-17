//go:build vif_gui && (linux || freebsd)

package main

import (
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/glyph"
	"github.com/lixenwraith/vif/pkg/vmath"
	"github.com/lixenwraith/vif/pkg/window"
)

func init() { platformHost.Presentation = &windowHost{} }

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
	atlas, err := glyph.Parse(asset.DefaultFont)
	if err != nil {
		return err
	}
	h.aspect = atlas.Aspect()
	h.win, err = window.Open(window.Config{Title: parameter.WindowTitle, Atlas: atlas,
		Cols: parameter.WindowCols, Rows: parameter.WindowRows, Scale: *flagScale})
	return err
}

func (h *windowHost) Surface() render.Surface             { return h.win }
func (h *windowHost) Size() (int, int)                    { return h.win.Size() }
func (h *windowHost) Blocked() time.Duration              { return h.win.Blocked() }
func (h *windowHost) CellAspect() vmath.Aspect            { return h.aspect }
func (h *windowHost) Events() <-chan terminal.Event       { return h.win.Events() }
func (h *windowHost) SetPointerMode(enabled, motion bool) { h.win.SetPointerMode(enabled, motion) }
