package mode

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/event"
	"github.com/lixenwraith/vif/internal/input"
	"github.com/lixenwraith/vif/internal/parameter"
)

// nativeMenu opens the menu on a run holding bots and other players, as a host
// drawing its own menu sees it
func nativeMenu(tb testing.TB) (*engine.GameContext, *Router, *awaySession) {
	tb.Helper()
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	session := &awaySession{menuSession: menuSession{bots: []engine.BotSeat{{ID: 10, Slot: 2, Graph: "default"}, {ID: 11, Slot: 3, Graph: "idle", Joining: true}, {ID: 12, Slot: 5, Graph: "default", Stopping: true}}},
		participants: []engine.SessionParticipant{{Entity: 7, Slot: 1}, {Entity: 8, Slot: 4, Holder: 1}, {Entity: 9, Slot: 6, Away: true}}}
	ctx.SessionCtl = session
	r := NewRouter(ctx, input.NewMachine())
	ExecuteCommand(ctx, "g")
	return ctx, r, session
}

func snapshot(tb testing.TB, r *Router) menuSnapshot {
	tb.Helper()
	var s menuSnapshot
	b := r.MenuSnapshot()
	if b == nil {
		tb.Fatal("the menu is closed")
	}
	if err := json.Unmarshal(b, &s); err != nil {
		tb.Fatal(err)
	}
	return s
}

// node is the snapshot's node of that id, zero when none
func (s menuSnapshot) node(id string) menuNode {
	var find func(nodes []menuNode) (menuNode, bool)
	find = func(nodes []menuNode) (menuNode, bool) {
		for _, n := range nodes {
			if n.ID == id {
				return n, true
			}
			if n, ok := find(n.Nodes); ok {
				return n, true
			}
		}
		return menuNode{}, false
	}
	n, _ := find(s.Nodes)
	return n
}

// TestAHostsOpReachesTheStateAKeyDoes: set turns a toggle to the word asked and
// cycles a choice to it, a word the node lacks or already shows changes nothing, step
// moves a stepper as Right does and a key moves what the op set; activate marks a
// pick, and runs an action, which closes the menu as Enter does.
func TestAHostsOpReachesTheStateAKeyDoes(t *testing.T) {
	ctx, r, _ := nativeMenu(t)
	r.MenuOp(MenuOp{"set", "settings/#Controls/free", "off"})
	if ctx.MouseFreeMode.Load() {
		t.Fatal("set off did not turn Follow mouse off")
	}
	ctx.SetStatusMessage("marker", parameter.StatusNotice)
	r.MenuOp(MenuOp{"set", "settings/#Controls/free", "off"})
	r.MenuOp(MenuOp{"set", "settings/#Controls/free", "sideways"})
	r.MenuOp(MenuOp{"step", "settings/#Controls/free", "+1"})
	if ctx.MouseFreeMode.Load() || ctx.GetStatusMessage() != "marker" {
		t.Fatalf("a set to the value shown, to a word the node lacks, or a step on a toggle acted: follow mouse %v, status %q", ctx.MouseFreeMode.Load(), ctx.GetStatusMessage())
	}
	showConfigMenu(ctx, "settings")
	ctx.SetOverlaySelection("settings/#Controls/free")
	r.changeConfigMenu(1, false)
	if !ctx.MouseFreeMode.Load() {
		t.Fatal("Right on the row the op set did not turn it over")
	}
	r.MenuOp(MenuOp{"set", "settings/#Controls/auto", "main"})
	if engine.AutoFireWords[ctx.AutoFire.Load()] != "main" {
		t.Fatalf("set main left auto-fire %q", engine.AutoFireWords[ctx.AutoFire.Load()])
	}
	r.MenuOp(MenuOp{"set", "settings/#Controls/auto", "off"})
	if engine.AutoFireWords[ctx.AutoFire.Load()] != "off" {
		t.Fatalf("set off left auto-fire %q", engine.AutoFireWords[ctx.AutoFire.Load()])
	}
	r.MenuOp(MenuOp{"step", "settings/#Display/fps", "+1"})
	if got, want := ctx.MaxFPS.Load(), int32(fpsStep(0, 1)); got != want {
		t.Fatalf("step +1 set max fps %d, want %d", got, want)
	}
	r.MenuOp(MenuOp{"step", "settings/#Display/fps", "-1"})
	if ctx.MaxFPS.Load() != 0 {
		t.Fatalf("step -1 left max fps %d", ctx.MaxFPS.Load())
	}
	r.MenuOp(MenuOp{"activate", "scenarios/#Installed/pick/tower", ""})
	s := snapshot(t, r)
	if configMenu(ctx).Choice != "tower" || !s.node("scenarios/#Installed/pick/tower").Marked || s.node("scenarios/start").Label != "Start tower" {
		t.Fatalf("activate on a pick: choice %q, start row %+v", configMenu(ctx).Choice, s.node("scenarios/start"))
	}
	ctx.World.Resources.Event.Queue.Consume()
	r.MenuOp(MenuOp{"activate", "game/pause", ""})
	held := slices.ContainsFunc(ctx.World.Resources.Event.Queue.Consume(), func(ev event.GameEvent) bool {
		p, ok := ev.Payload.(*event.GamePausePayload)
		return ok && ev.Type == event.EventGameHoldRequest && p.Paused
	})
	if configMenu(ctx) != nil || !held || r.MenuSnapshot() != nil {
		t.Fatalf("activate on Pause: menu open %v, pause asked %v", configMenu(ctx) != nil, held)
	}
}

