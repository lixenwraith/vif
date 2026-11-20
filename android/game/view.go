package game

import (
	"sync/atomic"

	"github.com/lixenwraith/terminal/android"
	"github.com/lixenwraith/terminal/atlas"
	"github.com/lixenwraith/terminal/glyph"
	"github.com/lixenwraith/vif/internal/mode"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/pkg/vmath"
)

// eventQueue holds what the app posts while the run's loop is busy; a post never
// waits, since the poster is Android's UI thread
const eventQueue = 256

// view is the activity's SurfaceView as an app.Presentation: an android.View,
// which holds the grid, the frames the drawer takes and the touches and keys as
// terminal events, with what is the game's. The app draws the menu itself: each
// snapshot goes to the listener, and its ops come back on a channel.
type view struct {
	*android.View
	listener Listener
	away     chan bool
	menuOps  chan mode.MenuOp
	held     atomic.Bool // the drawer was handed the texture this run opened on
}

// newView opens the view at cell, a zoom vif.toml kept, 0 for none
func newView(raster bool, density float32, cell float64) *view {
	return &view{
		View: android.NewView(android.Config{Font: atlas.Hack(), Raster: raster, Density: density, Cell: cell,
			Zoom:    glyph.Zoom{Least: parameter.CellLeast, Most: parameter.CellMost, Step: parameter.CellStep},
			BarRows: parameter.StatusRows}),
		away: make(chan bool, eventQueue), menuOps: make(chan mode.MenuOp, eventQueue)}
}

func (v *view) Name() string           { return "android" }
func (v *view) Dependencies() []string { return nil }
func (v *view) Init() error            { return nil }
func (v *view) Start() error           { return nil }
func (v *view) Stop() error            { return nil }

func (v *view) Surface() render.Surface     { return v }
func (v *view) Away() <-chan bool           { return v.away }
func (v *view) MenuOps() <-chan mode.MenuOp { return v.menuOps }

// Menu hands the app the menu to draw, as gomobile passes a tree: JSON in a string,
// empty for a closed menu
func (v *view) Menu(snapshot []byte) { v.listener.Menu(string(snapshot)) }

func (v *view) CellAspect() vmath.Aspect { return vmath.Aspect(v.Aspect()) }

// AtlasChanged hands over the texture the run opened on first, whichever strike it
// is: the app built its drawer from Atlas before a run had sized a cell
func (v *view) AtlasChanged() []byte {
	if v.held.CompareAndSwap(false, true) {
		return v.Atlas()
	}
	return v.View.AtlasChanged()
}

func (v *view) setAway(away bool) {
	select {
	case v.away <- away:
	default:
	}
}

// Probe is glyph.Probe over a strike of the shipped font: the oracle the
// instrumented test checks a Java drawer against, same frame, same pixels
type Probe struct {
	tex   *glyph.Texture
	probe *glyph.Probe
}

// NewProbe lays the probe out cols cells wide in the strike height pixels tall
func NewProbe(cols, height int32) *Probe {
	tex := glyph.NewTexture(atlas.Hack().Strike(int(height)))
	return &Probe{tex, glyph.NewProbe(tex, int(cols))}
}

// Atlas is the texture the probe's frame names the slots of
func (p *Probe) Atlas() []byte { return p.tex.Bytes() }

// Frame is the probe as a whole cell frame
func (p *Probe) Frame() []byte { return p.probe.Frame() }

// Pixels is the frame as glyph.Raster draws it, RGBA rows top first
func (p *Probe) Pixels() []byte { return p.probe.Pixels() }
