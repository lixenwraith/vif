package renderer

import (
	"cmp"
	"fmt"
	"math"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/lixenwraith/color"
	"github.com/lixenwraith/terminal"
	"github.com/lixenwraith/vif/internal/core"
	"github.com/lixenwraith/vif/internal/engine"
	"github.com/lixenwraith/vif/internal/parameter"
	"github.com/lixenwraith/vif/internal/parameter/visual"
	"github.com/lixenwraith/vif/internal/render"
	"github.com/lixenwraith/vif/internal/status"
)

// statusItem is one right-aligned run of status bar cells
type statusItem struct {
	text string
	fg   color.RGB
	bg   color.RGB
}

// with returns the item's colours around text
func (s statusItem) with(text string) statusItem {
	s.text = text
	return s
}

// statusText is one item's text as last formatted, with the values it shows, so a
// frame formats an item only when its value changed
type statusText struct {
	key  int64
	a, b string
	text string
}

// hold returns the text for key, a and b, formatting anew only when they changed
func (t *statusText) hold(key int64, a, b string, format func() string) string {
	if t.text == "" || key != t.key || a != t.a || b != t.b {
		t.key, t.a, t.b, t.text = key, a, b, format()
	}
	return t.text
}

// tenths keys a duration in seconds by the one decimal its text shows
func tenths(seconds float64) int64 { return int64(math.Round(seconds * 10)) }

// statusPalette holds the status bar's colours. TrueColor takes the theme's; a text console
// takes fixed entries, each apart from the items beside it and with the text that reads best on
// it, since a theme colour could land on any of eight backgrounds, black included.
type statusPalette struct {
	audio            [4]statusItem // by channel mask
	mode             [5]statusItem // normal, visual, insert, search, command
	net              [3]statusItem // by severity
	wait, speed      statusItem
	alarm            statusItem // a pending step, a breakpoint, the multiplier, a recording
	energy, negative statusItem
	blink            [5]statusItem // by blink type 1-4, then white
	energyError      color.RGB
	boost            statusItem
	apm, gt, fps     statusItem
	label            statusItem // the colour mode, text included
	phase            func(index, total int64) statusItem
}

func statusPaletteTrueColor() *statusPalette {
	on := func(bg color.RGB) statusItem { return statusItem{fg: visual.RgbBlack, bg: bg} }
	text := func(bg color.RGB) statusItem { return statusItem{fg: visual.RgbStatusText, bg: bg} }
	return &statusPalette{
		audio: [4]statusItem{
			on(visual.RgbAudioBothOff), on(visual.RgbAudioEffectsOnly), on(visual.RgbAudioMusicOnly), on(visual.RgbAudioBothOn),
		},
		mode: [5]statusItem{
			text(visual.RgbModeNormalBg), text(visual.RgbModeVisualBg), text(visual.RgbModeInsertBg),
			text(visual.RgbModeSearchBg), text(visual.RgbModeCommandBg),
		},
		net:         [3]statusItem{on(visual.RgbNetGoodBg), on(visual.RgbNetWarnBg), on(visual.RgbNetBadBg)},
		wait:        on(visual.RgbGtBg),
		speed:       on(visual.RgbGtBg),
		alarm:       on(visual.RgbCursorError),
		energy:      on(visual.RgbEnergyBg),
		negative:    statusItem{fg: visual.RgbEnergyBg, bg: visual.RgbBlack},
		energyError: visual.RgbCursorError,
		boost:       text(visual.RgbBoostBg),
		apm:         on(visual.RgbApmBg),
		gt:          on(visual.RgbGtBg),
		fps:         on(visual.RgbFpsBg),
		label:       on(visual.RgbColorModeIndicator).with(" TC "),
		blink: [5]statusItem{
			on(visual.RgbEnergyBlinkBlue), on(visual.RgbEnergyBlinkGreen), on(visual.RgbEnergyBlinkRed),
			on(visual.RgbGlyphGold), on(visual.RgbEnergyBlinkWhite),
		},
		phase: func(index, total int64) statusItem {
			return on(render.RainbowIndexColor(index, total, visual.RgbModeNormalBg))
		},
	}
}

