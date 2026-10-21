package renderer

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
)

func newStatusBar(t *testing.T) (*StatusBarRenderer, *engine.ManualClock) {
	t.Helper()
	clock := engine.NewManualClock()
	gameCtx := engine.NewGameContextWithClock(engine.NewWorld(), 80, 24, clock)
	gameCtx.World.Resources.Config.ColorMode = terminal.ColorModeTrueColor
	return NewStatusBarRenderer(gameCtx), clock
}

// seatPlayers fills the first n roster slots, which is what the badge counts.
func seatPlayers(r *StatusBarRenderer, n int) {
	w := r.gameCtx.World
	for slot := range n {
		w.Resources.Player.Bind(uint8(slot), w.CreateEntity(core.DomainShared))
	}
}

// TestStatusBarNetworkBadgeIsOneCellChosenBySeverity pins the collapse.
//
// One badge for the whole session: the round trip, coloured by how well this
// instance is keeping up, and at most one qualifier, so a worse fact hides a
// lesser one rather than sitting beside it.
func TestStatusBarNetworkBadgeIsOneCellChosenBySeverity(t *testing.T) {
	r, _ := newStatusBar(t)

	if item, ok := r.networkBadge(); ok {
		t.Fatalf("a solo run renders %#v", item)
	}

	r.statNet.Store("waiting")
	if item, ok := r.networkBadge(); !ok || item.text != " Net: wait " {
		t.Fatalf("waiting item = %#v, %t", item, ok)
	}

	// A link with no completed round trip yet has no reading to show. The count is
	// players, not links: a headless host's two guests read 2P:0 on every instance.
	r.statNet.Store("connected")
	seatPlayers(r, 2)
	if item, ok := r.networkBadge(); !ok || item.text != " 2P:0 -- " {
		t.Fatalf("unmeasured item = %#v, %t", item, ok)
	}

	r.statRTT.Store(42)
	if item, ok := r.networkBadge(); !ok || item.text != " 2P:0 42ms " || item.bg != visual.RgbNetGoodBg {
		t.Fatalf("converged item = %#v, %t", item, ok)
	}

	// A constrained link is the system working on a small link; the numbers behind
	// the word are in the status snapshot and in :session.
	r.statCadence.Store(8)
	r.statConstrained.Store(true)
	item, ok := r.networkBadge()
	if !ok || item.text != " 2P:0 42ms slow " || item.bg != visual.RgbNetWarnBg {
		t.Fatalf("constrained item = %#v, %t", item, ok)
	}

	// Loss outranks it: the link itself is dropping what the cadence plans around.
	r.statLoss.Store(7)
	if item, ok := r.networkBadge(); !ok || item.text != " 2P:0 42ms loss 7% " {
		t.Fatalf("lossy item = %#v, %t", item, ok)
	}

	// Being behind the session outranks both: it says whether this participant's own
	// actions are still landing on time, which is the one a player can act on.
	r.statStale.Store(true)
	r.statLag.Store(7)
	item, ok = r.networkBadge()
	if !ok || item.text != " 2P:0 42ms desync 7 " || item.bg != visual.RgbNetWarnBg {
		t.Fatalf("stale item = %#v, %t", item, ok)
	}

	// The floor outranks everything, and reads differently on purpose: not the
	// system degrading, the system unable to keep its guarantee.
	r.statFloor.Store(true)
	item, ok = r.networkBadge()
	if !ok || item.text != " 2P:0 42ms slow! " || item.bg != visual.RgbNetBadBg {
		t.Fatalf("floor item = %#v, %t", item, ok)
	}
}

// TestStatusBarBadgeColoursAnUnqualifiedLinkByItsRoundTrip is the reading the
// badge exists to give at a glance: a settled session with a slow link is not
// green, and nothing else on the bar says so.
func TestStatusBarBadgeColoursAnUnqualifiedLinkByItsRoundTrip(t *testing.T) {
	r, _ := newStatusBar(t)
	r.statNet.Store("connected")

	r.statRTT.Store(int64(parameter.StatusNetLatencyWarn / time.Millisecond))
	if item, _ := r.networkBadge(); item.bg != visual.RgbNetWarnBg {
		t.Fatalf("a %s link renders %#v", parameter.StatusNetLatencyWarn, item)
	}

	r.statRTT.Store(int64(parameter.StatusNetLatencyBad / time.Millisecond))
	if item, _ := r.networkBadge(); item.bg != visual.RgbNetBadBg {
		t.Fatalf("a %s link renders %#v", parameter.StatusNetLatencyBad, item)
	}
}

