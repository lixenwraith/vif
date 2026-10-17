//go:build js && wasm

package main

import (
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/browser"
	"github.com/lixenwraith/vif/pkg/glyph"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func init() { platformHost.Presentation = &browserHost{} }

// browserHost is the page as the run's Presentation. A scenario change rebuilds the
// run, not the page: it opens on the first Init and lives as long as the Worker.
type browserHost struct {
	host   *browser.Host
	aspect vmath.Aspect
}

func (h *browserHost) Name() string           { return "browser" }
func (h *browserHost) Dependencies() []string { return nil }
func (h *browserHost) Start() error           { return nil }
func (h *browserHost) Stop() error            { return nil }

func (h *browserHost) Init() error {
	if h.host != nil {
		return nil
	}
	atlas, err := glyph.Parse(asset.DefaultFont)
	if err != nil {
		return err
	}
	h.aspect = atlas.Aspect()
	h.host, err = browser.Open(atlas)
	return err
}

func (h *browserHost) Surface() render.Surface             { return h.host }
func (h *browserHost) Size() (int, int)                    { return h.host.Size() }
func (h *browserHost) Blocked() time.Duration              { return h.host.Blocked() }
func (h *browserHost) CellAspect() vmath.Aspect            { return h.aspect }
func (h *browserHost) Events() <-chan terminal.Event       { return h.host.Events() }
func (h *browserHost) SetPointerMode(enabled, motion bool) { h.host.SetPointerMode(enabled, motion) }