// statusPalette256 names console entries; the audio letter is drawn on the bar's own black, so it
// needs no background apart from the mode beside it
func statusPalette256(c *render.Console) *statusPalette {
	on := func(bg uint8) statusItem { return statusItem{fg: c.Color(c.TextOn(bg)), bg: c.Color(bg)} }
	letter := func(fg uint8) statusItem { return statusItem{fg: c.Color(fg), bg: visual.RgbBackground} }
	phase := on(visual.ConBlue)
	return &statusPalette{
		audio: [4]statusItem{
			letter(visual.ConBrightRed), letter(visual.ConBrightGreen), letter(visual.ConBrightYellow), letter(visual.ConBrightCyan),
		},
		mode: [5]statusItem{
			on(visual.ConCyan), on(visual.ConYellow), on(visual.ConGreen), on(visual.ConWhite), on(visual.ConMagenta),
		},
		net:         [3]statusItem{on(visual.ConGreen), on(visual.ConYellow), on(visual.ConRed)},
		wait:        on(visual.ConYellow),
		speed:       on(visual.ConYellow),
		alarm:       on(visual.ConRed),
		energy:      on(visual.ConWhite),
		negative:    letter(visual.ConBrightWhite),
		energyError: c.Color(visual.ConRed),
		boost:       on(visual.ConMagenta),
		apm:         on(visual.ConGreen),
		gt:          on(visual.ConYellow),
		fps:         on(visual.ConCyan),
		label:       on(visual.ConWhite).with(" 256 "),
		blink: [5]statusItem{
			on(visual.ConBlue), on(visual.ConGreen), on(visual.ConRed), on(visual.ConYellow), on(visual.ConWhite),
		},
		phase: func(int64, int64) statusItem { return phase },
	}
}

// StatusBarRenderer draws the status bar, the two rows at the bottom on every host:
// the status items across the whole width, then the mode, the key cell and the line
// a player types on and reads messages from, which no item ever narrows
type StatusBarRenderer struct {
	gameCtx *engine.GameContext
	pal     *statusPalette

	// Sound/Audio indicator
	statAudioMask *atomic.Int64

	// Cached metric pointers (zero-lock reads)
	statFPS   *atomic.Int64
	statAPM   *atomic.Int64
	statTicks *atomic.Int64

	// Time control telemetry
	statSpeed *status.AtomicString
	statStep  *atomic.Int64
	statBreak *status.AtomicString

	// The session, in the one badge networkItem renders. The rest of the
	// measurements behind it — jitter, cadence, keyframe interval, byte rate, the
	// map latch — are read in the status snapshot and in :session, not here.
	statNet         *status.AtomicString
	statStale       *atomic.Bool
	statPaceLate    *atomic.Int64
	statRTT         *atomic.Int64
	statLoss        *atomic.Int64
	statHostLost    *atomic.Bool
	statMigrating   *atomic.Bool
	statCadence     *atomic.Int64
	statConstrained *atomic.Bool
	statFloor       *atomic.Bool
	statRejoin      *atomic.Int64

	// FSM telemetry
	statFSMName    *status.AtomicString
	statFSMElapsed *atomic.Int64
	statFSMMaxDur  *atomic.Int64
	statFSMIndex   *atomic.Int64
	statFSMTotal   *atomic.Int64

	// Energy telemetry
	statDamageMultiplier *atomic.Int64

	// Cursor blink state
	cursorBlinkOn   bool
	lastBlinkToggle time.Time

	// The session cell as last drawn, and when; and since when it has said down.
	// See networkItem.
	netHeld   statusItem
	netHeldAt time.Time
	downSince time.Time

	// Scratch the bar reuses frame to frame, and each item's text as last formatted
	items  []statusItem
	widths []int
	slots  []int
	runes  []rune

	phaseText, energyText, damageText, boostText, gridText statusText
	apmText, gtText, fpsText, recordText, timeText         statusText
}

