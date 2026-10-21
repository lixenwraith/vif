//go:build js && wasm

package main

import (
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/browser"
	"github.com/lixenwraith/vif/pkg/glyph"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func init() {
	platformHost = app.Host{Presentation: &browserHost{}, Audio: browser.Speaker()}
	pageSettings = keptSettings
}

// keptSettings reads the settings document the page kept from an earlier visit
// over s; one this build cannot read is left out, and replaced at the first change.
func keptSettings(s *paths.Settings) paths.SettingsStore {
	kept := &pageStore{doc: browser.Settings()}
	if parsed, err := paths.ParseSettings(kept.doc); err == nil {
		*s = parsed
	} else {
		kept.doc = nil
	}
	return kept
}

// pageStore is the settings document as the page keeps it, in the browser's
// storage for this site
type pageStore struct{ doc []byte }

func (p *pageStore) Load() ([]byte, error) { return p.doc, nil }

func (p *pageStore) Store(doc []byte) error {
	p.doc = doc
	browser.KeepSettings(doc)
	return nil
}

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
	h.host, err = browser.Open(atlas, parameter.StatusRows)
	return err
}

func (h *browserHost) Surface() render.Surface             { return h.host }
func (h *browserHost) Size() (int, int)                    { return h.host.Size() }
func (h *browserHost) Blocked() time.Duration              { return h.host.Blocked() }
func (h *browserHost) CellAspect() vmath.Aspect            { return h.aspect }
func (h *browserHost) Events() <-chan terminal.Event       { return h.host.Events() }
func (h *browserHost) SetPointerMode(enabled, motion bool) { h.host.SetPointerMode(enabled, motion) }
func (h *browserHost) Covered() bool                       { return h.host.Covered() }

func (h *browserHost) SetKeyReport(report terminal.KeyReport, allow terminal.KeySource) error {
	return h.host.SetKeyReport(report, allow)
}