// TestAnOpOnAVanishedIdIsDropped: a bot that left takes its row with it, and an op
// naming it, or any id the tree does not hold, does nothing and says nothing.
func TestAnOpOnAVanishedIdIsDropped(t *testing.T) {
	ctx, r, session := nativeMenu(t)
	ctx.SetStatusMessage("marker", parameter.StatusNotice)
	session.bots = nil
	r.MenuOp(MenuOp{"activate", "multiplayer/#Bots/drop/10", ""})
	r.MenuOp(MenuOp{"set", "nowhere/at/all", "on"})
	r.MenuOp(MenuOp{"submit", "multiplayer/#Players/player/99", `["x"]`})
	if len(session.calls) != 0 || configMenu(ctx) == nil || ctx.GetStatusMessage() != "marker" {
		t.Fatalf("an op on a vanished id acted: calls %q, menu open %v, status %q", session.calls, configMenu(ctx) != nil, ctx.GetStatusMessage())
	}
}

// TestAnOpOnADisabledIdAnswersWithItsReason: a disabled row carries the sentence its
// command refuses with, an op on it changes nothing and puts that sentence on the
// status line, which the snapshot carries as a notice.
func TestAnOpOnADisabledIdAnswersWithItsReason(t *testing.T) {
	ctx, r, _ := nativeMenu(t)
	ctx.Viewer.Store(true)
	why := snapshot(t, r).node("game/speed").Disabled
	if why == "" {
		t.Fatal("a replay viewer's speed row is not disabled")
	}
	scale := ctx.TimeCtl.Scale()
	r.MenuOp(MenuOp{"step", "game/speed", "+1"})
	s := snapshot(t, r)
	if ctx.TimeCtl.Scale() != scale || s.Status == nil || s.Status.Text != why || s.Status.Kind != "notice" {
		t.Fatalf("a step on a disabled row: scale %v from %v, status %+v, want %q", ctx.TimeCtl.Scale(), scale, s.Status, why)
	}
}

// TestAFailedSubmitKeepsItsErrorAndASuccessfulOneClosesTheMenu: a submit the run
// refuses leaves its error on the form, as a sentence, until the next op; one that
// succeeds closes the menu, fields left out taking their defaults.
func TestAFailedSubmitKeepsItsErrorAndASuccessfulOneClosesTheMenu(t *testing.T) {
	ctx, r, session := nativeMenu(t)
	session.err = errors.New("address in use")
	r.MenuOp(MenuOp{"submit", "multiplayer/#Session/host", `["127.0.0.1:0", "host"]`})
	s := snapshot(t, r)
	if s.Form == nil || s.Form.ID != "multiplayer/#Session/host" || s.Form.Error != "Address in use" {
		t.Fatalf("a refused submit left form %+v", s.Form)
	}
	r.MenuOp(MenuOp{"set", "settings/#Controls/free", "off"})
	if s := snapshot(t, r); s.Form != nil {
		t.Fatalf("the next op kept the error: %+v", s.Form)
	}
	session.err = nil
	r.MenuOp(MenuOp{"submit", "multiplayer/#Bots/add", `["2"]`})
	if configMenu(ctx) != nil || ctx.GetMode() != core.ModeNormal || !slices.Equal(session.calls, []string{"add 2:default"}) {
		t.Fatalf("a submit that succeeded: menu open %v, mode %v, calls %q", configMenu(ctx) != nil, ctx.GetMode(), session.calls)
	}
}