// NewStatusBarRenderer creates a status bar renderer
func NewStatusBarRenderer(gameCtx *engine.GameContext) *StatusBarRenderer {
	statusReg := gameCtx.World.Resources.Status

	r := &StatusBarRenderer{
		gameCtx: gameCtx,
		pal:     statusPaletteTrueColor(),

		statAudioMask: statusReg.Ints.Get("audio.mask"),

		statFPS:   statusReg.Ints.Get("engine.fps"),
		statAPM:   statusReg.Ints.Get("engine.music_apm"), // the rate music follows, not the minute's sum
		statTicks: statusReg.Ints.Get("engine.ticks"),

		statSpeed:     statusReg.Strings.Get("engine.speed"),
		statStep:      statusReg.Ints.Get("engine.step"),
		statBreak:     statusReg.Strings.Get("engine.breakpoint"),
		statNet:       statusReg.Strings.Get("network.state"),
		statStale:     statusReg.Bools.Get("network.stale"),
		statPaceLate:  statusReg.Ints.Get("network.pace_late_ticks"),
		statRTT:       statusReg.Ints.Get("network.link_rtt_ms"),
		statLoss:      statusReg.Ints.Get("network.link_loss_pct"),
		statHostLost:  statusReg.Bools.Get("network.host_lost"),
		statMigrating: statusReg.Bools.Get("network.migrating"),

		statCadence:     statusReg.Ints.Get("snapshot.cadence_ticks"),
		statConstrained: statusReg.Bools.Get("snapshot.cadence_constrained"),
		statFloor:       statusReg.Bools.Get("snapshot.cadence_floor_breached"),
		statRejoin:      statusReg.Ints.Get("network.rejoin_attempts"),

		statFSMName:    statusReg.Strings.Get("fsm.state"),
		statFSMElapsed: statusReg.Ints.Get("fsm.elapsed"),
		statFSMMaxDur:  statusReg.Ints.Get("fsm.max_duration"),
		statFSMIndex:   statusReg.Ints.Get("fsm.state_index"),
		statFSMTotal:   statusReg.Ints.Get("fsm.state_count"),

		statDamageMultiplier: statusReg.Ints.Get("energy.damage_multiplier"),
	}
	if cfg := gameCtx.World.Resources.Config; cfg.ColorMode == terminal.ColorMode256 {
		r.pal = statusPalette256(render.ConsoleFor(cfg.ConsolePalette))
	}
	return r
}

