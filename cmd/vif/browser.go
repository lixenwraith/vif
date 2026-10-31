//go:build js && wasm

package main

import (
	"errors"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/app"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/browser"
	"github.com/lixenwraith/vif/pkg/glyph"
	"github.com/lixenwraith/vif/pkg/vmath"
)

func init() {
	var pres app.Presentation = &browserHost{}
	if browser.HasMenu() {
		pres = &menuHost{browserHost: &browserHost{}, ops: make(chan mode.MenuOp, menuOps)}
	}
	platformHost = app.Host{Presentation: pres, Audio: browser.Speaker(), Pressed: true, StartScreen: true}
	pageSettings = keptSettings
	pageClaims = tabClaims{}
}

// keptSettings reads the settings document the page kept from an earlier visit
// over s, naming the keys in it this build does not know; one this build cannot
// read at all is left out, and replaced at the first change.
func keptSettings(s *paths.Settings) (paths.SettingsStore, paths.UnknownKeys) {
	kept := &pageStore{doc: browser.Settings()}
	parsed, err := paths.ParseSettings(kept.doc)
	var unknown paths.UnknownKeys
	if err == nil || errors.As(err, &unknown) {
		*s = parsed
	} else {
		kept.doc = nil
	}
	return kept, unknown
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

// tabClaims is the claims document as the tab keeps it, across a reload
type tabClaims struct{}

func (tabClaims) Load() ([]byte, error) { return browser.Claims(), nil }

func (tabClaims) Store(doc []byte) error {
	browser.KeepClaims(doc)
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
	h.host, err = browser.Open(atlas, browser.Config{BarRows: parameter.StatusRows, Cell: settings.Presentation.Cell,
		CellLeast: parameter.CellLeast, CellMost: parameter.CellMost, CellStep: parameter.CellStep})
	return err
}

func (h *browserHost) Surface() render.Surface             { return h.host }
func (h *browserHost) Size() (int, int)                    { return h.host.Size() }
func (h *browserHost) Blocked() time.Duration              { return h.host.Blocked() }
func (h *browserHost) CellAspect() vmath.Aspect            { return h.aspect }
func (h *browserHost) Events() <-chan terminal.Event       { return h.host.Events() }
func (h *browserHost) SetPointerMode(enabled, motion bool) { h.host.SetPointerMode(enabled, motion) }
func (h *browserHost) Covered() bool                       { return h.host.Covered() }
func (h *browserHost) Cell() float64                       { return h.host.Cell() }
func (h *browserHost) Away() <-chan bool                   { return h.host.Away() }

func (h *browserHost) SetKeyReport(report terminal.KeyReport, allow terminal.KeySource) error {
	return h.host.SetKeyReport(report, allow)
}

// menuOps bounds the ops a page's menu queues; a post never waits on the run
const menuOps = 256

// menuHost is the page as a host that draws the configuration menu itself, which a
// worker defining vifMenu does: snapshots go to the page and its ops come back on a
// channel the loop selects on. The ops channel outlives a run, as the host does.
type menuHost struct {
	*browserHost
	ops chan mode.MenuOp
}

func (h *menuHost) Init() error {
	if err := h.browserHost.Init(); err != nil {
		return err
	}
	h.host.MenuOp = func(op, id, arg string) {
		select {
		case h.ops <- mode.MenuOp{Op: op, ID: id, Arg: arg}:
		default:
		}
	}
	return nil
}

func (h *menuHost) Menu(snapshot []byte)        { h.host.Menu(snapshot) }
func (h *menuHost) MenuOps() <-chan mode.MenuOp { return h.ops }