// TestStatusBarAuthorityLossHidesTheLinkItDescribed is the reading the severity
// order exists for: after the host has gone, a player count describes a session this
// instance is no longer in.
func TestStatusBarAuthorityLossHidesTheLinkItDescribed(t *testing.T) {
	r, _ := newStatusBar(t)
	r.statNet.Store("connected")
	r.statCadence.Store(4)
	r.statFloor.Store(true)

	r.statMigrating.Store(true)
	if item, ok := r.networkBadge(); !ok || item.text != " Migrating " || item.bg != visual.RgbNetWarnBg {
		t.Fatalf("migrating item = %#v, %t", item, ok)
	}

	// The loss that opened the succession stays behind it while the walk runs, with
	// the passes walked, and shows once the walk has given up.
	r.statHostLost.Store(true)
	r.statRejoin.Store(2)
	item, ok := r.networkBadge()
	if !ok || item.text != " Migrating 2 " || item.bg != visual.RgbNetWarnBg {
		t.Fatalf("walking item = %#v, %t", item, ok)
	}
	r.statMigrating.Store(false)
	r.statRejoin.Store(0)
	item, ok = r.networkBadge()
	if !ok || item.text != " Host lost " || item.bg != visual.RgbNetBadBg {
		t.Fatalf("host-loss item = %#v, %t", item, ok)
	}

	// And a link that has gone says so rather than reporting the peers it had.
	r.statHostLost.Store(false)
	r.statNet.Store("down")
	item, ok = r.networkBadge()
	if !ok || item.text != " Net: down " || item.bg != visual.RgbNetBadBg {
		t.Fatalf("down item = %#v, %t", item, ok)
	}
}

// TestStatusBarNetworkCellHoldsItsWidth is why the cell is held at all: its inputs
// move on the correction cadence, and its width reflows every item beside it, so an
// unheld cell repaints faster than it can be read.
func TestStatusBarNetworkCellHoldsItsWidth(t *testing.T) {
	r, clock := newStatusBar(t)
	r.statNet.Store("connected")
	seatPlayers(r, 2)
	r.statRTT.Store(42)
	first, _ := r.networkItem()

	r.statStale.Store(true)
	r.statLag.Store(7)
	if item, _ := r.networkItem(); item.text != first.text {
		t.Fatalf("the cell changed to %q inside its hold", item.text)
	}

	clock.Step(parameter.StatusNetworkHoldDuration)
	if item, _ := r.networkItem(); item.text != " 2P:0 42ms desync 7 " {
		t.Fatalf("the cell is %q after its hold, want the current reading", item.text)
	}
}

// statusContext is a 24-row screen whose status bar is its last two rows
func statusContext(width int) render.RenderContext {
	return render.RenderContext{ScreenWidth: width, ScreenHeight: 24, ViewportWidth: width,
		ViewportHeight: 24 - parameter.BottomMargin}
}

// statusRow renders the bar and reads one of its rows: its text and its backgrounds
func statusRow(r *StatusBarRenderer, rc render.RenderContext, y int) (string, []color.RGB) {
	buf := render.NewRenderBuffer(terminal.ColorModeTrueColor, rc.ScreenWidth, rc.ScreenHeight)
	r.Render(rc, buf)
	var line strings.Builder
	bgs := make([]color.RGB, rc.ScreenWidth)
	for x := range rc.ScreenWidth {
		line.WriteRune(buf.CellAt(x, y).Rune)
		bgs[x] = buf.BackgroundAt(x, y, visual.RgbBackground)
	}
	return line.String(), bgs
}

// edges is where each run of one background starts
func edges(bgs []color.RGB) []int {
	var at []int
	for x := range bgs {
		if x == 0 || bgs[x] != bgs[x-1] {
			at = append(at, x)
		}
	}
	return at
}

// TestStatusBarKeysAndAMessageKeepTheirOwnCells is the split: the keys and a message
// share the bottom row, and the message starts where it always does whatever the
// keys say.
func TestStatusBarKeysAndAMessageKeepTheirOwnCells(t *testing.T) {
	r, _ := newStatusBar(t)
	msg := "host :777: ports below 1024 need root"
	r.gameCtx.SetStatusMessage(msg, parameter.StatusAlert)
	at := -1
	for _, keys := range []string{"", "2d", "d3fwx"} {
		r.gameCtx.SetLastCommand(keys)
		bar, _ := statusRow(r, statusContext(80), 23)
		i := strings.Index(bar, msg)
		switch {
		case i < 0 || !strings.Contains(bar[:max(i, 0)], keys):
			t.Fatalf("with keys %q the bar reads %q", keys, bar)
		case at >= 0 && i != at:
			t.Fatalf("the message moved from %d to %d with keys %q", at, i, keys)
		}
		at = i
	}
}

// TestStatusItemsSpanTheirRowByPriority: the items take the row above the line edge
// to edge, in priority order, a narrower screen dropping the lowest first, and their
// edges hold while a value grows by a digit.
func TestStatusItemsSpanTheirRowByPriority(t *testing.T) {
	r, _ := newStatusBar(t)
	r.statFPS.Store(9)
	wide, bgs := statusRow(r, statusContext(120), 22)
	energy, fps, tc := strings.Index(wide, "Energy"), strings.Index(wide, "FPS"), strings.LastIndex(wide, "TC")
	if energy < 0 || fps < energy || tc < fps {
		t.Fatalf("the items read %q", wide)
	}
	if i := slices.Index(bgs, visual.RgbBackground); i >= 0 {
		t.Fatalf("cell %d of %q is the bar's own background", i, wide)
	}
	r.statFPS.Store(10)
	if _, now := statusRow(r, statusContext(120), 22); !slices.Equal(edges(now), edges(bgs)) {
		t.Fatalf("the edges moved from %v to %v", edges(bgs), edges(now))
	}
	narrow, bgs := statusRow(r, statusContext(36), 22)
	if strings.Contains(narrow, "TC") || !strings.Contains(narrow, "Energy") || slices.Contains(bgs, visual.RgbBackground) {
		t.Fatalf("at 36 columns the items read %q", narrow)
	}
}