// Render implements SystemRenderer
func (r *StatusBarRenderer) Render(ctx render.RenderContext, buf *render.RenderBuffer) {
	buf.SetWriteMask(visual.MaskUI)
	itemsY := ctx.GameYOffset + ctx.ViewportHeight + 1
	lineY := itemsY + parameter.StatusRows - 1
	if lineY >= ctx.ScreenHeight {
		return
	}
	for y := itemsY; y <= lineY; y++ {
		for x := range ctx.ScreenWidth {
			buf.SetWithBg(x, y, ' ', visual.RgbBackground, visual.RgbBackground)
		}
	}

	// Cursor blink runs on wall time: it must continue while the world is paused
	realNow := r.gameCtx.TimeCtl.RealTime()
	if realNow.Sub(r.lastBlinkToggle) >= parameter.StatusCursorBlinkDuration {
		r.cursorBlinkOn = !r.cursorBlinkOn
		r.lastBlinkToggle = realNow
	}

	// === BUILD ITEMS, BY PRIORITY ===

	items := r.items[:0]

	// Priority 0: one cell for the whole session. See networkItem.
	if item, ok := r.networkItem(); ok {
		items = append(items, item)
	}
	// Time control is absent at real time with nothing pending.
	if item, ok := r.timeItem(); ok {
		items = append(items, item)
	}

	// Priority 1: FSM Phase
	phaseName := r.statFSMName.Load()
	if phaseName != "" {
		elapsed := time.Duration(r.statFSMElapsed.Load())
		maxDur := time.Duration(r.statFSMMaxDur.Load())
		phaseIdx := r.statFSMIndex.Load()
		phaseTotal := r.statFSMTotal.Load()

		var timerVal float64
		if maxDur > 0 {
			remaining := max(maxDur-elapsed, 0)
			timerVal = remaining.Seconds()
		} else {
			timerVal = elapsed.Seconds()
		}

		text := r.phaseText.hold(tenths(timerVal), phaseName, "", func() string {
			return fmt.Sprintf(" %s: %.1fs ", phaseName, timerVal)
		})
		items = append(items, r.pal.phase(phaseIdx, phaseTotal).with(text))
	}

	// Priority 2: Energy
	var playerEntity core.Entity
	if r.gameCtx.World.Resources.Player.Valid() {
		playerEntity = r.gameCtx.World.Resources.Player.Entity
	}
	energyComp, hasEnergy := r.gameCtx.World.Components.Energy.GetPtr(playerEntity)
	var energyVal int64
	if hasEnergy {
		energyVal = energyComp.Current
	}
	energy := r.pal.energy
	if energyVal < 0 {
		energy = r.pal.negative
	}

	view, hasView := r.gameCtx.World.Components.CursorView.GetPtr(playerEntity)
	if hasView && view.BlinkActive && view.BlinkRemaining > 0 {
		switch typeCode := view.BlinkType; {
		case typeCode == 0:
			energy.fg = r.pal.energyError
		case typeCode > 0 && typeCode < len(r.pal.blink):
			energy = r.pal.blink[typeCode-1]
		default:
			energy = r.pal.blink[len(r.pal.blink)-1]
		}
	}
	items = append(items, energy.with(r.energyText.hold(energyVal, "", "", func() string {
		return fmt.Sprintf(" Energy: %s ", status.FormatCount(energyVal))
	})))

	// Priority 3: Damage Multiplier (cycle scaling)
	dmgMult := r.statDamageMultiplier.Load()
	if dmgMult > 1 {
		items = append(items, r.pal.alarm.with(r.damageText.hold(dmgMult, "", "", func() string {
			return fmt.Sprintf(" x%s ", status.FormatCount(dmgMult))
		})))
	}

	// Priority 4: Boost (conditional)
	boost, boostOk := r.gameCtx.World.Components.Boost.GetPtr(playerEntity)
	if boostOk && boost.Active {
		remaining := boost.Remaining.Seconds()
		if remaining < 0 {
			remaining = 0
		}
		items = append(items, r.pal.boost.with(r.boostText.hold(tenths(remaining), "", "", func() string {
			return fmt.Sprintf(" Boost: %.1fs ", remaining)
		})))
	}

	// Priority 5: Grid (conditional)
	if ping, ok := r.gameCtx.World.Components.Ping.GetPtr(playerEntity); ok && ping.GridActive {
		gridRemaining := ping.GridRemaining.Seconds()
		if gridRemaining < 0 {
			gridRemaining = 0
		}
		items = append(items, statusItem{
			text: r.gridText.hold(tenths(gridRemaining), "", "", func() string { return fmt.Sprintf(" Grid: %.1fs ", gridRemaining) }),
			fg:   visual.RgbGridTimerFg,
			bg:   visual.RgbBackground,
		})
	}

	// Priority 6-9: Metrics (lowest priority, dropped first)
	apm, ticks, fps := r.statAPM.Load(), r.statTicks.Load(), r.statFPS.Load()
	items = append(items,
		r.pal.apm.with(r.apmText.hold(apm, "", "", func() string { return fmt.Sprintf(" APM: %d ", apm) })),
		r.pal.gt.with(r.gtText.hold(ticks, "", "", func() string { return fmt.Sprintf(" GT: %d ", ticks) })),
		r.pal.fps.with(r.fpsText.hold(fps, "", "", func() string { return fmt.Sprintf(" FPS: %d ", fps) })),
		r.pal.label,
	)
	r.items = items
	r.renderItems(buf, itemsY, ctx.ScreenWidth, items)

	// === THE LINE: AUDIO, MODE, KEYS, THEN INPUT OR A MESSAGE ===
	// A keyboard covering it leaves the left half the host repeats above it
	width := ctx.ScreenWidth
	if ctx.StatusCovered {
		width /= 2
	}
	x := 0

	// Audio state indicator; -1 = no audio resource
	if mv := r.statAudioMask.Load(); mv >= 0 {
		mask := uint8(mv) & parameter.AudioChanAll
		audio := r.pal.audio[mask]
		for _, ch := range parameter.AudioText[mask] {
			if x >= width {
				return
			}
			buf.SetWithBg(x, lineY, ch, audio.fg, audio.bg)
			x++
		}
	}

	// Mode indicator
	var modeText string
	var mode statusItem
	if r.gameCtx.IsSearchMode() {
		modeText, mode = parameter.ModeTextSearch, r.pal.mode[3]
	} else if r.gameCtx.IsCommandMode() {
		modeText, mode = parameter.ModeTextCommand, r.pal.mode[4]
	} else if r.gameCtx.IsInsertMode() {
		modeText, mode = parameter.ModeTextInsert, r.pal.mode[2]
	} else if r.gameCtx.IsVisualMode() {
		modeText, mode = parameter.ModeTextVisual, r.pal.mode[1]
	} else {
		modeText, mode = parameter.ModeTextNormal, r.pal.mode[0]
	}
	for _, ch := range modeText {
		if x >= width {
			return
		}
		buf.SetWithBg(x, lineY, ch, mode.fg, mode.bg)
		x++
	}

	// Macro recording indicator
	if r.gameCtx.MacroRecording.Load() {
		label := r.gameCtx.MacroRecordingLabel.Load()
		recText := r.recordText.hold(int64(label), "", "", func() string {
			return fmt.Sprintf("%s: %c ", parameter.ModeTextRecord, label)
		})
		recX := x - len(modeText)
		for i, ch := range recText {
			if recX+i < width {
				buf.SetWithBg(recX+i, lineY, ch, r.pal.alarm.fg, r.pal.alarm.bg)
			}
		}
	}

	// The key cell keeps its width while empty, so a message after it never moves and
	// never shares its space or its time with the keys. Typed input starts at the gap.
	leftEndX := x + 1
	if !r.gameCtx.IsSearchMode() && !r.gameCtx.IsCommandMode() {
		keys := r.gameCtx.GetLastCommand()
		for extra := utf8.RuneCountInString(keys) - parameter.StatusKeyWidth; extra > 0; extra-- {
			_, size := utf8.DecodeRuneInString(keys)
			keys = keys[size:]
		}
		i := 0
		for _, ch := range keys {
			if leftEndX+i < width {
				buf.SetWithBg(leftEndX+i, lineY, ch, visual.RgbLastCommandText, visual.RgbBackground)
			}
			i++
		}
		leftEndX += parameter.StatusKeyWidth + 1
	}

	// === INPUT WITH ITS CURSOR, OR THE MESSAGE SHOWN ===
	textWidth := max(width-leftEndX, 0)
	switch {
	case r.gameCtx.IsSearchMode():
		text := "/" + r.gameCtx.GetSearchText()
		r.renderInput(buf, lineY, leftEndX, textWidth, text, visual.RgbSearchInputText, utf8.RuneCountInString(text))
	case r.gameCtx.IsCommandMode():
		text := ":" + r.gameCtx.GetCommandText()
		r.renderInput(buf, lineY, leftEndX, textWidth, text, visual.RgbCommandInputText, r.gameCtx.GetCommandCursorPos()+1)
	default:
		msg := r.runesOf(cmp.Or(r.gameCtx.ShownStatusMessage(realNow.UnixNano()), ctx.Prompt))
		// One longer than its room rises onto the items' row while it shows, as vim's
		// long message rises over the window, and ends here; a keyboard hides that row
		if len(msg) > textWidth && !ctx.StatusCovered {
			head := spill(msg, ctx.ScreenWidth, textWidth)
			for x := range ctx.ScreenWidth {
				ch := ' '
				if x < len(head) {
					ch = head[x]
				}
				buf.SetWithBg(x, itemsY, ch, visual.RgbStatusMessageText, visual.RgbBackground)
			}
			msg = msg[len(head):]
		}
		r.renderStatusMessage(buf, lineY, leftEndX, textWidth, msg)
	}
}