// TestSearchFindsWhatTheSlashKeyFinds: a search op lists the rows / lists, each by
// its id with its Page / Row label, in the snapshot and in the tui view alike; an
// empty search clears it.
func TestSearchFindsWhatTheSlashKeyFinds(t *testing.T) {
	ctx, r, _ := nativeMenu(t)
	r.MenuOp(MenuOp{Op: "search", Arg: "pause"})
	s := snapshot(t, r)
	var ids, keys []string
	for _, f := range s.Found {
		ids = append(ids, f.ID)
	}
	for _, row := range configMenu(ctx).Rows {
		keys = append(keys, row.Key)
	}
	want := []string{"game/pause", "settings/#Pause/pause_menu", "settings/#Pause/pause_command"}
	if s.Query != "pause" || !slices.Equal(ids, want) || !slices.Equal(keys, want) || s.Found[1].Label != "Settings / Pause in menu" {
		t.Fatalf("a search for pause: query %q, found %+v, tui rows %q", s.Query, s.Found, keys)
	}
	r.MenuOp(MenuOp{Op: "search"})
	if s := snapshot(t, r); s.Query != "" || s.Found != nil || configMenu(ctx).Rows[0].Key != "game" {
		t.Fatalf("an empty search: query %q, found %+v, first row %q", s.Query, s.Found, configMenu(ctx).Rows[0].Key)
	}
}

// TestCloseResumesAGameTheMenuPaused: the close op ends the pause the menu asked
// for and returns to Normal, with no menu to draw.
func TestCloseResumesAGameTheMenuPaused(t *testing.T) {
	ctx, r, _ := nativeMenu(t)
	asked := func() (out []bool) {
		for _, ev := range ctx.World.Resources.Event.Queue.Consume() {
			if p, ok := ev.Payload.(*event.GamePausePayload); ok && ev.Type == event.EventGamePauseRequest {
				out = append(out, p.Paused)
			}
		}
		return out
	}
	if got := asked(); !slices.Equal(got, []bool{true}) {
		t.Fatalf("opening the menu asked for pauses %v", got)
	}
	r.MenuOp(MenuOp{Op: "close"})
	if got := asked(); !slices.Equal(got, []bool{false}) || ctx.GetMode() != core.ModeNormal || ctx.IsOverlayActive() || r.MenuSnapshot() != nil {
		t.Fatalf("close: pauses asked %v, mode %v, overlay %v", got, ctx.GetMode(), ctx.IsOverlayActive())
	}
}

// TestTheSnapshotChangesOnlyWithTheMenu: the bytes are nil while the menu is closed,
// the same from one frame to the next while nothing changed, and different once an
// op changed a value, so a host is pushed only what it has not drawn.
func TestTheSnapshotChangesOnlyWithTheMenu(t *testing.T) {
	w := engine.NewWorld()
	ctx := engine.NewGameContext(w, 80, 24)
	ctx.SessionCtl = &menuSession{}
	r := NewRouter(ctx, input.NewMachine())
	if r.MenuSnapshot() != nil {
		t.Fatal("a closed menu has a snapshot")
	}
	ExecuteCommand(ctx, "g")
	a, b := r.MenuSnapshot(), r.MenuSnapshot()
	if a == nil || string(a) != string(b) {
		t.Fatalf("two frames of one menu differ:\n%s\n%s", a, b)
	}
	r.MenuOp(MenuOp{"set", "settings/#Controls/free", "off"})
	if c := r.MenuSnapshot(); string(c) == string(a) {
		t.Fatal("a value set did not change the snapshot")
	}
}