// TestALongMessageRisesOverTheItems: a message longer than its room on the bottom
// row starts on the items' row, broken after a slash, and ends on the bottom row,
// whole; under a keyboard, which hides the items' row, it stays on the bottom row.
func TestALongMessageRisesOverTheItems(t *testing.T) {
	r, _ := newStatusBar(t)
	msg := "Logging to /data/user/0/com.lixen.vif/files/state/vif/log/vif-log-261021-221500.jsonl"
	r.gameCtx.SetStatusMessage(msg, parameter.StatusNotice)
	rc := statusContext(63)
	items, _ := statusRow(r, rc, 22)
	line, _ := statusRow(r, rc, 23)
	head := strings.TrimRight(items, " ")
	if !strings.HasSuffix(head, "/") || !strings.HasPrefix(msg, head) ||
		!strings.HasSuffix(strings.TrimRight(line, " "), " "+msg[len(head):]) {
		t.Fatalf("the message reads %q over %q", items, line)
	}
	rc.StatusCovered = true
	if items, _ := statusRow(r, rc, 22); strings.Contains(items, "Logging") {
		t.Fatalf("under a keyboard the items' row reads %q", items)
	}
}

// TestConsoleStatusItemsReadOnTheirBackgrounds is the field that vanished: on a text console
// every item's text reads on its background, and only text-only items share the bar's black.
func TestConsoleStatusItemsReadOnTheirBackgrounds(t *testing.T) {
	t.Parallel()
	ubuntu := engine.ConsolePalette{BgColors: 8}
	for i, h := range [16]uint32{0x010101, 0xde382b, 0x39b54a, 0xffc706, 0x006fb8, 0x762671, 0x2cb5e9, 0xcccccc,
		0x808080, 0xff0000, 0x00ff00, 0xffff00, 0x0000ff, 0xff00ff, 0x00ffff, 0xffffff} {
		ubuntu.Colors[i] = color.RGB{R: uint8(h >> 16), G: uint8(h >> 8), B: uint8(h)}
	}
	var entries [16]uint8
	for i := range entries {
		entries[i] = uint8(i)
	}
	for name, p := range map[string]engine.ConsolePalette{"vga": render.VGAPalette, "ubuntu": ubuntu, "freebsd": render.FreeBSDPalette} {
		c := render.ConsoleFor(p)
		pal := statusPalette256(c)
		textOnly := append(pal.audio[:], pal.negative)
		items := append(append(append([]statusItem{pal.wait, pal.speed, pal.alarm, pal.energy, pal.boost,
			pal.apm, pal.gt, pal.fps, pal.label, pal.phase(0, 1)}, pal.mode[:]...), pal.net[:]...), pal.blink[:]...)
		for i, item := range append(items, textOnly...) {
			fg, bg := c.Nearest(item.fg, entries[:]...), c.NearestBackground(item.bg)
			if got := c.Contrast(fg, bg); got < 2.5 {
				t.Errorf("%s: item %d text %d on %d has contrast %.2f", name, i, fg, bg, got)
			}
			if i < len(items) && bg == visual.ConBlack {
				t.Errorf("%s: item %d takes the bar's own background", name, i)
			}
		}
	}
}

// TestStatusBarDownBadgeLeavesAfterItsDuration: a link that went down shows for
// StatusNetworkDownDuration and then leaves, a loss read once being noise beside a
// solo game; a link that comes back and goes down again starts its time over.
func TestStatusBarDownBadgeLeavesAfterItsDuration(t *testing.T) {
	r, clock := newStatusBar(t)
	r.statNet.Store("down")
	if item, ok := r.networkItem(); !ok || item.text != " Net: down " {
		t.Fatalf("a link just down shows %q %v", item.text, ok)
	}
	clock.Step(parameter.StatusNetworkDownDuration - time.Millisecond)
	if _, ok := r.networkItem(); !ok {
		t.Fatal("the badge left before its duration")
	}
	clock.Step(time.Millisecond)
	if item, ok := r.networkItem(); ok {
		t.Fatalf("the badge %q stayed past its duration", item.text)
	}
	r.statNet.Store("connected")
	seatPlayers(r, 2)
	r.networkItem()
	r.statNet.Store("down")
	clock.Step(parameter.StatusNetworkHoldDuration)
	if item, ok := r.networkItem(); !ok || item.text != " Net: down " {
		t.Fatalf("a link down again shows %q %v", item.text, ok)
	}
}