// spill is the head of a message for a row width wide, the rest going where room
// is left: all of it when it fits, else up to the last space or slash whose rest
// still fits the room, so a path breaks between directories, else the row full
func spill(msg []rune, width, room int) []rune {
	if len(msg) <= width {
		return msg
	}
	for i := width; i > 0 && len(msg)-i <= room; i-- {
		if msg[i-1] == ' ' || msg[i-1] == '/' {
			return msg[:i]
		}
	}
	return msg[:width]
}

// renderInput is typed input and, unless an overlay holds the keys, its blinking cursor
func (r *StatusBarRenderer) renderInput(buf *render.RenderBuffer, y, startX, maxWidth int, text string, fg color.RGB, cursorPos int) {
	cursorX := r.renderInputText(buf, y, startX, maxWidth, text, fg, cursorPos)
	if !r.gameCtx.IsOverlayActive() && r.cursorBlinkOn && cursorX < startX+maxWidth {
		buf.SetWithBg(cursorX, y, parameter.StatusCursorChar, visual.RgbStatusCursor, visual.RgbStatusCursorBg)
	}
}

// renderItems lays the items out across the row edge to edge: as many as fit, in
// priority order, each in a slot at least its width. The slots are as equal as
// those widths allow, so an item's edges move when the items shown change, not
// when a value grows by a digit; its text is centred on its own colours.
func (r *StatusBarRenderer) renderItems(buf *render.RenderBuffer, y, width int, items []statusItem) {
	widths, used := r.widths[:0], 0
	for _, item := range items {
		w := utf8.RuneCountInString(item.text)
		if used+w > width {
			break
		}
		widths, used = append(widths, w), used+w
	}
	r.widths = widths
	if len(widths) == 0 {
		return
	}
	// Water-filling: an item wider than an equal share keeps its width, and the
	// rest share what is left, until every share holds the item given it
	slots := append(r.slots[:0], widths...)
	r.slots = slots
	fixed, left, open := 0, width, len(slots)
	for open > 0 {
		share, grew := left/open, false
		for i, w := range widths {
			if fixed&(1<<i) == 0 && w > share {
				fixed, left, open, grew = fixed|1<<i, left-w, open-1, true
			}
		}
		if !grew {
			break
		}
	}
	extra := 0
	if open > 0 {
		extra = left % open
	}
	for i := range slots {
		if fixed&(1<<i) == 0 {
			slots[i] = left / open
			if extra > 0 {
				slots[i]++
				extra--
			}
		}
	}
	x := 0
	for i, slot := range slots {
		item := items[i]
		for j := range slot {
			buf.SetWithBg(x+j, y, ' ', item.fg, item.bg)
		}
		at := x + (slot-widths[i])/2
		for _, ch := range item.text {
			buf.SetWithBg(at, y, ch, item.fg, item.bg)
			at++
		}
		x += slot
	}
}

