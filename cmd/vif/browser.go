//go:build js && wasm

package main

import (
	"errors"
	"io"
	"syscall/js"
	"time"

	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/terminal/atlas"
	"github.com/lixenwraith/vif/internal/app"
	"github.com/lixenwraith/vif/internal/asset"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/paths"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// What crosses the page's bridge that is vif's own, as web/vif.js names it: the
// menu's snapshot out and its ops back, the mixer's PCM out, the player leaving for
// the start screen and returning, and the two documents the page keeps
const (
	kindMenu    = "menu"
	kindAudio   = "audio"
	kindAway    = "away"
	docSettings = "settings"
	docPlaces   = "places"
)

// pageQueue bounds what the page's own messages queue for the run: a post never
// waits, since the poster is the Worker's event loop, which a blocked callback holds
const pageQueue = 256

func init() {
	var pres app.Presentation = &browserHost{}
	if terminal.PageHas(kindMenu) {
		pres = &menuHost{browserHost: &browserHost{}, ops: make(chan mode.MenuOp, pageQueue)}
	}
	platformHost = app.Host{Presentation: pres, Audio: speaker(), Pressed: true, StartScreen: true}
	pageSettings = func(s *paths.Settings) (paths.SettingsStore, []string) {
		return keptSettings(terminal.PageKept(docSettings), s)
	}
	pagePlaces = tabPlaces{}
}

// keptSettings reads doc, the settings document the page kept from an earlier visit,
// over s, as this build brings it up to date (paths.UpgradeSettings), naming the
// changes; one it cannot bring up to date is read as it stands, for writing it back
// to say why, and one this build cannot read at all is replaced at the first change.
func keptSettings(doc []byte, s *paths.Settings) (paths.SettingsStore, []string) {
	kept := &pageStore{doc: doc}
	if len(kept.doc) == 0 {
		return kept, nil
	}
	doc, notes, err := paths.UpgradeSettings(kept.doc)
	if err != nil {
		doc, notes = kept.doc, []string{err.Error()}
	}
	parsed, err := paths.ParseSettings(doc)
	if err != nil && !errors.As(err, new(paths.UnknownKeys)) {
		kept.doc = nil
		return kept, nil
	}
	*s = parsed
	return kept, notes
}

// pageStore is the settings document as the page keeps it, in the browser's
// storage for this site; until the first change, the embedded one, as a vif.toml
// not there yet starts (paths.FileStore)
type pageStore struct{ doc []byte }

func (p *pageStore) Load() ([]byte, error) {
	if len(p.doc) == 0 {
		return asset.DefaultSettings, nil
	}
	return p.doc, nil
}

func (p *pageStore) Store(doc []byte) error {
	p.doc = doc
	terminal.PageKeep(docSettings, doc)
	return nil
}

// tabPlaces is the places document as the tab keeps it, across a reload
type tabPlaces struct{}

func (tabPlaces) Load() ([]byte, error) { return terminal.PageKept(docPlaces), nil }

func (tabPlaces) Store(doc []byte) error {
	terminal.PageKeep(docPlaces, doc)
	return nil
}

// speaker is the page's speakers, the writer the mixer streams to: each buffer goes
// to the page but a silent one, so a muted game posts nothing and the page opens no
// AudioContext until a sound plays. Nil on a page that plays no sound.
func speaker() io.WriteCloser {
	if !terminal.PageHas(kindAudio) {
		return nil
	}
	return pageSpeaker{}
}

type pageSpeaker struct{}

func (pageSpeaker) Write(p []byte) (int, error) {
	for _, b := range p {
		if b != 0 {
			terminal.PagePost(kindAudio, map[string]any{"data": terminal.PageBytes(p)})
			break
		}
	}
	return len(p), nil
}

func (pageSpeaker) Close() error { return nil }

// browserHost is the page as the run's Presentation. A scenario change rebuilds the
// run, not the page: it opens on the first Init and lives as long as the Worker.
type browserHost struct {
	page *terminal.Page
	away chan bool // the player left for the page's start screen, or came back
}

func (h *browserHost) Name() string           { return "browser" }
func (h *browserHost) Dependencies() []string { return nil }
func (h *browserHost) Start() error           { return nil }
func (h *browserHost) Stop() error            { return nil }

func (h *browserHost) Init() error {
	if h.page != nil {
		return nil
	}
	page, err := terminal.OpenPage(terminal.PageConfig{BarRows: parameter.StatusRows, Cell: settings.Presentation.Cell,
		CellLeast: parameter.CellLeast, CellMost: parameter.CellMost, CellStep: parameter.CellStep, Font: atlas.HackWeb()})
	if err != nil {
		return err
	}
	h.page, h.away = page, make(chan bool, pageQueue)
	page.Handle(kindAway, func(msg js.Value) {
		select {
		case h.away <- msg.Get("away").Truthy():
		default:
		}
	})
	return nil
}

func (h *browserHost) Surface() render.Surface             { return h.page }
func (h *browserHost) Size() (int, int)                    { return h.page.Size() }
func (h *browserHost) Blocked() time.Duration              { return h.page.Blocked() }
func (h *browserHost) Events() <-chan terminal.Event       { return h.page.Events() }
func (h *browserHost) SetPointerMode(enabled, motion bool) { h.page.SetPointerMode(enabled, motion) }
func (h *browserHost) Covered() int                        { return h.page.Covered() }
func (h *browserHost) Cell() float64                       { return h.page.Cell() }

// CellAspect is the page's: it draws every cell one wide to two tall, at any zoom
func (h *browserHost) CellAspect() vmath.Aspect { return vmath.RefAspect }

// Away reports the player leaving for the page's start screen and returning, as
// Android reports its app sent to the background: a solo run waits meanwhile
func (h *browserHost) Away() <-chan bool { return h.away }

func (h *browserHost) SetKeyReport(report terminal.KeyReport, allow terminal.KeySource) error {
	return h.page.SetKeyReport(report, allow)
}

// menuHost is the page as a host that draws the configuration menu itself, which a
// page whose script handles the menu does: snapshots go to the page and its ops come
// back on a channel the loop selects on. The ops channel outlives a run, as the host does.
type menuHost struct {
	*browserHost
	ops chan mode.MenuOp
}

func (h *menuHost) Init() error {
	if err := h.browserHost.Init(); err != nil {
		return err
	}
	h.page.Handle(kindMenu, func(msg js.Value) {
		select {
		case h.ops <- mode.MenuOp{Op: msg.Get("op").String(), ID: msg.Get("id").String(), Arg: msg.Get("arg").String()}:
		default:
		}
	})
	return nil
}

// Menu hands the page the menu to draw, JSON in a string, empty once it closed
func (h *menuHost) Menu(snapshot []byte) {
	terminal.PagePost(kindMenu, map[string]any{"json": string(snapshot)})
}

func (h *menuHost) MenuOps() <-chan mode.MenuOp { return h.ops }