// TestTheSnapshotCarriesEachKindWithWhatItsWidgetNeeds: a toggle its two words and
// the one shown, a choice its words, a step and a pick their value and a pick its
// mark, an action whether to confirm and no value, a form its fields with the
// keyboard each takes, an info row neither value nor reason, and a group its rows.
func TestTheSnapshotCarriesEachKindWithWhatItsWidgetNeeds(t *testing.T) {
	_, r, _ := nativeMenu(t)
	s := snapshot(t, r)
	for _, c := range []struct {
		id   string
		want menuNode
	}{
		{"settings/#Controls/mouse", menuNode{Kind: "toggle", Label: "Mouse input", Value: "enable", Choices: []string{"disable", "enable"}}},
		{"settings/#Sound/sfx", menuNode{Kind: "toggle", Label: "Sound effects", Value: "off", Choices: []string{"off", "on"}}},
		{"settings/#Controls/auto", menuNode{Kind: "choice", Label: "Auto-fire", Value: "on", Choices: []string{"off", "main", "on"}}},
		{"game/speed", menuNode{Kind: "step", Label: "Game speed", Value: "1x"}},
		{"game/restart", menuNode{Kind: "action", Label: "Restart game", Confirm: true}},
		{"scenarios/#Installed/pick/main", menuNode{Kind: "pick", Label: "main", Value: "current", Marked: true}},
		{"multiplayer/#Bots/drop/10", menuNode{Kind: "action", Label: "Remove 2: default", Confirm: true}},
		{"multiplayer/#Bots/drop/12", menuNode{Kind: "action", Label: "Remove 5: default", Confirm: true, Disabled: "This bot is leaving"}},
		{"multiplayer/#Players/player/9", menuNode{Kind: "action", Label: "Drop 6: player, away", Confirm: true, Disabled: "Only the host can remove other participants"}},
		{"launch/files", menuNode{Kind: "info", Label: "Paths and scenario"}},
		{"multiplayer/#Session/status", menuNode{Kind: "info", Label: "Current session", Description: "Test session"}},
	} {
		got := s.node(c.id)
		got.ID, got.Description = "", ""
		c.want.Description = ""
		if got.Kind != c.want.Kind || got.Label != c.want.Label || got.Value != c.want.Value || !slices.Equal(got.Choices, c.want.Choices) ||
			got.Marked != c.want.Marked || got.Confirm != c.want.Confirm || got.Disabled != c.want.Disabled {
			t.Errorf("%s is %+v, want %+v", c.id, got, c.want)
		}
	}
	if got := s.node("multiplayer/#Session/status").Description; got != "Test session" {
		t.Errorf("the session row's description is %q", got)
	}
	request := s.node("multiplayer/#Session/request")
	if request.Kind != "form" || len(request.Fields) != 3 || request.Fields[0].Input != "uri" || request.Fields[1].Input != "number" ||
		request.Fields[2].Input != "text" || request.Fields[0].Value != "https://lixen.com" || request.Fields[0].Help == "" {
		t.Errorf("the request form is %+v", request)
	}
	if sound := s.node("settings/#Sound"); sound.Kind != "group" || sound.Label != "Sound" || len(sound.Nodes) != 3 {
		t.Errorf("the Sound section is %+v", sound)
	}
	if len(s.Nodes) != len(configPages) || s.Nodes[0].ID != "game" || s.Title != "Menu · game running" {
		t.Errorf("the snapshot's pages and title: %d pages, first %q, title %q", len(s.Nodes), s.Nodes[0].ID, s.Title)
	}
}

// BenchmarkMenuSnapshot is the full tree built and marshalled, as a host drawing its
// own menu has every frame while the menu is open: three bots, three other players
// and the scenario list on a run that holds a session.
func BenchmarkMenuSnapshot(b *testing.B) {
	_, r, _ := nativeMenu(b)
	var bytes int
	for b.Loop() {
		bytes = len(r.MenuSnapshot())
	}
	b.ReportMetric(float64(bytes), "bytes")
}

// BenchmarkRefreshConfigMenu is the tui menu's refresh of its Multiplayer page every
// frame, which the snapshot's build replaces on a host that draws its own menu.
func BenchmarkRefreshConfigMenu(b *testing.B) {
	ctx, r, _ := nativeMenu(b)
	showConfigMenu(ctx, "multiplayer")
	for b.Loop() {
		r.RefreshConfigMenu()
	}
}