// networkItem is the badge as drawn, held for StatusNetworkHoldDuration. Its
// inputs move on the correction cadence and its width reflows every item beside
// it, so an unheld cell repaints faster than it can be read, and it is formatted
// only when the hold lapses.
func (r *StatusBarRenderer) networkItem() (statusItem, bool) {
	now := r.gameCtx.TimeCtl.RealTime() // [wall] readability, not simulation
	state, ok := r.networkShown()
	// A link that went down is news for a while, then noise beside a solo game
	if down := ok && state == "down"; !down {
		r.downSince = time.Time{}
	} else if r.downSince.IsZero() {
		r.downSince = now
	} else if now.Sub(r.downSince) >= parameter.StatusNetworkDownDuration {
		ok = false
	}
	switch {
	case !ok:
		r.netHeld = statusItem{}
		return statusItem{}, false
	case r.netHeld.text == "" || now.Sub(r.netHeldAt) >= parameter.StatusNetworkHoldDuration:
		r.netHeld, _ = r.networkBadge()
		r.netHeldAt = now
	}
	return r.netHeld, true
}

// networkShown reports whether the session shows a badge, and the link state it reads
func (r *StatusBarRenderer) networkShown() (state string, ok bool) {
	state = r.statNet.Load()
	return state, r.statMigrating.Load() || r.statHostLost.Load() || (state != "" && state != "off")
}

// networkBadge is the session in one badge: the round trip, which is the reading
// a player already has from every other networked game, coloured by how well this
// instance is keeping up, and at most one qualifier. A worse fact hides a lesser
// one; the numbers behind them are in :session and the status snapshot.
func (r *StatusBarRenderer) networkBadge() (statusItem, bool) {
	state, ok := r.networkShown()
	if !ok {
		return statusItem{}, false
	}
	// A succession in progress outranks the loss that opened it, and stays a fixed
	// number of ticks past the handoff, so the two states a player reads are either
	// side of it. The count is the reconnect walking the candidate list.
	if r.statMigrating.Load() {
		text := " Migrating "
		if n := r.statRejoin.Load(); n > 0 {
			text = fmt.Sprintf(" Migrating %d ", n)
		}
		return r.pal.net[severityDegrading].with(text), true
	}
	// Losing the authority with no succession open outranks everything else,
	// including the link state that described the host that went.
	if r.statHostLost.Load() {
		return r.pal.net[severityFailing].with(" Host lost "), true
	}
	switch state {
	case "down":
		return r.pal.net[severityFailing].with(" Net: down "), true
	case "connected":
	default:
		return r.pal.wait.with(" Net: wait "), true
	}

	// slow!     no cadence delivers a whole world inside the guaranteed window;
	// desync n  this instance is n ticks out of step with the authority's epochs,
	//           beyond what its pacing trims quietly: ahead, so committed
	//           crossings reach it late, or behind by a step, so its lead grows;
	// loss n%   probes went unanswered often enough for the link to be the cause;
	// slow      the cadence backed off and prediction carries more.
	rtt := r.statRTT.Load()
	// Players rather than links: a guest holds one link whatever the session's size.
	players := r.gameCtx.World.Resources.Player
	identity := fmt.Sprintf("%dP", players.Count())
	if slot := players.LocalSlot(); slot != parameter.NoPlayerSlot {
		identity += fmt.Sprintf(":%X", slot)
	}
	text := fmt.Sprintf(" %s %s", identity, status.FormatLatency(rtt))
	severity := latencySeverity(rtt)
	loss := r.statLoss.Load()
	switch {
	case r.statFloor.Load() && r.statCadence.Load() != 0:
		text, severity = text+" slow! ", severityFailing
	case r.statStale.Load():
		late := r.statPaceLate.Load()
		text, severity = fmt.Sprintf("%s desync %d ", text, max(late, -late)), max(severity, severityDegrading)
	case loss >= parameter.StatusNetLossWarnPct:
		text, severity = fmt.Sprintf("%s loss %d%% ", text, loss), max(severity, severityDegrading)
	case r.statConstrained.Load() && r.statCadence.Load() != 0:
		text, severity = text+" slow ", max(severity, severityDegrading)
	default:
		text += " "
	}
	return r.pal.net[severity].with(text), true
}

// Link health as a player reads it, worst wins; latencySeverity is the round trip
// alone, which colours a badge that has nothing else to say.
const (
	severitySettled = iota
	severityDegrading
	severityFailing
)

func latencySeverity(ms int64) int {
	switch rtt := time.Duration(ms) * time.Millisecond; {
	case rtt >= parameter.StatusNetLatencyBad:
		return severityFailing
	case rtt >= parameter.StatusNetLatencyWarn:
		return severityDegrading
	}
	return severitySettled
}

// timeItem builds the time control indicator, present only when the simulation is
// off real time or a step request is pending
func (r *StatusBarRenderer) timeItem() (statusItem, bool) {
	if step := r.statStep.Load(); step > 0 {
		return r.pal.alarm.with(r.timeText.hold(step, "", "", func() string { return fmt.Sprintf(" STEP %d ", step) })), true
	}

	speed := r.statSpeed.Load()
	if brk := r.statBreak.Load(); brk != "" && brk != "-" {
		return r.pal.alarm.with(r.timeText.hold(-1, speed, brk, func() string { return fmt.Sprintf(" %sx>%s ", speed, brk) })), true
	}
	if speed != "" && speed != "1" {
		return r.pal.speed.with(r.timeText.hold(-2, speed, "", func() string { return fmt.Sprintf(" %sx ", speed) })), true
	}
	return statusItem{}, false
}

func (r *StatusBarRenderer) renderInputText(buf *render.RenderBuffer, y, startX, maxWidth int, text string, fg color.RGB, cursorPos int) int {
	if maxWidth <= 0 {
		return startX
	}

	runes := r.runesOf(text)
	textLen := len(runes)
	if cursorPos > textLen {
		cursorPos = textLen
	}
	if cursorPos < 0 {
		cursorPos = 0
	}

	// No overflow: render all, return cursor screen position
	if textLen < maxWidth {
		for i, ch := range runes {
			buf.SetWithBg(startX+i, y, ch, fg, visual.RgbBackground)
		}
		return startX + cursorPos
	}

	// Overflow: compute scrolling window
	winStart, contentSlots, leftTrunc, rightTrunc := computeInputWindow(textLen, cursorPos, maxWidth)

	// Render indicators and content
	screenX := startX
	if leftTrunc {
		buf.SetWithBg(screenX, y, '<', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
		screenX++
	}
	winEnd := min(winStart+contentSlots, textLen)
	for i := winStart; i < winEnd; i++ {
		buf.SetWithBg(screenX, y, runes[i], fg, visual.RgbBackground)
		screenX++
	}
	if rightTrunc {
		buf.SetWithBg(screenX, y, '>', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
	}

	li := 0
	if leftTrunc {
		li = 1
	}
	return startX + li + (cursorPos - winStart)
}

// computeInputWindow determines the visible rune range for scrolled input text
// Returns window start index, content slot count, and truncation flags
func computeInputWindow(textLen, cursorPos, maxWidth int) (winStart, contentSlots int, leftTrunc, rightTrunc bool) {
	// Effective display length: cursor at end of text occupies one extra cell
	displayLen := textLen
	if cursorPos == textLen {
		displayLen++
	}

	// Initial placement: cursor at ~1/3 from left for typing comfort
	winStart = max(0, cursorPos-maxWidth/3)

	// Iterative fit: converge indicators and cursor visibility (max 2 passes)
	for range 3 {
		leftTrunc = winStart > 0
		li := 0
		if leftTrunc {
			li = 1
		}
		contentSlots = maxWidth - li
		winEnd := winStart + contentSlots

		rightTrunc = winEnd < displayLen
		if rightTrunc {
			contentSlots--
			winEnd = winStart + contentSlots
		}

		// Cursor must be in [winStart, winStart+contentSlots)
		if cursorPos < winStart {
			winStart = cursorPos
			continue
		}
		if cursorPos >= winStart+contentSlots {
			winStart = max(cursorPos-contentSlots+1, 0)
			continue
		}

		// Fill trailing slack: pull winStart back to maximize visible content
		visible := contentSlots
		if winStart+visible > displayLen {
			visible = displayLen - winStart
		}
		if visible < contentSlots && winStart > 0 {
			winStart -= min(contentSlots-visible, winStart)
			continue
		}

		break
	}

	return
}

// runesOf is text as runes in the bar's scratch, valid until the next call
func (r *StatusBarRenderer) runesOf(text string) []rune {
	r.runes = r.runes[:0]
	for _, ch := range text {
		r.runes = append(r.runes, ch)
	}
	return r.runes
}

// renderStatusMessage renders status message with right-truncation (shows start of text)
func (r *StatusBarRenderer) renderStatusMessage(buf *render.RenderBuffer, y, startX, maxWidth int, runes []rune) {
	if maxWidth <= 0 {
		return
	}

	textLen := len(runes)

	if textLen <= maxWidth {
		for i, ch := range runes {
			buf.SetWithBg(startX+i, y, ch, visual.RgbStatusMessageText, visual.RgbBackground)
		}
		return
	}

	if maxWidth == 1 {
		buf.SetWithBg(startX, y, '>', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
		return
	}

	for i := range maxWidth - 1 {
		buf.SetWithBg(startX+i, y, runes[i], visual.RgbStatusMessageText, visual.RgbBackground)
	}
	buf.SetWithBg(startX+maxWidth-1, y, '>', visual.RgbTruncateIndicator, visual.RgbTruncateIndicatorBg)
}
